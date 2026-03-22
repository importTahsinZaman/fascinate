package controlplane

import (
	"context"
	"testing"

	"fascinate/internal/database"
	machineruntime "fascinate/internal/runtime"
)

func TestServiceRegistersAndHeartbeatsLocalHost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

	hosts, err := service.ListHosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}

	host := hosts[0]
	if host.ID != "local-host" {
		t.Fatalf("unexpected host id: %q", host.ID)
	}
	if host.Status != hostStatusActive {
		t.Fatalf("unexpected host status: %q", host.Status)
	}
	if !host.HeartbeatFresh {
		t.Fatalf("expected fresh heartbeat")
	}
	if !host.PlacementEligible {
		t.Fatalf("expected placement-eligible host")
	}
	if host.Role != "combined" {
		t.Fatalf("unexpected host role: %q", host.Role)
	}
}

func TestServicePersistsMachineAndSnapshotHostOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	service := newTestService(store, runtime)

	machine, err := service.CreateMachine(ctx, CreateMachineInput{
		Name:       "habits",
		OwnerEmail: user.Email,
	})
	if err != nil {
		t.Fatal(err)
	}
	if machine.HostID != "local-host" {
		t.Fatalf("unexpected machine host id in response: %q", machine.HostID)
	}

	waitForTestCondition(t, func() bool {
		record, err := store.GetMachineByName(ctx, "habits")
		if err != nil {
			return false
		}
		return record.State == machineStateRunning
	})

	record, err := store.GetMachineByName(ctx, "habits")
	if err != nil {
		t.Fatal(err)
	}
	if record.HostID == nil || *record.HostID != "local-host" {
		t.Fatalf("unexpected stored machine host id: %+v", record.HostID)
	}

	snapshot, err := service.CreateSnapshot(ctx, CreateSnapshotInput{
		MachineName:  "habits",
		SnapshotName: "habits-snap",
		OwnerEmail:   user.Email,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostID != "local-host" {
		t.Fatalf("unexpected snapshot host id in response: %q", snapshot.HostID)
	}

	waitForTestCondition(t, func() bool {
		record, err := store.GetSnapshotByName(ctx, user.ID, "habits-snap")
		if err != nil {
			return false
		}
		return record.State == snapshotStateReady
	})

	snapshotRecord, err := store.GetSnapshotByName(ctx, user.ID, "habits-snap")
	if err != nil {
		t.Fatal(err)
	}
	if snapshotRecord.HostID == nil || *snapshotRecord.HostID != "local-host" {
		t.Fatalf("unexpected stored snapshot host id: %+v", snapshotRecord.HostID)
	}
}

func TestServiceClonePersistsSourceHostOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	service := newTestService(store, runtime)
	hostID := "local-host"
	if _, err := store.CreateMachine(ctx, database.CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		HostID:      &hostID,
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

	clone, err := service.CloneMachine(ctx, CloneMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: user.Email,
	})
	if err != nil {
		t.Fatal(err)
	}
	if clone.HostID != "local-host" {
		t.Fatalf("unexpected clone host id in response: %q", clone.HostID)
	}

	record, err := store.GetMachineByName(ctx, "habits-v2")
	if err != nil {
		t.Fatal(err)
	}
	if record.HostID == nil || *record.HostID != "local-host" {
		t.Fatalf("unexpected stored clone host id: %+v", record.HostID)
	}
}
