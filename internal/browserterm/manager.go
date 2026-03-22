package browserterm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
)

type machineManager interface {
	GetMachine(context.Context, string, string) (controlplane.Machine, error)
}

type Manager struct {
	cfg             config.Config
	machines        machineManager
	sshClientBinary string
	guestSSHKeyPath string
	localHostID     string
	guestReadyWait  time.Duration
	guestReadyPoll  time.Duration

	mu                  sync.Mutex
	sessions            map[string]*session
	totalCreated        int
	totalAttachFailures int
	totalDisconnects    int
}

type SessionInit struct {
	ID          string `json:"id"`
	HostID      string `json:"host_id"`
	MachineName string `json:"machine_name"`
	AttachURL   string `json:"attach_url"`
	ExpiresAt   string `json:"expires_at"`
}

type Diagnostics struct {
	ActiveSessions      int               `json:"active_sessions"`
	TotalCreated        int               `json:"total_created"`
	TotalAttachFailures int               `json:"total_attach_failures"`
	TotalDisconnects    int               `json:"total_disconnects"`
	Sessions            []SessionMetadata `json:"sessions"`
}

type SessionMetadata struct {
	ID          string  `json:"id"`
	UserEmail   string  `json:"user_email"`
	MachineName string  `json:"machine_name"`
	HostID      string  `json:"host_id"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
	ExpiresAt   string  `json:"expires_at"`
	AttachedAt  *string `json:"attached_at,omitempty"`
	LastError   string  `json:"last_error,omitempty"`
}

type session struct {
	id          string
	tokenHash   string
	userEmail   string
	machineName string
	hostID      string
	tmuxSession string
	cols        int
	rows        int
	createdAt   time.Time
	expiresAt   time.Time
	status      string
	attachedAt  *time.Time
	lastError   string
}

type controlMessage struct {
	Type   string `json:"type"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	SentAt int64  `json:"sent_at,omitempty"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

func New(cfg config.Config, machines machineManager) *Manager {
	localHostID := strings.TrimSpace(cfg.HostID)
	if localHostID == "" {
		localHostID = "local-host"
	}
	return &Manager{
		cfg:             cfg,
		machines:        machines,
		sshClientBinary: firstNonEmpty(strings.TrimSpace(cfg.SSHClientBinary), "ssh"),
		guestSSHKeyPath: strings.TrimSpace(cfg.GuestSSHKeyPath),
		localHostID:     localHostID,
		guestReadyWait:  20 * time.Second,
		guestReadyPoll:  1500 * time.Millisecond,
		sessions:        map[string]*session{},
	}
}

func (m *Manager) CreateSession(ctx context.Context, userEmail, machineName string, cols, rows int) (SessionInit, error) {
	m.pruneExpiredSessions()
	machine, err := m.machines.GetMachine(ctx, machineName, userEmail)
	if err != nil {
		return SessionInit{}, err
	}
	if !strings.EqualFold(machine.State, "RUNNING") {
		return SessionInit{}, fmt.Errorf("machine %q is %s", machineName, strings.ToLower(strings.TrimSpace(machine.State)))
	}

	hostID := strings.TrimSpace(machine.HostID)
	if hostID == "" {
		hostID = m.localHostID
	}
	if hostID != m.localHostID {
		return SessionInit{}, fmt.Errorf("browser terminal gateway for host %q is not available", hostID)
	}
	if machine.Runtime == nil {
		return SessionInit{}, fmt.Errorf("machine %q is not available", machineName)
	}

	id, err := randomHex(16)
	if err != nil {
		return SessionInit{}, err
	}
	token, err := randomHex(32)
	if err != nil {
		return SessionInit{}, err
	}
	expiresAt := time.Now().UTC().Add(m.cfg.TerminalSessionTTL)
	if expiresAt.IsZero() || m.cfg.TerminalSessionTTL <= 0 {
		expiresAt = time.Now().UTC().Add(5 * time.Minute)
	}

	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}

	now := time.Now().UTC()
	m.mu.Lock()
	m.sessions[id] = &session{
		id:          id,
		tokenHash:   hashToken(token),
		userEmail:   userEmail,
		machineName: machine.Name,
		hostID:      hostID,
		tmuxSession: "fascinate-" + id,
		cols:        cols,
		rows:        rows,
		createdAt:   now,
		expiresAt:   expiresAt,
		status:      "CREATED",
	}
	m.totalCreated++
	init := SessionInit{
		ID:          id,
		HostID:      hostID,
		MachineName: machine.Name,
		AttachURL:   fmt.Sprintf("/v1/terminal/sessions/%s/stream?token=%s", id, token),
		ExpiresAt:   expiresAt.Format(time.RFC3339),
	}
	m.mu.Unlock()

	return init, nil
}

func (m *Manager) ReattachSession(ctx context.Context, userEmail, sessionID string, cols, rows int) (SessionInit, error) {
	m.pruneExpiredSessions()
	machineName, err := m.sessionMachineName(sessionID, userEmail)
	if err != nil {
		return SessionInit{}, err
	}
	machine, err := m.machines.GetMachine(ctx, machineName, userEmail)
	if err != nil {
		return SessionInit{}, err
	}
	if !strings.EqualFold(machine.State, "RUNNING") {
		return SessionInit{}, fmt.Errorf("machine %q is %s", machine.Name, strings.ToLower(strings.TrimSpace(machine.State)))
	}

	hostID := strings.TrimSpace(machine.HostID)
	if hostID == "" {
		hostID = m.localHostID
	}
	if hostID != m.localHostID {
		return SessionInit{}, fmt.Errorf("browser terminal gateway for host %q is not available", hostID)
	}
	if machine.Runtime == nil {
		return SessionInit{}, fmt.Errorf("machine %q is not available", machine.Name)
	}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}

	token, err := randomHex(32)
	if err != nil {
		return SessionInit{}, err
	}
	expiresAt := m.nextExpiry()

	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[strings.TrimSpace(sessionID)]
	if !ok || sess.userEmail != userEmail {
		return SessionInit{}, fmt.Errorf("terminal session not found")
	}
	sess.tokenHash = hashToken(token)
	sess.hostID = hostID
	sess.cols = cols
	sess.rows = rows
	sess.expiresAt = expiresAt
	sess.status = "READY"
	sess.lastError = ""

	return SessionInit{
		ID:          sess.id,
		HostID:      hostID,
		MachineName: machine.Name,
		AttachURL:   fmt.Sprintf("/v1/terminal/sessions/%s/stream?token=%s", sess.id, token),
		ExpiresAt:   expiresAt.Format(time.RFC3339),
	}, nil
}

func (m *Manager) CloseSession(ctx context.Context, userEmail, sessionID string) error {
	sess, err := m.removeSession(sessionID, userEmail, "CLOSED", nil)
	if err != nil {
		return err
	}
	_ = m.destroyRemoteSession(ctx, sess)
	return nil
}

func (m *Manager) StreamSession(w http.ResponseWriter, r *http.Request, id string) error {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		return fmt.Errorf("token is required")
	}

	sess, err := m.startSession(id, token)
	if err != nil {
		return err
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return err
	}
	defer conn.CloseNow()

	ctx := r.Context()

	machine, err := m.resolveSessionMachine(ctx, sess)
	if err != nil {
		_ = m.writeControl(conn, ctx, controlMessage{Type: "error", Error: err.Error()})
		_ = conn.Close(websocket.StatusInternalError, err.Error())
		m.markAttachFailed(sess.id, err)
		return nil
	}

	ptmx, cmd, err := m.startGuestShell(ctx, sess, machine)
	if err != nil {
		_ = m.writeControl(conn, ctx, controlMessage{Type: "error", Error: err.Error()})
		_ = conn.Close(websocket.StatusInternalError, err.Error())
		m.markAttachFailed(sess.id, err)
		return nil
	}
	defer ptmx.Close()
	m.markConnected(sess.id)

	ptyDone := make(chan error, 1)
	wsDone := make(chan error, 1)
	waitDone := make(chan error, 1)

	go func() {
		buffer := make([]byte, 16*1024)
		for {
			n, readErr := ptmx.Read(buffer)
			if n > 0 {
				writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := conn.Write(writeCtx, websocket.MessageBinary, append([]byte(nil), buffer[:n]...))
				cancel()
				if err != nil {
					ptyDone <- err
					return
				}
			}
			if readErr != nil {
				ptyDone <- readErr
				return
			}
		}
	}()

	go func() {
		for {
			msgType, payload, err := conn.Read(ctx)
			if err != nil {
				wsDone <- err
				return
			}
			m.touchSession(sess.id)
			switch msgType {
			case websocket.MessageBinary:
				if _, err := ptmx.Write(payload); err != nil {
					wsDone <- err
					return
				}
			case websocket.MessageText:
				if err := m.handleControlMessage(ctx, conn, sess.id, ptmx, payload); err != nil {
					wsDone <- err
					return
				}
			}
		}
	}()

	go func() {
		waitDone <- cmd.Wait()
	}()

	var exitErr error
	select {
	case err := <-waitDone:
		exitErr = err
	case err := <-ptyDone:
		exitErr = err
	case err := <-wsDone:
		exitErr = err
	case <-ctx.Done():
		exitErr = ctx.Err()
	}

	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = ptmx.Close()

	status := websocket.StatusNormalClosure
	message := ""
	if exitErr != nil && !isExpectedSessionEndError(exitErr) {
		if code := websocket.CloseStatus(exitErr); code != -1 {
			status = code
		} else {
			status = websocket.StatusInternalError
		}
		message = exitErr.Error()
	}
	_ = m.writeControl(conn, context.Background(), controlMessage{Type: "exit", Status: 0, Error: message})
	_ = conn.Close(status, message)

	if exitErr != nil && !isExpectedSessionEndError(exitErr) {
		m.markAttachFailed(sess.id, exitErr)
		return nil
	}
	m.markDetached(sess.id)
	return nil
}

func (m *Manager) Diagnostics() Diagnostics {
	m.pruneExpiredSessions()
	m.mu.Lock()
	defer m.mu.Unlock()

	out := Diagnostics{
		TotalCreated:        m.totalCreated,
		TotalAttachFailures: m.totalAttachFailures,
		TotalDisconnects:    m.totalDisconnects,
	}
	for _, sess := range m.sessions {
		out.ActiveSessions++
		item := SessionMetadata{
			ID:          sess.id,
			UserEmail:   sess.userEmail,
			MachineName: sess.machineName,
			HostID:      sess.hostID,
			Status:      sess.status,
			CreatedAt:   sess.createdAt.Format(time.RFC3339),
			ExpiresAt:   sess.expiresAt.Format(time.RFC3339),
			LastError:   sess.lastError,
		}
		if sess.attachedAt != nil {
			value := sess.attachedAt.Format(time.RFC3339)
			item.AttachedAt = &value
		}
		out.Sessions = append(out.Sessions, item)
	}
	return out
}

func (m *Manager) handleControlMessage(ctx context.Context, conn *websocket.Conn, sessionID string, ptmx *os.File, payload []byte) error {
	var message controlMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		return err
	}
	switch message.Type {
	case "resize":
		return pty.Setsize(ptmx, &pty.Winsize{
			Cols: uint16(max(1, message.Cols)),
			Rows: uint16(max(1, message.Rows)),
		})
	case "ping":
		m.touchSession(sessionID)
		return m.writeControl(conn, ctx, controlMessage{Type: "pong", SentAt: message.SentAt})
	default:
		return nil
	}
}

func (m *Manager) writeControl(conn *websocket.Conn, ctx context.Context, message controlMessage) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func (m *Manager) nextExpiry() time.Time {
	ttl := m.cfg.TerminalSessionTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return time.Now().UTC().Add(ttl)
}

func (m *Manager) sessionMachineName(sessionID, userEmail string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[strings.TrimSpace(sessionID)]
	if !ok || sess.userEmail != strings.TrimSpace(userEmail) {
		return "", fmt.Errorf("terminal session not found")
	}
	if time.Now().UTC().After(sess.expiresAt) {
		delete(m.sessions, sess.id)
		m.totalAttachFailures++
		return "", fmt.Errorf("terminal session expired")
	}
	return sess.machineName, nil
}

func (m *Manager) resolveSessionMachine(ctx context.Context, sess session) (controlplane.Machine, error) {
	machine, err := m.machines.GetMachine(ctx, sess.machineName, sess.userEmail)
	if err != nil {
		return controlplane.Machine{}, err
	}
	if !strings.EqualFold(machine.State, "RUNNING") {
		return controlplane.Machine{}, fmt.Errorf("machine %q is %s", machine.Name, strings.ToLower(strings.TrimSpace(machine.State)))
	}
	hostID := strings.TrimSpace(machine.HostID)
	if hostID == "" {
		hostID = m.localHostID
	}
	if hostID != m.localHostID {
		return controlplane.Machine{}, fmt.Errorf("browser terminal gateway for host %q is not available", hostID)
	}
	if machine.Runtime == nil {
		return controlplane.Machine{}, fmt.Errorf("machine %q is not available", machine.Name)
	}
	return machine, nil
}

func (m *Manager) removeSession(sessionID, userEmail, status string, err error) (session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[strings.TrimSpace(sessionID)]
	if !ok || sess.userEmail != strings.TrimSpace(userEmail) {
		return session{}, fmt.Errorf("terminal session not found")
	}
	delete(m.sessions, sess.id)
	sess.status = status
	if err != nil {
		sess.lastError = err.Error()
		m.totalAttachFailures++
	}
	m.totalDisconnects++
	return cloneSession(sess), nil
}

func (m *Manager) pruneExpiredSessions() {
	now := time.Now().UTC()
	expired := make([]session, 0)

	m.mu.Lock()
	for id, sess := range m.sessions {
		if now.After(sess.expiresAt) {
			expired = append(expired, cloneSession(sess))
			delete(m.sessions, id)
			m.totalDisconnects++
		}
	}
	m.mu.Unlock()

	for _, sess := range expired {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = m.destroyRemoteSession(ctx, sess)
		cancel()
	}
}

func (m *Manager) startSession(id, token string) (session, error) {
	m.pruneExpiredSessions()
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[strings.TrimSpace(id)]
	if !ok {
		return session{}, fmt.Errorf("terminal session not found")
	}
	if time.Now().UTC().After(sess.expiresAt) {
		delete(m.sessions, sess.id)
		m.totalAttachFailures++
		return session{}, fmt.Errorf("terminal session expired")
	}
	if sess.tokenHash != hashToken(token) {
		m.totalAttachFailures++
		return session{}, fmt.Errorf("terminal session token is invalid")
	}
	now := time.Now().UTC()
	sess.status = "ATTACHING"
	sess.attachedAt = &now
	sess.expiresAt = m.nextExpiry()
	return cloneSession(sess), nil
}

func (m *Manager) markConnected(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return
	}
	sess.status = "CONNECTED"
	sess.lastError = ""
}

func (m *Manager) markDetached(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return
	}
	sess.status = "DETACHED"
	sess.lastError = ""
	sess.expiresAt = m.nextExpiry()
	m.totalDisconnects++
}

func (m *Manager) markAttachFailed(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return
	}
	sess.status = "ERROR"
	if err != nil {
		sess.lastError = err.Error()
	}
	sess.expiresAt = m.nextExpiry()
	m.totalAttachFailures++
	m.totalDisconnects++
}

func (m *Manager) touchSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return
	}
	sess.expiresAt = m.nextExpiry()
}

func (m *Manager) startGuestShell(ctx context.Context, sess session, machine controlplane.Machine) (*os.File, *exec.Cmd, error) {
	if machine.Runtime == nil {
		return nil, nil, fmt.Errorf("machine %q is not available", machine.Name)
	}
	targetHost := strings.TrimSpace(machine.Runtime.SSHHost)
	targetPort := machine.Runtime.SSHPort
	if targetHost == "" || targetPort <= 0 {
		return nil, nil, fmt.Errorf("machine %q does not have a reachable guest shell endpoint", machine.Name)
	}
	guestUser := strings.TrimSpace(machine.Runtime.GuestUser)
	if guestUser == "" {
		guestUser = "ubuntu"
	}
	if err := m.waitForGuestAccess(ctx, machine, targetHost, targetPort, guestUser); err != nil {
		return nil, nil, err
	}

	term := "xterm-256color"
	remoteCommand := guestSSHRemoteCommand(term, persistentGuestShellCommand(sess.tmuxSession, guestShellCommand()))
	args := append(m.guestSSHArgs(guestUser, targetHost, targetPort), "-tt", remoteCommand)
	cmd := exec.Command(m.sshClientBinary, args...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(max(1, sess.cols)),
		Rows: uint16(max(1, sess.rows)),
	})
	if err != nil {
		return nil, nil, err
	}
	return ptmx, cmd, nil
}

func (m *Manager) destroyRemoteSession(ctx context.Context, sess session) error {
	if strings.TrimSpace(sess.machineName) == "" || strings.TrimSpace(sess.userEmail) == "" || strings.TrimSpace(sess.tmuxSession) == "" {
		return nil
	}
	machine, err := m.machines.GetMachine(ctx, sess.machineName, sess.userEmail)
	if err != nil || machine.Runtime == nil {
		return nil
	}
	targetHost := strings.TrimSpace(machine.Runtime.SSHHost)
	targetPort := machine.Runtime.SSHPort
	if targetHost == "" || targetPort <= 0 {
		return nil
	}
	guestUser := strings.TrimSpace(machine.Runtime.GuestUser)
	if guestUser == "" {
		guestUser = "ubuntu"
	}
	command := fmt.Sprintf(
		"if command -v tmux >/dev/null 2>&1; then tmux kill-session -t %s 2>/dev/null || true; fi",
		shellLiteral(strings.TrimSpace(sess.tmuxSession)),
	)
	args := append(m.guestSSHArgs(guestUser, targetHost, targetPort), guestSSHRemoteCommand("xterm-256color", command))
	cmd := exec.CommandContext(ctx, m.sshClientBinary, args...)
	_, err = cmd.CombinedOutput()
	return err
}

func (m *Manager) waitForGuestAccess(ctx context.Context, machine controlplane.Machine, targetHost string, targetPort int, guestUser string) error {
	waitFor := m.guestReadyWait
	if waitFor <= 0 {
		waitFor = 20 * time.Second
	}
	pollEvery := m.guestReadyPoll
	if pollEvery <= 0 {
		pollEvery = 1500 * time.Millisecond
	}

	deadline := time.Now().Add(waitFor)
	for {
		err := m.probeGuestAccess(ctx, targetHost, targetPort, guestUser)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !isRetryableGuestAccessError(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("machine %q is still booting; try again in a few seconds", machine.Name)
		}
		time.Sleep(pollEvery)
	}
}

func (m *Manager) probeGuestAccess(ctx context.Context, targetHost string, targetPort int, guestUser string) error {
	args := append(m.guestSSHArgs(guestUser, targetHost, targetPort), "true")
	cmd := exec.CommandContext(ctx, m.sshClientBinary, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputText := strings.TrimSpace(string(output))
		if outputText == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, outputText)
	}
	return nil
}

func (m *Manager) guestSSHArgs(guestUser, targetHost string, targetPort int) []string {
	return []string{
		"-i", m.guestSSHKeyPath,
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
		"-p", strconv.Itoa(targetPort),
		fmt.Sprintf("%s@%s", guestUser, targetHost),
	}
}

func guestSSHRemoteCommand(term, shellCommand string) string {
	return fmt.Sprintf(
		"env TERM=%s SHELL=/bin/bash sh -lc '%s'",
		shellQuoteSingle(strings.TrimSpace(term)),
		shellQuoteSingle(shellCommand),
	)
}

func guestShellCommand() string {
	return strings.Join([]string{
		"if command -v gh >/dev/null 2>&1 && ! gh auth status --hostname github.com >/dev/null 2>&1; then",
		`  printf '\nGitHub not connected. For private GitHub repos, run: gh auth login && gh auth setup-git\n\n'`,
		"fi",
		"if command -v bash >/dev/null 2>&1; then exec bash -l; else exec sh -l; fi",
	}, "\n")
}

func persistentGuestShellCommand(tmuxSession, shellCommand string) string {
	startCommand := fmt.Sprintf(
		"env TERM=${TERM:-xterm-256color} SHELL=/bin/bash sh -lc %s",
		shellLiteral(shellCommand),
	)
	return strings.Join([]string{
		"if ! command -v tmux >/dev/null 2>&1; then",
		`  printf 'tmux is required for persistent browser terminals\n' >&2`,
		"  exit 1",
		"fi",
		fmt.Sprintf("session=%s", shellLiteral(strings.TrimSpace(tmuxSession))),
		fmt.Sprintf("start_command=%s", shellLiteral(startCommand)),
		`if ! tmux has-session -t "$session" 2>/dev/null; then`,
		`  tmux new-session -d -s "$session" "$start_command"`,
		"fi",
		`tmux set-option -t "$session" status off >/dev/null 2>&1 || true`,
		`tmux set-option -t "$session" mouse on >/dev/null 2>&1 || true`,
		`exec tmux attach-session -t "$session"`,
	}, "\n")
}

func shellLiteral(value string) string {
	return "'" + shellQuoteSingle(value) + "'"
}

func shellQuoteSingle(value string) string {
	return strings.ReplaceAll(value, `'`, `'\''`)
}

func isRetryableGuestAccessError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	retryableFragments := []string{
		"connection refused",
		"connection reset by peer",
		"operation timed out",
		"connection timed out",
		"no route to host",
		"network is unreachable",
	}
	for _, fragment := range retryableFragments {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func isIgnorableShellCopyError(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	value := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(value, "input/output error") || strings.Contains(value, "closed")
}

func isExpectedSessionEndError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || isIgnorableShellCopyError(err) {
		return true
	}
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway, websocket.StatusNoStatusRcvd:
		return true
	default:
		return false
	}
}

func randomHex(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func cloneSession(sess *session) session {
	if sess == nil {
		return session{}
	}
	out := *sess
	return out
}
