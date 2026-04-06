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
	"fascinate/internal/toolauth"
)

type fakeRuntime struct {
	machines      map[string]machineruntime.Machine
	snapshots     map[string]machineruntime.Snapshot
	createErr     error
	envSyncErr    error
	startErr      error
	deleteErr     error
	forkErr       error
	getErr        error
	listErr       error
	createStarted chan struct{}
	createBlock   <-chan struct{}
	forkDelay     time.Duration
	deleted       []string
	createdReq    []machineruntime.CreateMachineRequest
	envSyncReq    map[string]machineruntime.ManagedEnvRequest
	started       []string
	forkedReq     []machineruntime.ForkMachineRequest
}

type fakeToolAuth struct {
	restoreUserID    string
	restoreRuntime   string
	restoreGuestUser string
	restoreErr       error
	restoreCalls     []string
	restoreStarted   chan struct{}
	restoreBlock     chan struct{}

	captureUserID    string
	captureRuntime   string
	captureGuestUser string
	captureErr       error
	captureCalls     []string
	captureMode      string
	callOrder        []string
	captureStarted   chan struct{}
	captureBlock     chan struct{}

	profiles        []toolauth.Profile
	listProfilesErr error
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

func (f *fakeRuntime) SyncManagedEnv(_ context.Context, name string, req machineruntime.ManagedEnvRequest) error {
	if f.envSyncErr != nil {
		return f.envSyncErr
	}
	if f.envSyncReq == nil {
		f.envSyncReq = map[string]machineruntime.ManagedEnvRequest{}
	}
	copyEntries := make(map[string]string, len(req.Entries))
	for key, value := range req.Entries {
		copyEntries[key] = value
	}
	f.envSyncReq[name] = machineruntime.ManagedEnvRequest{Entries: copyEntries}
	return nil
}

func (f *fakeRuntime) StartMachine(_ context.Context, name string) (machineruntime.Machine, error) {
	if f.startErr != nil {
		return machineruntime.Machine{}, f.startErr
	}

	machine, ok := f.machines[name]
	if !ok {
		return machineruntime.Machine{}, machineruntime.ErrMachineNotFound
	}
	machine.State = machineStateRunning
	f.machines[name] = machine
	f.started = append(f.started, name)
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

func (f *fakeRuntime) ForkMachine(ctx context.Context, req machineruntime.ForkMachineRequest) (machineruntime.Machine, error) {
	if f.forkErr != nil {
		return machineruntime.Machine{}, f.forkErr
	}
	if f.forkDelay > 0 {
		select {
		case <-time.After(f.forkDelay):
		case <-ctx.Done():
			return machineruntime.Machine{}, ctx.Err()
		}
	}

	f.forkedReq = append(f.forkedReq, req)
	source, ok := f.machines[req.SourceName]
	if !ok {
		return machineruntime.Machine{}, machineruntime.ErrMachineNotFound
	}

	fork := source
	fork.Name = req.TargetName
	f.machines[req.TargetName] = fork
	return fork, nil
}

func (f *fakeRuntime) ListSnapshots(context.Context) ([]machineruntime.Snapshot, error) {
	out := make([]machineruntime.Snapshot, 0, len(f.snapshots))
	for _, snapshot := range f.snapshots {
		out = append(out, snapshot)
	}
	return out, nil
}

func (f *fakeRuntime) GetSnapshot(_ context.Context, name string) (machineruntime.Snapshot, error) {
	snapshot, ok := f.snapshots[name]
	if !ok {
		return machineruntime.Snapshot{}, machineruntime.ErrSnapshotNotFound
	}
	return snapshot, nil
}

func (f *fakeRuntime) CreateSnapshot(_ context.Context, req machineruntime.CreateSnapshotRequest) (machineruntime.Snapshot, error) {
	if f.snapshots == nil {
		f.snapshots = map[string]machineruntime.Snapshot{}
	}
	snapshot := machineruntime.Snapshot{
		Name:              req.SnapshotName,
		SourceMachineName: req.MachineName,
		State:             "READY",
		ArtifactDir:       req.ArtifactDir,
	}
	f.snapshots[req.SnapshotName] = snapshot
	return snapshot, nil
}

func (f *fakeRuntime) DeleteSnapshot(_ context.Context, name string) error {
	delete(f.snapshots, name)
	return nil
}

func (f *fakeToolAuth) RestoreAll(_ context.Context, userID, runtimeName, guestUser string) error {
	f.restoreUserID = userID
	f.restoreRuntime = runtimeName
	f.restoreGuestUser = guestUser
	f.restoreCalls = append(f.restoreCalls, runtimeName)
	f.callOrder = append(f.callOrder, "restore:"+runtimeName)
	if f.restoreStarted != nil {
		select {
		case f.restoreStarted <- struct{}{}:
		default:
		}
	}
	if f.restoreBlock != nil {
		<-f.restoreBlock
	}
	return f.restoreErr
}

func (f *fakeToolAuth) CaptureAll(_ context.Context, userID, runtimeName, guestUser string) error {
	f.captureUserID = userID
	f.captureRuntime = runtimeName
	f.captureGuestUser = guestUser
	f.captureMode = "exact"
	f.captureCalls = append(f.captureCalls, runtimeName)
	f.callOrder = append(f.callOrder, "capture:exact:"+runtimeName)
	if f.captureStarted != nil {
		select {
		case f.captureStarted <- struct{}{}:
		default:
		}
	}
	if f.captureBlock != nil {
		<-f.captureBlock
	}
	return f.captureErr
}

func (f *fakeToolAuth) CaptureAllNonDestructive(_ context.Context, userID, runtimeName, guestUser string) error {
	f.captureUserID = userID
	f.captureRuntime = runtimeName
	f.captureGuestUser = guestUser
	f.captureMode = "preserve"
	f.captureCalls = append(f.captureCalls, runtimeName)
	f.callOrder = append(f.callOrder, "capture:preserve:"+runtimeName)
	if f.captureStarted != nil {
		select {
		case f.captureStarted <- struct{}{}:
		default:
		}
	}
	if f.captureBlock != nil {
		<-f.captureBlock
	}
	return f.captureErr
}

func (f *fakeToolAuth) ListProfiles(_ context.Context, userID string) ([]toolauth.Profile, error) {
	if f.listProfilesErr != nil {
		return nil, f.listProfilesErr
	}
	var profiles []toolauth.Profile
	for _, profile := range f.profiles {
		if strings.TrimSpace(profile.Key.UserID) == strings.TrimSpace(userID) {
			profiles = append(profiles, profile)
		}
	}
	return profiles, nil
}

func TestServiceCreateForkAndDeleteMachine(t *testing.T) {
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

	forked, err := service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "admin@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if forked.Name != "habits-v2" {
		t.Fatalf("unexpected fork name: %q", forked.Name)
	}
	if len(runtime.forkedReq) != 1 || runtime.forkedReq[0].RootDiskSize != "20GiB" {
		t.Fatalf("expected fork request disk size 20GiB, got %+v", runtime.forkedReq)
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
	if auth.captureMode != "preserve" {
		t.Fatalf("expected non-destructive capture mode, got %q", auth.captureMode)
	}
	if len(auth.restoreCalls) != 1 || auth.restoreCalls[0] != "space-shooter" {
		t.Fatalf("expected restore into created machine, got %+v", auth.restoreCalls)
	}
	if auth.callOrder[0] != "capture:preserve:tic-tac-toe" || auth.callOrder[1] != "restore:space-shooter" {
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

func TestServiceCreateMachineFromSnapshotSkipsToolAuthSyncAndRestore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	auth := &fakeToolAuth{}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSnapshot(ctx, database.CreateSnapshotParams{
		ID:          "snapshot-1",
		Name:        "baseline",
		OwnerUserID: user.ID,
		RuntimeName: "snapshot-runtime-1",
		State:       snapshotStateReady,
		ArtifactDir: filepath.Join(t.TempDir(), "snapshot-runtime-1"),
	}); err != nil {
		t.Fatal(err)
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

	created, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:         "space-shooter",
		OwnerEmail:   "dev@example.com",
		SnapshotName: "baseline",
	})
	if err != nil {
		t.Fatal(err)
	}

	waitForTestCondition(t, func() bool { return len(runtime.createdReq) == 1 })
	if runtime.createdReq[0].Snapshot != "snapshot-runtime-1" {
		t.Fatalf("expected snapshot restore create request, got %+v", runtime.createdReq[0])
	}
	if len(auth.captureCalls) != 0 {
		t.Fatalf("expected no pre-create auth sync, got %+v", auth.captureCalls)
	}
	if len(auth.restoreCalls) != 0 {
		t.Fatalf("expected no auth restore after snapshot create, got %+v", auth.restoreCalls)
	}

	record, err := store.GetMachineByName(ctx, created.Name)
	if err != nil {
		t.Fatal(err)
	}
	if record.SourceSnapshotID == nil || strings.TrimSpace(*record.SourceSnapshotID) == "" {
		t.Fatalf("expected machine source snapshot to be recorded, got %+v", record)
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
	if auth.captureMode != "exact" {
		t.Fatalf("expected exact capture before delete, got %q", auth.captureMode)
	}
	if len(runtime.deleted) != 1 || runtime.deleted[0] != "habits" {
		t.Fatalf("expected runtime delete, got %+v", runtime.deleted)
	}
}

func TestServiceRejectsLoadingMachineActions(t *testing.T) {
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
		State:       machineStateCreating,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	service := newTestService(store, runtime)

	if err := service.DeleteMachine(ctx, "habits", "dev@example.com"); err == nil || !strings.Contains(err.Error(), "still creating") {
		t.Fatalf("expected delete to reject creating machine, got %v", err)
	}
	if _, err := service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "habits",
		TargetName: "habits-copy",
		OwnerEmail: "dev@example.com",
	}); err == nil || !strings.Contains(err.Error(), "still creating") {
		t.Fatalf("expected fork to reject creating machine, got %v", err)
	}
	if _, err := service.CreateSnapshot(ctx, CreateSnapshotInput{
		MachineName:  "habits",
		SnapshotName: "habits-snapshot",
		OwnerEmail:   "dev@example.com",
	}); err == nil || !strings.Contains(err.Error(), "still creating") {
		t.Fatalf("expected snapshot to reject creating machine, got %v", err)
	}

	if len(runtime.deleted) != 0 {
		t.Fatalf("expected no runtime deletes, got %+v", runtime.deleted)
	}
	if len(runtime.forkedReq) != 0 {
		t.Fatalf("expected no runtime forks, got %+v", runtime.forkedReq)
	}
}

func TestServiceForkMachineDoesNotRestoreToolAuth(t *testing.T) {
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
		Disk:      "20GiB",
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

	forked, err := service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "tic-tac-toe",
		TargetName: "space-shooter",
		OwnerEmail: "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if forked.Name != "space-shooter" {
		t.Fatalf("unexpected fork name %q", forked.Name)
	}
	if len(auth.restoreCalls) != 0 {
		t.Fatalf("expected fork to skip tool-auth restore, got %+v", auth.restoreCalls)
	}
}

