-- Covering indexes for the message list, its counts and the unified views.
-- The columns they filter on sit at the end of each row, past the bodies, so
-- these keep list reads out of the row store.

-- the list, its counts and the conversation list.
CREATE INDEX idx_messages_listing ON messages(
    folder_id, pending_delete, snooze_hidden, pinned, date, uid,
    flags, thread_id, account_id, message_id
);

-- the other copies of a message, for the one-copy rule of Flagged and Unread.
CREATE INDEX idx_messages_copies ON messages(
    message_id, account_id, pending_delete, snooze_hidden, folder_id, flags
);
