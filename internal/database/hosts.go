package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) UpsertHost(ctx context.Context, params UpsertHostParams) (HostRecord, error) {
	labelsJSON := strings.TrimSpace(params.LabelsJSON)
	if labelsJSON == "" {
		labelsJSON = "{}"
	}
	capabilitiesJSON := strings.TrimSpace(params.CapabilitiesJSON)
	if capabilitiesJSON == "" {
		capabilitiesJSON = "[]"
	}
	status := strings.TrimSpace(params.Status)
	if status == "" {
		status = "ACTIVE"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO hosts (id, name, region, role, status, labels_json, capabilities_json, runtime_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			region = excluded.region,
			role = excluded.role,
			labels_json = excluded.labels_json,
			capabilities_json = excluded.capabilities_json,
			runtime_version = excluded.runtime_version,
			status = CASE WHEN hosts.status = 'DRAINING' THEN hosts.status ELSE excluded.status END,
			updated_at = CURRENT_TIMESTAMP
	`, strings.TrimSpace(params.ID), strings.TrimSpace(params.Name), strings.TrimSpace(params.Region), strings.TrimSpace(params.Role), status, labelsJSON, capabilitiesJSON, strings.TrimSpace(params.RuntimeVersion))
	if err != nil {
		return HostRecord{}, err
	}

	return s.GetHostByID(ctx, params.ID)
}

func (s *Store) GetHostByID(ctx context.Context, id string) (HostRecord, error) {
	var record HostRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			id, name, region, role, status, labels_json, capabilities_json, runtime_version,
			heartbeat_at, total_cpu, allocated_cpu, total_memory_bytes, allocated_memory_bytes,
			total_disk_bytes, allocated_disk_bytes, available_disk_bytes, machine_count,
			snapshot_count, last_error, created_at, updated_at
		FROM hosts
		WHERE id = ?
	`, strings.TrimSpace(id)).Scan(
		&record.ID,
		&record.Name,
		&record.Region,
		&record.Role,
		&record.Status,
		&record.LabelsJSON,
		&record.CapabilitiesJSON,
		&record.RuntimeVersion,
		nullableString(&record.HeartbeatAt),
		&record.TotalCPU,
		&record.AllocatedCPU,
		&record.TotalMemoryBytes,
		&record.AllocatedMemoryBytes,
		&record.TotalDiskBytes,
		&record.AllocatedDiskBytes,
		&record.AvailableDiskBytes,
		&record.MachineCount,
		&record.SnapshotCount,
		nullableString(&record.LastError),
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HostRecord{}, ErrNotFound
		}
		return HostRecord{}, err
	}
	return record, nil
}

func (s *Store) ListHosts(ctx context.Context) ([]HostRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, name, region, role, status, labels_json, capabilities_json, runtime_version,
			heartbeat_at, total_cpu, allocated_cpu, total_memory_bytes, allocated_memory_bytes,
			total_disk_bytes, allocated_disk_bytes, available_disk_bytes, machine_count,
			snapshot_count, last_error, created_at, updated_at
		FROM hosts
		ORDER BY name ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []HostRecord
	for rows.Next() {
		var record HostRecord
		if err := rows.Scan(
			&record.ID,
			&record.Name,
			&record.Region,
			&record.Role,
			&record.Status,
			&record.LabelsJSON,
			&record.CapabilitiesJSON,
			&record.RuntimeVersion,
			nullableString(&record.HeartbeatAt),
			&record.TotalCPU,
			&record.AllocatedCPU,
			&record.TotalMemoryBytes,
			&record.AllocatedMemoryBytes,
			&record.TotalDiskBytes,
			&record.AllocatedDiskBytes,
			&record.AvailableDiskBytes,
			&record.MachineCount,
			&record.SnapshotCount,
			nullableString(&record.LastError),
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) UpdateHostHeartbeat(ctx context.Context, params UpdateHostHeartbeatParams) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE hosts
		SET
			runtime_version = ?,
			status = CASE
				WHEN status = 'DRAINING' THEN status
				WHEN ? THEN 'ACTIVE'
				ELSE 'UNHEALTHY'
			END,
			heartbeat_at = CURRENT_TIMESTAMP,
			total_cpu = ?,
			allocated_cpu = ?,
			total_memory_bytes = ?,
			allocated_memory_bytes = ?,
			total_disk_bytes = ?,
			allocated_disk_bytes = ?,
			available_disk_bytes = ?,
			machine_count = ?,
			snapshot_count = ?,
			last_error = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`,
		strings.TrimSpace(params.RuntimeVersion),
		boolToInt(params.Healthy) == 1,
		params.TotalCPU,
		params.AllocatedCPU,
		params.TotalMemoryBytes,
		params.AllocatedMemoryBytes,
		params.TotalDiskBytes,
		params.AllocatedDiskBytes,
		params.AvailableDiskBytes,
		params.MachineCount,
		params.SnapshotCount,
		nullStringValue(params.LastError),
		strings.TrimSpace(params.ID),
	)
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

func (s *Store) AssignHostToMachinesWithoutHost(ctx context.Context, hostID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE machines
		SET host_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE host_id IS NULL
	`, strings.TrimSpace(hostID))
	return err
}

func (s *Store) AssignHostToSnapshotsWithoutHost(ctx context.Context, hostID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE snapshots
		SET host_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE host_id IS NULL
	`, strings.TrimSpace(hostID))
	return err
}

func nullStringValue(value *string) any {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		return nil
	}
	return text
}
