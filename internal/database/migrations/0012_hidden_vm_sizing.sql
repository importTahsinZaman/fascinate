ALTER TABLE machines ADD COLUMN disk_usage_bytes INTEGER NOT NULL DEFAULT 0;

UPDATE machines
SET disk_usage_bytes = CASE
    WHEN disk_usage_bytes > 0 THEN disk_usage_bytes
    WHEN disk_bytes > 0 THEN disk_bytes
    ELSE 0
END;
