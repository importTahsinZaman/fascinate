package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fascinate/internal/browserauth"
	"fascinate/internal/browserterm"
	"fascinate/internal/config"
	"fascinate/internal/controlplane"
	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

type fakeRuntime struct {
	healthErr error
	listErr   error
	machines  []machineruntime.Machine
}

type fakeReadiness struct {
	ready  bool
	status string
}

func (f *fakeRuntime) HealthCheck(context.Context) error {
	return f.healthErr
}

func (f *fakeRuntime) ListMachines(context.Context) ([]machineruntime.Machine, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.machines, nil
}

func (f *fakeReadiness) ReadinessStatus() (bool, string) {
	return f.ready, f.status
}

type fakeMachineManager struct {
	listOwnerEmail      string
	listResult          []controlplane.Machine
	listErr             error
	getOwnerEmail       string
	getResult           controlplane.Machine
	getErr              error
	getEnvOwner         string
	getEnvName          string
	getEnvResult        controlplane.MachineEnv
	getEnvErr           error
	createInput         controlplane.CreateMachineInput
	createResult        controlplane.Machine
	createErr           error
	deleteName          string
	deleteOwner         string
	deleteErr           error
	forkInput           controlplane.ForkMachineInput
	forkResult          controlplane.Machine
	forkErr             error
	listSnapshotsOwner  string
	listSnapshotsResult []controlplane.Snapshot
	listSnapshotsErr    error

	createSnapshotInput  controlplane.CreateSnapshotInput
	createSnapshotResult controlplane.Snapshot
	createSnapshotErr    error
	deleteSnapshotName   string
	deleteSnapshotOwner  string
	deleteSnapshotErr    error
	listEnvOwner         string
	listEnvResult        []controlplane.EnvVar
	listEnvErr           error
	setEnvInput          controlplane.SetEnvVarInput
	setEnvResult         controlplane.EnvVar
	setEnvErr            error
	deleteEnvOwner       string
	deleteEnvKey         string
	deleteEnvErr         error

	diagMachineOwner   string
	diagMachineName    string
	diagMachineResult  controlplane.MachineDiagnostics
	diagMachineErr     error
	diagBudgetOwner    string
	diagBudgetResult   controlplane.BudgetDiagnostics
	diagBudgetErr      error
	diagSnapshotOwner  string
	diagSnapshotName   string
	diagSnapshotResult controlplane.SnapshotDiagnostics
	diagSnapshotErr    error
	diagToolAuthOwner  string
	diagToolAuthResult controlplane.ToolAuthDiagnostics
	diagToolAuthErr    error
	diagHostsResult    []controlplane.Host
	diagHostsErr       error
	diagEventsOwner    string
	diagEventsLimit    int
	diagEventsResult   []controlplane.Event
	diagEventsErr      error
}

type fakeBrowserAuth struct {
	requestEmail string
	verifyEmail  string
	verifyCode   string
	token        string
	session      browserauth.Session
	authErr      error
}

func (f *fakeBrowserAuth) Enabled() bool {
	return true
}

func (f *fakeBrowserAuth) RequestCode(_ context.Context, email string) error {
	f.requestEmail = email
	return nil
}

func (f *fakeBrowserAuth) VerifyCode(_ context.Context, email, code, _ string, _ string) (browserauth.Session, error) {
	f.verifyEmail = email
	f.verifyCode = code
	return f.session, f.authErr
}

func (f *fakeBrowserAuth) Authenticate(_ context.Context, rawToken string) (database.User, database.WebSessionRecord, error) {
	if f.authErr != nil {
		return database.User{}, database.WebSessionRecord{}, f.authErr
	}
	if rawToken != f.token {
		return database.User{}, database.WebSessionRecord{}, database.ErrNotFound
	}
	return f.session.User, f.session.Record, nil
}

func (f *fakeBrowserAuth) Logout(context.Context, string) error {
	return nil
}

type fakeTerminalManager struct {
	userEmail        string
	machineName      string
	sessionID        string
	cols             int
	rows             int
	cwd              string
	diffBatchRequest browserterm.GitDiffBatchRequest
	init             browserterm.SessionInit
	gitStatus        browserterm.GitRepoStatus
	gitDiffBatch     browserterm.GitDiffBatchResponse
	err              error
}

