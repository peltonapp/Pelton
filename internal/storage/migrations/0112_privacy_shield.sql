-- The privacy report's counters: one row per message that was scanned, with
-- what it tried to learn about its reader. A row is keyed by the account and
-- the message's Message-ID, so a resync or a second copy of the same mail is
-- not counted twice, and it outlives the message on purpose: the report is
-- history. Clearing the history deletes the rows.
CREATE TABLE IF NOT EXISTS privacy_scans (
    msg_key TEXT PRIMARY KEY,
    sender TEXT NOT NULL,
    sender_name TEXT NOT NULL DEFAULT '',
    list_id TEXT NOT NULL DEFAULT '',
    date TEXT NOT NULL DEFAULT '',
    pixels INTEGER NOT NULL DEFAULT 0,
    links INTEGER NOT NULL DEFAULT 0,
    receipt INTEGER NOT NULL DEFAULT 0,
    -- the tracking companies recognised in the message, separated by '|'
    companies TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_privacy_scans_date ON privacy_scans(date);
CREATE INDEX IF NOT EXISTS idx_privacy_scans_sender ON privacy_scans(sender, date);

-- Unsubscribes Pelton carried out, one per sender, with the list it was for
-- and whether mail kept coming after the grace period. dismissed_at is when
-- the user said to stop telling them; blocked is where new mail from the
-- sender is moved ('junk' or 'trash'), empty when it is not.
CREATE TABLE IF NOT EXISTS unsubscribes (
    sender TEXT PRIMARY KEY,
    list_id TEXT NOT NULL DEFAULT '',
    account_id INTEGER NOT NULL DEFAULT 0,
    method TEXT NOT NULL DEFAULT '',
    unsubscribed_at TEXT NOT NULL,
    still_count INTEGER NOT NULL DEFAULT 0,
    still_at TEXT NOT NULL DEFAULT '',
    still_message_id INTEGER NOT NULL DEFAULT 0,
    dismissed_at TEXT NOT NULL DEFAULT '',
    blocked TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_unsubscribes_list ON unsubscribes(list_id);
