package controlplane

import (
	"context"
	"testing"

	"fascinate/internal/config"
	"fascinate/internal/database"
	"fascinate/internal/runtime/incus"
)

type fakeRuntime struct {
	machines map[string]incus.Machine
}

func (f *fakeRuntime) HealthCheck(context.Context) error {
	return nil
}

func (f *fakeRuntime) ListMachines(context.Context) ([]incus.Machine, error) {
	out := make([]incus.Machine, 0, len(f.machines))
	for _, machine := range f.machines {
		out = append(out, machine)
	}
	return out, nil
}

func (f *fakeRuntime) GetMachine(_ context.Context, name string) (incus.Machine, error) {
	machine, ok := f.machines[name]
	if !ok {
		return incus.Machine{}, incus.ErrMachineNotFound
	}
	return machine, nil
}

func (f *fakeRuntime) CreateMachine(_ context.Context, req incus.CreateMachineRequest) (incus.Machine, error) {
	machine := incus.Machine{
		Name:  req.Name,
		Type:  "container",
		State: "RUNNING",
	}
	f.machines[req.Name] = machine
	return machine, nil
}

func (f *fakeRuntime) DeleteMachine(_ context.Context, name string) error {
	delete(f.machines, name)
	return nil
}

func (f *fakeRuntime) CloneMachine(_ context.Context, req incus.CloneMachineRequest) (incus.Machine, error) {
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
	store, err := database.Open(ctx, t.TempDir()+"/fascinate.db")
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
