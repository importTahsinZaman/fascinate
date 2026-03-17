package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
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

	record, err := store.CreateMachine(ctx, CreateMachineParams{
		ID:          "machine-1",
		Name:        "habits",
		OwnerUserID: user.ID,
		IncusName:   "habits",
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
