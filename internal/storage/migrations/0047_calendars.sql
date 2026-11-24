-- Calendars: synced from the user's own CalDAV server, or kept only here.
--
-- A calendar account is one CalDAV login; the calendars under it are found by
-- discovery and follow the server. A calendar with no account is local: it
-- lives in this database and nowhere else.

CREATE TABLE IF NOT EXISTS calendar_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    -- the mail account this login was discovered from, or null. Only used to
    -- explain where it came from.
    account_id INTEGER REFERENCES accounts(id) ON DELETE SET NULL,
    -- the CardDAV address book whose login this account shares, when both are
    -- on the same server. The password is then read from the book's keyring
    -- entry rather than stored twice.
    address_book_id INTEGER REFERENCES address_books(id) ON DELETE SET NULL,
    name TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL,
    username TEXT NOT NULL DEFAULT '',
    last_sync_at TEXT NOT NULL DEFAULT '',
    -- the last sync failure in the server's own words, cleared on success.
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS calendars (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    -- null for a local calendar.
    calendar_account_id INTEGER REFERENCES calendar_accounts(id) ON DELETE CASCADE,
    -- the collection's path on the server, empty for a local calendar.
    path TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    -- #rrggbb, from the server or chosen here. Empty means the ui picks one.
    color TEXT NOT NULL DEFAULT '',
    -- set once the user picks a color here, so a sync does not put the
    -- server's back.
    color_set INTEGER NOT NULL DEFAULT 0,
    -- the change markers from the last successful sync. When the server's
    -- ctag still matches, nothing needs fetching.
    ctag TEXT NOT NULL DEFAULT '',
    sync_token TEXT NOT NULL DEFAULT '',
    read_only INTEGER NOT NULL DEFAULT 0,
    visible INTEGER NOT NULL DEFAULT 1,
    position INTEGER NOT NULL DEFAULT 0,
    last_sync_at TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS calendar_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    calendar_id INTEGER NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    uid TEXT NOT NULL,
    -- where the event lives on the server and the version this copy came
    -- from. Both are empty for an event that has never been written there.
    path TEXT NOT NULL DEFAULT '',
    etag TEXT NOT NULL DEFAULT '',
    -- the calendar object exactly as the server holds it (or as it will be
    -- written), kept whole so an edit never drops what another client put in.
    data TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    -- the span the occurrences cover, in utc, so a window query only expands
    -- the events that can reach it. range_end is empty for a series with no
    -- end.
    range_start TEXT NOT NULL DEFAULT '',
    range_end TEXT NOT NULL DEFAULT '',
    recurring INTEGER NOT NULL DEFAULT 0,
    has_alarms INTEGER NOT NULL DEFAULT 0,
    -- a change made here that the server has not taken yet: 'create',
    -- 'update' or 'delete'. Empty once it is written.
    pending TEXT NOT NULL DEFAULT '',
    -- set when the server refused the pending change because its copy moved
    -- on. The change is kept until the user picks a side.
    conflict INTEGER NOT NULL DEFAULT 0,
    sync_error TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    UNIQUE (calendar_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_calendar_events_range ON calendar_events(calendar_id, range_start);
CREATE INDEX IF NOT EXISTS idx_calendar_events_path ON calendar_events(calendar_id, path);
CREATE INDEX IF NOT EXISTS idx_calendar_events_pending ON calendar_events(pending) WHERE pending != '';
CREATE INDEX IF NOT EXISTS idx_calendars_account ON calendars(calendar_account_id);

-- one local calendar, so there is somewhere to put an event before any server
-- is set up. The empty name is shown as the ui's own label for it.
INSERT INTO calendars (calendar_account_id, name, position, created_at)
VALUES (NULL, '', 0, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
