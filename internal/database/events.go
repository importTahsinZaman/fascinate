package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) CreateEvent(ctx context.Context, params CreateEventParams) (EventRecord, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO events (id, actor_user_id, machine_id, kind, payload_json)
		VALUES (?, ?, ?, ?, ?)
	`, strings.TrimSpace(params.ID), params.ActorUserID, params.MachineID, strings.TrimSpace(params.Kind), strings.TrimSpace(params.PayloadJSON))
	if err != nil {
		return EventRecord{}, err
	}

	return s.GetEventByID(ctx, params.ID)
}

func (s *Store) GetEventByID(ctx context.Context, id string) (EventRecord, error) {
	var record EventRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT id, actor_user_id, machine_id, kind, payload_json, created_at
		FROM events
		WHERE id = ?
	`, strings.TrimSpace(id)).Scan(
		&record.ID,
		nullableString(&record.ActorUserID),
		nullableString(&record.MachineID),
		&record.Kind,
		&record.PayloadJSON,
		&record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EventRecord{}, ErrNotFound
		}
		return EventRecord{}, err
	}

	return record, nil
}

func (s *Store) ListMachineEvents(ctx context.Context, machineID string, limit int) ([]EventRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_user_id, machine_id, kind, payload_json, created_at
		FROM events
		WHERE machine_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, strings.TrimSpace(machineID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

func (s *Store) ListActorEvents(ctx context.Context, actorUserID string, limit int) ([]EventRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_user_id, machine_id, kind, payload_json, created_at
		FROM events
		WHERE actor_user_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, strings.TrimSpace(actorUserID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]EventRecord, error) {
	var records []EventRecord
	for rows.Next() {
		var record EventRecord
		if err := rows.Scan(
			&record.ID,
			nullableString(&record.ActorUserID),
			nullableString(&record.MachineID),
			&record.Kind,
			&record.PayloadJSON,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
