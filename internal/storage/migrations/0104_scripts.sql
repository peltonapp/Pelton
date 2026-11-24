-- The user's automations. A script is Lua (source) or blocks (blocks, a JSON
-- tree compiled to Lua when it runs). trigger, permissions, approved,
-- settings_schema, settings and allow_unasked are JSON. Times are RFC 3339,
-- '' when unset.

CREATE TABLE IF NOT EXISTS scripts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT '',
    blocks TEXT NOT NULL DEFAULT '',
    trigger TEXT NOT NULL,
    permissions TEXT NOT NULL DEFAULT '[]',
    approved TEXT NOT NULL DEFAULT '[]',
    enabled INTEGER NOT NULL DEFAULT 0,
    origin TEXT NOT NULL DEFAULT 'user',
    settings_schema TEXT NOT NULL DEFAULT '[]',
    settings TEXT NOT NULL DEFAULT '{}',
    catch_up TEXT NOT NULL DEFAULT 'live',
    catch_up_cap INTEGER NOT NULL DEFAULT 50,
    allow_unasked TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    enabled_at TEXT NOT NULL DEFAULT '',
    alive_at TEXT NOT NULL DEFAULT '',
    last_run_at TEXT NOT NULL DEFAULT ''
);
