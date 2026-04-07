package browserterm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/google/uuid"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
)

type machineManager interface {
	GetMachine(context.Context, string, string) (controlplane.Machine, error)
}

var (
	ErrTerminalSessionNotFound = errors.New("terminal session not found")
	ErrTerminalSessionExpired  = errors.New("terminal session expired")
)

type Manager struct {
	cfg             config.Config
	store           *database.Store
	machines        machineManager
	sshClientBinary string
	guestSSHKeyPath string
	localHostID     string
	guestReadyWait  time.Duration
	guestReadyPoll  time.Duration

	mu                  sync.Mutex
	attachments         map[string]*attachment
	totalCreated        int
	totalAttachFailures int
	totalDisconnects    int

	gitCommandMu    sync.Mutex
	gitCommandGates map[string]chan struct{}

	execMu   sync.Mutex
	execJobs map[string]*execJob
}

type SessionInit struct {
	ID          string `json:"id"`
	HostID      string `json:"host_id"`
	MachineName string `json:"machine_name"`
	AttachURL   string `json:"attach_url"`
	ExpiresAt   string `json:"expires_at"`
}

type Shell struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	UserEmail      string  `json:"user_email,omitempty"`
	MachineName    string  `json:"machine_name"`
	HostID         string  `json:"host_id,omitempty"`
	State          string  `json:"state"`
	CWD            string  `json:"cwd,omitempty"`
	LastAttachedAt *string `json:"last_attached_at,omitempty"`
	LastError      string  `json:"last_error,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type GitRepoStatus struct {
	State     string           `json:"state"`
	RepoRoot  string           `json:"repo_root,omitempty"`
	Branch    string           `json:"branch,omitempty"`
	Additions int              `json:"additions,omitempty"`
	Deletions int              `json:"deletions,omitempty"`
	Files     []GitChangedFile `json:"files,omitempty"`
}

type GitChangedFile struct {
	Path           string `json:"path"`
	PreviousPath   string `json:"previous_path,omitempty"`
	Kind           string `json:"kind"`
	IndexStatus    string `json:"index_status,omitempty"`
	WorktreeStatus string `json:"worktree_status,omitempty"`
}

type GitDiffBatchFile struct {
	Path           string `json:"path"`
	PreviousPath   string `json:"previous_path,omitempty"`
	Kind           string `json:"kind,omitempty"`
	IndexStatus    string `json:"index_status,omitempty"`
	WorktreeStatus string `json:"worktree_status,omitempty"`
}

type GitDiffBatchRequest struct {
	RepoRoot string             `json:"repo_root"`
	Cwd      string             `json:"cwd"`
	Files    []GitDiffBatchFile `json:"files"`
}

type GitDiffBatchResponse struct {
	Diffs []GitFileDiff `json:"diffs"`
}

type GitFileDiff struct {
	State        string `json:"state"`
	Path         string `json:"path"`
	PreviousPath string `json:"previous_path,omitempty"`
	Patch        string `json:"patch,omitempty"`
	Additions    int    `json:"additions,omitempty"`
	Deletions    int    `json:"deletions,omitempty"`
	Message      string `json:"message,omitempty"`
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

type attachment struct {
	shellID    string
	tokenHash  string
	hostID     string
	cols       int
	rows       int
	expiresAt  time.Time
	createdAt  time.Time
	attachedAt *time.Time
}

type controlMessage struct {
	Type   string `json:"type"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	SentAt int64  `json:"sent_at,omitempty"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

const (
	gitRepoStatusReady    = "ready"
	gitRepoStatusNotRepo  = "not_repo"
	gitDiffStateReady     = "ready"
	gitDiffStateError     = "error"
	gitDiffStateBinary    = "binary"
	gitDiffStateTooLarge  = "too_large"
	gitDiffMaxBytes       = 200_000
	gitDiffMaxLines       = 4_000
	gitNotRepoSentinel    = "__FASCINATE_NOT_REPO__"
	gitCommandConcurrency = 2
)

func New(cfg config.Config, machines machineManager, stores ...*database.Store) *Manager {
	localHostID := strings.TrimSpace(cfg.HostID)
	if localHostID == "" {
		localHostID = "local-host"
	}
	var store *database.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	return &Manager{
		cfg:             cfg,
		store:           store,
		machines:        machines,
		sshClientBinary: firstNonEmpty(strings.TrimSpace(cfg.SSHClientBinary), "ssh"),
		guestSSHKeyPath: strings.TrimSpace(cfg.GuestSSHKeyPath),
		localHostID:     localHostID,
		guestReadyWait:  20 * time.Second,
		guestReadyPoll:  1500 * time.Millisecond,
		attachments:     map[string]*attachment{},
		gitCommandGates: map[string]chan struct{}{},
		execJobs:        map[string]*execJob{},
	}
}

func (m *Manager) CreateShell(ctx context.Context, userEmail, machineName, name string) (Shell, error) {
	if m.store == nil {
		return Shell{}, fmt.Errorf("shell store is not configured")
	}
	machine, err := m.machines.GetMachine(ctx, machineName, userEmail)
	if err != nil {
		return Shell{}, err
	}
	if !strings.EqualFold(machine.State, "RUNNING") {
		return Shell{}, fmt.Errorf("machine %q is %s", machineName, strings.ToLower(strings.TrimSpace(machine.State)))
	}
	hostID := strings.TrimSpace(machine.HostID)
	if hostID == "" {
		hostID = m.localHostID
	}
	if hostID != m.localHostID {
		return Shell{}, fmt.Errorf("browser terminal gateway for host %q is not available", hostID)
	}
	if machine.Runtime == nil {
		return Shell{}, fmt.Errorf("machine %q is not available", machineName)
	}
	user, err := m.store.GetUserByEmail(ctx, userEmail)
	if err != nil {
		return Shell{}, err
	}
	id, err := randomHex(16)
	if err != nil {
		return Shell{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = machine.Name + " shell"
	}
	record, err := m.store.CreateShell(ctx, database.CreateShellParams{
		ID:          id,
		UserID:      user.ID,
		MachineID:   machine.ID,
		HostID:      stringPointer(hostID),
		Name:        name,
		TmuxSession: "fascinate-" + id,
		State:       "READY",
	})
	if err != nil {
		return Shell{}, err
	}
	m.mu.Lock()
	m.totalCreated++
	m.mu.Unlock()
	m.recordShellEventBestEffort(record, "shell.created", map[string]any{
		"shell_id":     record.ID,
		"shell_name":   record.Name,
		"machine_id":   record.MachineID,
		"machine_name": record.MachineName,
		"host_id":      recordHostID(record),
		"shell_state":  record.State,
		"tmux_session": record.TmuxSession,
	})
	return shellFromRecord(record), nil
}

func (m *Manager) ListShells(ctx context.Context, userEmail string) ([]Shell, error) {
	if m.store == nil {
		return nil, fmt.Errorf("shell store is not configured")
	}
	records, err := m.store.ListShells(ctx, userEmail)
	if err != nil {
		return nil, err
	}
	out := make([]Shell, 0, len(records))
	for _, record := range records {
		out = append(out, shellFromRecord(record))
	}
	return out, nil
}

func (m *Manager) GetShell(ctx context.Context, userEmail, shellID string) (Shell, error) {
	record, err := m.loadShell(ctx, shellID, userEmail)
	if err != nil {
		return Shell{}, err
	}
	return shellFromRecord(record), nil
}

func (m *Manager) CreateAttachment(ctx context.Context, userEmail, shellID string, cols, rows int) (SessionInit, error) {
	m.pruneExpiredAttachments()
	record, err := m.loadShell(ctx, shellID, userEmail)
	if err != nil {
		return SessionInit{}, err
	}
	machine, err := m.resolveShellMachine(ctx, record)
	if err != nil {
		return SessionInit{}, err
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
	tokenHash := hashToken(token)
	m.mu.Lock()
	m.attachments[tokenHash] = &attachment{
		shellID:   record.ID,
		tokenHash: tokenHash,
		hostID:    firstNonEmpty(strings.TrimSpace(recordHostID(record)), m.localHostID),
		cols:      cols,
		rows:      rows,
		expiresAt: expiresAt,
		createdAt: time.Now().UTC(),
	}
	m.mu.Unlock()
	if m.store != nil {
		_ = m.store.TouchShellAttached(ctx, record.ID)
		_ = m.store.UpdateShellState(ctx, record.ID, "READY", nil)
	}
	m.recordShellEventBestEffort(record, "shell.attached", map[string]any{
		"shell_id":     record.ID,
		"machine_id":   record.MachineID,
		"machine_name": record.MachineName,
		"host_id":      firstNonEmpty(strings.TrimSpace(recordHostID(record)), machine.HostID, m.localHostID),
	})
	return SessionInit{
		ID:          record.ID,
		HostID:      firstNonEmpty(strings.TrimSpace(recordHostID(record)), machine.HostID, m.localHostID),
		MachineName: machine.Name,
		AttachURL:   fmt.Sprintf("/v1/terminal/sessions/%s/stream?token=%s", record.ID, token),
		ExpiresAt:   expiresAt.Format(time.RFC3339),
	}, nil
}

func (m *Manager) CreateSession(ctx context.Context, userEmail, machineName string, cols, rows int) (SessionInit, error) {
	shell, err := m.CreateShell(ctx, userEmail, machineName, "")
	if err != nil {
		return SessionInit{}, err
	}
	return m.CreateAttachment(ctx, userEmail, shell.ID, cols, rows)
}

func (m *Manager) ReattachSession(ctx context.Context, userEmail, sessionID string, cols, rows int) (SessionInit, error) {
	return m.CreateAttachment(ctx, userEmail, sessionID, cols, rows)
}

func (m *Manager) DeleteShell(ctx context.Context, userEmail, shellID string) error {
	record, err := m.loadShell(ctx, shellID, userEmail)
	if err != nil {
		if errors.Is(err, ErrTerminalSessionNotFound) {
			return nil
		}
		return err
	}
	if err := m.store.MarkShellDeleted(ctx, record.ID); err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	m.dropShellAttachments(record.ID)
	m.totalDisconnects++
	_ = m.destroyRemoteShell(ctx, record)
	m.recordShellEventBestEffort(record, "shell.deleted", map[string]any{
		"shell_id":     record.ID,
		"machine_id":   record.MachineID,
		"machine_name": record.MachineName,
		"host_id":      recordHostID(record),
	})
	return nil
}

func (m *Manager) CloseSession(ctx context.Context, userEmail, sessionID string) error {
	return m.DeleteShell(ctx, userEmail, sessionID)
}

func (m *Manager) CloseMachineSessions(ctx context.Context, userEmail, machineName string) error {
	if m.store == nil {
		return nil
	}
	ownerEmail := strings.TrimSpace(userEmail)
	targetMachine := strings.TrimSpace(machineName)
	if ownerEmail == "" || targetMachine == "" {
		return nil
	}
	shells, err := m.store.ListShells(ctx, ownerEmail)
	if err != nil {
		return err
	}
	for _, shell := range shells {
		if strings.TrimSpace(shell.MachineName) != targetMachine {
			continue
		}
		if err := m.DeleteShell(ctx, ownerEmail, shell.ID); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) GetGitStatus(ctx context.Context, userEmail, sessionID, cwd string) (GitRepoStatus, error) {
	record, err := m.loadShell(ctx, sessionID, userEmail)
	if err != nil {
		return GitRepoStatus{}, err
	}
	machine, err := m.resolveShellMachine(ctx, record)
	if err != nil {
		return GitRepoStatus{}, err
	}
	guestUser := guestUserForMachine(machine)
	cwd = normalizeGuestCwd(cwd, guestUser)
	if cwd == "" {
		return GitRepoStatus{}, fmt.Errorf("cwd is required")
	}
	release, err := m.acquireGitCommandSlot(ctx, machine.Name)
	if err != nil {
		return GitRepoStatus{}, err
	}
	defer release()

	script := strings.Join([]string{
		fmt.Sprintf("cwd=%s", shellLiteral(cwd)),
		`repo_root=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null || true)`,
		`if [ -z "$repo_root" ]; then`,
		fmt.Sprintf("  printf %s", shellLiteral(gitNotRepoSentinel)),
		"  exit 0",
		"fi",
		`branch=$(git -C "$repo_root" rev-parse --abbrev-ref HEAD 2>/dev/null || true)`,
		`additions=0`,
		`deletions=0`,
		`numstat_totals() {`,
		`  awk 'NF >= 2 { if ($1 != "-") add += $1; if ($2 != "-") del += $2 } END { printf "%d %d\n", add + 0, del + 0 }'`,
		`}`,
		`merge_totals() {`,
		`  totals=$1`,
		`  set -- $totals`,
		`  additions=$((additions + ${1:-0}))`,
		`  deletions=$((deletions + ${2:-0}))`,
		`}`,
		`if git -C "$repo_root" rev-parse --verify HEAD >/dev/null 2>&1; then`,
		`  merge_totals "$(git -C "$repo_root" -c color.ui=false --no-pager diff --numstat --find-renames --submodule=short HEAD -- | numstat_totals)"`,
		`else`,
		`  merge_totals "$(git -C "$repo_root" -c color.ui=false --no-pager diff --numstat --find-renames --submodule=short --cached -- | numstat_totals)"`,
		`  merge_totals "$(git -C "$repo_root" -c color.ui=false --no-pager diff --numstat --find-renames --submodule=short -- | numstat_totals)"`,
		`fi`,
		`while IFS= read -r untracked_path; do`,
		`  if [ -z "$untracked_path" ]; then`,
		`    continue`,
		`  fi`,
		`  merge_totals "$(git -c color.ui=false --no-pager diff --no-index --numstat -- /dev/null "$repo_root/$untracked_path" 2>/dev/null | numstat_totals)"`,
		`done <<EOF_UNTRACKED`,
		`$(git -C "$repo_root" ls-files --others --exclude-standard)`,
		`EOF_UNTRACKED`,
		`printf '%s\n%s\n%d\n%d\000' "$repo_root" "$branch" "$additions" "$deletions"`,
		`git -C "$repo_root" -c color.ui=false status --porcelain=v2 -z --untracked-files=all`,
	}, "\n")

	output, err := m.runGuestCommand(ctx, machine, script)
	if err != nil {
		return GitRepoStatus{}, err
	}
	if strings.TrimSpace(output) == gitNotRepoSentinel {
		m.touchShell(record.ID)
		return GitRepoStatus{State: gitRepoStatusNotRepo}, nil
	}
	status, err := parseGitRepoStatusOutput(output)
	if err != nil {
		return GitRepoStatus{}, err
	}
	m.touchShell(record.ID)
	return status, nil
}

func (m *Manager) GetGitDiffBatch(ctx context.Context, userEmail, sessionID string, req GitDiffBatchRequest) (GitDiffBatchResponse, error) {
	record, err := m.loadShell(ctx, sessionID, userEmail)
	if err != nil {
		return GitDiffBatchResponse{}, err
	}
	machine, err := m.resolveShellMachine(ctx, record)
	if err != nil {
		return GitDiffBatchResponse{}, err
	}

	req.Cwd = normalizeGuestCwd(req.Cwd, guestUserForMachine(machine))
	req.RepoRoot = strings.TrimSpace(req.RepoRoot)
	if req.Cwd == "" {
		return GitDiffBatchResponse{}, fmt.Errorf("cwd is required")
	}
	if req.RepoRoot == "" {
		return GitDiffBatchResponse{}, fmt.Errorf("repo root is required")
	}
	if len(req.Files) == 0 {
		return GitDiffBatchResponse{}, fmt.Errorf("at least one file is required")
	}
	if len(req.Files) > 8 {
		return GitDiffBatchResponse{}, fmt.Errorf("at most 8 files may be requested at once")
	}
	release, err := m.acquireGitCommandSlot(ctx, machine.Name)
	if err != nil {
		return GitDiffBatchResponse{}, err
	}
	defer release()

	output, err := m.runGuestCommand(ctx, machine, gitDiffBatchShellCommand(req))
	if err != nil {
		return GitDiffBatchResponse{}, err
	}

	var response GitDiffBatchResponse
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return GitDiffBatchResponse{}, err
	}
	if len(response.Diffs) != len(req.Files) {
		return GitDiffBatchResponse{}, fmt.Errorf("git diff batch returned %d result(s) for %d file(s)", len(response.Diffs), len(req.Files))
	}

	m.touchShell(record.ID)
	return response, nil
}

func (m *Manager) SendInput(ctx context.Context, userEmail, shellID, input string) error {
	record, err := m.loadShell(ctx, shellID, userEmail)
	if err != nil {
		return err
	}
	machine, err := m.resolveShellMachine(ctx, record)
	if err != nil {
		return err
	}
	if err := m.ensureRemoteShell(ctx, machine, record.TmuxSession); err != nil {
		return err
	}
	encodedInput := base64.StdEncoding.EncodeToString([]byte(input))
	command := strings.Join([]string{
		fmt.Sprintf("export FASCINATE_SHELL_INPUT=%s", shellLiteral(encodedInput)),
		fmt.Sprintf("export FASCINATE_TMUX_SESSION=%s", shellLiteral(record.TmuxSession)),
		`python3 - <<'PY'`,
		`import base64`,
		`import os`,
		`import subprocess`,
		`session = os.environ["FASCINATE_TMUX_SESSION"]`,
		`payload = base64.b64decode(os.environ["FASCINATE_SHELL_INPUT"]).decode("utf-8", errors="replace")`,
		`parts = payload.split("\n")`,
		`for index, part in enumerate(parts):`,
		`    if part:`,
		`        subprocess.run(["tmux", "send-keys", "-t", session, "-l", part], check=True)`,
		`    if index < len(parts) - 1:`,
		`        subprocess.run(["tmux", "send-keys", "-t", session, "Enter"], check=True)`,
		`PY`,
	}, "\n")
	if _, err := m.runGuestCommand(ctx, machine, command); err != nil {
		return err
	}
	m.touchShell(record.ID)
	return nil
}

func (m *Manager) ReadLines(ctx context.Context, userEmail, shellID string, limit int) ([]string, error) {
	record, err := m.loadShell(ctx, shellID, userEmail)
	if err != nil {
		return nil, err
	}
	machine, err := m.resolveShellMachine(ctx, record)
	if err != nil {
		return nil, err
	}
	if err := m.ensureRemoteShell(ctx, machine, record.TmuxSession); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	output, err := m.runGuestCommand(ctx, machine, fmt.Sprintf(
		"tmux capture-pane -p -J -S -%d -t %s",
		limit,
		shellLiteral(record.TmuxSession),
	))
	if err != nil {
		return nil, err
	}
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.TrimRight(output, "\n")
	if output == "" {
		return nil, nil
	}
	m.touchShell(record.ID)
	return strings.Split(output, "\n"), nil
}

func (m *Manager) StreamSession(w http.ResponseWriter, r *http.Request, id string) error {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		return fmt.Errorf("token is required")
	}

	record, attach, err := m.startAttachment(id, token)
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

	machine, err := m.resolveShellMachine(ctx, record)
	if err != nil {
		_ = m.writeControl(conn, ctx, controlMessage{Type: "error", Error: err.Error()})
		_ = conn.Close(websocket.StatusInternalError, err.Error())
		m.markAttachFailed(record.ID, err)
		return nil
	}

	ptmx, cmd, err := m.startGuestShell(ctx, record, attach, machine)
	if err != nil {
		_ = m.writeControl(conn, ctx, controlMessage{Type: "error", Error: err.Error()})
		_ = conn.Close(websocket.StatusInternalError, err.Error())
		m.markAttachFailed(record.ID, err)
		return nil
	}
	defer ptmx.Close()
	m.markConnected(record.ID)

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
			m.touchShell(record.ID)
			switch msgType {
			case websocket.MessageBinary:
				if _, err := ptmx.Write(payload); err != nil {
					wsDone <- err
					return
				}
			case websocket.MessageText:
				if err := m.handleControlMessage(ctx, conn, record.ID, ptmx, payload); err != nil {
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
		m.markAttachFailed(record.ID, exitErr)
		return nil
	}
	m.markDetached(record.ID)
	return nil
}

func (m *Manager) Diagnostics() Diagnostics {
	m.pruneExpiredAttachments()

	attachments := map[string]attachment{}
	m.mu.Lock()
	out := Diagnostics{
		TotalCreated:        m.totalCreated,
		TotalAttachFailures: m.totalAttachFailures,
		TotalDisconnects:    m.totalDisconnects,
	}
	for _, attach := range m.attachments {
		attachments[attach.shellID] = *attach
	}
	m.mu.Unlock()

	if m.store == nil {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	records, err := m.store.ListShells(ctx, "")
	if err != nil {
		return out
	}
	for _, record := range records {
		out.ActiveSessions++
		item := SessionMetadata{
			ID:          record.ID,
			UserEmail:   record.UserEmail,
			MachineName: record.MachineName,
			HostID:      recordHostID(record),
			Status:      record.State,
			CreatedAt:   record.CreatedAt,
			ExpiresAt:   record.UpdatedAt,
			LastError:   firstNonEmpty(derefString(record.LastError), ""),
		}
		if record.LastAttachedAt != nil {
			value := strings.TrimSpace(*record.LastAttachedAt)
			item.AttachedAt = &value
		}
		if attach, ok := attachments[record.ID]; ok {
			value := attach.expiresAt.Format(time.RFC3339)
			item.ExpiresAt = value
			if attach.attachedAt != nil {
				attachedAt := attach.attachedAt.Format(time.RFC3339)
				item.AttachedAt = &attachedAt
			}
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
		m.touchShell(sessionID)
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

func (m *Manager) loadShell(ctx context.Context, shellID, userEmail string) (database.ShellRecord, error) {
	if m.store == nil {
		return database.ShellRecord{}, fmt.Errorf("shell store is not configured")
	}
	record, err := m.store.GetShellByID(ctx, strings.TrimSpace(shellID))
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return database.ShellRecord{}, ErrTerminalSessionNotFound
		}
		return database.ShellRecord{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(record.UserEmail), strings.TrimSpace(userEmail)) {
		return database.ShellRecord{}, ErrTerminalSessionNotFound
	}
	return record, nil
}

func (m *Manager) resolveShellMachine(ctx context.Context, record database.ShellRecord) (controlplane.Machine, error) {
	machine, err := m.machines.GetMachine(ctx, record.MachineName, record.UserEmail)
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

func (m *Manager) dropShellAttachments(shellID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, attach := range m.attachments {
		if attach.shellID == shellID {
			delete(m.attachments, key)
		}
	}
}

func (m *Manager) pruneExpiredAttachments() {
	now := time.Now().UTC()

	m.mu.Lock()
	for key, attach := range m.attachments {
		if now.After(attach.expiresAt) {
			delete(m.attachments, key)
			m.totalDisconnects++
		}
	}
	m.mu.Unlock()
}

func (m *Manager) startAttachment(id, token string) (database.ShellRecord, attachment, error) {
	if m.store == nil {
		return database.ShellRecord{}, attachment{}, fmt.Errorf("shell store is not configured")
	}
	m.pruneExpiredAttachments()
	record, err := m.store.GetShellByID(context.Background(), strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return database.ShellRecord{}, attachment{}, ErrTerminalSessionNotFound
		}
		return database.ShellRecord{}, attachment{}, err
	}
	tokenHash := hashToken(token)
	m.mu.Lock()
	defer m.mu.Unlock()
	attach, ok := m.attachments[tokenHash]
	if !ok {
		return database.ShellRecord{}, attachment{}, ErrTerminalSessionNotFound
	}
	if attach.shellID != record.ID {
		m.totalAttachFailures++
		return database.ShellRecord{}, attachment{}, fmt.Errorf("terminal session token is invalid")
	}
	if time.Now().UTC().After(attach.expiresAt) {
		delete(m.attachments, tokenHash)
		m.totalAttachFailures++
		return database.ShellRecord{}, attachment{}, ErrTerminalSessionExpired
	}
	now := time.Now().UTC()
	delete(m.attachments, tokenHash)
	copy := *attach
	copy.attachedAt = &now
	return record, copy, nil
}

func (m *Manager) markConnected(id string) {
	if m.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = m.store.UpdateShellState(ctx, id, "READY", nil)
}

func (m *Manager) markDetached(id string) {
	m.touchShell(id)
	m.mu.Lock()
	m.totalDisconnects++
	m.mu.Unlock()
}

func (m *Manager) markAttachFailed(id string, err error) {
	var errText *string
	if err != nil {
		value := err.Error()
		errText = &value
	}
	if m.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = m.store.UpdateShellState(ctx, id, "ERROR", errText)
		record, loadErr := m.store.GetShellByID(ctx, id)
		if loadErr == nil {
			payload := map[string]any{
				"shell_id":     record.ID,
				"machine_id":   record.MachineID,
				"machine_name": record.MachineName,
				"host_id":      recordHostID(record),
			}
			if errText != nil {
				payload["error"] = *errText
			}
			m.recordShellEventBestEffort(record, "shell.attach.failed", payload)
		}
		cancel()
	}
	m.mu.Lock()
	m.totalAttachFailures++
	m.totalDisconnects++
	m.mu.Unlock()
}

func (m *Manager) touchShell(id string) {
	if m.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = m.store.TouchShellAttached(ctx, id)
	_ = m.store.UpdateShellState(ctx, id, "READY", nil)
}

func (m *Manager) startGuestShell(ctx context.Context, record database.ShellRecord, attach attachment, machine controlplane.Machine) (*os.File, *exec.Cmd, error) {
	if machine.Runtime == nil {
		return nil, nil, fmt.Errorf("machine %q is not available", machine.Name)
	}
	targetHost := strings.TrimSpace(machine.Runtime.SSHHost)
	targetPort := machine.Runtime.SSHPort
	if targetHost == "" || targetPort <= 0 {
		return nil, nil, fmt.Errorf("machine %q does not have a reachable guest shell endpoint", machine.Name)
	}
	guestUser := guestUserForMachine(machine)
	if err := m.waitForGuestAccess(ctx, machine, targetHost, targetPort, guestUser); err != nil {
		return nil, nil, err
	}

	term := "xterm-256color"
	remoteCommand := guestSSHRemoteCommand(term, persistentGuestShellCommand(record.TmuxSession, guestShellCommand()))
	args := append(m.guestSSHArgs(guestUser, targetHost, targetPort), "-tt", remoteCommand)
	cmd := exec.Command(m.sshClientBinary, args...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(max(1, attach.cols)),
		Rows: uint16(max(1, attach.rows)),
	})
	if err != nil {
		return nil, nil, err
	}
	return ptmx, cmd, nil
}

func (m *Manager) destroyRemoteShell(ctx context.Context, record database.ShellRecord) error {
	if strings.TrimSpace(record.MachineName) == "" || strings.TrimSpace(record.UserEmail) == "" || strings.TrimSpace(record.TmuxSession) == "" {
		return nil
	}
	machine, err := m.machines.GetMachine(ctx, record.MachineName, record.UserEmail)
	if err != nil || machine.Runtime == nil {
		return nil
	}
	targetHost := strings.TrimSpace(machine.Runtime.SSHHost)
	targetPort := machine.Runtime.SSHPort
	if targetHost == "" || targetPort <= 0 {
		return nil
	}
	guestUser := guestUserForMachine(machine)
	command := fmt.Sprintf(
		"if command -v tmux >/dev/null 2>&1; then tmux kill-session -t %s 2>/dev/null || true; fi",
		shellLiteral(strings.TrimSpace(record.TmuxSession)),
	)
	args := append(m.guestSSHArgs(guestUser, targetHost, targetPort), guestSSHRemoteCommand("xterm-256color", command))
	cmd := exec.CommandContext(ctx, m.sshClientBinary, args...)
	_, err = cmd.CombinedOutput()
	return err
}

func (m *Manager) ensureRemoteShell(ctx context.Context, machine controlplane.Machine, tmuxSession string) error {
	_, err := m.runGuestCommand(ctx, machine, persistentGuestShellSetupCommand(tmuxSession, guestShellCommand()))
	return err
}

func (m *Manager) recordShellEventBestEffort(record database.ShellRecord, kind string, payload map[string]any) {
	if m == nil || m.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = m.store.CreateEvent(ctx, database.CreateEventParams{
		ID:          uuid.NewString(),
		ActorUserID: &record.UserID,
		MachineID:   &record.MachineID,
		Kind:        kind,
		PayloadJSON: string(encoded),
	})
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

func (m *Manager) runGuestCommand(ctx context.Context, machine controlplane.Machine, shellCommand string) (string, error) {
	return m.runGuestCommandAllowExitCodes(ctx, machine, shellCommand)
}

func (m *Manager) runGuestCommandAllowExitCodes(ctx context.Context, machine controlplane.Machine, shellCommand string, allowedExitCodes ...int) (string, error) {
	if machine.Runtime == nil {
		return "", fmt.Errorf("machine %q is not available", machine.Name)
	}
	targetHost := strings.TrimSpace(machine.Runtime.SSHHost)
	targetPort := machine.Runtime.SSHPort
	if targetHost == "" || targetPort <= 0 {
		return "", fmt.Errorf("machine %q does not have a reachable guest shell endpoint", machine.Name)
	}
	guestUser := guestUserForMachine(machine)
	if err := m.waitForGuestAccess(ctx, machine, targetHost, targetPort, guestUser); err != nil {
		return "", err
	}

	args := append(m.guestSSHArgs(guestUser, targetHost, targetPort), guestSSHRemoteCommand("xterm-256color", shellCommand))
	cmd := exec.CommandContext(ctx, m.sshClientBinary, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		for _, allowed := range allowedExitCodes {
			if exitErr.ExitCode() == allowed {
				return string(output), nil
			}
		}
	}

	text := strings.TrimSpace(string(output))
	if text == "" {
		return "", err
	}
	return "", fmt.Errorf("%w: %s", err, text)
}

func (m *Manager) acquireGitCommandSlot(ctx context.Context, machineName string) (func(), error) {
	gate := m.gitCommandGate(machineName)
	select {
	case gate <- struct{}{}:
		return func() {
			<-gate
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *Manager) gitCommandGate(machineName string) chan struct{} {
	name := strings.TrimSpace(machineName)
	if name == "" {
		name = "unknown"
	}
	m.gitCommandMu.Lock()
	defer m.gitCommandMu.Unlock()
	if gate, ok := m.gitCommandGates[name]; ok {
		return gate
	}
	gate := make(chan struct{}, gitCommandConcurrency)
	m.gitCommandGates[name] = gate
	return gate
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

func guestUserForMachine(machine controlplane.Machine) string {
	if machine.Runtime == nil {
		return "ubuntu"
	}
	guestUser := strings.TrimSpace(machine.Runtime.GuestUser)
	if guestUser == "" {
		return "ubuntu"
	}
	return guestUser
}

func normalizeGuestCwd(cwd, guestUser string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	guestUser = strings.TrimSpace(guestUser)
	if guestUser == "" {
		guestUser = "ubuntu"
	}
	homeDir := "/home/" + guestUser
	switch {
	case cwd == "~":
		return homeDir
	case strings.HasPrefix(cwd, "~/"):
		return homeDir + strings.TrimPrefix(cwd, "~")
	default:
		return cwd
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
	setup := persistentGuestShellSetupCommand(tmuxSession, shellCommand)
	return strings.Join([]string{
		setup,
		`cwd=$(tmux display-message -p -t "$session" '#{pane_current_path}' 2>/dev/null || true)`,
		`if [ -n "$cwd" ]; then`,
		`  printf '\033]1337;FascinateCwd=%s\a' "$cwd"`,
		"fi",
		`exec tmux attach-session -t "$session"`,
	}, "\n")
}

func persistentGuestShellSetupCommand(tmuxSession, shellCommand string) string {
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
		`tmux set-option -t "$session" mouse off >/dev/null 2>&1 || true`,
		`tmux bind-key -n PageUp if-shell -F '#{pane_in_mode}' 'send-keys -X scroll-up' 'run-shell "tmux copy-mode -e -t #{pane_id}; tmux send-keys -X -t #{pane_id} scroll-up"' >/dev/null 2>&1 || true`,
		`tmux bind-key -n PageDown if-shell -F '#{pane_in_mode}' 'send-keys -X scroll-down' >/dev/null 2>&1 || true`,
	}, "\n")
}

func shellLiteral(value string) string {
	return "'" + shellQuoteSingle(value) + "'"
}

func shellQuoteSingle(value string) string {
	return strings.ReplaceAll(value, `'`, `'\''`)
}

func parseGitRepoStatusOutput(output string) (GitRepoStatus, error) {
	separator := strings.IndexByte(output, 0)
	if separator == -1 {
		return GitRepoStatus{}, fmt.Errorf("git status response missing header separator")
	}

	header := strings.Split(output[:separator], "\n")
	if len(header) == 0 || strings.TrimSpace(header[0]) == "" {
		return GitRepoStatus{}, fmt.Errorf("git status response missing repo root")
	}
	status := GitRepoStatus{
		State:    gitRepoStatusReady,
		RepoRoot: strings.TrimSpace(header[0]),
	}
	if len(header) > 1 {
		status.Branch = strings.TrimSpace(header[1])
	}
	if len(header) > 2 {
		additions, err := parseGitStatusCount(header[2])
		if err != nil {
			return GitRepoStatus{}, err
		}
		status.Additions = additions
	}
	if len(header) > 3 {
		deletions, err := parseGitStatusCount(header[3])
		if err != nil {
			return GitRepoStatus{}, err
		}
		status.Deletions = deletions
	}

	files, err := parseGitChangedFiles(output[separator+1:])
	if err != nil {
		return GitRepoStatus{}, err
	}
	status.Files = files
	return status, nil
}

func parseGitChangedFiles(output string) ([]GitChangedFile, error) {
	if output == "" {
		return nil, nil
	}

	records := strings.Split(output, "\x00")
	files := make([]GitChangedFile, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		switch {
		case strings.HasPrefix(record, "? "):
			path := strings.TrimSpace(record[2:])
			if path == "" {
				return nil, fmt.Errorf("git status response contained empty untracked path")
			}
			files = append(files, GitChangedFile{
				Path:           path,
				Kind:           "untracked",
				IndexStatus:    "?",
				WorktreeStatus: "?",
			})
		case strings.HasPrefix(record, "1 "), strings.HasPrefix(record, "u "):
			fields := strings.SplitN(record, " ", 9)
			if len(fields) < 9 {
				return nil, fmt.Errorf("invalid git status entry %q", record)
			}
			indexStatus, worktreeStatus := parseGitStatusPair(fields[1])
			files = append(files, GitChangedFile{
				Path:           fields[8],
				Kind:           gitChangeKind(indexStatus, worktreeStatus),
				IndexStatus:    indexStatus,
				WorktreeStatus: worktreeStatus,
			})
		case strings.HasPrefix(record, "2 "):
			fields := strings.SplitN(record, " ", 10)
			if len(fields) < 10 {
				return nil, fmt.Errorf("invalid git rename entry %q", record)
			}
			if index+1 >= len(records) {
				return nil, fmt.Errorf("git rename entry %q missing previous path", record)
			}
			indexStatus, worktreeStatus := parseGitStatusPair(fields[1])
			index++
			files = append(files, GitChangedFile{
				Path:           fields[9],
				PreviousPath:   records[index],
				Kind:           gitChangeKind(indexStatus, worktreeStatus),
				IndexStatus:    indexStatus,
				WorktreeStatus: worktreeStatus,
			})
		}
	}
	return files, nil
}

func parseGitStatusCount(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid git status count %q", value)
	}
	return count, nil
}

func parseGitStatusPair(pair string) (string, string) {
	value := []rune(strings.TrimSpace(pair))
	if len(value) < 2 {
		return "", ""
	}
	return gitStatusRune(value[0]), gitStatusRune(value[1])
}

func gitStatusRune(value rune) string {
	if value == '.' || value == ' ' {
		return ""
	}
	return string(value)
}

func gitChangeKind(indexStatus, worktreeStatus string) string {
	for _, status := range []string{indexStatus, worktreeStatus} {
		switch status {
		case "R":
			return "renamed"
		case "C":
			return "copied"
		case "A":
			return "added"
		case "D":
			return "deleted"
		case "T":
			return "typechange"
		case "U":
			return "conflicted"
		case "?":
			return "untracked"
		case "M":
			return "modified"
		}
	}
	return "modified"
}

func gitDiffBatchShellCommand(req GitDiffBatchRequest) string {
	payload, _ := json.Marshal(struct {
		RepoRoot string             `json:"repo_root"`
		MaxBytes int                `json:"max_bytes"`
		MaxLines int                `json:"max_lines"`
		Files    []GitDiffBatchFile `json:"files"`
	}{
		RepoRoot: strings.TrimSpace(req.RepoRoot),
		MaxBytes: gitDiffMaxBytes,
		MaxLines: gitDiffMaxLines,
		Files:    req.Files,
	})
	encodedPayload := base64.StdEncoding.EncodeToString(payload)

	return strings.Join([]string{
		fmt.Sprintf("export FASCINATE_GIT_DIFF_BATCH=%s", shellLiteral(encodedPayload)),
		`python3 - <<'PY'`,
		`import base64`,
		`import json`,
		`import os`,
		`import subprocess`,
		`import sys`,
		``,
		`payload = json.loads(base64.b64decode(os.environ["FASCINATE_GIT_DIFF_BATCH"]).decode("utf-8"))`,
		`repo_root = payload.get("repo_root", "").strip()`,
		`max_bytes = int(payload.get("max_bytes", 0) or 0)`,
		`max_lines = int(payload.get("max_lines", 0) or 0)`,
		`files = payload.get("files", [])`,
		``,
		`def command_output(args):`,
		`    completed = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=False)`,
		`    return completed.returncode, completed.stdout, completed.stderr`,
		``,
		`def decode_bytes(data):`,
		`    return data.decode("utf-8", errors="replace").strip()`,
		``,
		`def patch_stats(text):`,
		`    additions = 0`,
		`    deletions = 0`,
		`    for line in text.splitlines():`,
		`        if line.startswith("+++") or line.startswith("---") or line.startswith("@@"):`,
		`            continue`,
		`        if line.startswith("+"):`,
		`            additions += 1`,
		`        elif line.startswith("-"):`,
		`            deletions += 1`,
		`    return additions, deletions`,
		``,
		`def is_binary_diff(text):`,
		`    return "GIT binary patch" in text or "Binary files " in text`,
		``,
		`def finalize(result, patch_text):`,
		`    additions, deletions = patch_stats(patch_text)`,
		`    result["additions"] = additions`,
		`    result["deletions"] = deletions`,
		`    if is_binary_diff(patch_text):`,
		`        result["state"] = "binary"`,
		`        result["message"] = "Binary files are not rendered in the browser diff sidebar."`,
		`        return result`,
		`    if (max_bytes and len(patch_text.encode("utf-8")) > max_bytes) or (max_lines and patch_text.count("\n") > max_lines):`,
		`        result["state"] = "too_large"`,
		`        result["message"] = "Diff is too large to render inline. Use git in the shell for the full patch."`,
		`        return result`,
		`    result["state"] = "ready"`,
		`    result["patch"] = patch_text`,
		`    return result`,
		``,
		`head_code, _, _ = command_output(["git", "-C", repo_root, "rev-parse", "--verify", "HEAD"])`,
		`has_head = head_code == 0`,
		`diffs = []`,
		``,
		`for item in files:`,
		`    path = str(item.get("path", "") or "").strip()`,
		`    previous_path = str(item.get("previous_path", "") or "").strip()`,
		`    kind = str(item.get("kind", "") or "").strip()`,
		`    index_status = str(item.get("index_status", "") or "").strip()`,
		`    worktree_status = str(item.get("worktree_status", "") or "").strip()`,
		`    result = {"state": "error", "path": path}`,
		`    if previous_path:`,
		`        result["previous_path"] = previous_path`,
		`    if not path:`,
		`        result["message"] = "path is required"`,
		`        diffs.append(result)`,
		`        continue`,
		``,
		`    if kind == "untracked" or index_status == "?" or worktree_status == "?":`,
		`        abs_path = os.path.join(repo_root, path)`,
		`        if not os.path.exists(abs_path):`,
		`            result["message"] = f"path not found: {path}"`,
		`            diffs.append(result)`,
		`            continue`,
		`        code, stdout, stderr = command_output([`,
		`            "git", "-c", "color.ui=false", "--no-pager", "diff", "--no-index", "--no-ext-diff", "--unified=999999", "--", "/dev/null", abs_path,`,
		`        ])`,
		`        if code not in (0, 1):`,
		`            message = decode_bytes(stderr) or f"exit status {code}"`,
		`            result["message"] = message`,
		`            diffs.append(result)`,
		`            continue`,
		`        diffs.append(finalize(result, decode_bytes(stdout)))`,
		`        continue`,
		``,
		`    commands = []`,
		`    if has_head:`,
		`        commands.append([`,
		`            "git", "-C", repo_root, "-c", "color.ui=false", "--no-pager", "diff", "--no-ext-diff", "--find-renames", "--submodule=short", "--unified=999999", "HEAD", "--", path,`,
		`        ])`,
		`    else:`,
		`        commands.append([`,
		`            "git", "-C", repo_root, "-c", "color.ui=false", "--no-pager", "diff", "--no-ext-diff", "--find-renames", "--submodule=short", "--unified=999999", "--cached", "--", path,`,
		`        ])`,
		`        commands.append([`,
		`            "git", "-C", repo_root, "-c", "color.ui=false", "--no-pager", "diff", "--no-ext-diff", "--find-renames", "--submodule=short", "--unified=999999", "--", path,`,
		`        ])`,
		``,
		`    patch_parts = []`,
		`    failed = False`,
		`    for command in commands:`,
		`        code, stdout, stderr = command_output(command)`,
		`        if code not in (0, 1):`,
		`            message = decode_bytes(stderr) or f"exit status {code}"`,
		`            result["message"] = message`,
		`            diffs.append(result)`,
		`            failed = True`,
		`            break`,
		`        patch_parts.append(stdout)`,
		`    if failed:`,
		`        continue`,
		`    diffs.append(finalize(result, decode_bytes(b"".join(patch_parts))))`,
		``,
		`sys.stdout.write(json.dumps({"diffs": diffs}))`,
		`PY`,
	}, "\n")
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

func shellFromRecord(record database.ShellRecord) Shell {
	out := Shell{
		ID:             record.ID,
		Name:           record.Name,
		UserEmail:      record.UserEmail,
		MachineName:    record.MachineName,
		HostID:         recordHostID(record),
		State:          record.State,
		CWD:            record.CWD,
		LastAttachedAt: record.LastAttachedAt,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
	if record.LastError != nil {
		out.LastError = strings.TrimSpace(*record.LastError)
	}
	return out
}

func recordHostID(record database.ShellRecord) string {
	if record.HostID == nil {
		return ""
	}
	return strings.TrimSpace(*record.HostID)
}

func stringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
