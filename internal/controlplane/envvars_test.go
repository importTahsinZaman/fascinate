package controlplane

import (
	"context"
	"errors"
	"testing"

	"fascinate/internal/config"
	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

func TestValidateEnvVarKeyRejectsReservedPrefix(t *testing.T) {
	t.Parallel()

	if _, err := validateEnvVarKey("FASCINATE_PUBLIC_URL"); err == nil {
		t.Fatalf("expected reserved key rejection")
	}
}

func TestBuiltinEnvVarsMatchValidationBuiltins(t *testing.T) {
	t.Parallel()

	builtins := validationBuiltins(config.Config{})
	catalog := BuiltinEnvVars()
	if len(catalog) != len(builtins) {
		t.Fatalf("expected %d builtins in catalog, got %d", len(builtins), len(catalog))
	}

	for _, entry := range catalog {
		if entry.Description == "" {
			t.Fatalf("expected description for %q", entry.Key)
		}
		if _, ok := builtins[entry.Key]; !ok {
			t.Fatalf("catalog key %q missing from validation builtins", entry.Key)
		}
	}
}

func TestRenderEffectiveEnvInterpolatesBuiltinsAndUserVars(t *testing.T) {
	t.Parallel()

	values, err := renderEffectiveEnv(map[string]string{
		"FASCINATE_PUBLIC_URL": "https://m-1.fascinate.dev",
		"FASCINATE_MACHINE_ID": "machine-1",
	}, map[string]string{
		"FRONTEND_URL": "${FASCINATE_PUBLIC_URL}",
		"APP_URL":      "${FRONTEND_URL}/app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if values["FRONTEND_URL"] != "https://m-1.fascinate.dev" {
		t.Fatalf("unexpected FRONTEND_URL %q", values["FRONTEND_URL"])
	}
	if values["APP_URL"] != "https://m-1.fascinate.dev/app" {
		t.Fatalf("unexpected APP_URL %q", values["APP_URL"])
	}
	if values["FASCINATE_MACHINE_ID"] != "machine-1" {
		t.Fatalf("expected built-in to survive rendering")
	}
}

func TestRenderEffectiveEnvRejectsUndefinedReference(t *testing.T) {
	t.Parallel()

	_, err := renderEffectiveEnv(nil, map[string]string{
		"FRONTEND_URL": "${MISSING}",
	})
	if err == nil {
		t.Fatalf("expected undefined reference error")
	}
}

func TestRenderEffectiveEnvRejectsCycles(t *testing.T) {
	t.Parallel()

	_, err := renderEffectiveEnv(nil, map[string]string{
		"ONE": "${TWO}",
		"TWO": "${ONE}",
	})
	if err == nil {
		t.Fatalf("expected cycle error")
	}
}

func TestServiceGetMachineEnvCombinesBuiltinsAndUserVars(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

	if _, err := service.SetEnvVar(ctx, SetEnvVarInput{
		OwnerEmail: "dev@example.com",
		Key:        "FRONTEND_URL",
		Value:      "${FASCINATE_PUBLIC_URL}",
	}); err != nil {
		t.Fatal(err)
	}

	user, err := store.GetUserByEmail(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "m-1",
		OwnerUserID: user.ID,
		RuntimeName: "m-1",
		State:       machineStateRunning,
		PrimaryPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}

	env, err := service.GetMachineEnv(ctx, "m-1", "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if env.MachineName != "m-1" {
		t.Fatalf("unexpected machine env name %q", env.MachineName)
	}
	entries := map[string]string{}
	for _, entry := range env.Entries {
		entries[entry.Key] = entry.Value
	}
	if entries["FRONTEND_URL"] != "https://m-1.fascinate.dev" {
		t.Fatalf("unexpected FRONTEND_URL %q", entries["FRONTEND_URL"])
	}
	if entries["FASCINATE_MACHINE_ID"] != "machine-1" {
		t.Fatalf("unexpected FASCINATE_MACHINE_ID %q", entries["FASCINATE_MACHINE_ID"])
	}
}

func TestServiceSetEnvVarSyncsRunningMachines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

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
	runtime.machines["habits"] = machineruntime.Machine{Name: "habits", State: machineStateRunning}

	if _, err := service.SetEnvVar(ctx, SetEnvVarInput{
		OwnerEmail: "dev@example.com",
		Key:        "FRONTEND_URL",
		Value:      "${FASCINATE_PUBLIC_URL}",
	}); err != nil {
		t.Fatal(err)
	}

	waitForTestCondition(t, func() bool {
		_, ok := runtime.envSyncReq["habits"]
		return ok
	})
	if runtime.envSyncReq["habits"].Entries["FRONTEND_URL"] != "https://habits.fascinate.dev" {
		t.Fatalf("unexpected synced env %+v", runtime.envSyncReq["habits"].Entries)
	}
}

func TestServiceCreateMachineSyncsManagedEnv(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

	if _, err := service.SetEnvVar(ctx, SetEnvVarInput{
		OwnerEmail: "dev@example.com",
		Key:        "FRONTEND_URL",
		Value:      "${FASCINATE_PUBLIC_URL}",
	}); err != nil {
		t.Fatal(err)
	}

	created, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "space-shooter",
		OwnerEmail: "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	waitForTestCondition(t, func() bool {
		_, ok := runtime.envSyncReq["space-shooter"]
		return ok
	})

	req := runtime.envSyncReq["space-shooter"]
	if req.Entries["FASCINATE_MACHINE_NAME"] != created.Name {
		t.Fatalf("unexpected machine name env %+v", req.Entries)
	}
	if req.Entries["FRONTEND_URL"] != "https://space-shooter.fascinate.dev" {
		t.Fatalf("unexpected FRONTEND_URL %+v", req.Entries)
	}
}

func TestServiceCreateMachineFailsWhenManagedEnvSyncFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	runtime.envSyncErr = errors.New("sync failed")
	service := newTestService(store, runtime)

	created, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "space-shooter",
		OwnerEmail: "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	waitForTestCondition(t, func() bool {
		record, err := store.GetMachineByName(ctx, created.Name)
		return err == nil && record.State == machineStateFailed
	})
}

func TestServiceForkMachineSyncsManagedEnv(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

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
		Name:   "tic-tac-toe",
		State:  machineStateRunning,
		Disk:   "20GiB",
		CPU:    "1",
		Memory: "2GiB",
	}

	if _, err := service.SetEnvVar(ctx, SetEnvVarInput{
		OwnerEmail: "dev@example.com",
		Key:        "FRONTEND_URL",
		Value:      "${FASCINATE_PUBLIC_URL}",
	}); err != nil {
		t.Fatal(err)
	}

	forked, err := service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "tic-tac-toe",
		TargetName: "tic-tac-toe-v2",
		OwnerEmail: "dev@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	req, ok := runtime.envSyncReq["tic-tac-toe-v2"]
	if !ok {
		t.Fatalf("expected managed env sync for fork")
	}
	if req.Entries["FASCINATE_MACHINE_NAME"] != "tic-tac-toe-v2" {
		t.Fatalf("unexpected fork env %+v", req.Entries)
	}
	record, err := store.GetMachineByName(ctx, forked.Name)
	if err != nil {
		t.Fatal(err)
	}
	if req.Entries["FASCINATE_MACHINE_ID"] != record.ID {
		t.Fatalf("expected fork env to use target machine id, got %+v", req.Entries)
	}
}
