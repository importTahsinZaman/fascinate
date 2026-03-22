package controlplane

import (
	"context"
	"testing"
	"time"

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

func TestServiceListHostsMarksPlacementIneligibleWhenCapacityExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

	if err := store.UpdateHostHeartbeat(ctx, database.UpdateHostHeartbeatParams{
		ID:                   "local-host",
		RuntimeVersion:       "cloud-hypervisor",
		Healthy:              true,
		TotalCPU:             1,
		AllocatedCPU:         1,
		TotalMemoryBytes:     2 << 30,
		AllocatedMemoryBytes: 2 << 30,
		TotalDiskBytes:       20 << 30,
		AllocatedDiskBytes:   20 << 30,
		AvailableDiskBytes:   0,
	}); err != nil {
		t.Fatal(err)
	}

	hosts, err := service.ListHosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].PlacementEligible {
		t.Fatalf("expected host to be placement-ineligible when default-machine capacity is exhausted")
	}
}

func TestServiceCreateMachineFailsWhenNoHostHasDefaultCapacity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

	if err := store.UpdateHostHeartbeat(ctx, database.UpdateHostHeartbeatParams{
		ID:                   "local-host",
		RuntimeVersion:       "cloud-hypervisor",
		Healthy:              true,
		TotalCPU:             1,
		AllocatedCPU:         1,
		TotalMemoryBytes:     2 << 30,
		AllocatedMemoryBytes: 2 << 30,
		TotalDiskBytes:       20 << 30,
		AllocatedDiskBytes:   20 << 30,
		AvailableDiskBytes:   0,
	}); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.CreateMachine(ctx, CreateMachineInput{
		Name:       "blocked",
		OwnerEmail: user.Email,
	})
	if err == nil {
		t.Fatal("expected create to fail when no host can fit the default machine")
	}
	if got := err.Error(); got != "no eligible hosts available for placement" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostPlacementEligibleUsesRequestedCapacity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, runtime := newTestServiceDeps(t, ctx)
	service := newTestService(store, runtime)

	record := database.HostRecord{
		Status:               hostStatusActive,
		HeartbeatAt:          ptrString(time.Now().UTC().Format(time.RFC3339)),
		TotalCPU:             4,
		AllocatedCPU:         3,
		TotalMemoryBytes:     8 << 30,
		AllocatedMemoryBytes: 7 << 30,
		TotalDiskBytes:       100 << 30,
		AllocatedDiskBytes:   60 << 30,
		AvailableDiskBytes:   19 << 30,
	}

	if service.hostPlacementEligible(record, "1", "2GiB", "20GiB") {
		t.Fatalf("expected requested machine not to fit")
	}
	if !service.hostPlacementEligible(record, "1", "1GiB", "10GiB") {
		t.Fatalf("expected smaller requested machine to fit")
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

func ptrString(value string) *string {
	return &value
}

func TestServiceForkPersistsSourceHostOwnership(t *testing.T) {
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

	fork, err := service.ForkMachine(ctx, ForkMachineInput{
		SourceName: "habits",
		TargetName: "habits-v2",
		OwnerEmail: user.Email,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fork.HostID != "local-host" {
		t.Fatalf("unexpected fork host id in response: %q", fork.HostID)
	}

	record, err := store.GetMachineByName(ctx, "habits-v2")
	if err != nil {
		t.Fatal(err)
	}
	if record.HostID == nil || *record.HostID != "local-host" {
		t.Fatalf("unexpected stored fork host id: %+v", record.HostID)
	}
}
