-- Full text index over feed entries, built the way 0004 built the one over
-- messages: an external content fts5 table holding only the index, kept in
-- step with feed_entries by triggers.
CREATE VIRTUAL TABLE feed_entries_fts USING fts5(
    title,
    author,
    search_text,
    content='feed_entries',
    content_rowid='id'
);

CREATE TRIGGER feed_entries_fts_ai AFTER INSERT ON feed_entries BEGIN
    INSERT INTO feed_entries_fts(rowid, title, author, search_text)
    VALUES (new.id, new.title, new.author, new.search_text);
END;

CREATE TRIGGER feed_entries_fts_ad AFTER DELETE ON feed_entries BEGIN
    INSERT INTO feed_entries_fts(feed_entries_fts, rowid, title, author, search_text)
    VALUES ('delete', old.id, old.title, old.author, old.search_text);
END;

-- only a change to an indexed column touches the index, so marking entries
-- read or starred costs nothing here.
CREATE TRIGGER feed_entries_fts_au AFTER UPDATE OF title, author, search_text ON feed_entries BEGIN
    INSERT INTO feed_entries_fts(feed_entries_fts, rowid, title, author, search_text)
    VALUES ('delete', old.id, old.title, old.author, old.search_text);
    INSERT INTO feed_entries_fts(rowid, title, author, search_text)
    VALUES (new.id, new.title, new.author, new.search_text);
END;
