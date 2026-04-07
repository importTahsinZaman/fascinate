package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type EventStreamDiagnostics struct {
	ActiveSubscribers int     `json:"active_subscribers"`
	LatestEventID     string  `json:"latest_event_id,omitempty"`
	LatestEventKind   string  `json:"latest_event_kind,omitempty"`
	LatestCreatedAt   *string `json:"latest_created_at,omitempty"`
}

func (s *Store) CreateEvent(ctx context.Context, params CreateEventParams) (EventRecord, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO events (id, actor_user_id, machine_id, kind, payload_json)
		VALUES (?, ?, ?, ?, ?)
	`, strings.TrimSpace(params.ID), params.ActorUserID, params.MachineID, strings.TrimSpace(params.Kind), strings.TrimSpace(params.PayloadJSON))
	if err != nil {
		return EventRecord{}, err
	}

	record, err := s.GetEventByID(ctx, params.ID)
	if err != nil {
		return EventRecord{}, err
	}
	s.publishEvent(record)
	return record, nil
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

func (s *Store) SubscribeEvents(buffer int) (<-chan EventRecord, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan EventRecord, buffer)
	s.eventSubsMu.Lock()
	id := s.eventSubsNextID
	s.eventSubsNextID++
	s.eventSubs[id] = ch
	s.eventSubsMu.Unlock()

	return ch, func() {
		s.eventSubsMu.Lock()
		subscriber, ok := s.eventSubs[id]
		if ok {
			delete(s.eventSubs, id)
		}
		s.eventSubsMu.Unlock()
		if ok {
			close(subscriber)
		}
	}
}

func (s *Store) publishEvent(record EventRecord) {
	s.eventSubsMu.Lock()
	defer s.eventSubsMu.Unlock()
	for _, ch := range s.eventSubs {
		select {
		case ch <- record:
		default:
		}
	}
}

func (s *Store) ListActorEventsAfter(ctx context.Context, actorUserID, afterCreatedAt, afterID string, limit int) ([]EventRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, actor_user_id, machine_id, kind, payload_json, created_at
		FROM events
		WHERE actor_user_id = ?
		  AND (
		    created_at > ?
		    OR (created_at = ? AND id > ?)
		  )
		ORDER BY created_at ASC, id ASC
		LIMIT ?
	`, strings.TrimSpace(actorUserID), strings.TrimSpace(afterCreatedAt), strings.TrimSpace(afterCreatedAt), strings.TrimSpace(afterID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

func (s *Store) EventStreamDiagnostics(ctx context.Context) (EventStreamDiagnostics, error) {
	s.eventSubsMu.Lock()
	activeSubscribers := len(s.eventSubs)
	s.eventSubsMu.Unlock()

	diag := EventStreamDiagnostics{
		ActiveSubscribers: activeSubscribers,
	}

	record, err := s.latestEvent(ctx)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return diag, nil
		}
		return EventStreamDiagnostics{}, err
	}
	diag.LatestEventID = record.ID
	diag.LatestEventKind = record.Kind
	diag.LatestCreatedAt = &record.CreatedAt
	return diag, nil
}

func (s *Store) latestEvent(ctx context.Context) (EventRecord, error) {
	var record EventRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT id, actor_user_id, machine_id, kind, payload_json, created_at
		FROM events
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`).Scan(
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
