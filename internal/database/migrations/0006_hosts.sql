CREATE TABLE IF NOT EXISTS hosts (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    region TEXT NOT NULL,
    role TEXT NOT NULL,
    status TEXT NOT NULL,
    labels_json TEXT NOT NULL DEFAULT '{}',
    capabilities_json TEXT NOT NULL DEFAULT '[]',
    runtime_version TEXT NOT NULL DEFAULT '',
    heartbeat_at TEXT,
    total_cpu INTEGER NOT NULL DEFAULT 0,
    allocated_cpu INTEGER NOT NULL DEFAULT 0,
    total_memory_bytes INTEGER NOT NULL DEFAULT 0,
    allocated_memory_bytes INTEGER NOT NULL DEFAULT 0,
    total_disk_bytes INTEGER NOT NULL DEFAULT 0,
    allocated_disk_bytes INTEGER NOT NULL DEFAULT 0,
    available_disk_bytes INTEGER NOT NULL DEFAULT 0,
    machine_count INTEGER NOT NULL DEFAULT 0,
    snapshot_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_hosts_status ON hosts (status);
CREATE INDEX IF NOT EXISTS idx_hosts_region ON hosts (region);
