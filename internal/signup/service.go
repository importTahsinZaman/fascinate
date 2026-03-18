package signup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"fascinate/internal/config"
	"fascinate/internal/database"
)

const emailPurposeSignup = "signup"

type EmailSender interface {
	Enabled() bool
	SendSignupCode(context.Context, string, string) error
}

type Service struct {
	cfg    config.Config
	store  *database.Store
	sender EmailSender
}

func New(cfg config.Config, store *database.Store, sender EmailSender) *Service {
	return &Service{
		cfg:    cfg,
		store:  store,
		sender: sender,
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.sender != nil && s.sender.Enabled()
}

func (s *Service) RequestCode(ctx context.Context, email string) error {
	if !s.Enabled() {
		return fmt.Errorf("signup email delivery is not configured")
	}

	email = normalizeEmail(email)
	if email == "" {
		return fmt.Errorf("email is required")
	}

	code, err := randomDigits(6)
	if err != nil {
		return err
	}

	if _, err := s.store.CreateEmailCode(ctx, database.CreateEmailCodeParams{
		Email:     email,
		Purpose:   emailPurposeSignup,
		CodeHash:  hashCode(code),
		ExpiresAt: now().Add(s.cfg.SignupCodeTTL),
	}); err != nil {
		return err
	}

	return s.sender.SendSignupCode(ctx, email, code)
}

func (s *Service) VerifyAndRegisterKey(ctx context.Context, email, code, publicKey string) (database.User, error) {
	email = normalizeEmail(email)
	code = strings.TrimSpace(code)
	publicKey = strings.TrimSpace(publicKey)
	if email == "" || code == "" || publicKey == "" {
		return database.User{}, fmt.Errorf("email, code, and public key are required")
	}

	if err := s.store.ConsumeEmailCode(ctx, email, emailPurposeSignup, hashCode(code)); err != nil {
		return database.User{}, fmt.Errorf("invalid or expired verification code")
	}

	user, err := s.store.UpsertUser(ctx, email, isAdminEmail(s.cfg.AdminEmails, email))
	if err != nil {
		return database.User{}, err
	}

	authorizedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return database.User{}, err
	}

	fingerprint := ssh.FingerprintSHA256(authorizedKey)
	keyName := "key-" + shortFingerprint(fingerprint)

	if _, err := s.store.CreateSSHKey(ctx, database.CreateSSHKeyParams{
		UserID:      user.ID,
		Name:        keyName,
		PublicKey:   publicKey,
		Fingerprint: fingerprint,
	}); err != nil {
		if err == database.ErrConflict {
			record, lookupErr := s.store.GetSSHKeyByFingerprint(ctx, fingerprint)
			if lookupErr == nil && record.UserEmail == user.Email {
				return user, nil
			}
		}
		return database.User{}, err
	}

	return user, nil
}

var now = func() time.Time {
	return time.Now().UTC()
}

func randomDigits(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	out := make([]byte, length)
	for i, value := range bytes {
		out[i] = '0' + (value % 10)
	}

	return string(out), nil
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func shortFingerprint(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), "SHA256:")
	if len(value) <= 12 {
		return strings.ToLower(value)
	}
	return strings.ToLower(value[:12])
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isAdminEmail(adminEmails []string, email string) bool {
	email = normalizeEmail(email)
	for _, candidate := range adminEmails {
		if normalizeEmail(candidate) == email {
			return true
		}
	}

	return false
}
