CREATE TABLE IF NOT EXISTS snapshots (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    owner_user_id TEXT NOT NULL,
    source_machine_id TEXT,
    runtime_name TEXT NOT NULL UNIQUE,
    state TEXT NOT NULL,
    artifact_dir TEXT NOT NULL,
    disk_size_bytes INTEGER NOT NULL DEFAULT 0,
    memory_size_bytes INTEGER NOT NULL DEFAULT 0,
    runtime_version TEXT NOT NULL DEFAULT '',
    firmware_version TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TEXT,
    FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (source_machine_id) REFERENCES machines(id) ON DELETE SET NULL,
    UNIQUE (owner_user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_snapshots_owner_user_id ON snapshots (owner_user_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_source_machine_id ON snapshots (source_machine_id);
