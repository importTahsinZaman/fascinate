package sshfrontdoor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
)

type keyLookup interface {
	GetSSHKeyByFingerprint(context.Context, string) (database.SSHKeyRecord, error)
}

type machineLister interface {
	ListMachines(context.Context, string) ([]controlplane.Machine, error)
}

type Server struct {
	addr     string
	config   *ssh.ServerConfig
	machines machineLister
}

func New(cfg config.Config, keys keyLookup, machines machineLister) (*Server, error) {
	signer, err := loadOrCreateHostKey(cfg.SSHHostKeyPath)
	if err != nil {
		return nil, err
	}

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := ssh.FingerprintSHA256(key)

			record, err := keys.GetSSHKeyByFingerprint(context.Background(), fingerprint)
			if err != nil {
				if errors.Is(err, database.ErrNotFound) {
					return nil, fmt.Errorf("unauthorized")
				}
				return nil, err
			}

			return &ssh.Permissions{
				Extensions: map[string]string{
					"user_email":  record.UserEmail,
					"key_name":    record.Name,
					"fingerprint": record.Fingerprint,
				},
			}, nil
		},
	}
	serverConfig.AddHostKey(signer)

	return &Server{
		addr:     strings.TrimSpace(cfg.SSHAddr),
		config:   serverConfig,
		machines: machines,
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	if s == nil || s.addr == "" {
		<-ctx.Done()
		return nil
	}

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	serverConn, channels, requests, err := ssh.NewServerConn(conn, s.config)
	if err != nil {
		return
	}
	defer serverConn.Close()

	go ssh.DiscardRequests(requests)

	userEmail := serverConn.Permissions.Extensions["user_email"]
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go s.handleSession(channel, requests, userEmail)
	}
}

func (s *Server) handleSession(channel ssh.Channel, requests <-chan *ssh.Request, userEmail string) {
	defer channel.Close()

	for req := range requests {
		switch req.Type {
		case "pty-req", "window-change":
			req.Reply(true, nil)
		case "shell":
			req.Reply(true, nil)
			s.renderDashboard(channel, userEmail)
			writeExitStatus(channel, 0)
			return
		case "exec":
			req.Reply(true, nil)

			var payload struct {
				Command string
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				fmt.Fprintln(channel, "invalid exec payload")
				writeExitStatus(channel, 1)
				return
			}

			status := s.runCommand(channel, userEmail, payload.Command)
			writeExitStatus(channel, status)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (s *Server) runCommand(channel ssh.Channel, userEmail, command string) uint32 {
	command = strings.TrimSpace(command)
	switch command {
	case "", "dashboard", "help":
		s.renderDashboard(channel, userEmail)
		return 0
	case "whoami":
		fmt.Fprintln(channel, userEmail)
		return 0
	case "machines":
		if err := s.renderMachines(channel, userEmail); err != nil {
			fmt.Fprintf(channel, "error: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(channel, "unknown command: %s\n", command)
		fmt.Fprintln(channel, "available commands: help, dashboard, whoami, machines")
		return 127
	}
}

func (s *Server) renderDashboard(channel ssh.Channel, userEmail string) {
	fmt.Fprintln(channel, "fascinate")
	fmt.Fprintf(channel, "signed in as %s\n\n", userEmail)
	if err := s.renderMachines(channel, userEmail); err != nil {
		fmt.Fprintf(channel, "error loading machines: %v\n", err)
	}
	fmt.Fprintln(channel, "\ncommands: machines, whoami, help")
}

func (s *Server) renderMachines(channel ssh.Channel, userEmail string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	machines, err := s.machines.ListMachines(ctx, userEmail)
	if err != nil {
		return err
	}

	if len(machines) == 0 {
		fmt.Fprintln(channel, "no machines yet")
		return nil
	}

	for _, machine := range machines {
		fmt.Fprintf(channel, "- %s\t%s\t%s\n", machine.Name, machine.State, machine.URL)
	}

	return nil
}

func writeExitStatus(channel ssh.Channel, status uint32) {
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct {
		Status uint32
	}{Status: status}))
}

func loadOrCreateHostKey(path string) (ssh.Signer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	if privateKeyPEM, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(privateKeyPEM)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyBytes,
	})
	if err := os.WriteFile(path, privateKeyPEM, 0o600); err != nil {
		return nil, err
	}

	return ssh.NewSignerFromKey(privateKey)
}
