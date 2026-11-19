-- threads hands out conversation ids. AUTOINCREMENT keeps an id from being
-- reused after its thread was merged away or its account removed.
CREATE TABLE threads (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE
);

-- thread_keys maps every Message-ID a conversation has seen, including ids
-- only referenced by a reply, to its thread. A parent that arrives after its
-- reply finds the thread here, and a message linking two threads merges them.
CREATE TABLE thread_keys (
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    key        TEXT    NOT NULL,
    thread_id  INTEGER NOT NULL,
    PRIMARY KEY (account_id, key)
);
CREATE INDEX idx_thread_keys_thread ON thread_keys(thread_id);

-- thread_id 0 means not threaded yet; the backfill walks those rows.
-- thread_subject is the subject without reply prefixes, and thread_loose marks
-- a reply that carried no threading headers, both for the subject fallback.
ALTER TABLE messages ADD COLUMN thread_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN thread_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN thread_loose INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_messages_thread ON messages(thread_id);
CREATE INDEX idx_messages_thread_subject ON messages(account_id, thread_subject);

-- message_replies records what a message sent from Pelton answered or
-- forwarded, keyed by Message-ID on both sides so it survives the sent copy
-- arriving later through the Sent folder. sent stays 0 until the server
-- accepted the message.
CREATE TABLE message_replies (
    account_id          INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    reply_message_id    TEXT    NOT NULL,
    original_message_id TEXT    NOT NULL,
    kind                TEXT    NOT NULL,
    sent                INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT    NOT NULL,
    PRIMARY KEY (account_id, reply_message_id, kind)
);
CREATE INDEX idx_message_replies_original ON message_replies(account_id, original_message_id);
