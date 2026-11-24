-- Promises found in mail: what the user or someone else committed to, as a
-- suggestion on the mail until the user accepts it (it then has a task) or
-- dismisses it. A row is keyed by the mail's Message-ID and the sentence, so
-- a resync or a second copy of the mail never brings back one the user
-- dismissed.

CREATE TABLE IF NOT EXISTS promises (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    -- the Message-ID of the mail, without angle brackets
    mail_id TEXT NOT NULL,
    thread_id INTEGER NOT NULL DEFAULT 0,
    -- 'mine' when the user owes it, 'theirs' when it is owed to the user
    direction TEXT NOT NULL,
    person_name TEXT NOT NULL DEFAULT '',
    person_email TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    quote TEXT NOT NULL,
    quote_key TEXT NOT NULL,
    -- a date (2006-01-02), rfc3339, or empty
    due TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0,
    -- 'rules' or 'ai'
    source TEXT NOT NULL DEFAULT 'rules',
    -- 'suggested', 'accepted', 'dismissed' or 'closed'
    state TEXT NOT NULL DEFAULT 'suggested',
    task_uid TEXT NOT NULL DEFAULT '',
    -- the reply in the thread that closed it (theirs) or offers to (mine)
    reply_id TEXT NOT NULL DEFAULT '',
    reply_at TEXT NOT NULL DEFAULT '',
    -- replies an undo said do not close it, one per line
    keep_open TEXT NOT NULL DEFAULT '',
    mail_date TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (account_id, mail_id, quote_key)
);

CREATE INDEX IF NOT EXISTS idx_promises_state ON promises(state, account_id);
CREATE INDEX IF NOT EXISTS idx_promises_mail ON promises(account_id, mail_id);

-- the mails already read for promises, so each is read once
CREATE TABLE IF NOT EXISTS promise_scans (
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    mail_id TEXT NOT NULL,
    source TEXT NOT NULL,
    scanned_at TEXT NOT NULL,
    PRIMARY KEY (account_id, mail_id)
);

-- senders whose mail is no longer read for promises
CREATE TABLE IF NOT EXISTS promise_quiet_senders (
    email TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);
