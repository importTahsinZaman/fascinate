CREATE TABLE IF NOT EXISTS shells (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    host_id TEXT REFERENCES hosts(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    tmux_session TEXT NOT NULL,
    state TEXT NOT NULL,
    cwd TEXT NOT NULL DEFAULT '',
    last_error TEXT,
    last_attached_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_shells_user_deleted_created
    ON shells (user_id, deleted_at, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_shells_machine_deleted_created
    ON shells (machine_id, deleted_at, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_shells_machine_tmux_active
    ON shells (machine_id, tmux_session)
    WHERE deleted_at IS NULL;
