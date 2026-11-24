-- project notes become Markdown files in the notes folder. A note belongs to
-- a project through its front matter (project: and project-id:), so a project
-- needs an id that is not a local row id: uid, written as project-id and in
-- pelton://project/<uid> links.
ALTER TABLE projects ADD COLUMN uid TEXT NOT NULL DEFAULT '';
UPDATE projects SET uid = lower(hex(randomblob(16))) WHERE uid = '';
CREATE UNIQUE INDEX idx_projects_uid ON projects(uid) WHERE uid != '';

-- the notes kept in the database before are written into the notes folder
-- once one is open. vault_path is where a note goes, recorded before the file
-- is written so an interrupted move finds its own file again; migrated_at is
-- set once the file is written, checked and the card links point at it. The
-- rows stay as they were otherwise.
ALTER TABLE project_notes ADD COLUMN vault_path TEXT NOT NULL DEFAULT '';
ALTER TABLE project_notes ADD COLUMN migrated_at TEXT NOT NULL DEFAULT '';

-- the front matter properties of each indexed note, as a json array of
-- {key, value, list}. Every note is read again on the next scan to fill it.
ALTER TABLE vault_notes ADD COLUMN props TEXT NOT NULL DEFAULT '[]';
UPDATE vault_notes SET mtime = 0, hash = '';
