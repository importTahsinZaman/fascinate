package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) CreateMachine(ctx context.Context, params CreateMachineParams) (MachineRecord, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO machines (id, name, owner_user_id, host_id, runtime_name, source_snapshot_id, state, primary_port)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, params.ID, params.Name, params.OwnerUserID, params.HostID, params.RuntimeName, params.SourceSnapshotID, params.State, params.PrimaryPort)
	if err != nil {
		if isUniqueConstraint(err) {
			return MachineRecord{}, ErrConflict
		}
		return MachineRecord{}, err
	}

	return s.GetMachineByName(ctx, params.Name)
}

func (s *Store) GetMachineByName(ctx context.Context, name string) (MachineRecord, error) {
	var record MachineRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			m.id,
			m.name,
			m.owner_user_id,
			u.email,
			m.host_id,
			m.runtime_name,
			m.source_snapshot_id,
			m.state,
			m.primary_port,
			m.created_at,
			m.updated_at,
			m.deleted_at
		FROM machines m
		INNER JOIN users u ON u.id = m.owner_user_id
		WHERE m.name = ? AND m.deleted_at IS NULL
	`, strings.TrimSpace(name)).Scan(
		&record.ID,
		&record.Name,
		&record.OwnerUserID,
		&record.OwnerEmail,
		nullableString(&record.HostID),
		&record.RuntimeName,
		nullableString(&record.SourceSnapshotID),
		&record.State,
		&record.PrimaryPort,
		&record.CreatedAt,
		&record.UpdatedAt,
		nullableString(&record.DeletedAt),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MachineRecord{}, ErrNotFound
		}
		return MachineRecord{}, err
	}

	return record, nil
}

func (s *Store) ListMachines(ctx context.Context, ownerEmail string) ([]MachineRecord, error) {
	query := `
		SELECT
			m.id,
			m.name,
			m.owner_user_id,
			u.email,
			m.host_id,
			m.runtime_name,
			m.source_snapshot_id,
			m.state,
			m.primary_port,
			m.created_at,
			m.updated_at,
			m.deleted_at
		FROM machines m
		INNER JOIN users u ON u.id = m.owner_user_id
		WHERE m.deleted_at IS NULL
	`
	args := make([]any, 0, 1)
	if email := normalizeEmail(ownerEmail); email != "" {
		query += ` AND u.email = ?`
		args = append(args, email)
	}
	query += ` ORDER BY m.name ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MachineRecord
	for rows.Next() {
		var record MachineRecord
		if err := rows.Scan(
			&record.ID,
			&record.Name,
			&record.OwnerUserID,
			&record.OwnerEmail,
			nullableString(&record.HostID),
			&record.RuntimeName,
			nullableString(&record.SourceSnapshotID),
			&record.State,
			&record.PrimaryPort,
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

func (s *Store) UpdateMachineState(ctx context.Context, id, state string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE machines
		SET state = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
	`, state, id)
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

func (s *Store) MarkMachineDeleted(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE machines
		SET state = 'deleted', updated_at = CURRENT_TIMESTAMP, deleted_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
	`, id)
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

func nullableString(target **string) any {
	return sqlNullString{target: target}
}

type sqlNullString struct {
	target **string
}

func (n sqlNullString) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*n.target = nil
		return nil
	case string:
		copy := value
		*n.target = &copy
		return nil
	case []byte:
		copy := string(value)
		*n.target = &copy
		return nil
	default:
		return errors.New("unsupported nullable string source")
	}
}

func isUniqueConstraint(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