func TestServiceSyncRunningToolAuthUsesNonDestructiveCapture(t *testing.T) {
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

	if err := service.SyncRunningToolAuth(ctx); err != nil {
		t.Fatal(err)
	}
	if auth.captureRuntime != "tic-tac-toe" {
		t.Fatalf("expected sync from tic-tac-toe, got %q", auth.captureRuntime)
	}
	if auth.captureMode != "preserve" {
		t.Fatalf("expected non-destructive sync mode, got %q", auth.captureMode)
	}
}

func TestServiceCreateMachineDoesNotBlockOtherUsersOnToolAuthSync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	auth := &fakeToolAuth{
		captureStarted: make(chan struct{}, 1),
		captureBlock:   make(chan struct{}),
	}

	userOne, err := store.UpsertUser(ctx, "one@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "existing-one",
		OwnerUserID: userOne.ID,
		RuntimeName: "existing-one",
		State:       machineStateRunning,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	runtime.machines["existing-one"] = machineruntime.Machine{
		Name:      "existing-one",
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

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.CreateMachine(ctx, CreateMachineInput{
			Name:       "user-one-vm",
			OwnerEmail: "one@example.com",
		})
		firstDone <- err
	}()

	select {
	case <-auth.captureStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first user's tool auth sync to start")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := service.CreateMachine(ctx, CreateMachineInput{
			Name:       "user-two-vm",
			OwnerEmail: "two@example.com",
		})
		secondDone <- err
	}()

	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second user create failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("second user create blocked behind first user's tool auth sync")
	}

	close(auth.captureBlock)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first user create failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first user create to finish")
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

