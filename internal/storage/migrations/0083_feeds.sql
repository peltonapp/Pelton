-- The Feeds space: folders, the feeds the user subscribed to, and their
-- entries. Everything here is local; feeds are fetched straight from the
-- servers that publish them.

CREATE TABLE feed_folders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL
);

-- url is the feed document, site_url the page it belongs to. title is what the
-- feed calls itself; custom_title, when set, is what the user renamed it to.
-- icon is a data: url of the site's icon, fetched from that site only.
--
-- The fetch state: etag and last_modified are the validators of the last full
-- response, next_fetch_at is when the scheduler may fetch again (pushed out by
-- Cache-Control, Retry-After and failures), last_error is the reason the last
-- fetch failed ('' after a success) and error_count how many in a row did.
-- interval_minutes 0 follows the app-wide refresh interval.
CREATE TABLE feeds (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    folder_id        INTEGER REFERENCES feed_folders(id) ON DELETE SET NULL,
    url              TEXT    NOT NULL UNIQUE,
    site_url         TEXT    NOT NULL DEFAULT '',
    title            TEXT    NOT NULL DEFAULT '',
    custom_title     TEXT    NOT NULL DEFAULT '',
    description      TEXT    NOT NULL DEFAULT '',
    icon             TEXT    NOT NULL DEFAULT '',
    etag             TEXT    NOT NULL DEFAULT '',
    last_modified    TEXT    NOT NULL DEFAULT '',
    last_fetched_at  TEXT    NOT NULL DEFAULT '',
    last_success_at  TEXT    NOT NULL DEFAULT '',
    next_fetch_at    TEXT    NOT NULL DEFAULT '',
    last_error       TEXT    NOT NULL DEFAULT '',
    error_count      INTEGER NOT NULL DEFAULT 0,
    interval_minutes INTEGER NOT NULL DEFAULT 0,
    load_images      INTEGER NOT NULL DEFAULT 0,
    notify           INTEGER NOT NULL DEFAULT 0,
    position         INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT    NOT NULL
);
CREATE INDEX idx_feeds_folder ON feeds(folder_id, position);

-- One entry of a feed, unique per feed by guid. published is when the feed
-- says it appeared, or when it was first seen when the feed gave no date.
-- summary and content are the feed's html, unsanitized; search_text is the
-- plain text the search index reads. enclosures is a json array.
CREATE TABLE feed_entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id     INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid        TEXT    NOT NULL,
    url         TEXT    NOT NULL DEFAULT '',
    title       TEXT    NOT NULL DEFAULT '',
    author      TEXT    NOT NULL DEFAULT '',
    summary     TEXT    NOT NULL DEFAULT '',
    content     TEXT    NOT NULL DEFAULT '',
    search_text TEXT    NOT NULL DEFAULT '',
    image       TEXT    NOT NULL DEFAULT '',
    enclosures  TEXT    NOT NULL DEFAULT '[]',
    published   TEXT    NOT NULL,
    updated     TEXT    NOT NULL DEFAULT '',
    fetched_at  TEXT    NOT NULL,
    read        INTEGER NOT NULL DEFAULT 0,
    starred     INTEGER NOT NULL DEFAULT 0,
    read_later  INTEGER NOT NULL DEFAULT 0,
    UNIQUE (feed_id, guid)
);
CREATE INDEX idx_feed_entries_feed ON feed_entries(feed_id, published DESC);
CREATE INDEX idx_feed_entries_published ON feed_entries(published DESC);
CREATE INDEX idx_feed_entries_unread ON feed_entries(read, feed_id) WHERE read = 0;
CREATE INDEX idx_feed_entries_starred ON feed_entries(starred) WHERE starred = 1;
CREATE INDEX idx_feed_entries_later ON feed_entries(read_later) WHERE read_later = 1;
CREATE INDEX idx_feed_entries_url ON feed_entries(feed_id, url);

-- The guids retention removed, so an entry still listed in the feed does not
-- come back as new on the next fetch. They are dropped once old enough that
-- no feed keeps an entry that long.
CREATE TABLE feed_pruned (
    feed_id   INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid      TEXT    NOT NULL,
    pruned_at TEXT    NOT NULL,
    PRIMARY KEY (feed_id, guid)
);
