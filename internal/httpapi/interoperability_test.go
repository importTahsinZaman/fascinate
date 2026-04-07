package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"fascinate/internal/browserauth"
	"fascinate/internal/browserterm"
	"fascinate/internal/cli"
	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
)

type interoperabilityMachineManager struct {
	store *database.Store
}

func (m *interoperabilityMachineManager) ListMachines(ctx context.Context, ownerEmail string) ([]controlplane.Machine, error) {
	records, err := m.store.ListMachines(ctx, ownerEmail)
	if err != nil {
		return nil, err
	}
	out := make([]controlplane.Machine, 0, len(records))
	for _, record := range records {
		out = append(out, machineFromRecord(record))
	}
	return out, nil
}

func (m *interoperabilityMachineManager) GetMachine(ctx context.Context, name, ownerEmail string) (controlplane.Machine, error) {
	record, err := m.store.GetMachineByName(ctx, name)
	if err != nil {
		return controlplane.Machine{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(record.OwnerEmail), strings.TrimSpace(ownerEmail)) {
		return controlplane.Machine{}, database.ErrNotFound
	}
	return machineFromRecord(record), nil
}

func (m *interoperabilityMachineManager) GetPublicMachine(ctx context.Context, name string) (controlplane.Machine, error) {
	record, err := m.store.GetMachineByName(ctx, name)
	if err != nil {
		return controlplane.Machine{}, err
	}
	return machineFromRecord(record), nil
}

func (m *interoperabilityMachineManager) GetMachineEnv(_ context.Context, name, _ string) (controlplane.MachineEnv, error) {
	return controlplane.MachineEnv{
		MachineName: name,
		Entries: []controlplane.EffectiveEnvVar{
			{Key: "FASCINATE_MACHINE_NAME", Value: name},
		},
	}, nil
}

func (m *interoperabilityMachineManager) CreateMachine(ctx context.Context, input controlplane.CreateMachineInput) (controlplane.Machine, error) {
	user, err := m.store.UpsertUser(ctx, input.OwnerEmail, false)
	if err != nil {
		return controlplane.Machine{}, err
	}
	hostID := "fascinate-01"
	record, err := m.store.CreateMachine(ctx, database.CreateMachineParams{
		ID:             uuid.NewString(),
		Name:           strings.TrimSpace(input.Name),
		OwnerUserID:    user.ID,
		HostID:         &hostID,
		RuntimeName:    strings.TrimSpace(input.Name),
		State:          "RUNNING",
		CPU:            "2",
		MemoryBytes:    2 << 30,
		DiskBytes:      20 << 30,
		DiskUsageBytes: 1 << 30,
		PrimaryPort:    3000,
	})
	if err != nil {
		return controlplane.Machine{}, err
	}
	if err := publishTestEvent(ctx, m.store, &user.ID, &record.ID, "machine.created", map[string]any{
		"machine_id":   record.ID,
		"machine_name": record.Name,
		"state":        record.State,
	}); err != nil {
		return controlplane.Machine{}, err
	}
	return machineFromRecord(record), nil
}

func (m *interoperabilityMachineManager) DeleteMachine(ctx context.Context, name, ownerEmail string) error {
	record, err := m.store.GetMachineByName(ctx, name)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(record.OwnerEmail), strings.TrimSpace(ownerEmail)) {
		return database.ErrNotFound
	}
	if err := m.store.MarkMachineDeleted(ctx, record.ID); err != nil {
		return err
	}
	return publishTestEvent(ctx, m.store, &record.OwnerUserID, &record.ID, "machine.deleted", map[string]any{
		"machine_id":   record.ID,
		"machine_name": record.Name,
	})
}

func (m *interoperabilityMachineManager) ForkMachine(ctx context.Context, input controlplane.ForkMachineInput) (controlplane.Machine, error) {
	return m.CreateMachine(ctx, controlplane.CreateMachineInput{
		Name:       input.TargetName,
		OwnerEmail: input.OwnerEmail,
	})
}