func (f *fakeTerminalManager) CreateSession(_ context.Context, userEmail, machineName string, cols, rows int) (browserterm.SessionInit, error) {
	f.userEmail = userEmail
	f.machineName = machineName
	f.cols = cols
	f.rows = rows
	if f.err != nil {
		return browserterm.SessionInit{}, f.err
	}
	return f.init, nil
}

func (f *fakeTerminalManager) ReattachSession(_ context.Context, userEmail, sessionID string, cols, rows int) (browserterm.SessionInit, error) {
	f.userEmail = userEmail
	f.sessionID = sessionID
	f.cols = cols
	f.rows = rows
	if f.err != nil {
		return browserterm.SessionInit{}, f.err
	}
	return f.init, nil
}

func (f *fakeTerminalManager) CloseSession(_ context.Context, userEmail, sessionID string) error {
	f.userEmail = userEmail
	f.sessionID = sessionID
	return f.err
}

func (f *fakeTerminalManager) GetGitStatus(_ context.Context, userEmail, sessionID, cwd string) (browserterm.GitRepoStatus, error) {
	f.userEmail = userEmail
	f.sessionID = sessionID
	f.cwd = cwd
	if f.err != nil {
		return browserterm.GitRepoStatus{}, f.err
	}
	return f.gitStatus, nil
}

func (f *fakeTerminalManager) GetGitDiffBatch(_ context.Context, userEmail, sessionID string, request browserterm.GitDiffBatchRequest) (browserterm.GitDiffBatchResponse, error) {
	f.userEmail = userEmail
	f.sessionID = sessionID
	f.diffBatchRequest = request
	if f.err != nil {
		return browserterm.GitDiffBatchResponse{}, f.err
	}
	return f.gitDiffBatch, nil
}

func (f *fakeTerminalManager) StreamSession(http.ResponseWriter, *http.Request, string) error {
	return nil
}

func (f *fakeTerminalManager) Diagnostics() browserterm.Diagnostics {
	return browserterm.Diagnostics{ActiveSessions: 1, TotalCreated: 2}
}

func (f *fakeMachineManager) ListMachines(_ context.Context, ownerEmail string) ([]controlplane.Machine, error) {
	f.listOwnerEmail = ownerEmail
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}

