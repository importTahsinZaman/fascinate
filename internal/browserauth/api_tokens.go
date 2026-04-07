package browserauth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fascinate/internal/database"
)

type APITokenSession struct {
	User      database.User           `json:"user"`
	Record    database.APITokenRecord `json:"record"`
	RawToken  string                  `json:"-"`
	ExpiresAt time.Time               `json:"expires_at"`
}

func (s *Service) VerifyCodeForAPIToken(ctx context.Context, email, code, tokenName, userAgent, ipAddress string) (APITokenSession, error) {
	email = normalizeEmail(email)
	code = strings.TrimSpace(code)
	tokenName = strings.TrimSpace(tokenName)
	if email == "" || code == "" {
		return APITokenSession{}, fmt.Errorf("email and code are required")
	}
	if tokenName == "" {
		tokenName = "fascinate-cli"
	}
	if err := s.store.ConsumeEmailCode(ctx, email, emailPurposeBrowserLogin, hashCode(code)); err != nil {
		return APITokenSession{}, fmt.Errorf("invalid or expired verification code")
	}

	user, err := s.store.UpsertUser(ctx, email, isAdminEmail(s.cfg.AdminEmails, email))
	if err != nil {
		return APITokenSession{}, err
	}
	rawToken, err := randomHex(32)
	if err != nil {
		return APITokenSession{}, err
	}
	expiresAt := time.Now().UTC().Add(s.cfg.APITokenTTL)
	record, err := s.store.CreateAPIToken(ctx, database.CreateAPITokenParams{
		UserID:    user.ID,
		Name:      tokenName,
		TokenHash: hashToken(rawToken),
		ExpiresAt: expiresAt.Format("2006-01-02 15:04:05"),
		UserAgent: userAgent,
		IPAddress: ipAddress,
	})
	if err != nil {
		return APITokenSession{}, err
	}
	return APITokenSession{
		User:      user,
		Record:    record,
		RawToken:  rawToken,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) AuthenticateAPIToken(ctx context.Context, rawToken string) (database.User, database.APITokenRecord, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return database.User{}, database.APITokenRecord{}, database.ErrNotFound
	}
	record, err := s.store.GetActiveAPITokenByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		return database.User{}, database.APITokenRecord{}, err
	}
	if err := s.store.TouchAPIToken(ctx, record.ID); err != nil && err != database.ErrNotFound {
		return database.User{}, database.APITokenRecord{}, err
	}
	user, err := s.store.GetUserByEmail(ctx, record.UserEmail)
	if err != nil {
		return database.User{}, database.APITokenRecord{}, err
	}
	return user, record, nil
}

func (s *Service) LogoutAPIToken(ctx context.Context, rawToken string) error {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil
	}
	record, err := s.store.GetActiveAPITokenByTokenHash(ctx, hashToken(rawToken))
	if err != nil {
		if err == database.ErrNotFound {
			return nil
		}
		return err
	}
	return s.store.RevokeAPIToken(ctx, record.ID)
}
