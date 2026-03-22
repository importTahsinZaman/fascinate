package browserauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"fascinate/internal/config"
	"fascinate/internal/database"
)

const emailPurposeBrowserLogin = "browser-login"

type EmailSender interface {
	Enabled() bool
	SendSignupCode(context.Context, string, string) error
}

type Session struct {
	User      database.User             `json:"user"`
	Record    database.WebSessionRecord `json:"record"`
	RawToken  string                    `json:"-"`
	ExpiresAt time.Time                 `json:"expires_at"`
}

type Service struct {
	cfg    config.Config
	store  *database.Store
	sender EmailSender
}

func New(cfg config.Config, store *database.Store, sender EmailSender) *Service {
	return &Service{cfg: cfg, store: store, sender: sender}
}

func (s *Service) Enabled() bool {
	return s != nil && s.sender != nil && s.sender.Enabled()
}

func (s *Service) RequestCode(ctx context.Context, email string) error {
	if !s.Enabled() {
		return fmt.Errorf("browser email delivery is not configured")
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
		Purpose:   emailPurposeBrowserLogin,
		CodeHash:  hashCode(code),
		ExpiresAt: time.Now().UTC().Add(s.cfg.SignupCodeTTL),
	}); err != nil {
		return err
	}
	return s.sender.SendSignupCode(ctx, email, code)
}

func (s *Service) VerifyCode(ctx context.Context, email, code, userAgent, ipAddress string) (Session, error) {
	email = normalizeEmail(email)
	code = strings.TrimSpace(code)
	if email == "" || code == "" {
		return Session{}, fmt.Errorf("email and code are required")
	}
	if err := s.store.ConsumeEmailCode(ctx, email, emailPurposeBrowserLogin, hashCode(code)); err != nil {
		return Session{}, fmt.Errorf("invalid or expired verification code")
	}

	user, err := s.store.UpsertUser(ctx, email, isAdminEmail(s.cfg.AdminEmails, email))
	if err != nil {
		return Session{}, err
	}
	rawToken, err := randomHex(32)
	if err != nil {
		return Session{}, err
	}
	expiresAt := time.Now().UTC().Add(s.cfg.WebSessionTTL)
	record, err := s.store.CreateWebSession(ctx, database.CreateWebSessionParams{
		UserID:    user.ID,
		TokenHash: hashToken(rawToken),
		ExpiresAt: expiresAt.Format("2006-01-02 15:04:05"),
		UserAgent: userAgent,
		IPAddress: ipAddress,
	})
	if err != nil {
		return Session{}, err
	}
	return Session{
		User:      user,
		Record:    record,
		RawToken:  rawToken,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) Authenticate(ctx context.Context, rawToken string) (database.User, database.WebSessionRecord, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return database.User{}, database.WebSessionRecord{}, database.ErrNotFound
	}
	record, err := s.store.GetActiveWebSessionByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		return database.User{}, database.WebSessionRecord{}, err
	}
	if err := s.store.TouchWebSession(ctx, record.ID); err != nil && err != database.ErrNotFound {
		return database.User{}, database.WebSessionRecord{}, err
	}
	user, err := s.store.GetUserByEmail(ctx, record.UserEmail)
	if err != nil {
		return database.User{}, database.WebSessionRecord{}, err
	}
	return user, record, nil
}

func (s *Service) Logout(ctx context.Context, rawToken string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil
	}
	record, err := s.store.GetActiveWebSessionByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		if err == database.ErrNotFound {
			return nil
		}
		return err
	}
	return s.store.RevokeWebSession(ctx, record.ID)
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

func randomHex(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
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
