package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
)

func (s *Store) CreateAPIToken(ctx context.Context, params CreateAPITokenParams) (APITokenRecord, error) {
	id := strings.TrimSpace(params.ID)
	if id == "" {
		id = uuid.NewString()
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = "fascinate-cli"
	}

	var userAgent any
	if strings.TrimSpace(params.UserAgent) != "" {
		userAgent = strings.TrimSpace(params.UserAgent)
	}
	var ipAddress any
	if strings.TrimSpace(params.IPAddress) != "" {
		ipAddress = strings.TrimSpace(params.IPAddress)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, token_hash, expires_at, user_agent, ip_address)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, strings.TrimSpace(params.UserID), name, strings.TrimSpace(params.TokenHash), strings.TrimSpace(params.ExpiresAt), userAgent, ipAddress)
	if err != nil {
		if isUniqueConstraint(err) {
			return APITokenRecord{}, ErrConflict
		}
		return APITokenRecord{}, err
	}

	return s.GetAPITokenByID(ctx, id)
}

func (s *Store) GetAPITokenByID(ctx context.Context, id string) (APITokenRecord, error) {
	var record APITokenRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			t.id,
			t.user_id,
			u.email,
			t.name,
			t.token_hash,
			t.expires_at,
			t.last_used_at,
			t.user_agent,
			t.ip_address,
			t.revoked_at,
			t.created_at
		FROM api_tokens t
		INNER JOIN users u ON u.id = t.user_id
		WHERE t.id = ?
	`, strings.TrimSpace(id)).Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.Name,
		&record.TokenHash,
		&record.ExpiresAt,
		&record.LastUsedAt,
		nullableString(&record.UserAgent),
		nullableString(&record.IPAddress),
		nullableString(&record.RevokedAt),
		&record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return APITokenRecord{}, ErrNotFound
		}
		return APITokenRecord{}, err
	}
	return record, nil
}

func (s *Store) GetActiveAPITokenByTokenHash(ctx context.Context, tokenHash string) (APITokenRecord, error) {
	var record APITokenRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			t.id,
			t.user_id,
			u.email,
			t.name,
			t.token_hash,
			t.expires_at,
			t.last_used_at,
			t.user_agent,
			t.ip_address,
			t.revoked_at,
			t.created_at
		FROM api_tokens t
		INNER JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ?
		  AND t.revoked_at IS NULL
		  AND t.expires_at >= CURRENT_TIMESTAMP
	`, strings.TrimSpace(tokenHash)).Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.Name,
		&record.TokenHash,
		&record.ExpiresAt,
		&record.LastUsedAt,
		nullableString(&record.UserAgent),
		nullableString(&record.IPAddress),
		nullableString(&record.RevokedAt),
		&record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return APITokenRecord{}, ErrNotFound
		}
		return APITokenRecord{}, err
	}
	return record, nil
}

func (s *Store) TouchAPIToken(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens
		SET last_used_at = CURRENT_TIMESTAMP
		WHERE id = ?
		  AND revoked_at IS NULL
		  AND expires_at >= CURRENT_TIMESTAMP
	`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeAPIToken(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens
		SET revoked_at = CURRENT_TIMESTAMP
		WHERE id = ? AND revoked_at IS NULL
	`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
