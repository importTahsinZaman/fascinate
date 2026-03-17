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

func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	email = normalizeEmail(email)

	var user User
	var isAdmin int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, is_admin, created_at
		FROM users
		WHERE email = ?
	`, email).Scan(&user.ID, &user.Email, &isAdmin, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}

	user.IsAdmin = isAdmin == 1
	return user, nil
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
