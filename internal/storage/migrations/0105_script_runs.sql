-- The run log: one row per run of a script, with what it did as a JSON list
-- of steps. Rows older than the retention the user set are pruned.

CREATE TABLE IF NOT EXISTS script_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    script_id INTEGER NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
    trigger TEXT NOT NULL,
    started_at TEXT NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    steps TEXT NOT NULL DEFAULT '[]',
    changes INTEGER NOT NULL DEFAULT 0,
    depth INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_script_runs_script ON script_runs(script_id, id);
