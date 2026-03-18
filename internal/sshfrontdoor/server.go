package sshfrontdoor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
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
	GetMachine(context.Context, string, string) (controlplane.Machine, error)
	CreateMachine(context.Context, controlplane.CreateMachineInput) (controlplane.Machine, error)
	DeleteMachine(context.Context, string, string) error
	CloneMachine(context.Context, controlplane.CloneMachineInput) (controlplane.Machine, error)
}

type Server struct {
	addr        string
	incusBinary string
	config      *ssh.ServerConfig
	machines    machineManager
	signup      signupManager
	shellRunner func(ssh.Channel, <-chan *ssh.Request, windowSize, string) error
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
		addr:        strings.TrimSpace(cfg.SSHAddr),
		incusBinary: strings.TrimSpace(cfg.IncusBinary),
		config:      serverConfig,
		machines:    machines,
		signup:      signup,
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

	ptyState := sessionPTY{
		size: windowSize{width: 80, height: 24},
		term: "xterm-256color",
	}
	for req := range requests {
		switch req.Type {
		case "pty-req":
			ptyState = parsePTYRequest(req.Payload, ptyState)
			replyIfWanted(req, true)
		case "window-change":
			ptyState.size = parseWindowChange(req.Payload, ptyState.size)
			replyIfWanted(req, true)
		case "shell":
			replyIfWanted(req, true)
			if auth.signupRequired {
				userEmail, nextPTY, status := s.runSignup(channel, requests, auth.publicKey, ptyState)
				if status != 0 || userEmail == "" {
					writeExitStatus(channel, status)
					return
				}
				auth.userEmail = userEmail
				auth.signupRequired = false
				ptyState = nextPTY
			}

			writeExitStatus(channel, s.runInteractiveSession(channel, requests, auth.userEmail, ptyState))
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

			status := s.runCommand(channel, requests, auth, payload.Command, ptyState)
			writeExitStatus(channel, status)
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func (s *Server) runCommand(channel ssh.Channel, requests <-chan *ssh.Request, auth sessionAuth, command string, ptyState sessionPTY) uint32 {
	if auth.signupRequired {
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
		return s.deleteMachine(channel, auth.userEmail, fields)
	case "shell":
		return s.shellMachine(channel, requests, auth.userEmail, fields, ptyState)
	default:
		fmt.Fprintf(channel, "unknown command: %s\n", fields[0])
		fmt.Fprintln(channel, "available commands: help, dashboard, whoami, machines, create, clone, delete, shell, exit")
		return 127
	}
}

func (s *Server) runInteractiveSession(channel ssh.Channel, requests <-chan *ssh.Request, userEmail string, ptyState sessionPTY) uint32 {
	for {
		result, nextPTY, status := s.runDashboard(channel, requests, userEmail, ptyState)
		if status != 0 {
			return status
		}
		ptyState = nextPTY

		if strings.TrimSpace(result.shellTarget) == "" {
			return 0
		}
		if err := s.openAuthorizedMachineShell(channel, requests, ptyState, userEmail, result.shellTarget); err != nil {
			fmt.Fprintf(channel, "error: %v\r\n", err)
			continue
		}
	}
}

type dashboardResult struct {
	shellTarget string
}

func (s *Server) runDashboard(channel ssh.Channel, requests <-chan *ssh.Request, userEmail string, ptyState sessionPTY) (dashboardResult, sessionPTY, uint32) {
	model := tui.NewDashboard(userEmail, s.machines, ptyState.size.width, ptyState.size.height)
	finalModel, nextPTY, err := s.runProgram(channel, requests, ptyState, model)
	if err != nil {
		fmt.Fprintf(channel, "error: %v\r\n", err)
		return dashboardResult{}, nextPTY, 1
	}

	dashboardModel, ok := finalModel.(tui.Model)
	if !ok {
		return dashboardResult{}, nextPTY, 1
	}

	return dashboardResult{shellTarget: dashboardModel.ShellTarget()}, nextPTY, 0
}

func (s *Server) runSignup(channel ssh.Channel, requests <-chan *ssh.Request, publicKey string, ptyState sessionPTY) (string, sessionPTY, uint32) {
	if s.signup == nil || !s.signup.Enabled() {
		fmt.Fprintln(channel, "signup is not configured on this server")
		return "", ptyState, 1
	}

	model := tui.NewSignup(s.signup, publicKey)
	finalModel, nextPTY, err := s.runProgram(channel, requests, ptyState, model)
	if err != nil {
		fmt.Fprintf(channel, "error: %v\r\n", err)
		return "", nextPTY, 1
	}

	signupModel, ok := finalModel.(tui.SignupModel)
	if !ok {
		return "", nextPTY, 1
	}
	if !signupModel.Verified() {
		return "", nextPTY, 0
	}

	return signupModel.VerifiedEmail(), nextPTY, 0
}

func (s *Server) runProgram(channel ssh.Channel, requests <-chan *ssh.Request, ptyState sessionPTY, model tea.Model) (tea.Model, sessionPTY, error) {
	env := os.Environ()
	if ptyState.term != "" {
		env = append(env, "TERM="+ptyState.term)
	}

	sizedModel := withInitialWindowSize(model, ptyState.size)
	program := tea.NewProgram(
		sizedModel,
		tea.WithInput(channel),
		tea.WithOutput(channel),
		tea.WithEnvironment(env),
		tea.WithAltScreen(),
	)

	currentPTY := ptyState
	var stateMu sync.Mutex

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
					stateMu.Lock()
					currentPTY.size = parseWindowChange(req.Payload, currentPTY.size)
					nextPTY := currentPTY
					stateMu.Unlock()
					program.Send(tea.WindowSizeMsg{Width: nextPTY.size.width, Height: nextPTY.size.height})
				case "pty-req":
					stateMu.Lock()
					currentPTY = parsePTYRequest(req.Payload, currentPTY)
					nextPTY := currentPTY
					stateMu.Unlock()
					program.Send(tea.WindowSizeMsg{Width: nextPTY.size.width, Height: nextPTY.size.height})
					replyIfWanted(req, true)
				default:
					replyIfWanted(req, false)
				}
			}
		}
	}()

	finalModel, err := program.Run()
	stateMu.Lock()
	nextPTY := currentPTY
	stateMu.Unlock()

	if wrapped, ok := finalModel.(initialWindowSizeModel); ok {
		return wrapped.Model, nextPTY, err
	}

	return finalModel, nextPTY, err
}

