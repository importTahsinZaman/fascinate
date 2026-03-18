package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CreateEmailCodeParams struct {
	UserID    *string
	Email     string
	Purpose   string
	CodeHash  string
	ExpiresAt time.Time
}

func (s *Store) CreateEmailCode(ctx context.Context, params CreateEmailCodeParams) (EmailCodeRecord, error) {
	id := uuid.NewString()
	expiresAt := formatSQLiteTimestamp(params.ExpiresAt)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO email_codes (id, user_id, email, purpose, code_hash, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, params.UserID, normalizeEmail(params.Email), strings.TrimSpace(params.Purpose), strings.TrimSpace(params.CodeHash), expiresAt)
	if err != nil {
		return EmailCodeRecord{}, err
	}

	return s.GetEmailCodeByID(ctx, id)
}

func (s *Store) GetEmailCodeByID(ctx context.Context, id string) (EmailCodeRecord, error) {
	var record EmailCodeRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, email, purpose, code_hash, expires_at, consumed_at, created_at
		FROM email_codes
		WHERE id = ?
	`, id).Scan(
		&record.ID,
		nullableString(&record.UserID),
		&record.Email,
		&record.Purpose,
		&record.CodeHash,
		&record.ExpiresAt,
		nullableString(&record.ConsumedAt),
		&record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EmailCodeRecord{}, ErrNotFound
		}
		return EmailCodeRecord{}, err
	}

	return record, nil
}

func (s *Store) ConsumeEmailCode(ctx context.Context, email, purpose, codeHash string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE email_codes
		SET consumed_at = CURRENT_TIMESTAMP
		WHERE email = ?
		  AND purpose = ?
		  AND code_hash = ?
		  AND consumed_at IS NULL
		  AND expires_at >= CURRENT_TIMESTAMP
	`, normalizeEmail(email), strings.TrimSpace(purpose), strings.TrimSpace(codeHash))
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

func formatSQLiteTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05")
}
