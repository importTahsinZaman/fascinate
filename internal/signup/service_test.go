package signup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"fascinate/internal/config"
	"fascinate/internal/database"
)

type fakeSender struct {
	lastEmail string
	lastCode  string
}

func (f *fakeSender) Enabled() bool {
	return true
}

func (f *fakeSender) SendSignupCode(_ context.Context, email, code string) error {
	f.lastEmail = email
	f.lastCode = code
	return nil
}

func TestRequestCodeAndVerify(t *testing.T) {
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

	sender := &fakeSender{}
	service := New(config.Config{
		AdminEmails:   []string{"admin@example.com"},
		SignupCodeTTL: 15 * time.Minute,
	}, store, sender)

	if err := service.RequestCode(ctx, "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	if sender.lastEmail != "admin@example.com" || sender.lastCode == "" {
		t.Fatalf("unexpected sender payload: %+v", sender)
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatal(err)
	}

	user, err := service.VerifyAndRegisterKey(ctx, "admin@example.com", sender.lastCode, string(ssh.MarshalAuthorizedKey(publicKey)))
	if err != nil {
		t.Fatal(err)
	}
	if !user.IsAdmin {
		t.Fatalf("expected admin user")
	}

	record, err := store.GetSSHKeyByFingerprint(ctx, ssh.FingerprintSHA256(publicKey))
	if err != nil {
		t.Fatal(err)
	}
	if record.UserEmail != "admin@example.com" {
		t.Fatalf("unexpected ssh key owner: %q", record.UserEmail)
	}
}
