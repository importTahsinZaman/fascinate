package database

import (
	"context"
	"path/filepath"
	"testing"
)

func TestShellLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "fascinate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	user, err := store.UpsertUser(ctx, "dev@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := store.CreateMachine(ctx, CreateMachineParams{
		ID:             "machine-1",
		Name:           "m-1",
		OwnerUserID:    user.ID,
		RuntimeName:    "m-1",
		State:          "RUNNING",
		CPU:            "1",
		MemoryBytes:    1 << 30,
		DiskBytes:      10 << 30,
		DiskUsageBytes: 1 << 30,
		PrimaryPort:    8080,
	})
	if err != nil {
		t.Fatal(err)
	}

	record, err := store.CreateShell(ctx, CreateShellParams{
		ID:          "shell-1",
		UserID:      user.ID,
		MachineID:   machine.ID,
		Name:        "primary",
		TmuxSession: "fascinate-shell-1",
		State:       "READY",
		CWD:         "/home/ubuntu",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != "shell-1" || record.UserEmail != "dev@example.com" || record.MachineName != "m-1" {
		t.Fatalf("unexpected shell record %+v", record)
	}

	loaded, err := store.GetShellByID(ctx, "shell-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TmuxSession != "fascinate-shell-1" || loaded.CWD != "/home/ubuntu" {
		t.Fatalf("unexpected loaded shell %+v", loaded)
	}

	listed, err := store.ListShells(ctx, "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != "shell-1" {
		t.Fatalf("unexpected listed shells %+v", listed)
	}

	if err := store.TouchShellAttached(ctx, "shell-1"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateShellCWD(ctx, "shell-1", "/home/ubuntu/project"); err != nil {
		t.Fatal(err)
	}
	lastError := "attach failed"
	if err := store.UpdateShellState(ctx, "shell-1", "ERROR", &lastError); err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetShellByID(ctx, "shell-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != "ERROR" || updated.CWD != "/home/ubuntu/project" || updated.LastAttachedAt == nil || updated.LastError == nil || *updated.LastError != lastError {
		t.Fatalf("unexpected updated shell %+v", updated)
	}

	if err := store.MarkShellDeleted(ctx, "shell-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetShellByID(ctx, "shell-1"); err != ErrNotFound {
		t.Fatalf("expected deleted shell to be hidden, got %v", err)
	}
}