func TestServiceForkMachineRollsBackRuntimeOnDBConflict(t *testing.T) {
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

	_, err = service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "dev@example.com",
	})
	if !errors.Is(err, database.ErrConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if len(runtime.deleted) != 0 {
		t.Fatalf("expected no runtime cleanup before fork starts, got %+v", runtime.deleted)
	}
}

func TestServiceForkMachineUsesFreshPersistContextAfterLongRuntime(t *testing.T) {
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
		State:       machineStateRunning,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.machines["habits"] = machineruntime.Machine{
		Name:   "habits",
		Type:   "vm",
		State:  machineStateRunning,
		CPU:    "1",
		Memory: "2GiB",
		Disk:   "20GiB",
	}
	runtime.forkDelay = 25 * time.Millisecond

	service := newTestService(store, runtime)
	service.persistTTL = 10 * time.Millisecond

	forked, err := service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "dev@example.com",
	})
	if err != nil {
		t.Fatalf("expected fork success after long runtime, got %v", err)
	}
	if forked.Name != "habits-v2" || forked.State != machineStateRunning {
		t.Fatalf("unexpected fork result: %+v", forked)
	}
	if len(runtime.deleted) != 0 {
		t.Fatalf("expected no runtime cleanup on successful fork, got %+v", runtime.deleted)
	}

	record, err := store.GetMachineByName(ctx, "habits-v2")
	if err != nil {
		t.Fatal(err)
	}
	if record.State != machineStateRunning {
		t.Fatalf("expected persisted fork state %q, got %q", machineStateRunning, record.State)
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

func TestServiceReconcileRuntimeStateRestartsStoppedMachines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-stopped",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       machineStateRunning,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	runtime.machines["habits"] = machineruntime.Machine{
		Name:   "habits",
		Type:   "vm",
		State:  machineStateStopped,
		CPU:    "1",
		Memory: "2GiB",
		Disk:   "20GiB",
	}

	service := newTestService(store, runtime)
	if err := service.ReconcileRuntimeState(ctx); err != nil {
		t.Fatal(err)
	}
	if len(runtime.started) != 1 || runtime.started[0] != "habits" {
		t.Fatalf("expected stopped machine restart, got %+v", runtime.started)
	}

	record, err := store.GetMachineByName(ctx, "habits")
	if err != nil {
		t.Fatal(err)
	}
	if record.State != machineStateRunning {
		t.Fatalf("expected machine state RUNNING after reconcile, got %q", record.State)
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

	if _, err := service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "other@example.com",
	}); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("expected not found for fork, got %v", err)
	}
	if _, ok := runtime.machines["habits-v2"]; ok {
		t.Fatalf("expected no fork to be created")
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
	for _, name := range []string{"four", "five", "six"} {
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
		Name:       "seven",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "maximum 6 machines per user") {
		t.Fatalf("expected machine quota error, got %v", err)
	}
	if len(runtime.createdReq) != 0 {
		t.Fatalf("expected no runtime create, got %+v", runtime.createdReq)
	}
}

