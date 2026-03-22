package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
)

func (s *Store) GetWorkspaceLayout(ctx context.Context, userID, name string) (WorkspaceLayoutRecord, error) {
	var record WorkspaceLayoutRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			w.id,
			w.user_id,
			u.email,
			w.name,
			w.layout_json,
			w.created_at,
			w.updated_at
		FROM workspace_layouts w
		INNER JOIN users u ON u.id = w.user_id
		WHERE w.user_id = ? AND w.name = ?
	`, strings.TrimSpace(userID), strings.TrimSpace(name)).Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.Name,
		&record.LayoutJSON,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkspaceLayoutRecord{}, ErrNotFound
		}
		return WorkspaceLayoutRecord{}, err
	}
	return record, nil
}

func (s *Store) UpsertWorkspaceLayout(ctx context.Context, params UpsertWorkspaceLayoutParams) (WorkspaceLayoutRecord, error) {
	id := strings.TrimSpace(params.ID)
	if id == "" {
		id = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workspace_layouts (id, user_id, name, layout_json)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, name) DO UPDATE SET
			layout_json = excluded.layout_json,
			updated_at = CURRENT_TIMESTAMP
	`, id, strings.TrimSpace(params.UserID), strings.TrimSpace(params.Name), params.LayoutJSON)
	if err != nil {
		if isUniqueConstraint(err) {
			return WorkspaceLayoutRecord{}, ErrConflict
		}
		return WorkspaceLayoutRecord{}, err
	}
	return s.GetWorkspaceLayout(ctx, params.UserID, params.Name)
}
