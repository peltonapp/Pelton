-- the index of the Markdown notes in the user's notes folder (the vault). The
-- files on disk are the truth: these rows only describe them, so the notes
-- space can open fast and answer link and tag questions without reading every
-- file again. Rows are keyed by the vault's folder as well as the note's path,
-- so pointing the app at another folder never mixes two vaults.
--
-- path is relative to the vault and always uses forward slashes. mtime is in
-- unix nanoseconds; hash is the sha-256 of the file's bytes. headings and
-- aliases are json arrays.
CREATE TABLE IF NOT EXISTS vault_notes (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    vault    TEXT    NOT NULL,
    path     TEXT    NOT NULL,
    title    TEXT    NOT NULL,
    mtime    INTEGER NOT NULL,
    size     INTEGER NOT NULL,
    hash     TEXT    NOT NULL,
    headings TEXT    NOT NULL DEFAULT '[]',
    aliases  TEXT    NOT NULL DEFAULT '[]',
    -- the opening words of the note, for the note list.
    snippet  TEXT    NOT NULL DEFAULT '',
    UNIQUE (vault, path)
);

-- one row per tag a note carries, from its text (#tag) or its front matter.
CREATE TABLE IF NOT EXISTS vault_note_tags (
    note_id INTEGER NOT NULL REFERENCES vault_notes(id) ON DELETE CASCADE,
    tag     TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_vault_note_tags_note ON vault_note_tags(note_id);
CREATE INDEX IF NOT EXISTS idx_vault_note_tags_tag ON vault_note_tags(tag);
