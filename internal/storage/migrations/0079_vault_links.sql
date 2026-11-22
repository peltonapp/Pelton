-- the links each indexed note makes: wiki links, embeds, Markdown links to
-- other files and pelton:// links to mail, events, contacts and tasks. target
-- is the link as written, without the part after '#', which is in subpath.
-- Resolving a target to a note happens in the app, since adding or renaming a
-- note changes what a bare name points at.
CREATE TABLE IF NOT EXISTS vault_note_links (
    note_id INTEGER NOT NULL REFERENCES vault_notes(id) ON DELETE CASCADE,
    kind    TEXT    NOT NULL,
    embed   INTEGER NOT NULL DEFAULT 0,
    target  TEXT    NOT NULL,
    subpath TEXT    NOT NULL DEFAULT '',
    display TEXT    NOT NULL DEFAULT '',
    line    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_vault_note_links_note ON vault_note_links(note_id);
CREATE INDEX IF NOT EXISTS idx_vault_note_links_target ON vault_note_links(target);
