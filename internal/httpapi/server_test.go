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
	"os"
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
	requestEmail    string
	verifyEmail     string
	verifyCode      string
	verifyTokenName string
	token           string
	apiToken        string
	logoutToken     string
	logoutAPIToken  string
	logoutAPIErr    error
	session         browserauth.Session
	apiTokenSession browserauth.APITokenSession
	authErr         error
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

func (f *fakeBrowserAuth) Logout(_ context.Context, rawToken string) error {
	f.logoutToken = rawToken
	return nil
}

func (f *fakeBrowserAuth) VerifyCodeForAPIToken(_ context.Context, email, code, tokenName, _ string, _ string) (browserauth.APITokenSession, error) {
	f.verifyEmail = email
	f.verifyCode = code
	f.verifyTokenName = tokenName
	return f.apiTokenSession, f.authErr
}

func (f *fakeBrowserAuth) AuthenticateAPIToken(_ context.Context, rawToken string) (database.User, database.APITokenRecord, error) {
	if f.authErr != nil {
		return database.User{}, database.APITokenRecord{}, f.authErr
	}
	if rawToken != f.apiToken {
		return database.User{}, database.APITokenRecord{}, database.ErrNotFound
	}
	return f.apiTokenSession.User, f.apiTokenSession.Record, nil
}

func (f *fakeBrowserAuth) LogoutAPIToken(_ context.Context, rawToken string) error {
	f.logoutAPIToken = rawToken
	return f.logoutAPIErr
}

type fakeTerminalManager struct {
	userEmail         string
	machineName       string
	closedMachine     string
	sessionID         string
	cols              int
	rows              int
	cwd               string
	input             string
	listShellsUser    string
	listShellsResult  []browserterm.Shell
	getShellUser      string
	getShellID        string
	getShellResult    browserterm.Shell
	createShellUser   string
	createShellName   string
	createShellInput  string
	createShellResult browserterm.Shell
	deleteShellUser   string
	deleteShellID     string
	readLinesUser     string
	readLinesShellID  string
	readLinesLimit    int
	readLinesResult   []string
	listExecsUser     string
	listExecsLimit    int
	listExecsResult   []browserterm.Exec
	createExecUser    string
	createExecMachine string
	createExecRequest browserterm.ExecRequest
	createExecResult  browserterm.Exec
	getExecUser       string
	getExecID         string
	getExecResult     browserterm.Exec
	cancelExecUser    string
	cancelExecID      string
	execDiagUser      string
	execDiagLimit     int
	execDiagResult    browserterm.ExecDiagnostics
	diffBatchRequest  browserterm.GitDiffBatchRequest
	init              browserterm.SessionInit
	gitStatus         browserterm.GitRepoStatus
	gitDiffBatch      browserterm.GitDiffBatchResponse
	err               error
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

func (f *fakeTerminalManager) CloseMachineSessions(_ context.Context, userEmail, machineName string) error {
	f.userEmail = userEmail
	f.closedMachine = machineName
	return f.err
}

func (f *fakeTerminalManager) ListShells(_ context.Context, userEmail string) ([]browserterm.Shell, error) {
	f.listShellsUser = userEmail
	if f.err != nil {
		return nil, f.err
	}
	return f.listShellsResult, nil
}

func (f *fakeTerminalManager) GetShell(_ context.Context, userEmail, shellID string) (browserterm.Shell, error) {
	f.getShellUser = userEmail
	f.getShellID = shellID
	if f.err != nil {
		return browserterm.Shell{}, f.err
	}
	return f.getShellResult, nil
}

func (f *fakeTerminalManager) CreateShell(_ context.Context, userEmail, machineName, name string) (browserterm.Shell, error) {
	f.createShellUser = userEmail
	f.machineName = machineName
	f.createShellName = name
	if f.err != nil {
		return browserterm.Shell{}, f.err
	}
	return f.createShellResult, nil
}

func (f *fakeTerminalManager) DeleteShell(_ context.Context, userEmail, shellID string) error {
	f.deleteShellUser = userEmail
	f.deleteShellID = shellID
	return f.err
}

func (f *fakeTerminalManager) CreateAttachment(_ context.Context, userEmail, shellID string, cols, rows int) (browserterm.SessionInit, error) {
	f.userEmail = userEmail
	f.sessionID = shellID
	f.cols = cols
	f.rows = rows
	if f.err != nil {
		return browserterm.SessionInit{}, f.err
	}
	return f.init, nil
}

func (f *fakeTerminalManager) SendInput(_ context.Context, userEmail, shellID, input string) error {
	f.userEmail = userEmail
	f.sessionID = shellID
	f.input = input
	return f.err
}

func (f *fakeTerminalManager) ReadLines(_ context.Context, userEmail, shellID string, limit int) ([]string, error) {
	f.readLinesUser = userEmail
	f.readLinesShellID = shellID
	f.readLinesLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.readLinesResult, nil
}

func (f *fakeTerminalManager) CreateExec(_ context.Context, userEmail, machineName string, request browserterm.ExecRequest) (browserterm.Exec, error) {
	f.createExecUser = userEmail
	f.createExecMachine = machineName
	f.createExecRequest = request
	if f.err != nil {
		return browserterm.Exec{}, f.err
	}
	return f.createExecResult, nil
}

func (f *fakeTerminalManager) GetExec(_ context.Context, userEmail, execID string) (browserterm.Exec, error) {
	f.getExecUser = userEmail
	f.getExecID = execID
	if f.err != nil {
		return browserterm.Exec{}, f.err
	}
	return f.getExecResult, nil
}

func (f *fakeTerminalManager) ListExecs(_ context.Context, userEmail string, limit int) ([]browserterm.Exec, error) {
	f.listExecsUser = userEmail
	f.listExecsLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.listExecsResult, nil
}

func (f *fakeTerminalManager) CancelExec(_ context.Context, userEmail, execID string) error {
	f.cancelExecUser = userEmail
	f.cancelExecID = execID
	return f.err
}

func (f *fakeTerminalManager) StreamExec(w http.ResponseWriter, _ *http.Request, _ string, _ string) error {
	w.Header().Set("Content-Type", "text/event-stream")
	_, err := io.WriteString(w, "event: result\ndata: {\"type\":\"result\"}\n\n")
	return err
}

func (f *fakeTerminalManager) ExecDiagnostics(_ context.Context, userEmail string, limit int) (browserterm.ExecDiagnostics, error) {
	f.execDiagUser = userEmail
	f.execDiagLimit = limit
	if f.err != nil {
		return browserterm.ExecDiagnostics{}, f.err
	}
	return f.execDiagResult, nil
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
	terminals := &fakeTerminalManager{}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, manager, nil, terminals, nil, nil)

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
	if terminals.userEmail != "dev@example.com" || terminals.closedMachine != "habits" {
		t.Fatalf("expected matching machine sessions to be closed, got %+v", terminals)
	}
}

