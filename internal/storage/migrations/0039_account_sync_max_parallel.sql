-- Per-account override for the parallel sync connection limit. NULL means the
-- account follows the global sync_max_parallel setting.

ALTER TABLE accounts ADD COLUMN sync_max_parallel INTEGER NULL;
