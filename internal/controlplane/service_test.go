package controlplane

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

type fakeToolAuth struct {
	restoreUserID    string
	restoreRuntime   string
	restoreGuestUser string
	restoreErr       error
	restoreCalls     []string

	captureUserID    string
	captureRuntime   string
	captureGuestUser string
	captureErr       error
	captureCalls     []string
	callOrder        []string
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

func (f *fakeRuntime) CreateMachine(ctx context.Context, req machineruntime.CreateMachineRequest) (machineruntime.Machine, error) {
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
		select {
		case <-f.createBlock:
		case <-ctx.Done():
			return machineruntime.Machine{}, ctx.Err()
		}
	}

	f.createdReq = append(f.createdReq, req)
	machine := machineruntime.Machine{
		Name:   req.Name,
		Type:   "vm",
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

func (f *fakeToolAuth) RestoreAll(_ context.Context, userID, runtimeName, guestUser string) error {
	f.restoreUserID = userID
	f.restoreRuntime = runtimeName
	f.restoreGuestUser = guestUser
	f.restoreCalls = append(f.restoreCalls, runtimeName)
	f.callOrder = append(f.callOrder, "restore:"+runtimeName)
	return f.restoreErr
}

func (f *fakeToolAuth) CaptureAll(_ context.Context, userID, runtimeName, guestUser string) error {
	f.captureUserID = userID
	f.captureRuntime = runtimeName
	f.captureGuestUser = guestUser
	f.captureCalls = append(f.captureCalls, runtimeName)
	f.callOrder = append(f.callOrder, "capture:"+runtimeName)
	return f.captureErr
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
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
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
	if created.State != machineStateCreating {
		t.Fatalf("expected create to return %s, got %q", machineStateCreating, created.State)
	}
	if created.URL != "https://habits.fascinate.dev" {
		t.Fatalf("unexpected machine url: %q", created.URL)
	}
	waitForTestCondition(t, func() bool { return len(runtime.createdReq) == 1 })
	if runtime.createdReq[0].RootDiskSize != "20GiB" {
		t.Fatalf("expected create request disk size 20GiB, got %+v", runtime.createdReq)
	}
	waitForTestCondition(t, func() bool {
		record, err := store.GetMachineByName(ctx, "habits")
		return err == nil && strings.EqualFold(record.State, machineStateRunning)
	})

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

func TestServiceCreateMachineRestoresToolAuthBeforeRunning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	auth := &fakeToolAuth{}

	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultMachineDisk: "20GiB",
		DefaultPrimaryPort: 3000,
		GuestSSHUser:       "ubuntu",
	}, store, runtime, auth)

	created, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "space-shooter",
		OwnerEmail: "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForTestCondition(t, func() bool {
		return auth.restoreRuntime == "space-shooter"
	})
	if auth.restoreGuestUser != "ubuntu" {
		t.Fatalf("expected ubuntu guest user, got %q", auth.restoreGuestUser)
	}

	record, err := store.GetMachineByName(ctx, created.Name)
	if err != nil {
		t.Fatal(err)
	}
	if auth.restoreUserID != record.OwnerUserID {
		t.Fatalf("expected restore user %q, got %q", record.OwnerUserID, auth.restoreUserID)
	}
}

func TestServiceCreateMachineSyncsOwnerToolAuthBeforeRestore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	auth := &fakeToolAuth{}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "tic-tac-toe",
		OwnerUserID: user.ID,
		RuntimeName: "tic-tac-toe",
		State:       machineStateRunning,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	runtime.machines["tic-tac-toe"] = machineruntime.Machine{
		Name:      "tic-tac-toe",
		Type:      "vm",
		State:     machineStateRunning,
		GuestUser: "ubuntu",
	}

	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultMachineDisk: "20GiB",
		DefaultPrimaryPort: 3000,
		GuestSSHUser:       "ubuntu",
	}, store, runtime, auth)

	if _, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "space-shooter",
		OwnerEmail: "dev@example.com",
	}); err != nil {
		t.Fatal(err)
	}

	waitForTestCondition(t, func() bool {
		return len(auth.callOrder) >= 2
	})
	if len(auth.captureCalls) != 1 || auth.captureCalls[0] != "tic-tac-toe" {
		t.Fatalf("expected capture from existing running machine, got %+v", auth.captureCalls)
	}
	if len(auth.restoreCalls) != 1 || auth.restoreCalls[0] != "space-shooter" {
		t.Fatalf("expected restore into created machine, got %+v", auth.restoreCalls)
	}
	if auth.callOrder[0] != "capture:tic-tac-toe" || auth.callOrder[1] != "restore:space-shooter" {
		t.Fatalf("expected capture before restore, got %+v", auth.callOrder)
	}
}

