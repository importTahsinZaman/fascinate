package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAPITokenLifecycle(t *testing.T) {
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

	expiresAt := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	record, err := store.CreateAPIToken(ctx, CreateAPITokenParams{
		ID:        "token-1",
		UserID:    user.ID,
		Name:      "workstation",
		TokenHash: "hash-1",
		ExpiresAt: expiresAt,
		UserAgent: "Go test",
		IPAddress: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.UserEmail != "dev@example.com" {
		t.Fatalf("unexpected user email %q", record.UserEmail)
	}
	if record.Name != "workstation" {
		t.Fatalf("unexpected token name %q", record.Name)
	}

	active, err := store.GetActiveAPITokenByTokenHash(ctx, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != record.ID {
		t.Fatalf("unexpected token record %+v", active)
	}

	if err := store.TouchAPIToken(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeAPIToken(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetActiveAPITokenByTokenHash(ctx, "hash-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after revoke, got %v", err)
	}
}
