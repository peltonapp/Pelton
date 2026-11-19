-- follow_ups holds the "remind me if nobody replies" reminders set on sent
-- messages. message_id is the sent message's Message-ID without angle
-- brackets, which a reply names in In-Reply-To or References. outbox_id ties
-- the reminder to its queued send, so taking the send back drops it. fired_at
-- stays empty until the reminder came due without a reply.
CREATE TABLE follow_ups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_id TEXT    NOT NULL,
    outbox_id  INTEGER NOT NULL DEFAULT 0,
    subject    TEXT    NOT NULL DEFAULT '',
    recipients TEXT    NOT NULL DEFAULT '',
    sent_at    TEXT    NOT NULL,
    due_at     TEXT    NOT NULL,
    fired_at   TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL
);
CREATE INDEX idx_follow_ups_due ON follow_ups(due_at);
CREATE INDEX idx_follow_ups_outbox ON follow_ups(outbox_id) WHERE outbox_id != 0;
