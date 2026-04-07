package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func (s *Store) CreateExec(ctx context.Context, params CreateExecParams) (ExecRecord, error) {
	id := strings.TrimSpace(params.ID)
	if id == "" {
		id = uuid.NewString()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO execs (
			id, user_id, machine_id, host_id, command_text, cwd, state, requested_timeout_seconds
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, strings.TrimSpace(params.UserID), strings.TrimSpace(params.MachineID), params.HostID, strings.TrimSpace(params.CommandText), strings.TrimSpace(params.CWD), strings.TrimSpace(params.State), params.RequestedTimeoutSeconds)
	if err != nil {
		if isUniqueConstraint(err) {
			return ExecRecord{}, ErrConflict
		}
		return ExecRecord{}, err
	}

	return s.GetExecByID(ctx, id)
}

func (s *Store) GetExecByID(ctx context.Context, id string) (ExecRecord, error) {
	row := s.db.QueryRowContext(ctx, execSelectQuery+`
		WHERE ex.id = ?
	`, strings.TrimSpace(id))
	record, err := scanExecRow(row)
	if err != nil {
		return ExecRecord{}, err
	}
	return record, nil
}

func (s *Store) ListExecs(ctx context.Context, ownerEmail string, limit int) ([]ExecRecord, error) {
	if limit <= 0 {
		limit = 25
	}

	query := execSelectQuery
	args := make([]any, 0, 2)
	if email := normalizeEmail(ownerEmail); email != "" {
		query += ` WHERE u.email = ?`
		args = append(args, email)
	}
	query += ` ORDER BY ex.created_at DESC, ex.id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ExecRecord
	for rows.Next() {
		record, err := scanExecRows(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) MarkExecCancelRequested(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE execs
		SET cancel_requested_at = COALESCE(cancel_requested_at, CURRENT_TIMESTAMP),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
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

func (s *Store) CompleteExec(ctx context.Context, params FinishExecParams) (ExecRecord, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE execs
		SET state = ?,
			exit_code = ?,
			failure_class = ?,
			stdout_text = ?,
			stderr_text = ?,
			stdout_truncated = ?,
			stderr_truncated = ?,
			completed_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, strings.TrimSpace(params.State), params.ExitCode, params.FailureClass, params.StdoutText, params.StderrText, boolToInt(params.StdoutTruncated), boolToInt(params.StderrTruncated), strings.TrimSpace(params.ID))
	if err != nil {
		return ExecRecord{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ExecRecord{}, err
	}
	if affected == 0 {
		return ExecRecord{}, ErrNotFound
	}
	return s.GetExecByID(ctx, params.ID)
}

const execSelectQuery = `
	SELECT
		ex.id,
		ex.user_id,
		u.email,
		ex.machine_id,
		m.name,
		ex.host_id,
		ex.command_text,
		ex.cwd,
		ex.state,
		ex.requested_timeout_seconds,
		ex.exit_code,
		ex.failure_class,
		ex.stdout_text,
		ex.stderr_text,
		ex.stdout_truncated,
		ex.stderr_truncated,
		ex.started_at,
		ex.completed_at,
		ex.cancel_requested_at,
		ex.created_at,
		ex.updated_at
	FROM execs ex
	INNER JOIN users u ON u.id = ex.user_id
	INNER JOIN machines m ON m.id = ex.machine_id
`

type execRowScanner interface {
	Scan(dest ...any) error
}

func scanExecRow(row execRowScanner) (ExecRecord, error) {
	record, err := scanExecRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ExecRecord{}, ErrNotFound
		}
		return ExecRecord{}, err
	}
	return record, nil
}

func scanExecRows(rows *sql.Rows) (ExecRecord, error) {
	return scanExecRecord(rows)
}

func scanExecRecord(scanner execRowScanner) (ExecRecord, error) {
	var record ExecRecord
	var stdoutTruncated int
	var stderrTruncated int
	err := scanner.Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.MachineID,
		&record.MachineName,
		nullableString(&record.HostID),
		&record.CommandText,
		&record.CWD,
		&record.State,
		&record.RequestedTimeoutSeconds,
		nullableInt(&record.ExitCode),
		nullableString(&record.FailureClass),
		&record.StdoutText,
		&record.StderrText,
		&stdoutTruncated,
		&stderrTruncated,
		nullableString(&record.StartedAt),
		nullableString(&record.CompletedAt),
		nullableString(&record.CancelRequestedAt),
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return ExecRecord{}, err
	}
	record.StdoutTruncated = stdoutTruncated != 0
	record.StderrTruncated = stderrTruncated != 0
	return record, nil
}

func nullableInt(dest **int) any {
	return sqlNullInt{target: dest}
}

type sqlNullInt struct {
	target **int
}

func (n sqlNullInt) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*n.target = nil
		return nil
	case int64:
		converted := int(value)
		*n.target = &converted
		return nil
	case int:
		converted := value
		*n.target = &converted
		return nil
	case []byte:
		var converted int
		if _, err := fmt.Sscanf(string(value), "%d", &converted); err != nil {
			return err
		}
		*n.target = &converted
		return nil
	default:
		return errors.New("unsupported nullable int source")
	}
}