func (s *Server) renderDashboard(channel ssh.Channel, userEmail string) {
	fmt.Fprintln(channel, "fascinate")
	fmt.Fprintf(channel, "signed in as %s\n\n", userEmail)
	if err := s.renderMachines(channel, userEmail); err != nil {
		fmt.Fprintf(channel, "error loading machines: %v\n", err)
	}
	fmt.Fprintln(channel, "\ncommands: machines, create <name>, clone <source> <target>, delete <name> --confirm <name>, shell <name>, whoami, help, exit")
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

func (s *Server) deleteMachine(channel ssh.Channel, userEmail string, fields []string) uint32 {
	if len(fields) != 4 || fields[2] != "--confirm" || fields[3] != fields[1] {
		fmt.Fprintln(channel, "usage: delete <name> --confirm <name>")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := s.machines.DeleteMachine(ctx, fields[1], userEmail); err != nil {
		fmt.Fprintf(channel, "error: %v\n", err)
		return 1
	}

	fmt.Fprintf(channel, "deleted %s\n", fields[1])
	return 0
}

func (s *Server) shellMachine(channel ssh.Channel, requests <-chan *ssh.Request, userEmail string, fields []string, ptyState sessionPTY) uint32 {
	if len(fields) != 2 {
		fmt.Fprintln(channel, "usage: shell <name>")
		return 2
	}

	if err := s.openAuthorizedMachineShell(channel, requests, ptyState, userEmail, fields[1]); err != nil {
		fmt.Fprintf(channel, "error: %v\n", err)
		return 1
	}

	return 0
}

func (s *Server) openAuthorizedMachineShell(channel ssh.Channel, requests <-chan *ssh.Request, ptyState sessionPTY, userEmail, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	machine, err := s.machines.GetMachine(ctx, name, userEmail)
	if err != nil {
		return err
	}

	runtimeName := machine.Name
	if machine.Runtime != nil && strings.TrimSpace(machine.Runtime.Name) != "" {
		runtimeName = strings.TrimSpace(machine.Runtime.Name)
	}
	if runtimeName == "" {
		return fmt.Errorf("machine %q is not available", strings.TrimSpace(name))
	}

	runner := s.shellRunner
	if runner == nil {
		runner = s.runIncusShell
	}

	return runner(channel, requests, ptyState.size, runtimeName)
}

func (s *Server) runIncusShell(channel ssh.Channel, requests <-chan *ssh.Request, size windowSize, machineName string) error {
	term := "xterm-256color"
	args := []string{
		"exec",
		strings.TrimSpace(machineName),
		"--mode=interactive",
		"--",
		"env",
		"TERM=" + term,
		"HOME=/root",
		"SHELL=/bin/bash",
		"sh",
		"-lc",
		"if command -v bash >/dev/null 2>&1; then exec bash -l; else exec sh -l; fi",
	}

	cmd := exec.Command(s.incusBinary, args...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(max(1, size.width)),
		Rows: uint16(max(1, size.height)),
	})
	if err != nil {
		return err
	}
	defer ptmx.Close()

	stop := make(chan struct{})
	defer close(stop)
	go s.forwardShellRequests(requests, ptmx, size, stop)

	stdinDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(ptmx, channel)
		close(stdinDone)
	}()

	_, copyErr := io.Copy(channel, ptmx)
	_ = ptmx.Close()
	<-stdinDone
	waitErr := cmd.Wait()

	if copyErr != nil && !isIgnorableShellCopyError(copyErr) {
		return copyErr
	}
	if waitErr != nil {
		return waitErr
	}

	return nil
}

