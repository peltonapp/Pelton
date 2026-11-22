-- Guided imports from another mail program. A job is one run the user set
-- up: where it reads from, what they picked and where it goes, kept as JSON
-- so an interrupted run can carry on after a restart.
CREATE TABLE import_jobs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source_kind TEXT NOT NULL,
    source_path TEXT NOT NULL,
    plan        TEXT NOT NULL,
    -- 'running', 'paused', 'cancelled', 'done' or 'failed'.
    state       TEXT NOT NULL,
    -- the counters and the report, as JSON. The report names folders, files
    -- and reasons, never subjects, bodies or addresses.
    progress    TEXT NOT NULL DEFAULT '{}',
    report      TEXT NOT NULL DEFAULT '{}',
    error       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- how far each part of a job got: a byte offset into an mbox or a position in
-- a sorted list of message files. done marks a part finished.
CREATE TABLE import_positions (
    job_id   INTEGER NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
    part     TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    done     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (job_id, part)
);
