CREATE TABLE IF NOT EXISTS api_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    last_used_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_agent TEXT,
    ip_address TEXT,
    revoked_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_expires_at ON api_tokens (expires_at);
