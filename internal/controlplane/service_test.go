package controlplane

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"fascinate/internal/config"
	"fascinate/internal/database"
	"fascinate/internal/runtime/incus"
)

type fakeRuntime struct {
	machines   map[string]incus.Machine
	createErr  error
	deleteErr  error
	cloneErr   error
	getErr     error
	listErr    error
	deleted    []string
	createdReq []incus.CreateMachineRequest
	clonedReq  []incus.CloneMachineRequest
}

func (f *fakeRuntime) HealthCheck(context.Context) error {
	return nil
}

func (f *fakeRuntime) ListMachines(context.Context) ([]incus.Machine, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	out := make([]incus.Machine, 0, len(f.machines))
	for _, machine := range f.machines {
		out = append(out, machine)
	}
	return out, nil
}

func (f *fakeRuntime) GetMachine(_ context.Context, name string) (incus.Machine, error) {
	if f.getErr != nil {
		return incus.Machine{}, f.getErr
	}

	machine, ok := f.machines[name]
	if !ok {
		return incus.Machine{}, incus.ErrMachineNotFound
	}
	return machine, nil
}

func (f *fakeRuntime) CreateMachine(_ context.Context, req incus.CreateMachineRequest) (incus.Machine, error) {
	if f.createErr != nil {
		return incus.Machine{}, f.createErr
	}

	f.createdReq = append(f.createdReq, req)
	machine := incus.Machine{
		Name:  req.Name,
		Type:  "container",
		State: "RUNNING",
	}
	f.machines[req.Name] = machine
	return machine, nil
}

func (f *fakeRuntime) DeleteMachine(_ context.Context, name string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}

	delete(f.machines, name)
	f.deleted = append(f.deleted, name)
	return nil
}

func (f *fakeRuntime) CloneMachine(_ context.Context, req incus.CloneMachineRequest) (incus.Machine, error) {
	if f.cloneErr != nil {
		return incus.Machine{}, f.cloneErr
	}

	f.clonedReq = append(f.clonedReq, req)
	source, ok := f.machines[req.SourceName]
	if !ok {
		return incus.Machine{}, incus.ErrMachineNotFound
	}

	clone := source
	clone.Name = req.TargetName
	f.machines[req.TargetName] = clone
	return clone, nil
}

func TestServiceCreateCloneAndDeleteMachine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := database.Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		AdminEmails:        []string{"admin@example.com"},
		DefaultImage:       "images:ubuntu/24.04",
		IncusStoragePool:   "machines",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultPrimaryPort: 3000,
	}, store, &fakeRuntime{machines: map[string]incus.Machine{}})

	created, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "Habits",
		OwnerEmail: "admin@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "habits" {
		t.Fatalf("expected normalized name, got %q", created.Name)
	}
	if created.URL != "https://habits.fascinate.dev" {
		t.Fatalf("unexpected machine url: %q", created.URL)
	}

	cloned, err := service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cloned.Name != "habits-v2" {
		t.Fatalf("unexpected clone name: %q", cloned.Name)
	}

	list, err := service.ListMachines(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 machines, got %d", len(list))
	}

	if err := service.DeleteMachine(ctx, "habits"); err != nil {
		t.Fatal(err)
	}

	list, err = service.ListMachines(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 machine after delete, got %d", len(list))
	}
}

func TestServiceCreateMachineRollsBackRuntimeOnDBConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		IncusName:   "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	service := newTestService(store, runtime)
	_, err = service.CreateMachine(ctx, CreateMachineInput{
		Name:       "habits",
		OwnerEmail: "dev@example.com",
	})
	if !errors.Is(err, database.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if len(runtime.deleted) != 1 || runtime.deleted[0] != "habits" {
		t.Fatalf("expected runtime cleanup of habits, got %+v", runtime.deleted)
	}
}

func TestServiceCloneMachineRollsBackRuntimeOnDBConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-source",
		Name:        "habits",
		OwnerUserID: user.ID,
		IncusName:   "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-target",
		Name:        "habits-v2",
		OwnerUserID: user.ID,
		IncusName:   "habits-v2",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.machines["habits"] = incus.Machine{Name: "habits", Type: "container", State: "RUNNING"}
	service := newTestService(store, runtime)

	_, err = service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
	})
	if !errors.Is(err, database.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if len(runtime.deleted) != 1 || runtime.deleted[0] != "habits-v2" {
		t.Fatalf("expected runtime cleanup of habits-v2, got %+v", runtime.deleted)
	}
}

func TestServiceGetMachineMarksMissingWhenRuntimeDoesNotHaveIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		IncusName:   "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.getErr = incus.ErrMachineNotFound
	service := newTestService(store, runtime)

	machine, err := service.GetMachine(ctx, "habits")
	if err != nil {
		t.Fatal(err)
	}
	if machine.State != "missing" {
		t.Fatalf("expected missing state, got %q", machine.State)
	}

	record, err := store.GetMachineByName(ctx, "habits")
	if err != nil {
		t.Fatal(err)
	}
	if record.State != "missing" {
		t.Fatalf("expected persisted missing state, got %q", record.State)
	}
}

func newTestServiceDeps(t *testing.T, ctx context.Context) (*database.Store, *fakeRuntime) {
	t.Helper()

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

	return store, &fakeRuntime{machines: map[string]incus.Machine{}}
}

func newTestService(store *database.Store, runtime *fakeRuntime) *Service {
	return New(config.Config{
		BaseDomain:         "fascinate.dev",
		AdminEmails:        []string{"admin@example.com"},
		DefaultImage:       "images:ubuntu/24.04",
		IncusStoragePool:   "machines",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultPrimaryPort: 3000,
	}, store, runtime)
}
