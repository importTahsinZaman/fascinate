CREATE TABLE IF NOT EXISTS execs (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    machine_id TEXT NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    host_id TEXT REFERENCES hosts(id) ON DELETE SET NULL,
    command_text TEXT NOT NULL,
    cwd TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    requested_timeout_seconds INTEGER NOT NULL DEFAULT 0,
    exit_code INTEGER,
    failure_class TEXT,
    stdout_text TEXT NOT NULL DEFAULT '',
    stderr_text TEXT NOT NULL DEFAULT '',
    stdout_truncated INTEGER NOT NULL DEFAULT 0,
    stderr_truncated INTEGER NOT NULL DEFAULT 0,
    started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TEXT,
    cancel_requested_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_execs_user_created_at ON execs(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_execs_machine_created_at ON execs(machine_id, created_at DESC, id DESC);
