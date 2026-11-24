-- Automatic encryption. Times are unix seconds, 0 for never.

-- Autocrypt Level 1 peer state, one row per correspondent address.
CREATE TABLE autocrypt_peers (
    addr                TEXT PRIMARY KEY,
    last_seen           INTEGER NOT NULL DEFAULT 0,
    autocrypt_timestamp INTEGER NOT NULL DEFAULT 0,
    public_key          BLOB,
    prefer_encrypt      TEXT NOT NULL DEFAULT 'nopreference',
    gossip_timestamp    INTEGER NOT NULL DEFAULT 0,
    gossip_key          BLOB
);

-- what each lookup source answered for an address. A NULL key is a lookup
-- that found nothing, kept so it is not repeated on every keystroke.
CREATE TABLE key_lookup_cache (
    addr       TEXT NOT NULL,
    source     TEXT NOT NULL,
    public_key BLOB,
    fetched_at INTEGER NOT NULL,
    PRIMARY KEY (addr, source)
);

-- every key ever learned for an address and when it was first seen, so the
-- reader can tell a key seen before from a new one.
CREATE TABLE contact_key_history (
    addr        TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    source      TEXT NOT NULL,
    first_seen  INTEGER NOT NULL,
    PRIMARY KEY (addr, fingerprint)
);

-- the key last used or accepted for sending to an address, whether the user
-- compared its fingerprint, and the user's encryption preference for it.
CREATE TABLE contact_keys (
    addr        TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT '',
    accepted_at INTEGER NOT NULL DEFAULT 0,
    verified    INTEGER NOT NULL DEFAULT 0,
    preference  TEXT NOT NULL DEFAULT ''
);

-- every request a key lookup made, for the user to see.
CREATE TABLE key_lookup_log (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    at      INTEGER NOT NULL,
    source  TEXT NOT NULL,
    addr    TEXT NOT NULL,
    outcome TEXT NOT NULL
);
