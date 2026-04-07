package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
)

func (s *Store) CreateShell(ctx context.Context, params CreateShellParams) (ShellRecord, error) {
	id := strings.TrimSpace(params.ID)
	if id == "" {
		id = uuid.NewString()
	}

	cwd := strings.TrimSpace(params.CWD)
	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = id
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO shells (
			id, user_id, machine_id, host_id, name, tmux_session, state, cwd
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, strings.TrimSpace(params.UserID), strings.TrimSpace(params.MachineID), params.HostID, name, strings.TrimSpace(params.TmuxSession), strings.TrimSpace(params.State), cwd)
	if err != nil {
		if isUniqueConstraint(err) {
			return ShellRecord{}, ErrConflict
		}
		return ShellRecord{}, err
	}

	return s.GetShellByID(ctx, id)
}

func (s *Store) GetShellByID(ctx context.Context, id string) (ShellRecord, error) {
	var record ShellRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			sh.id,
			sh.user_id,
			u.email,
			sh.machine_id,
			m.name,
			sh.host_id,
			sh.name,
			sh.tmux_session,
			sh.state,
			sh.cwd,
			sh.last_error,
			sh.last_attached_at,
			sh.created_at,
			sh.updated_at,
			sh.deleted_at
		FROM shells sh
		INNER JOIN users u ON u.id = sh.user_id
		INNER JOIN machines m ON m.id = sh.machine_id
		WHERE sh.id = ? AND sh.deleted_at IS NULL
	`, strings.TrimSpace(id)).Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.MachineID,
		&record.MachineName,
		nullableString(&record.HostID),
		&record.Name,
		&record.TmuxSession,
		&record.State,
		&record.CWD,
		nullableString(&record.LastError),
		nullableString(&record.LastAttachedAt),
		&record.CreatedAt,
		&record.UpdatedAt,
		nullableString(&record.DeletedAt),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ShellRecord{}, ErrNotFound
		}
		return ShellRecord{}, err
	}
	return record, nil
}

func (s *Store) ListShells(ctx context.Context, ownerEmail string) ([]ShellRecord, error) {
	query := `
		SELECT
			sh.id,
			sh.user_id,
			u.email,
			sh.machine_id,
			m.name,
			sh.host_id,
			sh.name,
			sh.tmux_session,
			sh.state,
			sh.cwd,
			sh.last_error,
			sh.last_attached_at,
			sh.created_at,
			sh.updated_at,
			sh.deleted_at
		FROM shells sh
		INNER JOIN users u ON u.id = sh.user_id
		INNER JOIN machines m ON m.id = sh.machine_id
		WHERE sh.deleted_at IS NULL
	`
	args := make([]any, 0, 1)
	if email := normalizeEmail(ownerEmail); email != "" {
		query += ` AND u.email = ?`
		args = append(args, email)
	}
	query += ` ORDER BY sh.created_at ASC, sh.id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ShellRecord
	for rows.Next() {
		var record ShellRecord
		if err := rows.Scan(
			&record.ID,
			&record.UserID,
			&record.UserEmail,
			&record.MachineID,
			&record.MachineName,
			nullableString(&record.HostID),
			&record.Name,
			&record.TmuxSession,
			&record.State,
			&record.CWD,
			nullableString(&record.LastError),
			nullableString(&record.LastAttachedAt),
			&record.CreatedAt,
			&record.UpdatedAt,
			nullableString(&record.DeletedAt),
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s *Store) ListMachineShells(ctx context.Context, machineID string) ([]ShellRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			sh.id,
			sh.user_id,
			u.email,
			sh.machine_id,
			m.name,
			sh.host_id,
			sh.name,
			sh.tmux_session,
			sh.state,
			sh.cwd,
			sh.last_error,
			sh.last_attached_at,
			sh.created_at,
			sh.updated_at,
			sh.deleted_at
		FROM shells sh
		INNER JOIN users u ON u.id = sh.user_id
		INNER JOIN machines m ON m.id = sh.machine_id
		WHERE sh.machine_id = ?
		  AND sh.deleted_at IS NULL
		ORDER BY sh.created_at ASC, sh.id ASC
	`, strings.TrimSpace(machineID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ShellRecord
	for rows.Next() {
		var record ShellRecord
		if err := rows.Scan(
			&record.ID,
			&record.UserID,
			&record.UserEmail,
			&record.MachineID,
			&record.MachineName,
			nullableString(&record.HostID),
			&record.Name,
			&record.TmuxSession,
			&record.State,
			&record.CWD,
			nullableString(&record.LastError),
			nullableString(&record.LastAttachedAt),
			&record.CreatedAt,
			&record.UpdatedAt,
			nullableString(&record.DeletedAt),
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (s *Store) UpdateShellState(ctx context.Context, id, state string, lastError *string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE shells
		SET state = ?, last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
	`, strings.TrimSpace(state), lastError, strings.TrimSpace(id))
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

func (s *Store) UpdateShellCWD(ctx context.Context, id, cwd string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE shells
		SET cwd = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
	`, strings.TrimSpace(cwd), strings.TrimSpace(id))
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

func (s *Store) TouchShellAttached(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE shells
		SET last_attached_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
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

func (s *Store) MarkShellDeleted(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE shells
		SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
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
