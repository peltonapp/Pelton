-- what a project or one of its cards points at. task_uid is empty for a link
-- of the project itself and names the card otherwise.
--
-- kind 'mail' is a conversation: ref is the Message-ID of a message in it,
-- without angle brackets, and account_id its account. The conversation is
-- found through thread_keys, so it follows merges and a sent message that only
-- arrives later through the Sent folder. thread_id is the conversation as it
-- was known when the link was made, for a message without a Message-ID.
-- kind 'event' is an event by UID, 'contact' a person by lower-case address,
-- 'note' a project note by its id. label is what the link was called when it
-- was made, shown while the thing itself cannot be found.
CREATE TABLE project_links (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id  INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    task_uid    TEXT    NOT NULL DEFAULT '',
    kind        TEXT    NOT NULL,
    account_id  INTEGER NOT NULL DEFAULT 0,
    thread_id   INTEGER NOT NULL DEFAULT 0,
    ref         TEXT    NOT NULL,
    label       TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL
);
CREATE UNIQUE INDEX idx_project_links_unique ON project_links(project_id, task_uid, kind, account_id, ref);
CREATE INDEX idx_project_links_kind ON project_links(kind, account_id, ref);
CREATE INDEX idx_project_links_thread ON project_links(thread_id) WHERE thread_id != 0;
