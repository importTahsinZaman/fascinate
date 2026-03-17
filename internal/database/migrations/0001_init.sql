CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    is_admin INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ssh_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    public_key TEXT NOT NULL UNIQUE,
    fingerprint TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS machines (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    owner_user_id TEXT NOT NULL,
    incus_name TEXT NOT NULL UNIQUE,
    state TEXT NOT NULL,
    primary_port INTEGER NOT NULL DEFAULT 3000,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TEXT,
    FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS machine_ports (
    id TEXT PRIMARY KEY,
    machine_id TEXT NOT NULL,
    port INTEGER NOT NULL,
    protocol TEXT NOT NULL,
    is_primary INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (machine_id) REFERENCES machines(id) ON DELETE CASCADE,
    UNIQUE (machine_id, port, protocol)
);

CREATE TABLE IF NOT EXISTS email_codes (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    email TEXT NOT NULL,
    purpose TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    actor_user_id TEXT,
    machine_id TEXT,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL,
    FOREIGN KEY (machine_id) REFERENCES machines(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_ssh_keys_user_id ON ssh_keys (user_id);
CREATE INDEX IF NOT EXISTS idx_machines_owner_user_id ON machines (owner_user_id);
CREATE INDEX IF NOT EXISTS idx_machine_ports_machine_id ON machine_ports (machine_id);
CREATE INDEX IF NOT EXISTS idx_email_codes_email ON email_codes (email);
CREATE INDEX IF NOT EXISTS idx_events_machine_id ON events (machine_id);
CREATE INDEX IF NOT EXISTS idx_events_actor_user_id ON events (actor_user_id);

