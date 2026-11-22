-- Each script's own key value storage, written by its store module. value is
-- JSON.

CREATE TABLE IF NOT EXISTS script_kv (
    script_id INTEGER NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (script_id, key)
);
