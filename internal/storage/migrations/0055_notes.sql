-- private notes the user keeps on a message or a conversation. They live here
-- only: nothing syncs them, and nothing adds them to the message.
--
-- A message note is keyed by the message's Message-ID within its account, so
-- it follows the message between folders; a message without one is keyed by
-- '#' and its row id. A conversation note is keyed by its thread and has an
-- empty message_key.
CREATE TABLE notes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    thread_id   INTEGER NOT NULL DEFAULT 0,
    message_key TEXT    NOT NULL DEFAULT '',
    body        TEXT    NOT NULL,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);
CREATE UNIQUE INDEX idx_notes_message ON notes(account_id, message_key) WHERE message_key != '';
CREATE UNIQUE INDEX idx_notes_thread ON notes(thread_id) WHERE message_key = '';
