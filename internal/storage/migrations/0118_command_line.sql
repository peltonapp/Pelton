-- The pelton command. Every request it makes is logged here with what it
-- touched and how it ended. Message ids, folder and account names, counts,
-- never subjects, bodies or addresses.
CREATE TABLE cli_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    command    TEXT NOT NULL,
    touched    TEXT NOT NULL DEFAULT '',
    -- 'ok' or the error code the command got.
    result     TEXT NOT NULL,
    -- the switch that refused the request, '' for any other outcome.
    switch     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

-- whether the pelton command reads an unlocked vault, off like the scripts
-- and MCP switches.
ALTER TABLE mail_vaults ADD COLUMN allow_cli INTEGER NOT NULL DEFAULT 0;
