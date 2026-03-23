package browserterm

import (
	"context"
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

func TestPersistentGuestShellCommandDisablesTmuxStatusBar(t *testing.T) {
	t.Parallel()

	command := persistentGuestShellCommand("fascinate-test", "exec bash -l")
	if !strings.Contains(command, `tmux set-option -t "$session" status off`) {
		t.Fatalf("expected tmux status bar to be disabled, command was %q", command)
	}
	if !strings.Contains(command, `tmux set-option -t "$session" mouse on`) {
		t.Fatalf("expected tmux mouse mode to be enabled, command was %q", command)
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

func terminalToken(t *testing.T, attachURL string) string {
	t.Helper()
	parsed, err := url.Parse(attachURL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Query().Get("token")
}
