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

func (f *fakeRuntime) HealthCheck(context.Context) error {
	return f.healthErr
}

func (f *fakeRuntime) ListMachines(context.Context) ([]machineruntime.Machine, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.machines, nil
}

type fakeMachineManager struct {
	listOwnerEmail string
	listResult     []controlplane.Machine
	listErr        error
	getOwnerEmail  string
	getResult      controlplane.Machine
	getErr         error
	createInput    controlplane.CreateMachineInput
	createResult   controlplane.Machine
	createErr      error
	deleteName     string
	deleteOwner    string
	deleteErr      error
	cloneInput     controlplane.CloneMachineInput
	cloneResult    controlplane.Machine
	cloneErr       error
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

func (f *fakeMachineManager) CloneMachine(_ context.Context, input controlplane.CloneMachineInput) (controlplane.Machine, error) {
	f.cloneInput = input
	if f.cloneErr != nil {
		return controlplane.Machine{}, f.cloneErr
	}
	return f.cloneResult, nil
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

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
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

func TestCloneMachineEndpointReturnsNotFound(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, &fakeRuntime{}, &fakeMachineManager{cloneErr: database.ErrNotFound})

	req := httptest.NewRequest(http.MethodPost, "/v1/machines/habits/clone", bytes.NewBufferString(`{"target_name":"habits-v2","owner_email":"dev@example.com"}`))
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
				Name: "habits",
				IPv4: []string{host},
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
	if body := rec.Body.String(); !strings.Contains(body, "ssh -tt fascinate.dev shell habits") {
		t.Fatalf("expected shell command in body: %q", body)
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
				Name: "habits",
				IPv6: []string{host},
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

func newTestHandler(t *testing.T, runtime *fakeRuntime, machines *fakeMachineManager) http.Handler {
	t.Helper()
	return newTestHandlerWithConfig(t, config.Config{BaseDomain: "fascinate.dev"}, runtime, machines)
}

func newTestHandlerWithConfig(t *testing.T, cfg config.Config, runtime *fakeRuntime, machines *fakeMachineManager) http.Handler {
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

	return New(cfg, store, runtime, machines)
}