func (s *Server) forwardShellRequests(requests <-chan *ssh.Request, ptmx *os.File, size windowSize, stop <-chan struct{}) {
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
				_ = pty.Setsize(ptmx, &pty.Winsize{
					Cols: uint16(max(1, size.width)),
					Rows: uint16(max(1, size.height)),
				})
				replyIfWanted(req, true)
			case "pty-req":
				size = parsePTYRequest(req.Payload, sessionPTY{size: size}).size
				_ = pty.Setsize(ptmx, &pty.Winsize{
					Cols: uint16(max(1, size.width)),
					Rows: uint16(max(1, size.height)),
				})
				replyIfWanted(req, true)
			default:
				replyIfWanted(req, false)
			}
		}
	}
}

func isIgnorableShellCopyError(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}

	value := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(value, "input/output error") || strings.Contains(value, "closed")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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

type sessionPTY struct {
	size windowSize
	term string
}

type initialWindowSizeModel struct {
	tea.Model
	size windowSize
}

func withInitialWindowSize(model tea.Model, size windowSize) initialWindowSizeModel {
	return initialWindowSizeModel{Model: model, size: size}
}

func (m initialWindowSizeModel) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			return tea.WindowSizeMsg{Width: m.size.width, Height: m.size.height}
		},
		m.Model.Init(),
	)
}

func (m initialWindowSizeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.Model.Update(msg)
	m.Model = next
	return m, cmd
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

func parsePTYRequest(payload []byte, fallback sessionPTY) sessionPTY {
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

	next := fallback
	if strings.TrimSpace(msg.Term) != "" {
		next.term = strings.TrimSpace(msg.Term)
	}
	if msg.Width > 0 && msg.Height > 0 {
		next.size = windowSize{width: int(msg.Width), height: int(msg.Height)}
	}
	return next
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
