package database

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestMachineRecordLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	if !user.IsAdmin {
		t.Fatalf("expected admin user")
	}
	if user.TutorialCompletedAt != nil {
		t.Fatalf("expected tutorial to start incomplete")
	}

	if err := store.MarkUserTutorialCompleted(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	user, err = store.GetUserByEmail(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if user.TutorialCompletedAt == nil {
		t.Fatalf("expected tutorial completion timestamp")
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	authorizedKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatal(err)
	}

	sshKeyRecord, err := store.CreateSSHKey(ctx, CreateSSHKeyParams{
		UserID:      user.ID,
		Name:        "laptop",
		PublicKey:   string(ssh.MarshalAuthorizedKey(authorizedKey)),
		Fingerprint: ssh.FingerprintSHA256(authorizedKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sshKeyRecord.UserEmail != "dev@example.com" {
		t.Fatalf("unexpected ssh key owner email: %q", sshKeyRecord.UserEmail)
	}

	record, err := store.CreateMachine(ctx, CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.GetMachineByName(ctx, "habits")
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerEmail != "dev@example.com" {
		t.Fatalf("unexpected owner email: %q", got.OwnerEmail)
	}

	list, err := store.ListMachines(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 machine, got %d", len(list))
	}

	if err := store.UpdateMachineState(ctx, record.ID, "STOPPED"); err != nil {
		t.Fatal(err)
	}

	got, err = store.GetMachineByName(ctx, "habits")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "STOPPED" {
		t.Fatalf("unexpected machine state: %q", got.State)
	}

	if err := store.MarkMachineDeleted(ctx, record.ID); err != nil {
		t.Fatal(err)
	}

	_, err = store.GetMachineByName(ctx, "habits")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestHostLifecycleAndOwnershipAssignment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	machine, err := store.CreateMachine(ctx, CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		RuntimeName: "habits",
		State:       "RUNNING",
		PrimaryPort: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.CreateSnapshot(ctx, CreateSnapshotParams{
		ID:              "snapshot-1",
		Name:            "baseline",
		OwnerUserID:     user.ID,
		RuntimeName:     "snapshot-runtime",
		State:           "READY",
		SourceMachineID: &machine.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostID != nil {
		t.Fatalf("expected snapshot host to start nil, got %+v", snapshot.HostID)
	}

	host, err := store.UpsertHost(ctx, UpsertHostParams{
		ID:               "ovh-bhs-01",
		Name:             "ovh-bhs-01",
		Region:           "ca-east",
		Role:             "combined",
		Status:           "ACTIVE",
		LabelsJSON:       `{"provider":"ovh"}`,
		CapabilitiesJSON: `["vm","snapshot","clone"]`,
		RuntimeVersion:   "cloud-hypervisor 45.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if host.ID != "ovh-bhs-01" {
		t.Fatalf("unexpected host id: %q", host.ID)
	}

	if err := store.AssignHostToMachinesWithoutHost(ctx, host.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AssignHostToSnapshotsWithoutHost(ctx, host.ID); err != nil {
		t.Fatal(err)
	}

	machine, err = store.GetMachineByName(ctx, "habits")
	if err != nil {
		t.Fatal(err)
	}
	if machine.HostID == nil || *machine.HostID != host.ID {
		t.Fatalf("unexpected machine host id: %+v", machine.HostID)
	}

	snapshot, err = store.GetSnapshotByName(ctx, user.ID, "baseline")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostID == nil || *snapshot.HostID != host.ID {
		t.Fatalf("unexpected snapshot host id: %+v", snapshot.HostID)
	}

	if err := store.UpdateHostHeartbeat(ctx, UpdateHostHeartbeatParams{
		ID:                   host.ID,
		RuntimeVersion:       "cloud-hypervisor 45.0",
		Healthy:              true,
		TotalCPU:             24,
		AllocatedCPU:         1,
		TotalMemoryBytes:     125 << 30,
		AllocatedMemoryBytes: 2 << 30,
		TotalDiskBytes:       900 << 30,
		AllocatedDiskBytes:   20 << 30,
		AvailableDiskBytes:   800 << 30,
		MachineCount:         1,
		SnapshotCount:        1,
	}); err != nil {
		t.Fatal(err)
	}

	hosts, err := store.ListHosts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].HeartbeatAt == nil {
		t.Fatalf("expected heartbeat timestamp")
	}
}

func TestUserEnvVarLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	record, err := store.UpsertUserEnvVar(ctx, UpsertEnvVarParams{
		UserID:   user.ID,
		Key:      "FRONTEND_URL",
		RawValue: "${FASCINATE_PUBLIC_URL}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Key != "FRONTEND_URL" {
		t.Fatalf("unexpected env key %q", record.Key)
	}

	record, err = store.UpsertUserEnvVar(ctx, UpsertEnvVarParams{
		UserID:   user.ID,
		Key:      "FRONTEND_URL",
		RawValue: "https://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.RawValue != "https://example.com" {
		t.Fatalf("unexpected env value %q", record.RawValue)
	}

	records, err := store.ListUserEnvVars(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 env var, got %d", len(records))
	}

	if err := store.DeleteUserEnvVar(ctx, user.ID, "FRONTEND_URL"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetUserEnvVar(ctx, user.ID, "FRONTEND_URL"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestWebSessionLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	record, err := store.CreateWebSession(ctx, CreateWebSessionParams{
		UserID:    user.ID,
		TokenHash: "token-hash",
		ExpiresAt: "2099-01-01 00:00:00",
		UserAgent: "Vitest",
		IPAddress: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.UserEmail != "dev@example.com" {
		t.Fatalf("unexpected session owner %q", record.UserEmail)
	}

	record, err = store.GetActiveWebSessionByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatal(err)
	}
	if record.LastSeenAt == "" {
		t.Fatalf("expected last_seen_at to be populated")
	}

	if err := store.TouchWebSession(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeWebSession(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetActiveWebSessionByTokenHash(ctx, "token-hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected revoked session to disappear, got %v", err)
	}
}

func TestWorkspaceLayoutLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}

	record, err := store.UpsertWorkspaceLayout(ctx, UpsertWorkspaceLayoutParams{
		UserID:     user.ID,
		Name:       "default",
		LayoutJSON: `{"version":1,"windows":[{"id":"one"}]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Name != "default" {
		t.Fatalf("unexpected layout name %q", record.Name)
	}

	record, err = store.UpsertWorkspaceLayout(ctx, UpsertWorkspaceLayoutParams{
		UserID:     user.ID,
		Name:       "default",
		LayoutJSON: `{"version":1,"windows":[{"id":"two"}]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.LayoutJSON != `{"version":1,"windows":[{"id":"two"}]}` {
		t.Fatalf("unexpected layout json %q", record.LayoutJSON)
	}

	record, err = store.GetWorkspaceLayout(ctx, user.ID, "default")
	if err != nil {
		t.Fatal(err)
	}
	if record.UserEmail != "dev@example.com" {
		t.Fatalf("unexpected layout owner %q", record.UserEmail)
	}
}
