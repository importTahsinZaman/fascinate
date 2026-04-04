package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func (s *Store) UpsertUser(ctx context.Context, email string, isAdmin bool) (User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return User{}, fmt.Errorf("email is required")
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, is_admin)
		VALUES (?, ?, ?)
		ON CONFLICT(email) DO UPDATE SET is_admin = excluded.is_admin
	`, uuid.NewString(), email, boolToInt(isAdmin))
	if err != nil {
		return User{}, err
	}

	return s.GetUserByEmail(ctx, email)
}

func (s *Store) ApplyUserBudgetDefaults(ctx context.Context, userID string, defaults UserBudgetDefaults) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET
			max_cpu = CASE
				WHEN TRIM(max_cpu) = '' THEN ?
				ELSE max_cpu
			END,
			max_memory_bytes = CASE
				WHEN max_memory_bytes <= 0 THEN ?
				ELSE max_memory_bytes
			END,
			max_disk_bytes = CASE
				WHEN max_disk_bytes <= 0 THEN ?
				ELSE max_disk_bytes
			END,
			max_machine_count = CASE
				WHEN max_machine_count <= 0 THEN ?
				ELSE max_machine_count
			END,
			max_snapshot_count = CASE
				WHEN max_snapshot_count <= 0 THEN ?
				ELSE max_snapshot_count
			END
		WHERE id = ?
	`, strings.TrimSpace(defaults.MaxCPU), defaults.MaxMemoryBytes, defaults.MaxDiskBytes, defaults.MaxMachineCount, defaults.MaxSnapshotCount, strings.TrimSpace(userID))
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

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	email = normalizeEmail(email)

	var user User
	var isAdmin int
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id,
			email,
			is_admin,
			max_cpu,
			max_memory_bytes,
			max_disk_bytes,
			max_machine_count,
			max_snapshot_count,
			tutorial_completed_at,
			created_at
		FROM users
		WHERE email = ?
	`, email).Scan(
		&user.ID,
		&user.Email,
		&isAdmin,
		&user.MaxCPU,
		&user.MaxMemoryBytes,
		&user.MaxDiskBytes,
		&user.MaxMachineCount,
		&user.MaxSnapshotCount,
		nullableString(&user.TutorialCompletedAt),
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}

	user.IsAdmin = isAdmin == 1
	return user, nil
}

func (s *Store) MarkUserTutorialCompleted(ctx context.Context, userID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET tutorial_completed_at = COALESCE(tutorial_completed_at, CURRENT_TIMESTAMP)
		WHERE id = ?
	`, strings.TrimSpace(userID))
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

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}