func TestServiceRejectsCreateWhenUserCPUBudgetIsExceeded(t *testing.T) {
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
			State:       machineStateRunning,
			CPU:         "1",
			MemoryBytes: 2 << 30,
			DiskBytes:   20 << 30,
			PrimaryPort: 3000,
		}); err != nil {
			t.Fatal(err)
		}
		runtime.machines[name] = machineruntime.Machine{Name: name, Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
	}

	service := New(config.Config{
		BaseDomain:              "fascinate.dev",
		DefaultImage:            "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:       "1",
		DefaultMachineRAM:       "2GiB",
		DefaultMachineDisk:      "20GiB",
		DefaultUserMaxCPU:       "2",
		DefaultUserMaxRAM:       "8GiB",
		DefaultUserMaxDisk:      "100GiB",
		DefaultUserMaxMachines:  25,
		DefaultUserMaxSnapshots: 5,
		MaxMachineCPU:           "2",
		MaxMachineRAM:           "4GiB",
		MaxMachineDisk:          "20GiB",
		HostMinFreeDisk:         "0B",
		DefaultPrimaryPort:      3000,
	}, store, runtime)

	_, err = service.CreateMachine(ctx, CreateMachineInput{
		Name:       "three",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "user cpu budget exceeded") {
		t.Fatalf("expected cpu budget error, got %v", err)
	}
	if len(runtime.createdReq) != 0 {
		t.Fatalf("expected no runtime create, got %+v", runtime.createdReq)
	}
}

