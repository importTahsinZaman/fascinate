DELETE FROM snapshots
WHERE deleted_at IS NOT NULL;

DELETE FROM machines
WHERE deleted_at IS NOT NULL;
