-- projects: a board of tasks plus the mail, events, people and notes that
-- belong with them. Everything here is local to this device; the tasks
-- themselves stay VTODOs in their lists and sync as before.
CREATE TABLE projects (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL,
    -- #rrggbb from the palette, empty for the one picked by id.
    color       TEXT    NOT NULL DEFAULT '',
    -- the name of the icon the ui draws, empty for the default.
    icon        TEXT    NOT NULL DEFAULT '',
    description TEXT    NOT NULL DEFAULT '',
    -- 'active' or 'archived'.
    status      TEXT    NOT NULL DEFAULT 'active',
    -- dates as 2006-01-02, empty for none.
    start_date  TEXT    NOT NULL DEFAULT '',
    due_date    TEXT    NOT NULL DEFAULT '',
    position    INTEGER NOT NULL DEFAULT 0,
    -- the task list new cards are made in, 0 for the tasks default.
    list_id     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL,
    archived_at TEXT    NOT NULL DEFAULT ''
);

-- the columns of a project's board, in position order. One column at most is
-- the done column: a card dropped there completes its task, and a task
-- completed anywhere else moves there.
CREATE TABLE project_columns (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    -- how many cards the column should hold, 0 for no limit.
    wip_limit  INTEGER NOT NULL DEFAULT 0,
    is_done    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_project_columns_project ON project_columns(project_id, position);
CREATE UNIQUE INDEX idx_project_columns_done ON project_columns(project_id) WHERE is_done = 1;

-- a card is a task on a board, keyed by the task's UID: it survives the task
-- moving between lists, and a deleted task brought back by undo finds its
-- place again. A task is on one board at most. prev_column_id remembers where
-- a card came from before it went to the done column, so reopening the task
-- puts it back. source_kind and source_ref are null for a card made here; a
-- card mirrored from somewhere else (an issue tracker, later) names it there.
CREATE TABLE project_cards (
    task_uid       TEXT    PRIMARY KEY,
    project_id     INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    column_id      INTEGER NOT NULL REFERENCES project_columns(id) ON DELETE CASCADE,
    position       INTEGER NOT NULL DEFAULT 0,
    prev_column_id INTEGER NOT NULL DEFAULT 0,
    source_kind    TEXT,
    source_ref     TEXT,
    added_at       TEXT    NOT NULL
);
CREATE INDEX idx_project_cards_column ON project_cards(project_id, column_id, position);

-- cards look their tasks up by UID.
CREATE INDEX IF NOT EXISTS idx_calendar_events_uid ON calendar_events(uid);
