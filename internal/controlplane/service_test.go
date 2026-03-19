package controlplane

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fascinate/internal/config"
	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

type fakeRuntime struct {
	machines      map[string]machineruntime.Machine
	createErr     error
	deleteErr     error
	cloneErr      error
	getErr        error
	listErr       error
	createStarted chan struct{}
	createBlock   <-chan struct{}
	deleted       []string
	createdReq    []machineruntime.CreateMachineRequest
	clonedReq     []machineruntime.CloneMachineRequest
}

func (f *fakeRuntime) HealthCheck(context.Context) error {
	return nil
}

func (f *fakeRuntime) ListMachines(context.Context) ([]machineruntime.Machine, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	out := make([]machineruntime.Machine, 0, len(f.machines))
	for _, machine := range f.machines {
		out = append(out, machine)
	}
	return out, nil
}

func (f *fakeRuntime) GetMachine(_ context.Context, name string) (machineruntime.Machine, error) {
	if f.getErr != nil {
		return machineruntime.Machine{}, f.getErr
	}

	machine, ok := f.machines[name]
	if !ok {
		return machineruntime.Machine{}, machineruntime.ErrMachineNotFound
	}
	return machine, nil
}

func (f *fakeRuntime) CreateMachine(_ context.Context, req machineruntime.CreateMachineRequest) (machineruntime.Machine, error) {
	if f.createErr != nil {
		return machineruntime.Machine{}, f.createErr
	}
	if f.createStarted != nil {
		select {
		case f.createStarted <- struct{}{}:
		default:
		}
	}
	if f.createBlock != nil {
		<-f.createBlock
	}

	f.createdReq = append(f.createdReq, req)
	machine := machineruntime.Machine{
		Name:   req.Name,
		Type:   "container",
		State:  "RUNNING",
		CPU:    req.CPU,
		Memory: req.Memory,
		Disk:   req.RootDiskSize,
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

func (f *fakeRuntime) CloneMachine(_ context.Context, req machineruntime.CloneMachineRequest) (machineruntime.Machine, error) {
	if f.cloneErr != nil {
		return machineruntime.Machine{}, f.cloneErr
	}

	f.clonedReq = append(f.clonedReq, req)
	source, ok := f.machines[req.SourceName]
	if !ok {
		return machineruntime.Machine{}, machineruntime.ErrMachineNotFound
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

	runtime := &fakeRuntime{machines: map[string]machineruntime.Machine{}}
	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		AdminEmails:        []string{"admin@example.com"},
		DefaultImage:       "images:ubuntu/24.04",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultMachineDisk: "20GiB",
		DefaultPrimaryPort: 3000,
	}, store, runtime)

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
	if len(runtime.createdReq) != 1 || runtime.createdReq[0].RootDiskSize != "20GiB" {
		t.Fatalf("expected create request disk size 20GiB, got %+v", runtime.createdReq)
	}

	cloned, err := service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "admin@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cloned.Name != "habits-v2" {
		t.Fatalf("unexpected clone name: %q", cloned.Name)
	}
	if len(runtime.clonedReq) != 1 || runtime.clonedReq[0].RootDiskSize != "20GiB" {
		t.Fatalf("expected clone request disk size 20GiB, got %+v", runtime.clonedReq)
	}

	list, err := service.ListMachines(ctx, "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 machines, got %d", len(list))
	}

	if err := service.DeleteMachine(ctx, "habits", "admin@example.com"); err != nil {
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
		RuntimeName: "habits",
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
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-target",
		Name:        "habits-v2",
		OwnerUserID: user.ID,
		RuntimeName: "habits-v2",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "container", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
	service := newTestService(store, runtime)

	_, err = service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "dev@example.com",
	})
	if !errors.Is(err, database.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if len(runtime.deleted) != 1 || runtime.deleted[0] != "habits-v2" {
		t.Fatalf("expected runtime cleanup of habits-v2, got %+v", runtime.deleted)
	}
}

func TestServiceReconcileRuntimeStateDeletesOrphans(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	runtime.machines["orphan-vm"] = machineruntime.Machine{
		Name:   "orphan-vm",
		Type:   "vm",
		State:  "RUNNING",
		CPU:    "1",
		Memory: "2GiB",
		Disk:   "20GiB",
	}

	service := newTestService(store, runtime)
	if err := service.ReconcileRuntimeState(ctx); err != nil {
		t.Fatal(err)
	}
	if len(runtime.deleted) != 1 || runtime.deleted[0] != "orphan-vm" {
		t.Fatalf("expected orphan runtime cleanup, got %+v", runtime.deleted)
	}
}

