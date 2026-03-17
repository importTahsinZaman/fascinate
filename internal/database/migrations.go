package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return err
	}

	entries, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)

	for _, entry := range entries {
		name := filepath.Base(entry)
		applied, err := migrationApplied(ctx, s.db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		body, err := migrationFiles.ReadFile(entry)
		if err != nil {
			return err
		}

		if err := applyMigration(ctx, s.db, name, string(body)); err != nil {
			return err
		}
	}

	return nil
}

func migrationApplied(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, name).Scan(&count); err != nil {
		return false, err
	}

	return count > 0, nil
}

func applyMigration(ctx context.Context, db *sql.DB, name, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("run migration %s: %w", name, err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
		return err
	}

	return tx.Commit()
}
