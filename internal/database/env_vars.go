package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
)

func (s *Store) ListUserEnvVars(ctx context.Context, userID string) ([]EnvVarRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, key, raw_value, created_at, updated_at
		FROM user_env_vars
		WHERE user_id = ?
		ORDER BY key ASC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []EnvVarRecord
	for rows.Next() {
		var record EnvVarRecord
		if err := rows.Scan(
			&record.ID,
			&record.UserID,
			&record.Key,
			&record.RawValue,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s *Store) GetUserEnvVar(ctx context.Context, userID, key string) (EnvVarRecord, error) {
	var record EnvVarRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, key, raw_value, created_at, updated_at
		FROM user_env_vars
		WHERE user_id = ? AND key = ?
	`, strings.TrimSpace(userID), strings.TrimSpace(key)).Scan(
		&record.ID,
		&record.UserID,
		&record.Key,
		&record.RawValue,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EnvVarRecord{}, ErrNotFound
		}
		return EnvVarRecord{}, err
	}
	return record, nil
}

func (s *Store) UpsertUserEnvVar(ctx context.Context, params UpsertEnvVarParams) (EnvVarRecord, error) {
	id := strings.TrimSpace(params.ID)
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_env_vars (id, user_id, key, raw_value)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET
			raw_value = excluded.raw_value,
			updated_at = CURRENT_TIMESTAMP
	`, id, strings.TrimSpace(params.UserID), strings.TrimSpace(params.Key), params.RawValue)
	if err != nil {
		return EnvVarRecord{}, err
	}

	return s.GetUserEnvVar(ctx, params.UserID, params.Key)
}

func (s *Store) DeleteUserEnvVar(ctx context.Context, userID, key string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM user_env_vars
		WHERE user_id = ? AND key = ?
	`, strings.TrimSpace(userID), strings.TrimSpace(key))
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