func TestServiceCreateMachineReconcilesRuntimeBeforeCreate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	runtime.machines["orphan-vm"] = machineruntime.Machine{
		Name:   "orphan-vm",
		Type:   "vm",
		State:  "RUNNING",
		CPU:    "1",
		Memory: "2GiB",
		Disk:   "20GiB",
	}

	service := newTestService(store, runtime)
	_, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "habits",
		OwnerEmail: "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.deleted) == 0 || runtime.deleted[0] != "orphan-vm" {
		t.Fatalf("expected orphan cleanup before create, got %+v", runtime.deleted)
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
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.getErr = machineruntime.ErrMachineNotFound
	service := newTestService(store, runtime)

	machine, err := service.GetMachine(ctx, "habits", "dev@example.com")
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

func TestServiceRejectsWrongOwnerForSensitiveOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "owner@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "container", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
	service := newTestService(store, runtime)

	if _, err := service.GetMachine(ctx, "habits", "other@example.com"); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("expected not found for get, got %v", err)
	}

	if err := service.DeleteMachine(ctx, "habits", "other@example.com"); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("expected not found for delete, got %v", err)
	}
	if len(runtime.deleted) != 0 {
		t.Fatalf("expected no runtime deletes, got %+v", runtime.deleted)
	}

	if _, err := service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "other@example.com",
	}); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("expected not found for clone, got %v", err)
	}
	if _, ok := runtime.machines["habits-v2"]; ok {
		t.Fatalf("expected no clone to be created")
	}
}

func TestServiceEnforcesMaxMachinesPerUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"one", "two", "three"} {
		if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
			ID:          "machine-" + name,
			Name:        name,
			OwnerUserID: user.ID,
			RuntimeName: name,
			State:       "RUNNING",
			PrimaryPort: 3000,
		}); err != nil {
			t.Fatal(err)
		}
		runtime.machines[name] = machineruntime.Machine{Name: name, Type: "container", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
	}

	service := newTestService(store, runtime)

	_, err = service.CreateMachine(ctx, CreateMachineInput{
		Name:       "four",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "maximum 3 machines per user") {
		t.Fatalf("expected machine quota error, got %v", err)
	}
	if len(runtime.createdReq) != 0 {
		t.Fatalf("expected no runtime create, got %+v", runtime.createdReq)
	}
}

func TestServiceRejectsOversizedMachineResources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		DefaultImage:       "images:ubuntu/24.04",
		DefaultMachineCPU:  "3",
		DefaultMachineRAM:  "8GiB",
		DefaultMachineDisk: "20GiB",
		MaxMachinesPerUser: 3,
		MaxMachineCPU:      "2",
		MaxMachineRAM:      "4GiB",
		MaxMachineDisk:     "20GiB",
		DefaultPrimaryPort: 3000,
	}, store, runtime)

	_, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "habits",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "cpu 3 > 2") {
		t.Fatalf("expected cpu size error, got %v", err)
	}
	if len(runtime.createdReq) != 0 {
		t.Fatalf("expected no runtime create, got %+v", runtime.createdReq)
	}
}

func TestServiceRejectsCloneWhenSourceExceedsSizeLimit(t *testing.T) {
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
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "container", State: "RUNNING", CPU: "4", Memory: "2GiB", Disk: "20GiB"}
	service := newTestService(store, runtime)

	_, err = service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "cpu 4 > 2") {
		t.Fatalf("expected clone size error, got %v", err)
	}
	if len(runtime.clonedReq) != 0 {
		t.Fatalf("expected no runtime clone, got %+v", runtime.clonedReq)
	}
}

func TestServiceRejectsOversizedMachineDisk(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		DefaultImage:       "images:ubuntu/24.04",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultMachineDisk: "25GiB",
		MaxMachinesPerUser: 3,
		MaxMachineCPU:      "2",
		MaxMachineRAM:      "4GiB",
		MaxMachineDisk:     "20GiB",
		DefaultPrimaryPort: 3000,
	}, store, runtime)

	_, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "habits",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "disk 25GiB > 20GiB") {
		t.Fatalf("expected disk size error, got %v", err)
	}
	if len(runtime.createdReq) != 0 {
		t.Fatalf("expected no runtime create, got %+v", runtime.createdReq)
	}
}