func TestStartMachineEndpointNotExposed(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{})

	req := httptest.NewRequest(http.MethodPost, "/v1/machines/habits/start?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStopMachineEndpointNotExposed(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{})

	req := httptest.NewRequest(http.MethodPost, "/v1/machines/habits/stop?owner_email=dev@example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
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
	if !strings.Contains(rec.Body.String(), `"active_subscribers":0`) {
		t.Fatalf("expected stream diagnostics, got %q", rec.Body.String())
	}
}

func TestOwnerEventsStreamReplaysEventsAfterLastEventID(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User:   database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{ID: "token-1", UserID: "user-1", UserEmail: "dev@example.com", Name: "fascinate-cli"},
		},
	}

	var firstEventID string
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, func(ctx context.Context, store *database.Store) {
		user, err := store.UpsertUser(ctx, "dev@example.com", false)
		if err != nil {
			t.Fatal(err)
		}
		first, err := store.CreateEvent(ctx, database.CreateEventParams{
			ID:          "event-1",
			ActorUserID: &user.ID,
			Kind:        "machine.create.started",
			PayloadJSON: `{"machine_name":"m-1"}`,
		})
		if err != nil {
			t.Fatal(err)
		}
		firstEventID = first.ID
		if _, err := store.CreateEvent(ctx, database.CreateEventParams{
			ID:          "event-2",
			ActorUserID: &user.ID,
			Kind:        "machine.create.succeeded",
			PayloadJSON: `{"machine_name":"m-1"}`,
		}); err != nil {
			t.Fatal(err)
		}
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream?last_event_id="+firstEventID, nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer cli-token")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind":"machine.create.succeeded"`) {
		t.Fatalf("expected replayed event, got %s", rec.Body.String())
	}
}

