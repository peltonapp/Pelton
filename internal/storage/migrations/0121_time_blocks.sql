-- Time blocks: time set aside in the calendar to work on a task or deal with
-- a mail. A block lives on this device only, unless it is also written to one
-- of the user's calendars as a busy event; it then keeps the link to that
-- event so a move or a deletion reaches it.
CREATE TABLE IF NOT EXISTS time_blocks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    -- empty while the title comes from the linked mail, which is read when
    -- the block is shown so a vault mail's subject is never stored here.
    title TEXT NOT NULL DEFAULT '',
    -- utc instants, rfc3339.
    start_at TEXT NOT NULL,
    end_at TEXT NOT NULL,
    -- the task's UID, which survives the task moving to another list.
    task_uid TEXT NOT NULL DEFAULT '',
    -- the mail it is for: the stored message and its Message-ID, which finds
    -- the conversation again after the message was moved or synced anew.
    message_id INTEGER NOT NULL DEFAULT 0,
    message_header TEXT NOT NULL DEFAULT '',
    done INTEGER NOT NULL DEFAULT 0,
    -- the calendar the block is written to as a busy event, null for a block
    -- kept here only, and that event's UID.
    calendar_id INTEGER REFERENCES calendars(id) ON DELETE SET NULL,
    event_uid TEXT NOT NULL DEFAULT '',
    -- what the event is called: '' follows the setting, 'generic' always the
    -- generic title, 'title' the block's own title.
    event_title TEXT NOT NULL DEFAULT '',
    -- '' while the block and its event agree, 'out_of_step' when the event
    -- changed in a way the block cannot follow, 'event_gone' when the event
    -- was deleted from the calendar.
    link_state TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_time_blocks_start ON time_blocks(start_at);
CREATE INDEX IF NOT EXISTS idx_time_blocks_task ON time_blocks(task_uid) WHERE task_uid != '';
