-- Body-fetch targets are a folder's incomplete rows, newest first. Without
-- body_complete in the index every row of the folder is visited to find them.

CREATE INDEX IF NOT EXISTS idx_messages_folder_body_date ON messages(folder_id, body_complete, date DESC, uid DESC);
