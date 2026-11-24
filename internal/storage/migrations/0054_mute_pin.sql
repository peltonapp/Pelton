-- a muted conversation keeps its mail out of the inbox views and quiet in
-- notifications. It stays in every other folder and in search.
ALTER TABLE threads ADD COLUMN muted INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_threads_muted ON threads(id) WHERE muted = 1;

-- pinned messages list above the rest. pin_pending marks a pin change the next
-- sync still has to store on the server as the $Pinned keyword.
ALTER TABLE messages ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE messages ADD COLUMN pin_pending INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_messages_pinned ON messages(folder_id, thread_id) WHERE pinned = 1;
