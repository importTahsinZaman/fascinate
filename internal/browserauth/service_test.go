package browserauth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"fascinate/internal/config"
	"fascinate/internal/database"
)

type fakeEmailSender struct {
	enabled bool
	email   string
	code    string
}

func (f *fakeEmailSender) Enabled() bool {
	return f.enabled
}

func (f *fakeEmailSender) SendSignupCode(_ context.Context, email, code string) error {
	f.email = email
	f.code = code
	return nil
}

func TestBrowserLoginLifecycle(t *testing.T) {
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

	sender := &fakeEmailSender{enabled: true}
	service := New(config.Config{
		SignupCodeTTL: 15 * time.Minute,
		WebSessionTTL: 24 * time.Hour,
		AdminEmails:   []string{"admin@example.com"},
	}, store, sender)

	if err := service.RequestCode(ctx, " Admin@Example.com "); err != nil {
		t.Fatal(err)
	}
	if sender.email != "admin@example.com" {
		t.Fatalf("expected normalized email, got %q", sender.email)
	}
	if len(sender.code) != 6 {
		t.Fatalf("expected 6-digit code, got %q", sender.code)
	}

	session, err := service.VerifyCode(ctx, "admin@example.com", sender.code, "Vitest", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if session.User.Email != "admin@example.com" {
		t.Fatalf("unexpected user email %q", session.User.Email)
	}
	if !session.User.IsAdmin {
		t.Fatalf("expected admin user")
	}
	if session.RawToken == "" {
		t.Fatalf("expected session token")
	}

	user, record, err := service.Authenticate(ctx, session.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != session.User.Email {
		t.Fatalf("unexpected authenticated user %q", user.Email)
	}
	if record.UserEmail != session.User.Email {
		t.Fatalf("unexpected record user email %q", record.UserEmail)
	}

	if err := service.Logout(ctx, session.RawToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Authenticate(ctx, session.RawToken); err != database.ErrNotFound {
		t.Fatalf("expected ErrNotFound after logout, got %v", err)
	}
}

func TestAPITokenLoginLifecycle(t *testing.T) {
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

	sender := &fakeEmailSender{enabled: true}
	service := New(config.Config{
		SignupCodeTTL: 15 * time.Minute,
		APITokenTTL:   7 * 24 * time.Hour,
		AdminEmails:   []string{"admin@example.com"},
	}, store, sender)

	if err := service.RequestCode(ctx, " Admin@Example.com "); err != nil {
		t.Fatal(err)
	}

	tokenSession, err := service.VerifyCodeForAPIToken(ctx, "admin@example.com", sender.code, "macbook", "Go test", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if tokenSession.User.Email != "admin@example.com" {
		t.Fatalf("unexpected user email %q", tokenSession.User.Email)
	}
	if tokenSession.Record.Name != "macbook" {
		t.Fatalf("unexpected token name %q", tokenSession.Record.Name)
	}
	if tokenSession.RawToken == "" {
		t.Fatalf("expected API token")
	}

	user, record, err := service.AuthenticateAPIToken(ctx, tokenSession.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != tokenSession.User.Email {
		t.Fatalf("unexpected authenticated user %q", user.Email)
	}
	if record.ID != tokenSession.Record.ID {
		t.Fatalf("unexpected token record %+v", record)
	}

	if err := service.LogoutAPIToken(ctx, tokenSession.RawToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.AuthenticateAPIToken(ctx, tokenSession.RawToken); err != database.ErrNotFound {
		t.Fatalf("expected ErrNotFound after token logout, got %v", err)
	}
}
