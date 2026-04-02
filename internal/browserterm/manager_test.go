package browserterm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	machineruntime "fascinate/internal/runtime"
)

type fakeMachineManager struct {
	machine controlplane.Machine
	err     error
	owner   string
	name    string
}

func (f *fakeMachineManager) GetMachine(_ context.Context, name, ownerEmail string) (controlplane.Machine, error) {
	f.name = name
	f.owner = ownerEmail
	if f.err != nil {
		return controlplane.Machine{}, f.err
	}
	return f.machine, nil
}

func TestCreateSessionReturnsAttachDetails(t *testing.T) {
	t.Parallel()

	machines := &fakeMachineManager{machine: controlplane.Machine{
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager := New(config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines)

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

	manager := New(config.Config{HostID: "host-a"}, &fakeMachineManager{machine: controlplane.Machine{
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
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager := New(config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines)

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
		Name:   "m-1",
		HostID: "local-host",
		State:  "RUNNING",
		Runtime: &machineruntime.Machine{
			SSHHost: "127.0.0.1",
			SSHPort: 2222,
		},
	}}
	manager := New(config.Config{HostID: "local-host", TerminalSessionTTL: 2 * time.Minute}, machines)
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

func TestPersistentGuestShellCommandDisablesTmuxStatusBarAndMouseMode(t *testing.T) {
	t.Parallel()

	command := persistentGuestShellCommand("fascinate-test", "exec bash -l")
	if !strings.Contains(command, `tmux set-option -t "$session" status off`) {
		t.Fatalf("expected tmux status bar to be disabled, command was %q", command)
	}
	if !strings.Contains(command, `tmux set-option -t "$session" mouse off`) {
		t.Fatalf("expected tmux mouse mode to be disabled, command was %q", command)
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

	manager := New(config.Config{}, &fakeMachineManager{})
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

func terminalToken(t *testing.T, attachURL string) string {
	t.Helper()
	parsed, err := url.Parse(attachURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get("token")
}