func TestServiceRejectsCloneWhenSourceDiskExceedsSizeLimit(t *testing.T) {
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
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "container", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "25GiB"}
	service := newTestService(store, runtime)

	_, err = service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "disk 25GiB > 20GiB") {
		t.Fatalf("expected clone disk size error, got %v", err)
	}
	if len(runtime.clonedReq) != 0 {
		t.Fatalf("expected no runtime clone, got %+v", runtime.clonedReq)
	}
}

func TestServiceSerializesQuotaCheckedCreates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"one", "two"} {
		if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
			ID:          "machine-" + name,
			Name:        name,
			OwnerUserID: user.ID,
			RuntimeName: name,
			State:       "RUNNING",
			PrimaryPort: 3000,
		}); err != nil {
			t.Fatal(err)
		}
		runtime.machines[name] = machineruntime.Machine{Name: name, Type: "container", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
	}

	started := make(chan struct{}, 1)
	block := make(chan struct{})
	runtime.createStarted = started
	runtime.createBlock = block
	service := newTestService(store, runtime)

	var wg sync.WaitGroup
	results := make(chan error, 2)
	create := func(name string) {
		defer wg.Done()
		_, err := service.CreateMachine(ctx, CreateMachineInput{
			Name:       name,
			OwnerEmail: "dev@example.com",
		})
		results <- err
	}

	wg.Add(2)
	go create("three")
	<-started
	go create("four")
	close(block)
	wg.Wait()
	close(results)

	var successCount int
	var quotaErrors int
	for err := range results {
		if err == nil {
			successCount++
			continue
		}
		if strings.Contains(err.Error(), "maximum 3 machines per user") {
			quotaErrors++
			continue
		}
		t.Fatalf("unexpected error: %v", err)
	}

	if successCount != 1 || quotaErrors != 1 {
		t.Fatalf("expected one success and one quota error, got success=%d quota=%d", successCount, quotaErrors)
	}
}

func TestServiceShowsTutorialForSingleFirstMachine(t *testing.T) {
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
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "container", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}

	service := newTestService(store, runtime)
	machines, err := service.ListMachines(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(machines) != 1 || !machines[0].ShowTutorial {
		t.Fatalf("expected tutorial to be offered on first machine, got %+v", machines)
	}
}

func TestServiceCreateMachineMarksTutorialCompletedAfterSecondMachine(t *testing.T) {
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
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "container", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}

	service := newTestService(store, runtime)
	if _, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "notes",
		OwnerEmail: "dev@example.com",
	}); err != nil {
		t.Fatal(err)
	}

	updatedUser, err := store.GetUserByEmail(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if updatedUser.TutorialCompletedAt == nil {
		t.Fatalf("expected tutorial to be marked complete after second machine")
	}
}

func TestServiceCloneMachineMarksTutorialCompleted(t *testing.T) {
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
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "container", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}

	service := newTestService(store, runtime)
	if _, err := service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "dev@example.com",
	}); err != nil {
		t.Fatal(err)
	}

	updatedUser, err := store.GetUserByEmail(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if updatedUser.TutorialCompletedAt == nil {
		t.Fatalf("expected tutorial to be marked complete after clone")
	}
}

func TestServiceCompleteTutorialMarksUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	if _, err := store.UpsertUser(ctx, "dev@example.com", false); err != nil {
		t.Fatal(err)
	}

	service := newTestService(store, runtime)
	if err := service.CompleteTutorial(ctx, "dev@example.com"); err != nil {
		t.Fatal(err)
	}

	user, err := store.GetUserByEmail(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if user.TutorialCompletedAt == nil {
		t.Fatalf("expected tutorial completion timestamp")
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

	return store, &fakeRuntime{machines: map[string]machineruntime.Machine{}}
}

func newTestService(store *database.Store, runtime *fakeRuntime) *Service {
	return New(config.Config{
		BaseDomain:         "fascinate.dev",
		AdminEmails:        []string{"admin@example.com"},
		DefaultImage:       "images:ubuntu/24.04",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultMachineDisk: "20GiB",
		MaxMachinesPerUser: 3,
		MaxMachineCPU:      "2",
		MaxMachineRAM:      "4GiB",
		MaxMachineDisk:     "20GiB",
		DefaultPrimaryPort: 3000,
	}, store, runtime)
}
