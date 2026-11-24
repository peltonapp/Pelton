-- snippets are reusable text blocks the composer inserts: a toolbar picker,
-- the command palette, or a trigger word typed in the body. body is plain
-- text with {placeholders}; subject and the recipient lists are optional and
-- only fill fields that are still empty. position is the order the user put
-- them in.
CREATE TABLE snippets (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT    NOT NULL,
    body         TEXT    NOT NULL DEFAULT '',
    subject      TEXT    NOT NULL DEFAULT '',
    to_addresses TEXT    NOT NULL DEFAULT '',
    cc_addresses TEXT    NOT NULL DEFAULT '',
    trigger_word TEXT    NOT NULL DEFAULT '',
    position     INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT    NOT NULL,
    updated_at   TEXT    NOT NULL
);
