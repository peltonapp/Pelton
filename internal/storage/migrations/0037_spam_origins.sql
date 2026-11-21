-- Where a message sat before the user marked it as spam.
--
-- The server does not say which folder a message would have reached, so "Not
-- spam" can only send it back where it came from if that was written down when
-- it was marked. Keyed by Message-ID, since the move gives it a new UID. Only
-- marks made here are known; spam the server filtered goes back to the inbox.
CREATE TABLE spam_origins (
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_id TEXT    NOT NULL,
    folder_id  INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    marked_at  TEXT    NOT NULL,
    PRIMARY KEY (account_id, message_id)
);