func TestServiceRejectsCreateSnapshotWhenSnapshotBudgetIsExceeded(t *testing.T) {
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
		State:       machineStateRunning,
		CPU:         "1",
		MemoryBytes: 2 << 30,
		DiskBytes:   20 << 30,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", Type: "vm", State: "RUNNING", CPU: "1", Memory: "2GiB", Disk: "20GiB"}
	if _, err := store.CreateSnapshot(ctx, database.CreateSnapshotParams{
		ID:              "snapshot-1",
		Name:            "baseline",
		OwnerUserID:     user.ID,
		SourceMachineID: ptrString("machine-1"),
		RuntimeName:     "snapshot-1",
		State:           snapshotStateReady,
		CPU:             "1",
		MemoryBytes:     2 << 30,
		DiskBytes:       20 << 30,
		ArtifactDir:     filepath.Join(t.TempDir(), "snapshot-1"),
		DiskSizeBytes:   20 << 30,
		MemorySizeBytes: 2 << 30,
	}); err != nil {
		t.Fatal(err)
	}

	service := New(config.Config{
		BaseDomain:              "fascinate.dev",
		DefaultImage:            "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:       "1",
		DefaultMachineRAM:       "2GiB",
		DefaultMachineDisk:      "20GiB",
		DefaultUserMaxCPU:       "2",
		DefaultUserMaxRAM:       "8GiB",
		DefaultUserMaxDisk:      "100GiB",
		DefaultUserMaxMachines:  25,
		DefaultUserMaxSnapshots: 1,
		MaxMachineCPU:           "2",
		MaxMachineRAM:           "4GiB",
		MaxMachineDisk:          "20GiB",
		HostMinFreeDisk:         "0B",
		DefaultPrimaryPort:      3000,
	}, store, runtime)

	_, err = service.CreateSnapshot(ctx, CreateSnapshotInput{
		MachineName:  "habits",
		SnapshotName: "second",
		OwnerEmail:   "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "snapshot quota exceeded") {
		t.Fatalf("expected snapshot quota error, got %v", err)
	}
}

func TestServiceRejectsCreateWhenHostFreeDiskHeadroomWouldBeViolated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)

	service := New(config.Config{
		BaseDomain:              "fascinate.dev",
		DefaultImage:            "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:       "1",
		DefaultMachineRAM:       "2GiB",
		DefaultMachineDisk:      "20GiB",
		DefaultUserMaxCPU:       "2",
		DefaultUserMaxRAM:       "8GiB",
		DefaultUserMaxDisk:      "100GiB",
		DefaultUserMaxMachines:  25,
		DefaultUserMaxSnapshots: 5,
		MaxMachineCPU:           "2",
		MaxMachineRAM:           "4GiB",
		MaxMachineDisk:          "20GiB",
		HostMinFreeDisk:         "10GiB",
		DefaultPrimaryPort:      3000,
	}, store, runtime)

	if err := store.UpdateHostHeartbeat(ctx, database.UpdateHostHeartbeatParams{
		ID:                   "local-host",
		RuntimeVersion:       "cloud-hypervisor",
		Healthy:              true,
		TotalCPU:             24,
		AllocatedCPU:         0,
		TotalMemoryBytes:     125 << 30,
		AllocatedMemoryBytes: 0,
		TotalDiskBytes:       100 << 30,
		AllocatedDiskBytes:   0,
		AvailableDiskBytes:   25 << 30,
	}); err != nil {
		t.Fatal(err)
	}

	_, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "blocked",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "no eligible hosts available for placement") {
		t.Fatalf("expected placement rejection, got %v", err)
	}
}

