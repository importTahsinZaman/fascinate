package browserterm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

type fakeMachineManager struct {
	machine        controlplane.Machine
	machinesByName map[string]controlplane.Machine
	err            error
	owner          string
	name           string
}

func (f *fakeMachineManager) GetMachine(_ context.Context, name, ownerEmail string) (controlplane.Machine, error) {
	f.name = name
	f.owner = ownerEmail
	if f.err != nil {
		return controlplane.Machine{}, f.err
	}
	if f.machinesByName != nil {
		machine, ok := f.machinesByName[name]
		if !ok {
			return controlplane.Machine{}, errors.New("machine not found")
		}
		return machine, nil
	}
	return f.machine, nil
}

func TestCreateSessionReturnsAttachDetails(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{machine: controlplane.Machine{
		ID:     "machine-1",
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager := newTestManager(t, config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines)

	init, err := manager.CreateSession(context.Background(), "dev@example.com", "m-1", 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	if machines.owner != "dev@example.com" {
		t.Fatalf("unexpected owner %q", machines.owner)
	}
	if !strings.Contains(init.AttachURL, "/v1/terminal/sessions/") {
		t.Fatalf("unexpected attach url %q", init.AttachURL)
	}
	if diag := manager.Diagnostics(); diag.ActiveSessions != 1 || diag.TotalCreated != 1 {
		t.Fatalf("unexpected diagnostics %+v", diag)
	}
}

func TestCreateSessionRejectsRemoteHost(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, config.Config{HostID: "host-a"}, &fakeMachineManager{machine: controlplane.Machine{
		ID:     "machine-1",
		Name:   "m-1",
		HostID: "host-b",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}})

	_, err := manager.CreateSession(context.Background(), "dev@example.com", "m-1", 80, 24)
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected remote host error, got %v", err)
	}
}

func TestReattachSessionRotatesAttachToken(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{machine: controlplane.Machine{
		ID:     "machine-1",
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager := newTestManager(t, config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines)

	init, err := manager.CreateSession(context.Background(), "dev@example.com", "m-1", 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	firstToken := terminalToken(t, init.AttachURL)

	next, err := manager.ReattachSession(context.Background(), "dev@example.com", init.ID, 160, 60)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != init.ID {
		t.Fatalf("expected same session id, got %q", next.ID)
	}
	if token := terminalToken(t, next.AttachURL); token == firstToken {
		t.Fatalf("expected reattach to rotate attach token")
	}
}

func TestCloseSessionRemovesSession(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{machine: controlplane.Machine{
		ID:     "machine-1",
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager := newTestManager(t, config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines)
	manager.sshClientBinary = "true"

	init, err := manager.CreateSession(context.Background(), "dev@example.com", "m-1", 120, 40)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.CloseSession(context.Background(), "dev@example.com", init.ID); err != nil {
		t.Fatal(err)
	}
	if diag := manager.Diagnostics(); diag.ActiveSessions != 0 {
		t.Fatalf("expected session to be removed, got %+v", diag)
	}
}

func TestCloseSessionAllowsMissingSession(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, &fakeMachineManager{})

	if err := manager.CloseSession(context.Background(), "dev@example.com", "missing-session"); err != nil {
		t.Fatalf("expected missing session close to succeed, got %v", err)
	}
	if diag := manager.Diagnostics(); diag.ActiveSessions != 0 {
		t.Fatalf("expected no active sessions, got %+v", diag)
	}
}

func TestCloseMachineSessionsRemovesAllMatchingSessions(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{
		machinesByName: map[string]controlplane.Machine{
			"m-1": {
				ID:     "machine-1",
				Name:   "m-1",
				HostID: "local-host",
				State:  "RUNNING",
				Runtime: &machineruntime.Machine{
					SSHHost: "127.0.0.1",
					SSHPort: 2222,
				},
			},
			"m-2": {
				ID:     "machine-2",
				Name:   "m-2",
				HostID: "local-host",
				State:  "RUNNING",
				Runtime: &machineruntime.Machine{
					SSHHost: "127.0.0.1",
					SSHPort: 2223,
				},
			},
		},
	}
	manager := newTestManager(t, config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines)
	manager.sshClientBinary = "true"

	if _, err := manager.CreateSession(context.Background(), "dev@example.com", "m-1", 120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateSession(context.Background(), "dev@example.com", "m-1", 120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateSession(context.Background(), "ops@example.com", "m-1", 120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateSession(context.Background(), "dev@example.com", "m-2", 120, 40); err != nil {
		t.Fatal(err)
	}

	if err := manager.CloseMachineSessions(context.Background(), "dev@example.com", "m-1"); err != nil {
		t.Fatal(err)
	}

	diag := manager.Diagnostics()
	if diag.ActiveSessions != 2 {
		t.Fatalf("expected 2 active sessions after machine cleanup, got %+v", diag)
	}
	for _, sess := range diag.Sessions {
		if sess.UserEmail == "dev@example.com" && sess.MachineName == "m-1" {
			t.Fatalf("expected matching machine sessions to be removed, got %+v", diag)
		}
	}
}

func TestCreateAttachmentSurvivesManagerRestart(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{machine: controlplane.Machine{
		ID:     "machine-1",
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager, store := newTestManagerWithStore(t, config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines)

	shell, err := manager.CreateShell(context.Background(), "dev@example.com", "m-1", "primary")
	if err != nil {
		t.Fatal(err)
	}

	restarted := New(config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines, store)
	init, err := restarted.CreateAttachment(context.Background(), "dev@example.com", shell.ID, 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	if init.ID != shell.ID || !strings.Contains(init.AttachURL, shell.ID) {
		t.Fatalf("unexpected attachment after restart %+v", init)
	}
}

func TestPersistentGuestShellCommandDisablesTmuxStatusBarAndConfiguresHistoryScrollKeys(t *testing.T) {
	t.Parallel()

	command := persistentGuestShellCommand("fascinate-test", "exec bash -l")
	if !strings.Contains(command, `tmux set-option -t "$session" status off`) {
		t.Fatalf("expected tmux status bar to be disabled, command was %q", command)
	}
	if !strings.Contains(command, `tmux set-option -t "$session" mouse off`) {
		t.Fatalf("expected tmux mouse mode to be disabled, command was %q", command)
	}
	if !strings.Contains(command, `tmux bind-key -n PageUp if-shell -F '#{pane_in_mode}' 'send-keys -X scroll-up' 'run-shell "tmux copy-mode -e -t #{pane_id}; tmux send-keys -X -t #{pane_id} scroll-up"'`) {
		t.Fatalf("expected PageUp to scroll tmux history, command was %q", command)
	}
	if !strings.Contains(command, `tmux bind-key -n PageDown if-shell -F '#{pane_in_mode}' 'send-keys -X scroll-down'`) {
		t.Fatalf("expected PageDown to scroll tmux history back down, command was %q", command)
	}
	if !strings.Contains(command, `#{pane_current_path}`) {
		t.Fatalf("expected tmux current path lookup to be included, command was %q", command)
	}
	if !strings.Contains(command, `FascinateCwd=`) {
		t.Fatalf("expected cwd metadata sequence to be emitted, command was %q", command)
	}
}

func TestExpectedSessionEndErrorTreatsNormalWebsocketCloseAsClean(t *testing.T) {
	t.Parallel()

	if !isExpectedSessionEndError(websocket.CloseError{Code: websocket.StatusNormalClosure}) {
		t.Fatalf("expected normal websocket close to be treated as clean")
	}
	if !isExpectedSessionEndError(websocket.CloseError{Code: websocket.StatusGoingAway}) {
		t.Fatalf("expected going-away websocket close to be treated as clean")
	}
	if isExpectedSessionEndError(errors.New("boom")) {
		t.Fatalf("unexpectedly treated generic error as clean")
	}
}

func TestParseGitRepoStatusOutputParsesTrackedUntrackedAndRenamedFiles(t *testing.T) {
	t.Parallel()

	output := strings.Join([]string{
		"/home/ubuntu/project",
		"main",
		"2",
		"1",
	}, "\n") + "\x00" +
		"1 .M N... 100644 100644 100644 abcdef1 abcdef1 web/src/app.tsx\x00" +
		"2 R. N... 100644 100644 100644 abcdef1 abcdef2 R100 web/src/new.tsx\x00web/src/old.tsx\x00" +
		"? README.md\x00"

	status, err := parseGitRepoStatusOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != gitRepoStatusReady || status.RepoRoot != "/home/ubuntu/project" || status.Branch != "main" {
		t.Fatalf("unexpected status %+v", status)
	}
	if status.Additions != 2 || status.Deletions != 1 {
		t.Fatalf("unexpected status totals %+v", status)
	}
	if len(status.Files) != 3 {
		t.Fatalf("expected 3 files, got %+v", status.Files)
	}
	if status.Files[0].Path != "web/src/app.tsx" || status.Files[0].Kind != "modified" || status.Files[0].WorktreeStatus != "M" {
		t.Fatalf("unexpected modified file %+v", status.Files[0])
	}
	if status.Files[1].Path != "web/src/new.tsx" || status.Files[1].PreviousPath != "web/src/old.tsx" || status.Files[1].Kind != "renamed" {
		t.Fatalf("unexpected rename file %+v", status.Files[1])
	}
	if status.Files[2].Path != "README.md" || status.Files[2].Kind != "untracked" {
		t.Fatalf("unexpected untracked file %+v", status.Files[2])
	}
}

func TestGitDiffBatchShellCommandEmbedsBatchPayload(t *testing.T) {
	t.Parallel()

	command := gitDiffBatchShellCommand(GitDiffBatchRequest{
		RepoRoot: "/home/ubuntu/project",
		Files: []GitDiffBatchFile{
			{Path: "web/src/app.tsx", Kind: "modified", WorktreeStatus: "M"},
			{Path: "README.md", Kind: "untracked", WorktreeStatus: "?"},
		},
	})

	if !strings.Contains(command, "export FASCINATE_GIT_DIFF_BATCH=") {
		t.Fatalf("expected batch payload env var, got %q", command)
	}
	if !strings.Contains(command, `python3 - <<'PY'`) {
		t.Fatalf("expected embedded python batch script, got %q", command)
	}

	prefix := "export FASCINATE_GIT_DIFF_BATCH='"
	start := strings.Index(command, prefix)
	if start == -1 {
		t.Fatalf("expected encoded payload prefix in command, got %q", command)
	}
	start += len(prefix)
	end := strings.Index(command[start:], "'\npython3 - <<'PY'")
	if end == -1 {
		t.Fatalf("expected encoded payload terminator in command, got %q", command)
	}
	encodedPayload := command[start : start+end]
	payloadJSON, err := base64.StdEncoding.DecodeString(encodedPayload)
	if err != nil {
		t.Fatalf("expected valid base64 payload, got %v", err)
	}
	var payload struct {
		RepoRoot string             `json:"repo_root"`
		Files    []GitDiffBatchFile `json:"files"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("expected valid json payload, got %v", err)
	}
	if payload.RepoRoot != "/home/ubuntu/project" {
		t.Fatalf("unexpected repo root %+v", payload)
	}
	if len(payload.Files) != 2 || payload.Files[0].Path != "web/src/app.tsx" || payload.Files[1].Path != "README.md" {
		t.Fatalf("unexpected payload files %+v", payload.Files)
	}
}

func TestAcquireGitCommandSlotHonorsPerMachineConcurrency(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, config.Config{}, &fakeMachineManager{})
	releaseOne, err := manager.acquireGitCommandSlot(context.Background(), "m-1")
	if err != nil {
		t.Fatal(err)
	}
	releaseTwo, err := manager.acquireGitCommandSlot(context.Background(), "m-1")
	if err != nil {
		t.Fatal(err)
	}
	blockedCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err = manager.acquireGitCommandSlot(blockedCtx, "m-1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected third slot acquisition to block, got %v", err)
	}

	releaseOne()
	releaseTwo()
}

func TestNormalizeGuestCwd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cwd       string
		guestUser string
		want      string
	}{
		{
			name:      "absolute path is unchanged",
			cwd:       "/home/ubuntu/react-recall",
			guestUser: "ubuntu",
			want:      "/home/ubuntu/react-recall",
		},
		{
			name:      "tilde expands to guest home",
			cwd:       "~",
			guestUser: "ubuntu",
			want:      "/home/ubuntu",
		},
		{
			name:      "tilde child path expands to guest home",
			cwd:       "~/react-recall",
			guestUser: "ubuntu",
			want:      "/home/ubuntu/react-recall",
		},
		{
			name:      "blank guest user falls back to ubuntu",
			cwd:       "~/project",
			guestUser: "",
			want:      "/home/ubuntu/project",
		},
		{
			name:      "whitespace is trimmed",
			cwd:       "  ~/project/web  ",
			guestUser: "devuser",
			want:      "/home/devuser/project/web",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := normalizeGuestCwd(tt.cwd, tt.guestUser)
			if got != tt.want {
				t.Fatalf("normalizeGuestCwd(%q, %q) = %q, want %q", tt.cwd, tt.guestUser, got, tt.want)
			}
		})
	}
}

func TestCreateExecCapturesOutputAndExitCode(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{machine: controlplane.Machine{
		ID:     "machine-1",
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager, store := newTestManagerWithStore(t, config.Config{HostID: "local-host"}, machines)
	manager.sshClientBinary = fakeSSHBinary(t)

	execResult, err := manager.CreateExec(context.Background(), "dev@example.com", "m-1", ExecRequest{
		CommandText: `printf 'hello\n'; printf 'warn\n' >&2; exit 7`,
	})
	if err != nil {
		t.Fatal(err)
	}

	record := waitForExecRecord(t, store, execResult.ID)
	if record.State != execStateFailed {
		t.Fatalf("expected failed exec state, got %+v", record)
	}
	if record.ExitCode == nil || *record.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %+v", record.ExitCode)
	}
	if !strings.Contains(record.StdoutText, "hello") {
		t.Fatalf("expected stdout to be captured, got %q", record.StdoutText)
	}
	if !strings.Contains(record.StderrText, "warn") {
		t.Fatalf("expected stderr to be captured, got %q", record.StderrText)
	}
}

func TestCreateExecHonorsTimeout(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{machine: controlplane.Machine{
		ID:     "machine-1",
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager, store := newTestManagerWithStore(t, config.Config{HostID: "local-host"}, machines)
	manager.sshClientBinary = fakeSSHBinary(t)

	execResult, err := manager.CreateExec(context.Background(), "dev@example.com", "m-1", ExecRequest{
		CommandText: `sleep 5`,
		Timeout:     50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	record := waitForExecRecord(t, store, execResult.ID)
	if record.State != execStateTimedOut {
		t.Fatalf("expected timed out exec state, got %+v", record)
	}
	if record.FailureClass == nil || *record.FailureClass != execFailureTimeout {
		t.Fatalf("expected timeout failure class, got %+v", record.FailureClass)
	}
}

func TestCancelExecMarksCommandCancelled(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{machine: controlplane.Machine{
		ID:     "machine-1",
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager, store := newTestManagerWithStore(t, config.Config{HostID: "local-host"}, machines)
	manager.sshClientBinary = fakeSSHBinary(t)

	execResult, err := manager.CreateExec(context.Background(), "dev@example.com", "m-1", ExecRequest{
		CommandText: `sleep 5`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.CancelExec(context.Background(), "dev@example.com", execResult.ID); err != nil {
		t.Fatal(err)
	}

	record := waitForExecRecord(t, store, execResult.ID)
	if record.State != execStateCancelled {
		t.Fatalf("expected cancelled exec state, got %+v", record)
	}
	if record.FailureClass == nil || *record.FailureClass != execFailureCancelled {
		t.Fatalf("expected cancelled failure class, got %+v", record.FailureClass)
	}
	if record.CancelRequestedAt == nil {
		t.Fatalf("expected cancel request timestamp, got %+v", record)
	}
}

func newTestManager(t *testing.T, cfg config.Config, machines *fakeMachineManager) *Manager {
	t.Helper()
	manager, _ := newTestManagerWithStore(t, cfg, machines)
	return manager
}

func fakeSSHBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-ssh.sh")
	body := `#!/usr/bin/env bash
set -euo pipefail
command="${@: -1}"
eval "${command}"
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForExecRecord(t *testing.T, store *database.Store, execID string) database.ExecRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err := store.GetExecByID(context.Background(), execID)
		if err == nil && execStateIsTerminal(record.State) {
			return record
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for exec %s to finish", execID)
	return database.ExecRecord{}
}

func execStateIsTerminal(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case execStateSucceeded, execStateFailed, execStateTimedOut, execStateCancelled, execStateError:
		return true
	default:
		return false
	}
}

func newTestManagerWithStore(t *testing.T, cfg config.Config, machines *fakeMachineManager) (*Manager, *database.Store) {
	t.Helper()

	store, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	devUser, err := store.UpsertUser(context.Background(), "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	opsUser, err := store.UpsertUser(context.Background(), "ops@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	seedHost := func(hostID string) {
		hostID = strings.TrimSpace(hostID)
		if hostID == "" {
			return
		}
		if _, err := store.UpsertHost(context.Background(), database.UpsertHostParams{
			ID:               hostID,
			Name:             hostID,
			Region:           "local",
			Role:             "combined",
			Status:           "ACTIVE",
			LabelsJSON:       "{}",
			CapabilitiesJSON: `["shell"]`,
			RuntimeVersion:   "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	seedHost(cfg.HostID)
	seedMachine := func(machine controlplane.Machine, ownerID string) {
		if strings.TrimSpace(machine.ID) == "" || strings.TrimSpace(machine.Name) == "" {
			return
		}
		seedHost(machine.HostID)
		if _, err := store.CreateMachine(context.Background(), database.CreateMachineParams{
			ID:             machine.ID,
			Name:           machine.Name,
			OwnerUserID:    ownerID,
			HostID:         stringPtr(machine.HostID),
			RuntimeName:    machine.Name,
			State:          "RUNNING",
			CPU:            "1",
			MemoryBytes:    1 << 30,
			DiskBytes:      10 << 30,
			DiskUsageBytes: 1 << 30,
			PrimaryPort:    8080,
		}); err != nil && !errors.Is(err, database.ErrConflict) {
			t.Fatal(err)
		}
	}
	seedMachine(machines.machine, devUser.ID)
	for _, machine := range machines.machinesByName {
		ownerID := devUser.ID
		if strings.EqualFold(machine.Name, "m-2") {
			ownerID = opsUser.ID
		}
		seedMachine(machine, ownerID)
	}
	return New(cfg, machines, store), store
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func terminalToken(t *testing.T, attachURL string) string {
	t.Helper()
	parsed, err := url.Parse(attachURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get("token")
}