func (f *fakeMachineManager) GetMachine(_ context.Context, _ string, ownerEmail string) (controlplane.Machine, error) {
	f.getOwnerEmail = ownerEmail
	if f.getErr != nil {
		return controlplane.Machine{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeMachineManager) GetPublicMachine(context.Context, string) (controlplane.Machine, error) {
	if f.getErr != nil {
		return controlplane.Machine{}, f.getErr
	}
	return f.getResult, nil
}

func (f *fakeMachineManager) GetMachineEnv(_ context.Context, name, ownerEmail string) (controlplane.MachineEnv, error) {
	f.getEnvName = name
	f.getEnvOwner = ownerEmail
	if f.getEnvErr != nil {
		return controlplane.MachineEnv{}, f.getEnvErr
	}
	return f.getEnvResult, nil
}

func (f *fakeMachineManager) CreateMachine(_ context.Context, input controlplane.CreateMachineInput) (controlplane.Machine, error) {
	f.createInput = input
	if f.createErr != nil {
		return controlplane.Machine{}, f.createErr
	}
	return f.createResult, nil
}

func (f *fakeMachineManager) DeleteMachine(_ context.Context, name, ownerEmail string) error {
	f.deleteName = name
	f.deleteOwner = ownerEmail
	return f.deleteErr
}

func (f *fakeMachineManager) ForkMachine(_ context.Context, input controlplane.ForkMachineInput) (controlplane.Machine, error) {
	f.forkInput = input
	if f.forkErr != nil {
		return controlplane.Machine{}, f.forkErr
	}
	return f.forkResult, nil
}

func (f *fakeMachineManager) ListSnapshots(_ context.Context, ownerEmail string) ([]controlplane.Snapshot, error) {
	f.listSnapshotsOwner = ownerEmail
	if f.listSnapshotsErr != nil {
		return nil, f.listSnapshotsErr
	}
	return f.listSnapshotsResult, nil
}

func (f *fakeMachineManager) CreateSnapshot(_ context.Context, input controlplane.CreateSnapshotInput) (controlplane.Snapshot, error) {
	f.createSnapshotInput = input
	if f.createSnapshotErr != nil {
		return controlplane.Snapshot{}, f.createSnapshotErr
	}
	return f.createSnapshotResult, nil
}

func (f *fakeMachineManager) DeleteSnapshot(_ context.Context, name, ownerEmail string) error {
	f.deleteSnapshotName = name
	f.deleteSnapshotOwner = ownerEmail
	return f.deleteSnapshotErr
}

func (f *fakeMachineManager) ListEnvVars(_ context.Context, ownerEmail string) ([]controlplane.EnvVar, error) {
	f.listEnvOwner = ownerEmail
	if f.listEnvErr != nil {
		return nil, f.listEnvErr
	}
	return f.listEnvResult, nil
}

func (f *fakeMachineManager) SetEnvVar(_ context.Context, input controlplane.SetEnvVarInput) (controlplane.EnvVar, error) {
	f.setEnvInput = input
	if f.setEnvErr != nil {
		return controlplane.EnvVar{}, f.setEnvErr
	}
	return f.setEnvResult, nil
}

func (f *fakeMachineManager) DeleteEnvVar(_ context.Context, ownerEmail, key string) error {
	f.deleteEnvOwner = ownerEmail
	f.deleteEnvKey = key
	return f.deleteEnvErr
}

func (f *fakeMachineManager) GetMachineDiagnostics(_ context.Context, name, ownerEmail string) (controlplane.MachineDiagnostics, error) {
	f.diagMachineName = name
	f.diagMachineOwner = ownerEmail
	if f.diagMachineErr != nil {
		return controlplane.MachineDiagnostics{}, f.diagMachineErr
	}
	return f.diagMachineResult, nil
}

func (f *fakeMachineManager) GetBudgetDiagnostics(_ context.Context, ownerEmail string) (controlplane.BudgetDiagnostics, error) {
	f.diagBudgetOwner = ownerEmail
	if f.diagBudgetErr != nil {
		return controlplane.BudgetDiagnostics{}, f.diagBudgetErr
	}
	return f.diagBudgetResult, nil
}

func (f *fakeMachineManager) GetSnapshotDiagnostics(_ context.Context, name, ownerEmail string) (controlplane.SnapshotDiagnostics, error) {
	f.diagSnapshotName = name
	f.diagSnapshotOwner = ownerEmail
	if f.diagSnapshotErr != nil {
		return controlplane.SnapshotDiagnostics{}, f.diagSnapshotErr
	}
	return f.diagSnapshotResult, nil
}

func (f *fakeMachineManager) GetToolAuthDiagnostics(_ context.Context, ownerEmail string) (controlplane.ToolAuthDiagnostics, error) {
	f.diagToolAuthOwner = ownerEmail
	if f.diagToolAuthErr != nil {
		return controlplane.ToolAuthDiagnostics{}, f.diagToolAuthErr
	}
	return f.diagToolAuthResult, nil
}

func (f *fakeMachineManager) ListHosts(_ context.Context) ([]controlplane.Host, error) {
	if f.diagHostsErr != nil {
		return nil, f.diagHostsErr
	}
	return f.diagHostsResult, nil
}

func (f *fakeMachineManager) ListOwnerEvents(_ context.Context, ownerEmail string, limit int) ([]controlplane.Event, error) {
	f.diagEventsOwner = ownerEmail
	f.diagEventsLimit = limit
	if f.diagEventsErr != nil {
		return nil, f.diagEventsErr
	}
	return f.diagEventsResult, nil
}

func TestListMachinesEndpointPassesOwnerEmail(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{
		listResult: []controlplane.Machine{{Name: "habits"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/machines?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Machines []controlplane.Machine `json:"machines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Machines) != 1 || body.Machines[0].Name != "habits" {
		t.Fatalf("unexpected machine list: %+v", body.Machines)
	}
}

func TestListMachinesEndpointRequiresOwnerEmail(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{})

	req := httptest.NewRequest(http.MethodGet, "/v1/machines", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestListMachinesEndpointUsesBrowserSessionOwnerWithoutOwnerEmail(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		token: "session-token",
		session: browserauth.Session{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.WebSessionRecord{
				ID:        "session-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
			},
		},
	}
	manager := &fakeMachineManager{
		listResult: []controlplane.Machine{{Name: "habits"}},
	}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, manager, auth, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/machines", nil)
	req.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if manager.listOwnerEmail != "dev@example.com" {
		t.Fatalf("expected session owner email, got %q", manager.listOwnerEmail)
	}
}

func TestListMachinesEndpointRejectsMismatchedOwnerEmailForBrowserSession(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		token: "session-token",
		session: browserauth.Session{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.WebSessionRecord{
				ID:        "session-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
			},
		},
	}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/machines?owner_email=other@example.com", nil)
	req.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "owner email does not match") {
		t.Fatalf("expected owner mismatch error, got %s", rec.Body.String())
	}
}

func TestCreateMachineEndpointReturnsConflict(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{createErr: database.ErrConflict}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodPost, "/v1/machines", bytes.NewBufferString(`{"name":"habits","owner_email":"dev@example.com"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if manager.createInput.OwnerEmail != "dev@example.com" || manager.createInput.Name != "habits" {
		t.Fatalf("unexpected create input: %+v", manager.createInput)
	}
}

func TestCreateMachineEndpointReturnsAcceptedForAsyncProvisioning(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{
		createResult: controlplane.Machine{Name: "habits", State: "CREATING", URL: "https://habits.fascinate.dev"},
	}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodPost, "/v1/machines", bytes.NewBufferString(`{"name":"habits","owner_email":"dev@example.com"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
}

func TestCreateMachineEndpointRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{})

	req := httptest.NewRequest(http.MethodPost, "/v1/machines", bytes.NewBufferString(`{"name":"habits","owner_email":"dev@example.com","extra":true}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestBudgetDiagnosticsEndpointPassesOwnerEmail(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{
		diagBudgetResult: controlplane.BudgetDiagnostics{
			OwnerEmail: "dev@example.com",
			Limits:     controlplane.BudgetSummary{CPU: "2"},
		},
	}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/budgets?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if manager.diagBudgetOwner != "dev@example.com" {
		t.Fatalf("expected owner email to be forwarded, got %q", manager.diagBudgetOwner)
	}
}

func TestForkMachineEndpointReturnsNotFound(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{forkErr: database.ErrNotFound})

	req := httptest.NewRequest(http.MethodPost, "/v1/machines/habits/fork", bytes.NewBufferString(`{"target_name":"habits-v2","owner_email":"dev@example.com"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteMachineEndpointCallsManager(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodDelete, "/v1/machines/habits?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if manager.deleteName != "habits" {
		t.Fatalf("expected delete of habits, got %q", manager.deleteName)
	}
	if manager.deleteOwner != "dev@example.com" {
		t.Fatalf("expected delete owner dev@example.com, got %q", manager.deleteOwner)
	}
}

func TestDiagnosticsEventsEndpointCallsManager(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{
		diagEventsResult: []controlplane.Event{{ID: "event-1", Kind: "machine.create.succeeded"}},
	}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/events?owner_email=dev@example.com&limit=25", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if manager.diagEventsOwner != "dev@example.com" || manager.diagEventsLimit != 25 {
		t.Fatalf("unexpected diagnostics events args: owner=%q limit=%d", manager.diagEventsOwner, manager.diagEventsLimit)
	}
	if !strings.Contains(rec.Body.String(), "machine.create.succeeded") {
		t.Fatalf("expected event body, got %q", rec.Body.String())
	}
}

func TestDiagnosticsMachineEndpointCallsManager(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{
		diagMachineResult: controlplane.MachineDiagnostics{
			Machine: controlplane.Machine{Name: "habits", OwnerEmail: "dev@example.com"},
		},
	}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/machines/habits?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if manager.diagMachineOwner != "dev@example.com" || manager.diagMachineName != "habits" {
		t.Fatalf("unexpected machine diagnostics args: owner=%q name=%q", manager.diagMachineOwner, manager.diagMachineName)
	}
}

func TestDiagnosticsSnapshotEndpointCallsManager(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{
		diagSnapshotResult: controlplane.SnapshotDiagnostics{
			Snapshot: controlplane.Snapshot{Name: "baseline", OwnerEmail: "dev@example.com"},
		},
	}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/snapshots/baseline?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if manager.diagSnapshotOwner != "dev@example.com" || manager.diagSnapshotName != "baseline" {
		t.Fatalf("unexpected snapshot diagnostics args: owner=%q name=%q", manager.diagSnapshotOwner, manager.diagSnapshotName)
	}
}

func TestDiagnosticsToolAuthEndpointCallsManager(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{
		diagToolAuthResult: controlplane.ToolAuthDiagnostics{
			OwnerEmail: "dev@example.com",
		},
	}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/tool-auth?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if manager.diagToolAuthOwner != "dev@example.com" {
		t.Fatalf("unexpected tool-auth diagnostics owner: %q", manager.diagToolAuthOwner)
	}
}

func TestDiagnosticsHostsEndpointCallsManager(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{
		diagHostsResult: []controlplane.Host{{ID: "local-host", Name: "local-host", PlacementEligible: true}},
	}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/hosts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "local-host") {
		t.Fatalf("expected host body, got %q", rec.Body.String())
	}
}

func TestRootEndpointDoesNotLeakAdminEmails(t *testing.T) {
	t.Parallel()

	handler := newTestHandlerWithConfig(t, config.Config{
		BaseDomain:  "fascinate.dev",
		AdminEmails: []string{"admin@example.com"},
	}, &fakeRuntime{}, &fakeMachineManager{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "admin@example.com") {
		t.Fatalf("unexpected admin email leak: %q", rec.Body.String())
	}
}

func TestReadyzReturnsUnavailableWhenRuntimeFails(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{healthErr: errors.New("runtime down")}, &fakeMachineManager{})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestReadyzReturnsUnavailableDuringStartupRecovery(t *testing.T) {
	t.Parallel()

	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, nil, nil, nil, &fakeReadiness{
		ready:  false,
		status: "startup recovery in progress",
	})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "startup recovery in progress") {
		t.Fatalf("expected startup recovery body, got %q", rec.Body.String())
	}
}

func TestMachineSubdomainProxiesToRuntime(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "habits.fascinate.dev" {
			t.Fatalf("unexpected host: %q", r.Host)
		}
		if r.URL.Path != "/app" {
			t.Fatalf("unexpected path: %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, "proxied")
	}))
	defer upstream.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	primaryPort, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{
		getResult: controlplane.Machine{
			Name:        "habits",
			OwnerEmail:  "dev@example.com",
			PrimaryPort: primaryPort,
			Runtime: &machineruntime.Machine{
				Name:    "habits",
				AppHost: host,
				AppPort: primaryPort,
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "http://habits.fascinate.dev/app", nil)
	req.Host = "habits.fascinate.dev"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "proxied" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestMachineSubdomainShowsStatusPageWhenNoRuntimeAddress(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{
		getResult: controlplane.Machine{
			Name:        "habits",
			OwnerEmail:  "dev@example.com",
			PrimaryPort: 3000,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "http://habits.fascinate.dev/", nil)
	req.Host = "habits.fascinate.dev"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "No services detected") {
		t.Fatalf("unexpected body: %q", body)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Open Fascinate to inspect the machine and start a browser shell.") {
		t.Fatalf("expected browser guidance in body: %q", body)
	}
}

func TestMachineSubdomainProxiesToRuntimeIPv6Fallback(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "habits.fascinate.dev" {
			t.Fatalf("unexpected host: %q", r.Host)
		}
		_, _ = io.WriteString(w, "proxied-ipv6")
	}))

	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	upstream.Listener = listener
	upstream.Start()
	defer upstream.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	primaryPort, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{
		getResult: controlplane.Machine{
			Name:        "habits",
			OwnerEmail:  "dev@example.com",
			PrimaryPort: primaryPort,
			Runtime: &machineruntime.Machine{
				Name:    "habits",
				AppHost: host,
				AppPort: primaryPort,
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "http://habits.fascinate.dev/", nil)
	req.Host = "habits.fascinate.dev"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "proxied-ipv6" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestMachineSubdomainReturnsNotFoundForUnknownMachine(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{getErr: database.ErrNotFound})

	req := httptest.NewRequest(http.MethodGet, "http://missing.fascinate.dev/", nil)
	req.Host = "missing.fascinate.dev"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Unknown machine") {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestListEnvVarsEndpoint(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{
		listEnvResult: []controlplane.EnvVar{{Key: "FRONTEND_URL", RawValue: "${FASCINATE_PUBLIC_URL}"}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/env-vars?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FRONTEND_URL") {
		t.Fatalf("unexpected response body %q", rec.Body.String())
	}
}

func TestSetEnvVarEndpoint(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{
		setEnvResult: controlplane.EnvVar{Key: "FRONTEND_URL", RawValue: "${FASCINATE_PUBLIC_URL}"},
	}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	body := `{"owner_email":"dev@example.com","key":"FRONTEND_URL","value":"${FASCINATE_PUBLIC_URL}"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/env-vars", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if manager.setEnvInput.Key != "FRONTEND_URL" || manager.setEnvInput.OwnerEmail != "dev@example.com" {
		t.Fatalf("unexpected env input %+v", manager.setEnvInput)
	}
}

func TestDeleteEnvVarEndpoint(t *testing.T) {
	t.Parallel()

	manager := &fakeMachineManager{}
	handler := newTestHandler(t, &fakeRuntime{}, manager)

	req := httptest.NewRequest(http.MethodDelete, "/v1/env-vars/FRONTEND_URL?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if manager.deleteEnvOwner != "dev@example.com" || manager.deleteEnvKey != "FRONTEND_URL" {
		t.Fatalf("unexpected delete env call owner=%q key=%q", manager.deleteEnvOwner, manager.deleteEnvKey)
	}
}

func TestGetMachineEnvEndpoint(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{
		getEnvResult: controlplane.MachineEnv{
			MachineName: "m-1",
			Entries: []controlplane.EffectiveEnvVar{
				{Key: "FASCINATE_PUBLIC_URL", Value: "https://m-1.fascinate.dev"},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/machines/m-1/env?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "FASCINATE_PUBLIC_URL") {
		t.Fatalf("unexpected response body %q", rec.Body.String())
	}
}

func newTestHandler(t *testing.T, runtime *fakeRuntime, machines *fakeMachineManager) http.Handler {
	t.Helper()
	return newTestHandlerWithConfig(t, config.Config{BaseDomain: "fascinate.dev"}, runtime, machines)
}

func newTestHandlerWithConfig(t *testing.T, cfg config.Config, runtime *fakeRuntime, machines *fakeMachineManager) http.Handler {
	t.Helper()
	return newTestHandlerWithExtras(t, cfg, runtime, machines, nil, nil, nil, nil)
}

func newTestHandlerWithExtras(t *testing.T, cfg config.Config, runtime *fakeRuntime, machines *fakeMachineManager, auth browserAuthService, terminals terminalManager, seed func(context.Context, *database.Store), readiness readinessChecker) http.Handler {
	t.Helper()

	ctx := context.Background()
	store, err := database.Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	if seed != nil {
		seed(ctx, store)
	}

	return New(cfg, store, runtime, machines, auth, terminals, readiness)
}

func TestVerifyBrowserLoginSetsSessionCookie(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		session: browserauth.Session{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.WebSessionRecord{
				ID:        "session-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
			},
			RawToken:  "raw-session-token",
			ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		},
	}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify", strings.NewReader(`{"email":"dev@example.com","code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if auth.verifyEmail != "dev@example.com" || auth.verifyCode != "123456" {
		t.Fatalf("unexpected verify call %+v", auth)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "fascinate_session" || cookies[0].Value != "raw-session-token" {
		t.Fatalf("unexpected cookies %+v", cookies)
	}
}

func TestWorkspaceEndpointPersistsLayoutForBrowserSession(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		token:   "session-token",
		session: browserauth.Session{},
	}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, func(ctx context.Context, store *database.Store) {
		user, err := store.UpsertUser(ctx, "dev@example.com", false)
		if err != nil {
			t.Fatal(err)
		}
		auth.session.User = user
		auth.session.Record = database.WebSessionRecord{
			ID:        "session-1",
			UserID:    user.ID,
			UserEmail: user.Email,
		}
	}, nil)

	putReq := httptest.NewRequest(http.MethodPut, "/v1/workspaces/default", strings.NewReader(`{"layout":{"version":1,"windows":[{"id":"one","machineName":"m-1","title":"m-1 shell","x":1,"y":2,"width":320,"height":220,"z":1}]}}`))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putRec.Code, putRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/workspaces/default", nil)
	getReq.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	if !strings.Contains(getRec.Body.String(), `"machineName":"m-1"`) {
		t.Fatalf("expected saved workspace layout, got %s", getRec.Body.String())
	}
}

func TestCreateTerminalSessionUsesBrowserSession(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		token: "session-token",
		session: browserauth.Session{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.WebSessionRecord{
				ID:        "session-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
			},
		},
	}
	terminals := &fakeTerminalManager{
		init: browserterm.SessionInit{
			ID:          "term-1",
			HostID:      "local-host",
			MachineName: "m-1",
			AttachURL:   "/v1/terminal/sessions/term-1/stream?token=abc",
			ExpiresAt:   time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
		},
	}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/terminal/sessions", strings.NewReader(`{"machine_name":"m-1","cols":120,"rows":40}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.userEmail != "dev@example.com" || terminals.machineName != "m-1" {
		t.Fatalf("unexpected terminal create args %+v", terminals)
	}
}

func TestReattachTerminalSessionUsesBrowserSession(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		token: "session-token",
		session: browserauth.Session{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.WebSessionRecord{
				ID:        "session-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
			},
		},
	}
	terminals := &fakeTerminalManager{
		init: browserterm.SessionInit{
			ID:          "term-1",
			HostID:      "local-host",
			MachineName: "m-1",
			AttachURL:   "/v1/terminal/sessions/term-1/stream?token=abc",
			ExpiresAt:   time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
		},
	}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/terminal/sessions/term-1/attach", strings.NewReader(`{"cols":160,"rows":60}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.userEmail != "dev@example.com" || terminals.sessionID != "term-1" || terminals.cols != 160 || terminals.rows != 60 {
		t.Fatalf("unexpected terminal attach args %+v", terminals)
	}
}

func TestCloseTerminalSessionUsesBrowserSession(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		token: "session-token",
		session: browserauth.Session{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.WebSessionRecord{
				ID:        "session-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
			},
		},
	}
	terminals := &fakeTerminalManager{}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/v1/terminal/sessions/term-1", nil)
	req.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.userEmail != "dev@example.com" || terminals.sessionID != "term-1" {
		t.Fatalf("unexpected terminal close args %+v", terminals)
	}
}

func TestTerminalGitStatusUsesBrowserSession(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		token: "session-token",
		session: browserauth.Session{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.WebSessionRecord{
				ID:        "session-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
			},
		},
	}
	terminals := &fakeTerminalManager{
		gitStatus: browserterm.GitRepoStatus{
			State:     "ready",
			RepoRoot:  "/home/ubuntu/project",
			Branch:    "main",
			Additions: 7,
			Deletions: 3,
			Files: []browserterm.GitChangedFile{
				{Path: "web/src/app.tsx", Kind: "modified", WorktreeStatus: "M"},
			},
		},
	}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/terminal/sessions/term-1/git/status", strings.NewReader(`{"cwd":"/home/ubuntu/project/web"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.userEmail != "dev@example.com" || terminals.sessionID != "term-1" || terminals.cwd != "/home/ubuntu/project/web" {
		t.Fatalf("unexpected git status args %+v", terminals)
	}
	if !strings.Contains(rec.Body.String(), `"repo_root":"/home/ubuntu/project"`) {
		t.Fatalf("expected repo root in response, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"additions":7`) || !strings.Contains(rec.Body.String(), `"deletions":3`) {
		t.Fatalf("expected repo totals in response, got %s", rec.Body.String())
	}
}

func TestTerminalGitDiffBatchUsesBrowserSession(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		token: "session-token",
		session: browserauth.Session{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.WebSessionRecord{
				ID:        "session-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
			},
		},
	}
	terminals := &fakeTerminalManager{
		gitDiffBatch: browserterm.GitDiffBatchResponse{
			Diffs: []browserterm.GitFileDiff{
				{State: "ready", Path: "web/src/app.tsx", Additions: 1, Deletions: 1, Patch: "diff --git a/web/src/app.tsx b/web/src/app.tsx\n"},
				{State: "too_large", Path: "README.md", Message: "Diff is too large to render inline. Use git in the shell for the full patch."},
			},
		},
	}
	handler := newTestHandlerWithExtras(t, config.Config{
		BaseDomain:           "fascinate.dev",
		WebSessionCookieName: "fascinate_session",
	}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/terminal/sessions/term-1/git/diffs", strings.NewReader(`{"cwd":"/home/ubuntu/project/web","repo_root":"/home/ubuntu/project","files":[{"path":"web/src/app.tsx","kind":"modified","worktree_status":"M"},{"path":"README.md","kind":"modified","worktree_status":"M"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "fascinate_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.userEmail != "dev@example.com" || terminals.sessionID != "term-1" {
		t.Fatalf("unexpected git diff args %+v", terminals)
	}
	if terminals.diffBatchRequest.RepoRoot != "/home/ubuntu/project" || len(terminals.diffBatchRequest.Files) != 2 {
		t.Fatalf("unexpected git diff batch request %+v", terminals.diffBatchRequest)
	}
	if !strings.Contains(rec.Body.String(), `"diffs"`) || !strings.Contains(rec.Body.String(), `"too_large"`) {
		t.Fatalf("expected batched diffs in response, got %s", rec.Body.String())
	}
}
