package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
)

func (s *Store) CreateWebSession(ctx context.Context, params CreateWebSessionParams) (WebSessionRecord, error) {
	id := strings.TrimSpace(params.ID)
	if id == "" {
		id = uuid.NewString()
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
		INSERT INTO web_sessions (id, user_id, token_hash, expires_at, user_agent, ip_address)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, strings.TrimSpace(params.UserID), strings.TrimSpace(params.TokenHash), strings.TrimSpace(params.ExpiresAt), userAgent, ipAddress)
	if err != nil {
		if isUniqueConstraint(err) {
			return WebSessionRecord{}, ErrConflict
		}
		return WebSessionRecord{}, err
	}

	return s.GetWebSessionByID(ctx, id)
}

func (s *Store) GetWebSessionByID(ctx context.Context, id string) (WebSessionRecord, error) {
	var record WebSessionRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			ws.id,
			ws.user_id,
			u.email,
			ws.token_hash,
			ws.expires_at,
			ws.last_seen_at,
			ws.user_agent,
			ws.ip_address,
			ws.revoked_at,
			ws.created_at
		FROM web_sessions ws
		INNER JOIN users u ON u.id = ws.user_id
		WHERE ws.id = ?
	`, strings.TrimSpace(id)).Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.TokenHash,
		&record.ExpiresAt,
		&record.LastSeenAt,
		nullableString(&record.UserAgent),
		nullableString(&record.IPAddress),
		nullableString(&record.RevokedAt),
		&record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WebSessionRecord{}, ErrNotFound
		}
		return WebSessionRecord{}, err
	}
	return record, nil
}

func (s *Store) GetActiveWebSessionByTokenHash(ctx context.Context, tokenHash string) (WebSessionRecord, error) {
	var record WebSessionRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			ws.id,
			ws.user_id,
			u.email,
			ws.token_hash,
			ws.expires_at,
			ws.last_seen_at,
			ws.user_agent,
			ws.ip_address,
			ws.revoked_at,
			ws.created_at
		FROM web_sessions ws
		INNER JOIN users u ON u.id = ws.user_id
		WHERE ws.token_hash = ?
		  AND ws.revoked_at IS NULL
		  AND ws.expires_at >= CURRENT_TIMESTAMP
	`, strings.TrimSpace(tokenHash)).Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.TokenHash,
		&record.ExpiresAt,
		&record.LastSeenAt,
		nullableString(&record.UserAgent),
		nullableString(&record.IPAddress),
		nullableString(&record.RevokedAt),
		&record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WebSessionRecord{}, ErrNotFound
		}
		return WebSessionRecord{}, err
	}
	return record, nil
}

func (s *Store) TouchWebSession(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE web_sessions
		SET last_seen_at = CURRENT_TIMESTAMP
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

func (s *Store) RevokeWebSession(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE web_sessions
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
