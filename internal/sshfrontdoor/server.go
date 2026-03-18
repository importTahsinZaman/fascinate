package sshfrontdoor

import (
	"bufio"
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

type machineManager interface {
	ListMachines(context.Context, string) ([]controlplane.Machine, error)
	CreateMachine(context.Context, controlplane.CreateMachineInput) (controlplane.Machine, error)
	DeleteMachine(context.Context, string) error
	CloneMachine(context.Context, controlplane.CloneMachineInput) (controlplane.Machine, error)
}

type Server struct {
	addr     string
	config   *ssh.ServerConfig
	machines machineManager
}

func New(cfg config.Config, keys keyLookup, machines machineManager) (*Server, error) {
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
			s.runShell(channel, userEmail)
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
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		s.renderDashboard(channel, userEmail)
		return 0
	}

	switch fields[0] {
	case "dashboard", "help":
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
	case "create":
		return s.createMachine(channel, userEmail, fields)
	case "clone":
		return s.cloneMachine(channel, userEmail, fields)
	case "delete":
		return s.deleteMachine(channel, fields)
	default:
		fmt.Fprintf(channel, "unknown command: %s\n", fields[0])
		fmt.Fprintln(channel, "available commands: help, dashboard, whoami, machines, create, clone, delete, exit")
		return 127
	}
}

func (s *Server) runShell(channel ssh.Channel, userEmail string) {
	s.renderDashboard(channel, userEmail)
	fmt.Fprintln(channel)

	scanner := bufio.NewScanner(channel)
	for {
		fmt.Fprint(channel, "fascinate> ")
		if !scanner.Scan() {
			return
		}

		command := strings.TrimSpace(scanner.Text())
		if command == "" {
			continue
		}

		switch command {
		case "exit", "quit":
			fmt.Fprintln(channel, "bye")
			return
		default:
			_ = s.runCommand(channel, userEmail, command)
			fmt.Fprintln(channel)
		}
	}
}

func (s *Server) renderDashboard(channel ssh.Channel, userEmail string) {
	fmt.Fprintln(channel, "fascinate")
	fmt.Fprintf(channel, "signed in as %s\n\n", userEmail)
	if err := s.renderMachines(channel, userEmail); err != nil {
		fmt.Fprintf(channel, "error loading machines: %v\n", err)
	}
	fmt.Fprintln(channel, "\ncommands: machines, create <name>, clone <source> <target>, delete <name> --confirm <name>, whoami, help, exit")
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

func (s *Server) createMachine(channel ssh.Channel, userEmail string, fields []string) uint32 {
	if len(fields) != 2 {
		fmt.Fprintln(channel, "usage: create <name>")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	machine, err := s.machines.CreateMachine(ctx, controlplane.CreateMachineInput{
		Name:       fields[1],
		OwnerEmail: userEmail,
	})
	if err != nil {
		fmt.Fprintf(channel, "error: %v\n", err)
		return 1
	}

	fmt.Fprintf(channel, "created %s\t%s\n", machine.Name, machine.URL)
	return 0
}

func (s *Server) cloneMachine(channel ssh.Channel, userEmail string, fields []string) uint32 {
	if len(fields) != 3 {
		fmt.Fprintln(channel, "usage: clone <source> <target>")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	machine, err := s.machines.CloneMachine(ctx, controlplane.CloneMachineInput{
		SourceName: fields[1],
		TargetName: fields[2],
		OwnerEmail: userEmail,
	})
	if err != nil {
		fmt.Fprintf(channel, "error: %v\n", err)
		return 1
	}

	fmt.Fprintf(channel, "cloned %s -> %s\t%s\n", fields[1], machine.Name, machine.URL)
	return 0
}

func (s *Server) deleteMachine(channel ssh.Channel, fields []string) uint32 {
	if len(fields) != 4 || fields[2] != "--confirm" || fields[3] != fields[1] {
		fmt.Fprintln(channel, "usage: delete <name> --confirm <name>")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := s.machines.DeleteMachine(ctx, fields[1]); err != nil {
		fmt.Fprintf(channel, "error: %v\n", err)
		return 1
	}

	fmt.Fprintf(channel, "deleted %s\n", fields[1])
	return 0
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
