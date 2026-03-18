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

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
	"fascinate/internal/tui"
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
	signup   signupManager
}

type signupManager interface {
	Enabled() bool
	RequestCode(context.Context, string) error
	VerifyAndRegisterKey(context.Context, string, string, string) (database.User, error)
}

func New(cfg config.Config, keys keyLookup, machines machineManager, signup signupManager) (*Server, error) {
	signer, err := loadOrCreateHostKey(cfg.SSHHostKeyPath)
	if err != nil {
		return nil, err
	}

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			fingerprint := ssh.FingerprintSHA256(key)

			record, err := keys.GetSSHKeyByFingerprint(context.Background(), fingerprint)
			if err != nil {
				if errors.Is(err, database.ErrNotFound) && signup != nil && signup.Enabled() {
					return &ssh.Permissions{
						Extensions: map[string]string{
							"signup_required": "true",
							"fingerprint":     fingerprint,
							"public_key":      strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))),
						},
					}, nil
				}
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
		signup:   signup,
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

	auth := sessionAuth{
		userEmail:      serverConn.Permissions.Extensions["user_email"],
		publicKey:      serverConn.Permissions.Extensions["public_key"],
		signupRequired: serverConn.Permissions.Extensions["signup_required"] == "true",
	}
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}

		go s.handleSession(channel, requests, auth)
	}
}

func (s *Server) handleSession(channel ssh.Channel, requests <-chan *ssh.Request, auth sessionAuth) {
	defer channel.Close()

	size := windowSize{width: 80, height: 24}
	for req := range requests {
		switch req.Type {
		case "pty-req":
			size = parsePTYRequest(req.Payload, size)
			replyIfWanted(req, true)
		case "window-change":
			size = parseWindowChange(req.Payload, size)
			replyIfWanted(req, true)
		case "shell":
			replyIfWanted(req, true)
			if auth.signupRequired {
				userEmail, status := s.runSignup(channel, requests, auth.publicKey, size)
				if status != 0 || userEmail == "" {
					writeExitStatus(channel, status)
					return
				}
				auth.userEmail = userEmail
				auth.signupRequired = false
			}

			writeExitStatus(channel, s.runDashboard(channel, requests, auth.userEmail, size))
			return
		case "exec":
			replyIfWanted(req, true)

			var payload struct {
				Command string
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				fmt.Fprintln(channel, "invalid exec payload")
				writeExitStatus(channel, 1)
				return
			}

			status := s.runCommand(channel, auth, payload.Command)
			writeExitStatus(channel, status)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (s *Server) runCommand(channel ssh.Channel, auth sessionAuth, command string) uint32 {
	if auth.signupRequired {
		fmt.Fprintln(channel, "this SSH key is not registered yet")
		fmt.Fprintln(channel, "open an interactive SSH session to complete signup")
		return 1
	}

	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		s.renderDashboard(channel, auth.userEmail)
		return 0
	}

	switch fields[0] {
	case "dashboard", "help":
		s.renderDashboard(channel, auth.userEmail)
		return 0
	case "whoami":
		fmt.Fprintln(channel, auth.userEmail)
		return 0
	case "machines":
		if err := s.renderMachines(channel, auth.userEmail); err != nil {
			fmt.Fprintf(channel, "error: %v\n", err)
			return 1
		}
		return 0
	case "create":
		return s.createMachine(channel, auth.userEmail, fields)
	case "clone":
		return s.cloneMachine(channel, auth.userEmail, fields)
	case "delete":
		return s.deleteMachine(channel, fields)
	default:
		fmt.Fprintf(channel, "unknown command: %s\n", fields[0])
		fmt.Fprintln(channel, "available commands: help, dashboard, whoami, machines, create, clone, delete, exit")
		return 127
	}
}

func (s *Server) runDashboard(channel ssh.Channel, requests <-chan *ssh.Request, userEmail string, size windowSize) uint32 {
	model := tui.NewDashboard(userEmail, s.machines, size.width, size.height)
	if _, err := s.runProgram(channel, requests, size, model); err != nil {
		fmt.Fprintf(channel, "error: %v\r\n", err)
		return 1
	}

	return 0
}

func (s *Server) runSignup(channel ssh.Channel, requests <-chan *ssh.Request, publicKey string, size windowSize) (string, uint32) {
	if s.signup == nil || !s.signup.Enabled() {
		fmt.Fprintln(channel, "signup is not configured on this server")
		return "", 1
	}

	model := tui.NewSignup(s.signup, publicKey)
	finalModel, err := s.runProgram(channel, requests, size, model)
	if err != nil {
		fmt.Fprintf(channel, "error: %v\r\n", err)
		return "", 1
	}

	signupModel, ok := finalModel.(tui.SignupModel)
	if !ok {
		return "", 1
	}
	if !signupModel.Verified() {
		return "", 0
	}

	return signupModel.VerifiedEmail(), 0
}

func (s *Server) runProgram(channel ssh.Channel, requests <-chan *ssh.Request, size windowSize, model tea.Model) (tea.Model, error) {
	program := tea.NewProgram(
		model,
		tea.WithInput(channel),
		tea.WithOutput(channel),
		tea.WithAltScreen(),
	)

	stop := make(chan struct{})
	defer close(stop)

	go func() {
		for {
			select {
			case <-stop:
				return
			case req, ok := <-requests:
				if !ok {
					return
				}

				switch req.Type {
				case "window-change":
					size = parseWindowChange(req.Payload, size)
					program.Send(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				case "pty-req":
					size = parsePTYRequest(req.Payload, size)
					program.Send(tea.WindowSizeMsg{Width: size.width, Height: size.height})
					replyIfWanted(req, true)
				default:
					replyIfWanted(req, false)
				}
			}
		}
	}()

	return program.Run()
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

type sessionAuth struct {
	userEmail      string
	publicKey      string
	signupRequired bool
}

func replyIfWanted(req *ssh.Request, ok bool) {
	if req.WantReply {
		req.Reply(ok, nil)
	}
}

type windowSize struct {
	width  int
	height int
}

func parsePTYRequest(payload []byte, fallback windowSize) windowSize {
	var msg struct {
		Term   string
		Width  uint32
		Height uint32
		PxW    uint32
		PxH    uint32
		Modes  string
	}
	if err := ssh.Unmarshal(payload, &msg); err != nil {
		return fallback
	}
	if msg.Width == 0 || msg.Height == 0 {
		return fallback
	}
	return windowSize{width: int(msg.Width), height: int(msg.Height)}
}

func parseWindowChange(payload []byte, fallback windowSize) windowSize {
	var msg struct {
		Width  uint32
		Height uint32
		PxW    uint32
		PxH    uint32
	}
	if err := ssh.Unmarshal(payload, &msg); err != nil {
		return fallback
	}
	if msg.Width == 0 || msg.Height == 0 {
		return fallback
	}
	return windowSize{width: int(msg.Width), height: int(msg.Height)}
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
