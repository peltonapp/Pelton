-- Lua modules the user writes once and loads from several scripts with
-- require("name"). The name is what require takes, so it is unique. Times
-- are RFC 3339.

CREATE TABLE IF NOT EXISTS script_libraries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    origin TEXT NOT NULL DEFAULT 'user',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