func TestServiceCreateMachineFailsWhenToolAuthRestoreFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	auth := &fakeToolAuth{restoreErr: errors.New("restore failed")}

	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultMachineDisk: "20GiB",
		DefaultPrimaryPort: 3000,
		GuestSSHUser:       "ubuntu",
	}, store, runtime, auth)

	created, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "space-shooter",
		OwnerEmail: "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	waitForTestCondition(t, func() bool {
		record, err := store.GetMachineByName(ctx, created.Name)
		return err == nil && strings.EqualFold(record.State, machineStateFailed)
	})
	if len(runtime.deleted) != 1 || runtime.deleted[0] != "space-shooter" {
		t.Fatalf("expected failed create cleanup, got %+v", runtime.deleted)
	}
}

func TestServiceDeleteMachineCapturesToolAuthBestEffort(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	auth := &fakeToolAuth{captureErr: errors.New("boom")}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       machineStateRunning,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	runtime.machines["habits"] = machineruntime.Machine{
		Name:      "habits",
		Type:      "vm",
		State:     machineStateRunning,
		GuestUser: "ubuntu",
	}

	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultMachineDisk: "20GiB",
		DefaultPrimaryPort: 3000,
		GuestSSHUser:       "ubuntu",
	}, store, runtime, auth)

	if err := service.DeleteMachine(ctx, "habits", "dev@example.com"); err != nil {
		t.Fatal(err)
	}
	if auth.captureRuntime != "habits" || auth.captureUserID != user.ID {
		t.Fatalf("expected capture before delete, got runtime=%q user=%q", auth.captureRuntime, auth.captureUserID)
	}
	if len(runtime.deleted) != 1 || runtime.deleted[0] != "habits" {
		t.Fatalf("expected runtime delete, got %+v", runtime.deleted)
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
	if len(runtime.deleted) != 0 {
		t.Fatalf("expected no runtime cleanup on preflight conflict, got %+v", runtime.deleted)
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

	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
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

func TestServiceReconcileRuntimeStateQueuesCreatingMachines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-creating",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       machineStateCreating,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.createStarted = make(chan struct{}, 1)

	service := newTestService(store, runtime)
	if err := service.ReconcileRuntimeState(ctx); err != nil {
		t.Fatal(err)
	}

	waitForTestCondition(t, func() bool {
		return len(runtime.createdReq) == 1
	})
	if runtime.createdReq[0].Name != "habits" {
		t.Fatalf("expected queued create for habits, got %+v", runtime.createdReq)
	}
}

func TestMachineFromRecordDoesNotPromoteCreatingFromRuntimeLiveness(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := New(config.Config{
		BaseDomain:         "fascinate.dev",
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:  "1",
		DefaultMachineRAM:  "2GiB",
		DefaultMachineDisk: "20GiB",
		DefaultPrimaryPort: 3000,
	}, store, runtime)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	record, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-creating",
		Name:        "space-shooter",
		OwnerUserID: user.ID,
		RuntimeName: "space-shooter",
		State:       machineStateCreating,
		PrimaryPort: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}

	runtimeMachine := machineruntime.Machine{
		Name:      "space-shooter",
		Type:      "vm",
		State:     "RUNNING",
		CPU:       "1",
		Memory:    "2GiB",
		Disk:      "20GiB",
		IPv4:      []string{"10.42.0.11"},
		GuestUser: "ubuntu",
	}

	machine := service.machineFromRecord(ctx, record, runtimeMachine)
	if machine.State != machineStateCreating {
		t.Fatalf("expected machine to remain %s, got %q", machineStateCreating, machine.State)
	}

	persisted, err := store.GetMachineByName(ctx, "space-shooter")
	if err != nil {
		t.Fatal(err)
	}
	if persisted.State != machineStateCreating {
		t.Fatalf("expected persisted state to remain %s, got %q", machineStateCreating, persisted.State)
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

func TestServiceGetMachineKeepsCreatingStateWhenRuntimeMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-creating",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       machineStateCreating,
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
	if machine.State != machineStateCreating {
		t.Fatalf("expected creating state, got %q", machine.State)
	}

	record, err := store.GetMachineByName(ctx, "habits")
	if err != nil {
		t.Fatal(err)
	}
	if record.State != machineStateCreating {
		t.Fatalf("expected persisted creating state, got %q", record.State)
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

	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
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
		runtime.machines[name] = machineruntime.Machine{Name: name, Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
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
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
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

	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "vm", State: "RUNNING", CPU: "4", Memory: "2GiB", Disk: "20GiB"}
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
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
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

	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "25GiB"}
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
		runtime.machines[name] = machineruntime.Machine{Name: name, Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
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
	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}

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
	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}

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
	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}

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
		DefaultImage:       "/var/lib/fascinate/images/fascinate-base.raw",
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

func waitForTestCondition(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("condition not met before timeout")
}
