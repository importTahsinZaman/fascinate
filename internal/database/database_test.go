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
		RuntimeName:   "habits",
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
