-- Columns and indexes for the newsletter manager and the mailbox insights page.

-- list_id is the List-Id header of mailing list mail, '' when there is none or
-- the message was cached before this column existed.
ALTER TABLE messages ADD COLUMN list_id TEXT NOT NULL DEFAULT '';

-- The per sender aggregates group by the bare lower-case address. Synced mail
-- keeps the whole "Name <addr>" string in from_address, so the address is cut
-- out by an expression, which must match senderEmailSQL in mailstats.go
-- character for character for sqlite to use these indexes.
--
-- Every column a statistic reads is in its index, from_address included since
-- the expression is computed from it, so no aggregate opens a message row and
-- its bodies. The mail splits into two partial indexes: list mail (offering
-- List-Unsubscribe) for the newsletter manager, everything else for the people
-- statistics. The list index carries list_unsubscribe because sqlite only
-- treats an equality in a partial index's WHERE as known.
CREATE INDEX idx_messages_newsletters ON messages(
    lower(trim(CASE WHEN instr(from_address, '<') > 0 AND instr(from_address, '>') > instr(from_address, '<') THEN substr(from_address, instr(from_address, '<') + 1, instr(from_address, '>') - instr(from_address, '<') - 1) ELSE from_address END)),
    date, folder_id, flags, size_bytes, pending_delete, from_address, list_unsubscribe
) WHERE list_unsubscribe != '';

CREATE INDEX idx_messages_people ON messages(
    lower(trim(CASE WHEN instr(from_address, '<') > 0 AND instr(from_address, '>') > instr(from_address, '<') THEN substr(from_address, instr(from_address, '<') + 1, instr(from_address, '>') - instr(from_address, '<') - 1) ELSE from_address END)),
    date, folder_id, size_bytes, pending_delete, from_address
) WHERE list_unsubscribe = '';

-- volume over a date range, the weekday and hour grid, and storage per folder.
CREATE INDEX idx_messages_date ON messages(date, folder_id, pending_delete, size_bytes);

-- reply times walk each conversation in date order. It leads with thread_id,
-- so it also serves every lookup the plain thread index did.
CREATE INDEX idx_messages_thread_date ON messages(thread_id, date, folder_id, pending_delete, from_address);
DROP INDEX IF EXISTS idx_messages_thread;

-- the biggest attachments.
CREATE INDEX idx_attachments_size ON attachments(size_bytes);