func TestServiceBudgetDiagnosticsReportsUsage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

	user, err := service.ensureUser(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       machineStateRunning,
		CPU:         "1",
		MemoryBytes: 2 << 30,
		DiskBytes:   20 << 30,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSnapshot(ctx, database.CreateSnapshotParams{
		ID:              "snapshot-1",
		Name:            "baseline",
		OwnerUserID:     user.ID,
		RuntimeName:     "snapshot-1",
		State:           snapshotStateReady,
		CPU:             "1",
		MemoryBytes:     2 << 30,
		DiskBytes:       20 << 30,
		ArtifactDir:     filepath.Join(t.TempDir(), "snapshot-1"),
		DiskSizeBytes:   20 << 30,
		MemorySizeBytes: 2 << 30,
	}); err != nil {
		t.Fatal(err)
	}

	diag, err := service.GetBudgetDiagnostics(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if diag.Limits.CPU != "6" {
		t.Fatalf("unexpected cpu limit %+v", diag)
	}
	if diag.Usage.CPU != "1" || diag.Usage.MemoryBytes != 2<<30 || diag.Usage.MachineCount != 1 || diag.Usage.SnapshotCount != 1 {
		t.Fatalf("unexpected usage %+v", diag)
	}
	if diag.Usage.DiskBytes != (20<<30)+(20<<30)+(2<<30) {
		t.Fatalf("unexpected disk usage %+v", diag)
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

func TestServiceRejectsForkWhenSourceExceedsSizeLimit(t *testing.T) {
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

	_, err = service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "cpu 4 > 2") {
		t.Fatalf("expected fork size error, got %v", err)
	}
	if len(runtime.forkedReq) != 0 {
		t.Fatalf("expected no runtime fork, got %+v", runtime.forkedReq)
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

func TestServiceRejectsForkWhenSourceDiskExceedsSizeLimit(t *testing.T) {
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

	_, err = service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: "dev@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "disk 25GiB > 20GiB") {
		t.Fatalf("expected fork disk size error, got %v", err)
	}
	if len(runtime.forkedReq) != 0 {
		t.Fatalf("expected no runtime fork, got %+v", runtime.forkedReq)
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

	for _, name := range []string{"one", "two", "three", "four", "five"} {
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
	go create("six")
	go create("seven")
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
		if strings.Contains(err.Error(), "maximum 6 machines per user") {
			quotaErrors++
			continue
		}
		t.Fatalf("unexpected error: %v", err)
	}

	if successCount != 1 || quotaErrors != 1 {
		t.Fatalf("expected one success and one quota error, got success=%d quota=%d", successCount, quotaErrors)
	}
}

func TestServiceConcurrentCreatesAcrossUsersRespectHostCapacity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

	if err := store.UpdateHostHeartbeat(ctx, database.UpdateHostHeartbeatParams{
		ID:                   "local-host",
		RuntimeVersion:       "cloud-hypervisor",
		Healthy:              true,
		TotalCPU:             1,
		AllocatedCPU:         0,
		TotalMemoryBytes:     8 << 30,
		AllocatedMemoryBytes: 0,
		TotalDiskBytes:       100 << 30,
		AllocatedDiskBytes:   0,
		AvailableDiskBytes:   100 << 30,
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	create := func(name, email string) {
		defer wg.Done()
		<-start
		_, err := service.CreateMachine(ctx, CreateMachineInput{
			Name:       name,
			OwnerEmail: email,
		})
		results <- err
	}

	wg.Add(2)
	go create("alpha", "a@example.com")
	go create("bravo", "b@example.com")
	close(start)
	wg.Wait()
	close(results)

	var successCount int
	var hostCapacityErrors int
	for err := range results {
		if err == nil {
			successCount++
			continue
		}
		if strings.Contains(err.Error(), "host local-host lacks cpu capacity") {
			hostCapacityErrors++
			continue
		}
		t.Fatalf("unexpected create error: %v", err)
	}

	if successCount != 1 || hostCapacityErrors != 1 {
		t.Fatalf("expected one success and one host-capacity error, got success=%d host_capacity=%d", successCount, hostCapacityErrors)
	}

	records, err := store.ListMachines(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected exactly one persisted machine reservation, got %d", len(records))
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

func TestServiceForkMachineMarksTutorialCompleted(t *testing.T) {
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
	if _, err := service.ForkMachine(ctx, ForkMachineInput{
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
		t.Fatalf("expected tutorial to be marked complete after fork")
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
		BaseDomain:              "fascinate.dev",
		AdminEmails:             []string{"admin@example.com"},
		DefaultImage:            "/var/lib/fascinate/images/fascinate-base.raw",
		DefaultMachineCPU:       "1",
		DefaultMachineRAM:       "2GiB",
		DefaultMachineDisk:      "20GiB",
		DefaultUserMaxCPU:       "6",
		DefaultUserMaxRAM:       "16GiB",
		DefaultUserMaxDisk:      "200GiB",
		DefaultUserMaxMachines:  6,
		DefaultUserMaxSnapshots: 5,
		MaxMachinesPerUser:      6,
		MaxMachineCPU:           "2",
		MaxMachineRAM:           "4GiB",
		MaxMachineDisk:          "20GiB",
		HostMinFreeDisk:         "0B",
		DefaultPrimaryPort:      3000,
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
