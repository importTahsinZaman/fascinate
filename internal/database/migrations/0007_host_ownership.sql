ALTER TABLE machines ADD COLUMN host_id TEXT REFERENCES hosts(id) ON DELETE SET NULL;
ALTER TABLE snapshots ADD COLUMN host_id TEXT REFERENCES hosts(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_machines_host_id ON machines (host_id);
CREATE INDEX IF NOT EXISTS idx_snapshots_host_id ON snapshots (host_id);
