-- notes kept with a project: a title and a Markdown body. They live here
-- only and go with the project when it is deleted.
CREATE TABLE project_notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title      TEXT    NOT NULL DEFAULT '',
    body       TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);
CREATE INDEX idx_project_notes_project ON project_notes(project_id, updated_at);

-- conversations whose "add to project" suggestion the user waved away. The
-- suggestion is not offered for them again.
CREATE TABLE project_dismissals (
    thread_id  INTEGER PRIMARY KEY,
    created_at TEXT    NOT NULL
);
