package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
)

type CreateSSHKeyParams struct {
	UserID      string
	Name        string
	PublicKey   string
	Fingerprint string
}

func (s *Store) CreateSSHKey(ctx context.Context, params CreateSSHKeyParams) (SSHKeyRecord, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ssh_keys (id, user_id, name, public_key, fingerprint)
		VALUES (?, ?, ?, ?, ?)
	`, uuid.NewString(), params.UserID, strings.TrimSpace(params.Name), strings.TrimSpace(params.PublicKey), strings.TrimSpace(params.Fingerprint))
	if err != nil {
		if isUniqueConstraint(err) {
			return SSHKeyRecord{}, ErrConflict
		}
		return SSHKeyRecord{}, err
	}

	return s.GetSSHKeyByFingerprint(ctx, params.Fingerprint)
}

func (s *Store) GetSSHKeyByFingerprint(ctx context.Context, fingerprint string) (SSHKeyRecord, error) {
	var record SSHKeyRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT
			k.id,
			k.user_id,
			u.email,
			k.name,
			k.public_key,
			k.fingerprint,
			k.created_at
		FROM ssh_keys k
		INNER JOIN users u ON u.id = k.user_id
		WHERE k.fingerprint = ?
	`, strings.TrimSpace(fingerprint)).Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.Name,
		&record.PublicKey,
		&record.Fingerprint,
		&record.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SSHKeyRecord{}, ErrNotFound
		}
		return SSHKeyRecord{}, err
	}

	return record, nil
}
