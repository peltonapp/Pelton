-- Stubs whose search entry is missing or stale: a list refresh that changes
-- a stub's subject, sender, recipients, date or preview drops its
-- search_state to 0. The reindex pass finds them behind the id watermark
-- through this index, as idx_messages_search_pending finds complete rows
-- whose body is not indexed.

CREATE INDEX IF NOT EXISTS idx_messages_search_stale_stub ON messages(id) WHERE body_complete = 0 AND search_state = 0;
