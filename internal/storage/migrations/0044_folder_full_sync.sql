-- When a folder last finished a full reconcile, in unix seconds. NULL means
-- never. Ordinary syncs only ask the server what changed; a folder whose full
-- reconcile is older than sync_full_reconcile_days is re-listed in full in the
-- background, which heals anything a delta missed.

ALTER TABLE folders ADD COLUMN last_full_sync_at INTEGER NULL;
