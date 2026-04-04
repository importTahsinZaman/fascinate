package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) CreateSnapshot(ctx context.Context, params CreateSnapshotParams) (SnapshotRecord, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO snapshots (
			id, name, owner_user_id, host_id, source_machine_id, runtime_name, state,
			cpu, memory_bytes, disk_bytes, artifact_dir, disk_size_bytes, memory_size_bytes,
			runtime_version, firmware_version
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, params.ID, params.Name, params.OwnerUserID, params.HostID, params.SourceMachineID, params.RuntimeName, params.State,
		params.CPU, params.MemoryBytes, params.DiskBytes, params.ArtifactDir,
		params.DiskSizeBytes, params.MemorySizeBytes, params.RuntimeVersion, params.FirmwareVersion)
	if err != nil {
		if isUniqueConstraint(err) {
			return SnapshotRecord{}, ErrConflict
		}
		return SnapshotRecord{}, err
	}

	return s.GetSnapshotByName(ctx, params.OwnerUserID, params.Name)
}

func (s *Store) GetSnapshotByName(ctx context.Context, ownerUserID, name string) (SnapshotRecord, error) {
	var record SnapshotRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			s.id,
			s.name,
			s.owner_user_id,
			u.email,
			s.host_id,
			s.source_machine_id,
			m.name,
			s.runtime_name,
			s.state,
			s.cpu,
			s.memory_bytes,
			s.disk_bytes,
			s.artifact_dir,
			s.disk_size_bytes,
			s.memory_size_bytes,
			s.runtime_version,
			s.firmware_version,
			s.created_at,
			s.updated_at,
			s.deleted_at
		FROM snapshots s
		INNER JOIN users u ON u.id = s.owner_user_id
		LEFT JOIN machines m ON m.id = s.source_machine_id
		WHERE s.owner_user_id = ? AND s.name = ? AND s.deleted_at IS NULL
	`, strings.TrimSpace(ownerUserID), strings.TrimSpace(name)).Scan(
		&record.ID,
		&record.Name,
		&record.OwnerUserID,
		&record.OwnerEmail,
		nullableString(&record.HostID),
		nullableString(&record.SourceMachineID),
		nullableString(&record.SourceMachineName),
		&record.RuntimeName,
		&record.State,
		&record.CPU,
		&record.MemoryBytes,
		&record.DiskBytes,
		&record.ArtifactDir,
		&record.DiskSizeBytes,
		&record.MemorySizeBytes,
		&record.RuntimeVersion,
		&record.FirmwareVersion,
		&record.CreatedAt,
		&record.UpdatedAt,
		nullableString(&record.DeletedAt),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SnapshotRecord{}, ErrNotFound
		}
		return SnapshotRecord{}, err
	}

	return record, nil
}

func (s *Store) GetSnapshotByID(ctx context.Context, id string) (SnapshotRecord, error) {
	var record SnapshotRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			s.id,
			s.name,
			s.owner_user_id,
			u.email,
			s.host_id,
			s.source_machine_id,
			m.name,
			s.runtime_name,
			s.state,
			s.cpu,
			s.memory_bytes,
			s.disk_bytes,
			s.artifact_dir,
			s.disk_size_bytes,
			s.memory_size_bytes,
			s.runtime_version,
			s.firmware_version,
			s.created_at,
			s.updated_at,
			s.deleted_at
		FROM snapshots s
		INNER JOIN users u ON u.id = s.owner_user_id
		LEFT JOIN machines m ON m.id = s.source_machine_id
		WHERE s.id = ? AND s.deleted_at IS NULL
	`, strings.TrimSpace(id)).Scan(
		&record.ID,
		&record.Name,
		&record.OwnerUserID,
		&record.OwnerEmail,
		nullableString(&record.HostID),
		nullableString(&record.SourceMachineID),
		nullableString(&record.SourceMachineName),
		&record.RuntimeName,
		&record.State,
		&record.CPU,
		&record.MemoryBytes,
		&record.DiskBytes,
		&record.ArtifactDir,
		&record.DiskSizeBytes,
		&record.MemorySizeBytes,
		&record.RuntimeVersion,
		&record.FirmwareVersion,
		&record.CreatedAt,
		&record.UpdatedAt,
		nullableString(&record.DeletedAt),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SnapshotRecord{}, ErrNotFound
		}
		return SnapshotRecord{}, err
	}

	return record, nil
}

func (s *Store) ListSnapshots(ctx context.Context, ownerEmail string) ([]SnapshotRecord, error) {
	query := `
		SELECT
			s.id,
			s.name,
			s.owner_user_id,
			u.email,
			s.host_id,
			s.source_machine_id,
			m.name,
			s.runtime_name,
			s.state,
			s.cpu,
			s.memory_bytes,
			s.disk_bytes,
			s.artifact_dir,
			s.disk_size_bytes,
			s.memory_size_bytes,
			s.runtime_version,
			s.firmware_version,
			s.created_at,
			s.updated_at,
			s.deleted_at
		FROM snapshots s
		INNER JOIN users u ON u.id = s.owner_user_id
		LEFT JOIN machines m ON m.id = s.source_machine_id
		WHERE s.deleted_at IS NULL
	`
	args := make([]any, 0, 1)
	if email := normalizeEmail(ownerEmail); email != "" {
		query += ` AND u.email = ?`
		args = append(args, email)
	}
	query += ` ORDER BY s.created_at DESC, s.name ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SnapshotRecord
	for rows.Next() {
		var record SnapshotRecord
		if err := rows.Scan(
			&record.ID,
			&record.Name,
			&record.OwnerUserID,
			&record.OwnerEmail,
			nullableString(&record.HostID),
			nullableString(&record.SourceMachineID),
			nullableString(&record.SourceMachineName),
			&record.RuntimeName,
			&record.State,
			&record.CPU,
			&record.MemoryBytes,
			&record.DiskBytes,
			&record.ArtifactDir,
			&record.DiskSizeBytes,
			&record.MemorySizeBytes,
			&record.RuntimeVersion,
			&record.FirmwareVersion,
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

func (s *Store) UpdateSnapshotState(ctx context.Context, id, state string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE snapshots
		SET state = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
	`, state, strings.TrimSpace(id))
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

func (s *Store) UpdateSnapshotArtifacts(ctx context.Context, id string, diskSizeBytes, memorySizeBytes int64, runtimeVersion, firmwareVersion string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE snapshots
		SET disk_size_bytes = ?, memory_size_bytes = ?, runtime_version = ?, firmware_version = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND deleted_at IS NULL
	`, diskSizeBytes, memorySizeBytes, strings.TrimSpace(runtimeVersion), strings.TrimSpace(firmwareVersion), strings.TrimSpace(id))
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

func (s *Store) MarkSnapshotDeleted(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM snapshots
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
