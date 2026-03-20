ALTER TABLE machines ADD COLUMN source_snapshot_id TEXT REFERENCES snapshots(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_machines_source_snapshot_id ON machines (source_snapshot_id);