func (m *interoperabilityMachineManager) ListSnapshots(context.Context, string) ([]controlplane.Snapshot, error) {
	return []controlplane.Snapshot{}, nil
}

func (m *interoperabilityMachineManager) CreateSnapshot(context.Context, controlplane.CreateSnapshotInput) (controlplane.Snapshot, error) {
	return controlplane.Snapshot{}, fmt.Errorf("snapshots are not implemented in interoperability tests")
}

func (m *interoperabilityMachineManager) DeleteSnapshot(context.Context, string, string) error {
	return fmt.Errorf("snapshots are not implemented in interoperability tests")
}

func (m *interoperabilityMachineManager) ListEnvVars(context.Context, string) ([]controlplane.EnvVar, error) {
	return []controlplane.EnvVar{}, nil
}

func (m *interoperabilityMachineManager) SetEnvVar(_ context.Context, input controlplane.SetEnvVarInput) (controlplane.EnvVar, error) {
	return controlplane.EnvVar{Key: input.Key, RawValue: input.Value}, nil
}

func (m *interoperabilityMachineManager) DeleteEnvVar(context.Context, string, string) error {
	return nil
}

type interoperabilityTerminalManager struct {
	store *database.Store
}

func (m *interoperabilityTerminalManager) CreateSession(context.Context, string, string, int, int) (browserterm.SessionInit, error) {
	return browserterm.SessionInit{}, fmt.Errorf("terminal sessions are not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) ReattachSession(context.Context, string, string, int, int) (browserterm.SessionInit, error) {
	return browserterm.SessionInit{}, fmt.Errorf("terminal sessions are not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) CloseSession(context.Context, string, string) error {
	return nil
}

func (m *interoperabilityTerminalManager) CloseMachineSessions(context.Context, string, string) error {
	return nil
}

func (m *interoperabilityTerminalManager) ListShells(ctx context.Context, userEmail string) ([]browserterm.Shell, error) {
	records, err := m.store.ListShells(ctx, userEmail)
	if err != nil {
		return nil, err
	}
	out := make([]browserterm.Shell, 0, len(records))
	for _, record := range records {
		out = append(out, shellFromRecord(record))
	}
	return out, nil
}

func (m *interoperabilityTerminalManager) GetShell(ctx context.Context, userEmail, shellID string) (browserterm.Shell, error) {
	record, err := m.store.GetShellByID(ctx, shellID)
	if err != nil {
		return browserterm.Shell{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(record.UserEmail), strings.TrimSpace(userEmail)) {
		return browserterm.Shell{}, database.ErrNotFound
	}
	return shellFromRecord(record), nil
}

func (m *interoperabilityTerminalManager) CreateShell(ctx context.Context, userEmail, machineName, name string) (browserterm.Shell, error) {
	user, err := m.store.GetUserByEmail(ctx, userEmail)
	if err != nil {
		return browserterm.Shell{}, err
	}
	machine, err := m.store.GetMachineByName(ctx, machineName)
	if err != nil {
		return browserterm.Shell{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(machine.OwnerEmail), strings.TrimSpace(userEmail)) {
		return browserterm.Shell{}, database.ErrNotFound
	}
	record, err := m.store.CreateShell(ctx, database.CreateShellParams{
		ID:          uuid.NewString(),
		UserID:      user.ID,
		MachineID:   machine.ID,
		HostID:      machine.HostID,
		Name:        strings.TrimSpace(name),
		TmuxSession: "interop-" + uuid.NewString(),
		State:       "READY",
		CWD:         "/home/ubuntu",
	})
	if err != nil {
		return browserterm.Shell{}, err
	}
	if err := publishTestEvent(ctx, m.store, &user.ID, &machine.ID, "shell.created", map[string]any{
		"shell_id":     record.ID,
		"machine_id":   machine.ID,
		"machine_name": record.MachineName,
		"state":        record.State,
	}); err != nil {
		return browserterm.Shell{}, err
	}
	return shellFromRecord(record), nil
}

func (m *interoperabilityTerminalManager) DeleteShell(ctx context.Context, userEmail, shellID string) error {
	record, err := m.store.GetShellByID(ctx, shellID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(record.UserEmail), strings.TrimSpace(userEmail)) {
		return database.ErrNotFound
	}
	if err := m.store.MarkShellDeleted(ctx, shellID); err != nil {
		return err
	}
	return publishTestEvent(ctx, m.store, &record.UserID, &record.MachineID, "shell.deleted", map[string]any{
		"shell_id":     record.ID,
		"machine_id":   record.MachineID,
		"machine_name": record.MachineName,
	})
}

func (m *interoperabilityTerminalManager) CreateAttachment(context.Context, string, string, int, int) (browserterm.SessionInit, error) {
	return browserterm.SessionInit{}, fmt.Errorf("attachments are not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) SendInput(context.Context, string, string, string) error {
	return nil
}

func (m *interoperabilityTerminalManager) ReadLines(context.Context, string, string, int) ([]string, error) {
	return []string{}, nil
}

func (m *interoperabilityTerminalManager) CreateExec(context.Context, string, string, browserterm.ExecRequest) (browserterm.Exec, error) {
	return browserterm.Exec{}, fmt.Errorf("exec is not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) GetExec(context.Context, string, string) (browserterm.Exec, error) {
	return browserterm.Exec{}, fmt.Errorf("exec is not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) ListExecs(context.Context, string, int) ([]browserterm.Exec, error) {
	return []browserterm.Exec{}, nil
}

func (m *interoperabilityTerminalManager) CancelExec(context.Context, string, string) error {
	return fmt.Errorf("exec is not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) StreamExec(w http.ResponseWriter, _ *http.Request, _ string, _ string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	_, err := io.WriteString(w, "event: result\ndata: {\"type\":\"result\"}\n\n")
	return err
}

func (m *interoperabilityTerminalManager) ExecDiagnostics(context.Context, string, int) (browserterm.ExecDiagnostics, error) {
	return browserterm.ExecDiagnostics{}, nil
}

func (m *interoperabilityTerminalManager) UploadArchive(context.Context, string, string, string, io.Reader) (browserterm.FileTransfer, error) {
	return browserterm.FileTransfer{}, fmt.Errorf("file transfer is not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) DownloadArchive(context.Context, string, string, string, io.Writer) (browserterm.FileTransfer, error) {
	return browserterm.FileTransfer{}, fmt.Errorf("file transfer is not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) GetGitStatus(context.Context, string, string, string) (browserterm.GitRepoStatus, error) {
	return browserterm.GitRepoStatus{}, fmt.Errorf("git status is not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) GetGitDiffBatch(context.Context, string, string, browserterm.GitDiffBatchRequest) (browserterm.GitDiffBatchResponse, error) {
	return browserterm.GitDiffBatchResponse{}, fmt.Errorf("git diff is not implemented in interoperability tests")
}

func (m *interoperabilityTerminalManager) StreamSession(http.ResponseWriter, *http.Request, string) error {
	return nil
}

func (m *interoperabilityTerminalManager) Diagnostics() browserterm.Diagnostics {
	return browserterm.Diagnostics{}
}

func TestCLIAndWebInteroperateAcrossRestartAndReconnect(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fascinate.db")

	store, err := database.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertHost(ctx, database.UpsertHostParams{
		ID:             "fascinate-01",
		Name:           "fascinate-01",
		Region:         "local",
		Role:           "combined",
		RuntimeVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}
	auth := &fakeBrowserAuth{
		token: "session-token",
		session: browserauth.Session{
			User:   user,
			Record: database.WebSessionRecord{ID: "session-1", UserID: user.ID, UserEmail: user.Email},
		},
		apiToken: "api-token",
		apiTokenSession: browserauth.APITokenSession{
			User: user,
			Record: database.APITokenRecord{
				ID:        "token-1",
				UserID:    user.ID,
				UserEmail: user.Email,
				Name:      "fascinate-cli",
			},
		},
	}

	newServer := func(store *database.Store) *httptest.Server {
		handler := New(
			cfg,
			store,
			&fakeRuntime{},
			&interoperabilityMachineManager{store: store},
			auth,
			&interoperabilityTerminalManager{store: store},
			nil,
		)
		return httptest.NewServer(handler)
	}

	server := newServer(store)
	defer server.Close()
	t.Setenv("FASCINATE_CLI_CONFIG", filepath.Join(t.TempDir(), "cli-config.json"))
	t.Setenv("FASCINATE_BASE_URL", server.URL)
	t.Setenv("FASCINATE_TOKEN", "api-token")

	events, closeEvents := startOwnerEventStream(t, server.URL, cfg.WebSessionCookieName, "session-token", "")
	defer closeEvents()
	waitForActiveSubscribers(t, store, 1)

	machineStdout := runCLICommand(t, []string{"machine", "create", "--json", "m-1"})
	var createdMachine controlplane.Machine
	if err := json.Unmarshal([]byte(machineStdout), &createdMachine); err != nil {
		t.Fatalf("decode machine create output: %v", err)
	}
	if createdMachine.Name != "m-1" || createdMachine.State != "RUNNING" {
		t.Fatalf("unexpected machine create output %+v", createdMachine)
	}

	machineEvent := waitForOwnerEvent(t, events, "machine.created")
	if got := machineEvent.Payload["machine_name"]; got != "m-1" {
		t.Fatalf("expected machine event for m-1, got %+v", machineEvent.Payload)
	}
	webMachines := listWebMachines(t, server.URL, cfg.WebSessionCookieName, "session-token")
	if len(webMachines) != 1 || webMachines[0].Name != "m-1" {
		t.Fatalf("expected web machine list to include m-1, got %+v", webMachines)
	}

	shellStdout := runCLICommand(t, []string{"shell", "create", "--json", "m-1"})
	var createdShell browserterm.Shell
	if err := json.Unmarshal([]byte(shellStdout), &createdShell); err != nil {
		t.Fatalf("decode shell create output: %v", err)
	}
	if createdShell.MachineName != "m-1" || strings.TrimSpace(createdShell.ID) == "" {
		t.Fatalf("unexpected shell create output %+v", createdShell)
	}

	shellEvent := waitForOwnerEvent(t, events, "shell.created")
	if got := shellEvent.Payload["shell_id"]; got != createdShell.ID {
		t.Fatalf("expected shell event for %s, got %+v", createdShell.ID, shellEvent.Payload)
	}
	webShells := listWebShells(t, server.URL, cfg.WebSessionCookieName, "session-token")
	if len(webShells) != 1 || webShells[0].ID != createdShell.ID {
		t.Fatalf("expected web shell list to include created shell, got %+v", webShells)
	}

	closeEvents()
	server.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = database.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = store.Close()
	}()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	server = newServer(store)
	defer server.Close()
	t.Setenv("FASCINATE_BASE_URL", server.URL)

	reconnectedEvents, closeReconnectedEvents := startOwnerEventStream(t, server.URL, cfg.WebSessionCookieName, "session-token", shellEvent.ID)
	defer closeReconnectedEvents()
	waitForActiveSubscribers(t, store, 1)

	webMachines = listWebMachines(t, server.URL, cfg.WebSessionCookieName, "session-token")
	if len(webMachines) != 1 || webMachines[0].Name != "m-1" {
		t.Fatalf("expected reconnected web machine list to preserve m-1, got %+v", webMachines)
	}
	webShells = listWebShells(t, server.URL, cfg.WebSessionCookieName, "session-token")
	if len(webShells) != 1 || webShells[0].ID != createdShell.ID {
		t.Fatalf("expected reconnected web shell list to preserve %s, got %+v", createdShell.ID, webShells)
	}

	secondShellStdout := runCLICommand(t, []string{"shell", "create", "--json", "--name", "after-restart", "m-1"})
	var secondShell browserterm.Shell
	if err := json.Unmarshal([]byte(secondShellStdout), &secondShell); err != nil {
		t.Fatalf("decode second shell create output: %v", err)
	}
	if secondShell.Name != "after-restart" {
		t.Fatalf("unexpected second shell create output %+v", secondShell)
	}

	reconnectedShellEvent := waitForOwnerEvent(t, reconnectedEvents, "shell.created")
	if got := reconnectedShellEvent.Payload["shell_id"]; got != secondShell.ID {
		t.Fatalf("expected post-restart shell event for %s, got %+v", secondShell.ID, reconnectedShellEvent.Payload)
	}
	webShells = listWebShells(t, server.URL, cfg.WebSessionCookieName, "session-token")
	if len(webShells) != 2 {
		t.Fatalf("expected both shells after restart, got %+v", webShells)
	}
}

func runCLICommand(t *testing.T, args []string) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	runner := cli.Runner{
		Stdin:  bytes.NewBuffer(nil),
		Stdout: stdout,
		Stderr: stderr,
	}
	if err := runner.Run(context.Background(), args); err != nil {
		t.Fatalf("cli %q failed: %v\nstderr:\n%s\nstdout:\n%s", strings.Join(args, " "), err, stderr.String(), stdout.String())
	}
	return stdout.String()
}

func startOwnerEventStream(t *testing.T, baseURL, cookieName, cookieValue, lastEventID string) (<-chan ownerEvent, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
	if strings.TrimSpace(lastEventID) != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	events := make(chan ownerEvent, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(events)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			t.Errorf("open event stream: %v", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("event stream returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 4096), 1<<20)
		var data string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				if strings.TrimSpace(data) != "" {
					var event ownerEvent
					if err := json.Unmarshal([]byte(data), &event); err != nil {
						t.Errorf("decode event stream payload: %v", err)
						return
					}
					events <- event
					data = ""
				}
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				data += strings.TrimPrefix(line, "data: ")
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			t.Errorf("read event stream: %v", err)
		}
	}()

	return events, func() {
		cancel()
		<-done
	}
}

func waitForActiveSubscribers(t *testing.T, store *database.Store, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		diag, err := store.EventStreamDiagnostics(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if diag.ActiveSubscribers == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	diag, err := store.EventStreamDiagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("timed out waiting for %d active subscribers, got %d", want, diag.ActiveSubscribers)
}

func waitForOwnerEvent(t *testing.T, events <-chan ownerEvent, kind string) ownerEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("event stream closed before %s arrived", kind)
			}
			if event.Kind == kind {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", kind)
		}
	}
}

func listWebMachines(t *testing.T, baseURL, cookieName, cookieValue string) []controlplane.Machine {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/machines", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list web machines returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var body struct {
		Machines []controlplane.Machine `json:"machines"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Machines
}

func listWebShells(t *testing.T, baseURL, cookieName, cookieValue string) []browserterm.Shell {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/shells", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("list web shells returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var body struct {
		Shells []browserterm.Shell `json:"shells"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Shells
}

func publishTestEvent(ctx context.Context, store *database.Store, actorUserID, machineID *string, kind string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = store.CreateEvent(ctx, database.CreateEventParams{
		ID:          uuid.NewString(),
		ActorUserID: actorUserID,
		MachineID:   machineID,
		Kind:        kind,
		PayloadJSON: string(body),
	})
	return err
}

func machineFromRecord(record database.MachineRecord) controlplane.Machine {
	hostID := ""
	if record.HostID != nil {
		hostID = *record.HostID
	}
	return controlplane.Machine{
		ID:             record.ID,
		Name:           record.Name,
		OwnerEmail:     record.OwnerEmail,
		HostID:         hostID,
		State:          record.State,
		DiskUsageBytes: record.DiskUsageBytes,
		PrimaryPort:    record.PrimaryPort,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
}

func shellFromRecord(record database.ShellRecord) browserterm.Shell {
	hostID := ""
	if record.HostID != nil {
		hostID = *record.HostID
	}
	return browserterm.Shell{
		ID:             record.ID,
		Name:           record.Name,
		UserEmail:      record.UserEmail,
		MachineName:    record.MachineName,
		HostID:         hostID,
		State:          record.State,
		CWD:            record.CWD,
		LastAttachedAt: record.LastAttachedAt,
		LastError:      valueOrEmpty(record.LastError),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
