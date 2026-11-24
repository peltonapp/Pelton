-- How the reader shows mail from one sender, remembered per bare address:
-- whether html mail from them adapts to a dark theme or keeps its own colors
-- ('' follows the global setting), and the text size the reader was set to
-- for them (0 follows the global size).
--
-- It stays on this device: the reading pane's choices are not part of any
-- account and are not synced anywhere.

CREATE TABLE IF NOT EXISTS sender_reading (
    address TEXT PRIMARY KEY,
    colors TEXT NOT NULL DEFAULT '',
    font_size INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT ''
);