func TestOwnerEventsStreamFansOutLiveEvents(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User:   database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{ID: "token-1", UserID: "user-1", UserEmail: "dev@example.com", Name: "fascinate-cli"},
		},
	}

	var storeRef *database.Store
	var userID string
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, func(ctx context.Context, store *database.Store) {
		user, err := store.UpsertUser(ctx, "dev@example.com", false)
		if err != nil {
			t.Fatal(err)
		}
		userID = user.ID
		storeRef = store
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/v1/events/stream", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer cli-token")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := storeRef.CreateEvent(context.Background(), database.CreateEventParams{
		ID:          "event-live-1",
		ActorUserID: &userID,
		Kind:        "shell.created",
		PayloadJSON: `{"shell_id":"shell-1"}`,
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind":"shell.created"`) {
		t.Fatalf("expected live event, got %s", rec.Body.String())
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

func TestReservedDownloadsSubdomainServesCLIAssets(t *testing.T) {
	t.Parallel()

	publicDir := t.TempDir()
	distDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(publicDir, "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "cli", "index.json"), []byte("{\"latestVersion\":\"0.1.0\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(distDir, "index.html"), []byte("<html>app</html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := newTestHandlerWithConfig(t, config.Config{
		BaseDomain:      "fascinate.dev",
		PublicAssetsDir: publicDir,
		WebDistDir:      distDir,
	}, &fakeRuntime{}, &fakeMachineManager{})

	req := httptest.NewRequest(http.MethodGet, "http://downloads.fascinate.dev/cli/index.json", nil)
	req.Host = "downloads.fascinate.dev"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); body != "{\"latestVersion\":\"0.1.0\"}\n" {
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

	var body struct {
		EnvVars []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"env_vars"`
		BuiltinEnvVars []controlplane.BuiltinEnvVar `json:"builtin_env_vars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.EnvVars) != 1 || body.EnvVars[0].Key != "FRONTEND_URL" || body.EnvVars[0].Value != "${FASCINATE_PUBLIC_URL}" {
		t.Fatalf("unexpected env vars %+v", body.EnvVars)
	}
	if len(body.BuiltinEnvVars) == 0 {
		t.Fatalf("expected built-in env vars in response")
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
	var bodyResp struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bodyResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if bodyResp.Key != "FRONTEND_URL" || bodyResp.Value != "${FASCINATE_PUBLIC_URL}" {
		t.Fatalf("unexpected response %+v", bodyResp)
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

func TestVerifyCLILoginReturnsBearerToken(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	auth := &fakeBrowserAuth{
		apiTokenSession: browserauth.APITokenSession{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{
				ID:         "token-1",
				UserID:     "user-1",
				UserEmail:  "dev@example.com",
				Name:       "agent-shell",
				CreatedAt:  time.Now().UTC().Format(time.RFC3339),
				ExpiresAt:  expiresAt.Format(time.RFC3339),
				LastUsedAt: expiresAt.Format(time.RFC3339),
			},
			RawToken:  "cli-token",
			ExpiresAt: expiresAt,
		},
	}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/cli/auth/verify", strings.NewReader(`{"email":"dev@example.com","code":"123456","token_name":"agent-shell"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if auth.verifyEmail != "dev@example.com" || auth.verifyCode != "123456" || auth.verifyTokenName != "agent-shell" {
		t.Fatalf("unexpected cli verify call %+v", auth)
	}
	var body struct {
		User      database.User `json:"user"`
		Token     string        `json:"token"`
		TokenID   string        `json:"token_id"`
		TokenName string        `json:"token_name"`
		ExpiresAt string        `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.User.Email != "dev@example.com" || body.Token != "cli-token" || body.TokenID != "token-1" || body.TokenName != "agent-shell" || body.ExpiresAt == "" {
		t.Fatalf("unexpected cli verify body %+v", body)
	}
}

func TestCLISessionEndpointUsesBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{
				ID:         "token-1",
				UserID:     "user-1",
				UserEmail:  "dev@example.com",
				Name:       "fascinate-cli",
				CreatedAt:  "2026-04-06T00:00:00Z",
				ExpiresAt:  "2026-07-05T00:00:00Z",
				LastUsedAt: "2026-04-06T00:00:00Z",
			},
		},
	}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/cli/auth/session", nil)
	req.Header.Set("Authorization", "Bearer cli-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"email":"dev@example.com"`) || !strings.Contains(rec.Body.String(), `"name":"fascinate-cli"`) {
		t.Fatalf("unexpected cli session body %s", rec.Body.String())
	}
}

func TestCLILogoutEndpointRevokesBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{apiToken: "cli-token"}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/cli/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer cli-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if auth.logoutAPIToken != "cli-token" {
		t.Fatalf("expected cli token logout, got %q", auth.logoutAPIToken)
	}
}

func TestCLILogoutEndpointRequiresBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/cli/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if auth.logoutAPIToken != "" {
		t.Fatalf("expected no logout call without bearer token, got %q", auth.logoutAPIToken)
	}
}

func TestListMachinesEndpointUsesBearerTokenOwnerWithoutOwnerEmail(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User: database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{
				ID:        "token-1",
				UserID:    "user-1",
				UserEmail: "dev@example.com",
				Name:      "fascinate-cli",
			},
		},
	}
	manager := &fakeMachineManager{
		listResult: []controlplane.Machine{{Name: "habits"}},
	}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, manager, auth, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer cli-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if manager.listOwnerEmail != "dev@example.com" {
		t.Fatalf("expected bearer owner email, got %q", manager.listOwnerEmail)
	}
}

func TestListShellsEndpointUsesBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User:   database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{ID: "token-1", UserID: "user-1", UserEmail: "dev@example.com", Name: "fascinate-cli"},
		},
	}
	terminals := &fakeTerminalManager{
		listShellsResult: []browserterm.Shell{{ID: "shell-1", Name: "primary", MachineName: "m-1", State: "READY"}},
	}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/shells", nil)
	req.Header.Set("Authorization", "Bearer cli-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.listShellsUser != "dev@example.com" || !strings.Contains(rec.Body.String(), `"shell-1"`) {
		t.Fatalf("unexpected shell list behavior user=%q body=%s", terminals.listShellsUser, rec.Body.String())
	}
}

func TestCreateShellEndpointUsesBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User:   database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{ID: "token-1", UserID: "user-1", UserEmail: "dev@example.com", Name: "fascinate-cli"},
		},
	}
	terminals := &fakeTerminalManager{
		createShellResult: browserterm.Shell{ID: "shell-1", Name: "primary", MachineName: "m-1", State: "READY"},
	}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/shells", strings.NewReader(`{"machine_name":"m-1","name":"primary"}`))
	req.Header.Set("Authorization", "Bearer cli-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.createShellUser != "dev@example.com" || terminals.machineName != "m-1" || terminals.createShellName != "primary" {
		t.Fatalf("unexpected create shell args %+v", terminals)
	}
}

func TestShellAttachInputAndLinesEndpointsUseBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User:   database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{ID: "token-1", UserID: "user-1", UserEmail: "dev@example.com", Name: "fascinate-cli"},
		},
	}
	terminals := &fakeTerminalManager{
		init:            browserterm.SessionInit{ID: "shell-1", HostID: "local-host", MachineName: "m-1", AttachURL: "/v1/terminal/sessions/shell-1/stream?token=abc", ExpiresAt: "2026-04-06T00:00:00Z"},
		readLinesResult: []string{"$ pwd", "/home/ubuntu/project"},
	}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	attachReq := httptest.NewRequest(http.MethodPost, "/v1/shells/shell-1/attach", strings.NewReader(`{"cols":160,"rows":60}`))
	attachReq.Header.Set("Authorization", "Bearer cli-token")
	attachReq.Header.Set("Content-Type", "application/json")
	attachRec := httptest.NewRecorder()
	handler.ServeHTTP(attachRec, attachReq)

	if attachRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", attachRec.Code, attachRec.Body.String())
	}
	if terminals.userEmail != "dev@example.com" || terminals.sessionID != "shell-1" || terminals.cols != 160 || terminals.rows != 60 {
		t.Fatalf("unexpected shell attach args %+v", terminals)
	}

	inputReq := httptest.NewRequest(http.MethodPost, "/v1/shells/shell-1/input", strings.NewReader(`{"input":"pwd\n"}`))
	inputReq.Header.Set("Authorization", "Bearer cli-token")
	inputReq.Header.Set("Content-Type", "application/json")
	inputRec := httptest.NewRecorder()
	handler.ServeHTTP(inputRec, inputReq)

	if inputRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", inputRec.Code, inputRec.Body.String())
	}
	if terminals.input != "pwd\n" {
		t.Fatalf("unexpected shell input %+v", terminals)
	}

	linesReq := httptest.NewRequest(http.MethodGet, "/v1/shells/shell-1/lines?limit=50", nil)
	linesReq.Header.Set("Authorization", "Bearer cli-token")
	linesRec := httptest.NewRecorder()
	handler.ServeHTTP(linesRec, linesReq)

	if linesRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", linesRec.Code, linesRec.Body.String())
	}
	if terminals.readLinesUser != "dev@example.com" || terminals.readLinesShellID != "shell-1" || terminals.readLinesLimit != 50 {
		t.Fatalf("unexpected shell lines args %+v", terminals)
	}
	if !strings.Contains(linesRec.Body.String(), `/home/ubuntu/project`) {
		t.Fatalf("unexpected lines body %s", linesRec.Body.String())
	}
}

func TestDeleteShellEndpointUsesBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User:   database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{ID: "token-1", UserID: "user-1", UserEmail: "dev@example.com", Name: "fascinate-cli"},
		},
	}
	terminals := &fakeTerminalManager{}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/v1/shells/shell-1", nil)
	req.Header.Set("Authorization", "Bearer cli-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.deleteShellUser != "dev@example.com" || terminals.deleteShellID != "shell-1" {
		t.Fatalf("unexpected delete shell args %+v", terminals)
	}
}

func TestCreateExecEndpointUsesBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User:   database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{ID: "token-1", UserID: "user-1", UserEmail: "dev@example.com", Name: "fascinate-cli"},
		},
	}
	terminals := &fakeTerminalManager{
		createExecResult: browserterm.Exec{ID: "exec-1", MachineName: "m-1", State: "RUNNING"},
	}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/execs", strings.NewReader(`{"machine_name":"m-1","command_text":"pwd","cwd":"/home/ubuntu/project","timeout_seconds":30}`))
	req.Header.Set("Authorization", "Bearer cli-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if terminals.createExecUser != "dev@example.com" || terminals.createExecMachine != "m-1" || terminals.createExecRequest.CommandText != "pwd" {
		t.Fatalf("unexpected create exec args %+v", terminals)
	}
	if !strings.Contains(rec.Body.String(), `"stream_url":"/v1/execs/exec-1/stream"`) {
		t.Fatalf("expected stream url in response, got %s", rec.Body.String())
	}
}

func TestExecEndpointsUseBearerToken(t *testing.T) {
	t.Parallel()

	auth := &fakeBrowserAuth{
		apiToken: "cli-token",
		apiTokenSession: browserauth.APITokenSession{
			User:   database.User{ID: "user-1", Email: "dev@example.com"},
			Record: database.APITokenRecord{ID: "token-1", UserID: "user-1", UserEmail: "dev@example.com", Name: "fascinate-cli"},
		},
	}
	terminals := &fakeTerminalManager{
		listExecsResult: []browserterm.Exec{{ID: "exec-1", MachineName: "m-1", State: "SUCCEEDED"}},
		getExecResult:   browserterm.Exec{ID: "exec-1", MachineName: "m-1", State: "SUCCEEDED"},
		execDiagResult:  browserterm.ExecDiagnostics{Active: 1, Execs: []browserterm.Exec{{ID: "exec-1", MachineName: "m-1", State: "SUCCEEDED"}}},
	}
	handler := newTestHandlerWithExtras(t, config.Config{BaseDomain: "fascinate.dev"}, &fakeRuntime{}, &fakeMachineManager{}, auth, terminals, nil, nil)

	listReq := httptest.NewRequest(http.MethodGet, "/v1/execs?limit=10", nil)
	listReq.Header.Set("Authorization", "Bearer cli-token")
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}
	if terminals.listExecsUser != "dev@example.com" || terminals.listExecsLimit != 10 {
		t.Fatalf("unexpected exec list args %+v", terminals)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/execs/exec-1", nil)
	getReq.Header.Set("Authorization", "Bearer cli-token")
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}
	if terminals.getExecUser != "dev@example.com" || terminals.getExecID != "exec-1" {
		t.Fatalf("unexpected get exec args %+v", terminals)
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/v1/execs/exec-1/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer cli-token")
	cancelRec := httptest.NewRecorder()
	handler.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", cancelRec.Code, cancelRec.Body.String())
	}
	if terminals.cancelExecUser != "dev@example.com" || terminals.cancelExecID != "exec-1" {
		t.Fatalf("unexpected cancel exec args %+v", terminals)
	}

	diagReq := httptest.NewRequest(http.MethodGet, "/v1/diagnostics/execs?limit=5", nil)
	diagReq.Header.Set("Authorization", "Bearer cli-token")
	diagRec := httptest.NewRecorder()
	handler.ServeHTTP(diagRec, diagReq)
	if diagRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", diagRec.Code, diagRec.Body.String())
	}
	if terminals.execDiagUser != "dev@example.com" || terminals.execDiagLimit != 5 {
		t.Fatalf("unexpected exec diagnostics args %+v", terminals)
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
