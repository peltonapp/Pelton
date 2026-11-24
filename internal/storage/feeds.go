package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrFeedNotFound is returned when a feed id has no row.
var ErrFeedNotFound = errors.New("storage: feed not found")

// ErrFeedFolderNotFound is returned when a feed folder id has no row.
var ErrFeedFolderNotFound = errors.New("storage: feed folder not found")

// ErrFeedEntryNotFound is returned when a feed entry id has no row.
var ErrFeedEntryNotFound = errors.New("storage: feed entry not found")

// ErrFeedExists is returned when a feed with the same address is already
// subscribed.
var ErrFeedExists = errors.New("storage: feed already subscribed")

// FeedFolder groups feeds in the Feeds sidebar.
type FeedFolder struct {
	ID       int64
	Name     string
	Position int
}

// Feed is one subscription and its fetch state. FolderID 0 means the feed is
// in no folder. See migration 0080 for what each field holds.
type Feed struct {
	ID              int64
	FolderID        int64
	URL             string
	SiteURL         string
	Title           string
	CustomTitle     string
	Description     string
	Icon            string
	ETag            string
	LastModified    string
	LastFetchedAt   time.Time
	LastSuccessAt   time.Time
	NextFetchAt     time.Time
	LastError       string
	ErrorCount      int
	IntervalMinutes int
	LoadImages      bool
	Notify          bool
	Position        int
	CreatedAt       time.Time
}

// DisplayTitle is the name the user sees: their own, the feed's, or its
// address.
func (f Feed) DisplayTitle() string {
	if t := strings.TrimSpace(f.CustomTitle); t != "" {
		return t
	}
	if t := strings.TrimSpace(f.Title); t != "" {
		return t
	}
	return f.URL
}

// FeedEnclosure is a file attached to an entry.
type FeedEnclosure struct {
	URL    string `json:"url"`
	Type   string `json:"type"`
	Length int64  `json:"length"`
	Title  string `json:"title,omitempty"`
}

// FeedEntry is one entry of a feed. In list queries Summary and Content are
// left empty and Snippet holds the start of SearchText.
type FeedEntry struct {
	ID         int64
	FeedID     int64
	GUID       string
	URL        string
	Title      string
	Author     string
	Summary    string
	Content    string
	SearchText string
	Snippet    string
	Image      string
	Enclosures []FeedEnclosure
	Published  time.Time
	Updated    time.Time
	FetchedAt  time.Time
	Read       bool
	Starred    bool
	ReadLater  bool
}

// --- folders --------------------------------------------------------------------

// ListFeedFolders returns the folders in the user's order.
func (d *DB) ListFeedFolders(ctx context.Context) ([]FeedFolder, error) {
	var out []FeedFolder
	err := d.queryRows(ctx, "feed folders", `SELECT id, name, position FROM feed_folders ORDER BY position, id`, nil, func(rows *sql.Rows) error {
		var f FeedFolder
		if err := rows.Scan(&f.ID, &f.Name, &f.Position); err != nil {
			return err
		}
		out = append(out, f)
		return nil
	})
	return out, err
}

// CreateFeedFolder adds a folder at the end of the list.
func (d *DB) CreateFeedFolder(ctx context.Context, name string) (*FeedFolder, error) {
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO feed_folders (name, position, created_at)
VALUES (?, (SELECT COALESCE(MAX(position), -1) + 1 FROM feed_folders), ?)`, name, nowText())
	if err != nil {
		return nil, fmt.Errorf("storage: insert feed folder: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("storage: feed folder id: %w", err)
	}
	f := &FeedFolder{ID: id, Name: name}
	if err := d.sql.QueryRowContext(ctx, `SELECT position FROM feed_folders WHERE id = ?`, id).Scan(&f.Position); err != nil {
		return nil, fmt.Errorf("storage: feed folder position: %w", err)
	}
	return f, nil
}

// RenameFeedFolder renames a folder, or returns ErrFeedFolderNotFound.
func (d *DB) RenameFeedFolder(ctx context.Context, id int64, name string) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE feed_folders SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("storage: rename feed folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFeedFolderNotFound)
}

// DeleteFeedFolder removes a folder. Its feeds stay, outside any folder.
func (d *DB) DeleteFeedFolder(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM feed_folders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete feed folder %d: %w", id, err)
	}
	return requireOneRow(res, ErrFeedFolderNotFound)
}

// ReorderFeedFolders puts the folders in the order of ids.
func (d *DB) ReorderFeedFolders(ctx context.Context, ids []int64) error {
	return d.inTx(ctx, "reorder feed folders", func(tx *sql.Tx) error {
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE feed_folders SET position = ? WHERE id = ?`, i, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// --- feeds ----------------------------------------------------------------------

const feedColumns = `id, COALESCE(folder_id, 0), url, site_url, title, custom_title, description, icon,
etag, last_modified, last_fetched_at, last_success_at, next_fetch_at, last_error, error_count,
interval_minutes, load_images, notify, position, created_at`

func scanFeed(row rowScanner) (*Feed, error) {
	var (
		f                               Feed
		fetched, success, next, created string
		loadImages, notify              int
	)
	if err := row.Scan(&f.ID, &f.FolderID, &f.URL, &f.SiteURL, &f.Title, &f.CustomTitle, &f.Description, &f.Icon,
		&f.ETag, &f.LastModified, &fetched, &success, &next, &f.LastError, &f.ErrorCount,
		&f.IntervalMinutes, &loadImages, &notify, &f.Position, &created); err != nil {
		return nil, err
	}
	f.LoadImages, f.Notify = loadImages != 0, notify != 0
	var err error
	for _, p := range []struct {
		dst *time.Time
		src string
	}{{&f.LastFetchedAt, fetched}, {&f.LastSuccessAt, success}, {&f.NextFetchAt, next}, {&f.CreatedAt, created}} {
		if *p.dst, err = parseTime(p.src); err != nil {
			return nil, err
		}
	}
	return &f, nil
}

// ListFeeds returns every feed, by folder and then in the user's order.
func (d *DB) ListFeeds(ctx context.Context) ([]Feed, error) {
	var out []Feed
	err := d.queryRows(ctx, "feeds", `SELECT `+feedColumns+` FROM feeds ORDER BY COALESCE(folder_id, 0), position, id`, nil, func(rows *sql.Rows) error {
		f, err := scanFeed(rows)
		if err != nil {
			return err
		}
		out = append(out, *f)
		return nil
	})
	return out, err
}

// GetFeed returns one feed, or ErrFeedNotFound.
func (d *DB) GetFeed(ctx context.Context, id int64) (*Feed, error) {
	f, err := scanFeed(d.sql.QueryRowContext(ctx, `SELECT `+feedColumns+` FROM feeds WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFeedNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get feed %d: %w", id, err)
	}
	return f, nil
}

// FeedByURL returns the feed subscribed at url, or ErrFeedNotFound.
func (d *DB) FeedByURL(ctx context.Context, url string) (*Feed, error) {
	f, err := scanFeed(d.sql.QueryRowContext(ctx, `SELECT `+feedColumns+` FROM feeds WHERE url = ?`, url))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFeedNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: feed by url: %w", err)
	}
	return f, nil
}

// CreateFeed subscribes to a feed at the end of its folder and fills in its
// id, position and creation time. A feed already subscribed at the same
// address is ErrFeedExists.
func (d *DB) CreateFeed(ctx context.Context, f *Feed) error {
	now := time.Now().UTC()
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO feeds (folder_id, url, site_url, title, custom_title, description, icon, interval_minutes, load_images, notify, position, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM feeds WHERE COALESCE(folder_id, 0) = ?), ?)
ON CONFLICT(url) DO NOTHING`,
		nullableFolder(f.FolderID), f.URL, f.SiteURL, f.Title, f.CustomTitle, f.Description, f.Icon,
		f.IntervalMinutes, boolToInt(f.LoadImages), boolToInt(f.Notify), f.FolderID, formatTime(now))
	if err != nil {
		return fmt.Errorf("storage: insert feed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrFeedExists
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("storage: feed id: %w", err)
	}
	saved, err := d.GetFeed(ctx, id)
	if err != nil {
		return err
	}
	*f = *saved
	return nil
}

// FeedSettings are the parts of a feed the user edits.
type FeedSettings struct {
	CustomTitle     string
	FolderID        int64
	IntervalMinutes int
	LoadImages      bool
	Notify          bool
}

// UpdateFeedSettings saves what the user set on a feed. Moving it to another
// folder puts it at the end there.
func (d *DB) UpdateFeedSettings(ctx context.Context, id int64, s FeedSettings) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE feeds SET custom_title = ?, interval_minutes = ?, load_images = ?, notify = ?,
  position = CASE WHEN COALESCE(folder_id, 0) = ? THEN position
                  ELSE (SELECT COALESCE(MAX(position), -1) + 1 FROM feeds WHERE COALESCE(folder_id, 0) = ?) END,
  folder_id = ?
WHERE id = ?`,
		s.CustomTitle, s.IntervalMinutes, boolToInt(s.LoadImages), boolToInt(s.Notify), s.FolderID, s.FolderID, nullableFolder(s.FolderID), id)
	if err != nil {
		return fmt.Errorf("storage: update feed %d: %w", id, err)
	}
	return requireOneRow(res, ErrFeedNotFound)
}

// UpdateFeedMeta stores what the feed document says about itself.
func (d *DB) UpdateFeedMeta(ctx context.Context, id int64, title, siteURL, description string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE feeds SET title = ?, site_url = ?, description = ? WHERE id = ?`, title, siteURL, description, id)
	if err != nil {
		return fmt.Errorf("storage: update feed meta %d: %w", id, err)
	}
	return nil
}

// SetFeedIcon stores a feed's icon as a data url.
func (d *DB) SetFeedIcon(ctx context.Context, id int64, icon string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE feeds SET icon = ? WHERE id = ?`, icon, id)
	if err != nil {
		return fmt.Errorf("storage: set feed icon %d: %w", id, err)
	}
	return nil
}

// SetFeedURL moves a feed to a new address, after a permanent redirect. It
// returns ErrFeedExists when another subscription already has that address.
func (d *DB) SetFeedURL(ctx context.Context, id int64, url string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE feeds SET url = ? WHERE id = ?`, url, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrFeedExists
		}
		return fmt.Errorf("storage: set feed url %d: %w", id, err)
	}
	return nil
}

// FeedFetch is the outcome of one fetch, for RecordFeedFetch. Err "" is a
// success; a success keeps the validators it carries, a failure keeps the
// stored ones.
type FeedFetch struct {
	At           time.Time
	Next         time.Time
	ETag         string
	LastModified string
	Err          string
}

// RecordFeedFetch stores when a feed was fetched, when it may be fetched
// again, and how it went.
func (d *DB) RecordFeedFetch(ctx context.Context, id int64, r FeedFetch) error {
	var err error
	if r.Err == "" {
		_, err = d.sql.ExecContext(ctx, `
UPDATE feeds SET last_fetched_at = ?, last_success_at = ?, next_fetch_at = ?, etag = ?, last_modified = ?,
  last_error = '', error_count = 0 WHERE id = ?`,
			formatTime(r.At), formatTime(r.At), formatTime(r.Next), r.ETag, r.LastModified, id)
	} else {
		_, err = d.sql.ExecContext(ctx, `
UPDATE feeds SET last_fetched_at = ?, next_fetch_at = ?, last_error = ?, error_count = error_count + 1 WHERE id = ?`,
			formatTime(r.At), formatTime(r.Next), r.Err, id)
	}
	if err != nil {
		return fmt.Errorf("storage: record feed fetch %d: %w", id, err)
	}
	return nil
}

// DeleteFeed unsubscribes from a feed; its entries go with it.
func (d *DB) DeleteFeed(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM feeds WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete feed %d: %w", id, err)
	}
	return requireOneRow(res, ErrFeedNotFound)
}

// MoveFeeds puts the feeds of ids into a folder (0 for none) in that order.
// Feeds already there and not named keep their place after them.
func (d *DB) MoveFeeds(ctx context.Context, folderID int64, ids []int64) error {
	return d.inTx(ctx, "move feeds", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE feeds SET position = position + ? WHERE COALESCE(folder_id, 0) = ?`, len(ids), folderID); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE feeds SET folder_id = ?, position = ? WHERE id = ?`, nullableFolder(folderID), i, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// DueFeeds returns the feeds whose next fetch time has come.
func (d *DB) DueFeeds(ctx context.Context, now time.Time) ([]Feed, error) {
	var out []Feed
	err := d.queryRows(ctx, "due feeds", `SELECT `+feedColumns+` FROM feeds WHERE next_fetch_at = '' OR next_fetch_at <= ? ORDER BY next_fetch_at, id`,
		[]any{formatTime(now)}, func(rows *sql.Rows) error {
			f, err := scanFeed(rows)
			if err != nil {
				return err
			}
			out = append(out, *f)
			return nil
		})
	return out, err
}

// --- entries --------------------------------------------------------------------

// UpsertFeedEntries stores the entries of one fetch. A new guid is inserted
// unread; a known one (or, when the feed changed its ids, a known link) has its
// text updated and keeps its read, star and read-later state. An entry that
// retention removed is not brought back. Entries without a date take now. It
// returns the ids of the entries that were new.
func (d *DB) UpsertFeedEntries(ctx context.Context, feedID int64, entries []FeedEntry, now time.Time) ([]int64, error) {
	var added []int64
	err := d.inTx(ctx, "store feed entries", func(tx *sql.Tx) error {
		stamp := formatTime(now)
		for _, e := range entries {
			var pruned int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM feed_pruned WHERE feed_id = ? AND guid = ?`, feedID, e.GUID).Scan(&pruned); err != nil {
				return err
			}
			if pruned > 0 {
				if _, err := tx.ExecContext(ctx, `UPDATE feed_pruned SET pruned_at = ? WHERE feed_id = ? AND guid = ?`, stamp, feedID, e.GUID); err != nil {
					return err
				}
				continue
			}
			enc, err := json.Marshal(nonNilEnclosures(e.Enclosures))
			if err != nil {
				return err
			}
			id, err := existingEntry(ctx, tx, feedID, e)
			if err != nil {
				return err
			}
			if id != 0 {
				if _, err := tx.ExecContext(ctx, `
UPDATE feed_entries SET guid = ?, url = ?, title = ?, author = ?, summary = ?, content = ?, search_text = ?, image = ?,
  enclosures = ?, updated = ?
WHERE id = ? AND (guid != ? OR url != ? OR title != ? OR author != ? OR summary != ? OR content != ? OR image != ? OR enclosures != ? OR updated != ?)`,
					e.GUID, e.URL, e.Title, e.Author, e.Summary, e.Content, e.SearchText, e.Image, string(enc), formatTime(e.Updated),
					id, e.GUID, e.URL, e.Title, e.Author, e.Summary, e.Content, e.Image, string(enc), formatTime(e.Updated)); err != nil {
					return err
				}
				continue
			}
			published := e.Published
			if published.IsZero() {
				published = now
			}
			res, err := tx.ExecContext(ctx, `
INSERT INTO feed_entries (feed_id, guid, url, title, author, summary, content, search_text, image, enclosures, published, updated, fetched_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				feedID, e.GUID, e.URL, e.Title, e.Author, e.Summary, e.Content, e.SearchText, e.Image, string(enc),
				formatTime(published), formatTime(e.Updated), stamp)
			if err != nil {
				return err
			}
			newID, err := res.LastInsertId()
			if err != nil {
				return err
			}
			added = append(added, newID)
		}
		return nil
	})
	return added, err
}

// existingEntry finds the stored copy of an entry: by guid, or by link when
// the feed gave the same entry a new id.
func existingEntry(ctx context.Context, tx *sql.Tx, feedID int64, e FeedEntry) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM feed_entries WHERE feed_id = ? AND guid = ?`, feedID, e.GUID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if e.URL == "" {
		return 0, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT id FROM feed_entries WHERE feed_id = ? AND url = ? ORDER BY id LIMIT 1`, feedID, e.URL).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func nonNilEnclosures(list []FeedEnclosure) []FeedEnclosure {
	if list == nil {
		return []FeedEnclosure{}
	}
	return list
}

// FeedEntryQuery selects entries for the list. Every set field narrows it.
// FeedIDs nil means every feed; an empty non-nil slice matches nothing.
type FeedEntryQuery struct {
	FeedIDs    []int64
	UnreadOnly bool
	Starred    bool
	ReadLater  bool
	// Since keeps entries published at or after it.
	Since time.Time
	// Search is free text matched against title, author and text.
	Search string
	Limit  int
	Offset int
}

func (q FeedEntryQuery) where() (string, []any) {
	clauses := []string{"1 = 1"}
	var args []any
	if q.FeedIDs != nil {
		marks, ids := inClause(q.FeedIDs)
		if len(q.FeedIDs) == 0 {
			marks = "NULL"
		}
		clauses = append(clauses, "e.feed_id IN ("+marks+")")
		args = append(args, ids...)
	}
	if q.UnreadOnly {
		clauses = append(clauses, "e.read = 0")
	}
	if q.Starred {
		clauses = append(clauses, "e.starred = 1")
	}
	if q.ReadLater {
		clauses = append(clauses, "e.read_later = 1")
	}
	if !q.Since.IsZero() {
		clauses = append(clauses, "e.published >= ?")
		args = append(args, formatTime(q.Since))
	}
	if match := ftsQuery(q.Search); match != "" {
		clauses = append(clauses, "e.id IN (SELECT rowid FROM feed_entries_fts WHERE feed_entries_fts MATCH ?)")
		args = append(args, match)
	}
	return strings.Join(clauses, " AND "), args
}

// ftsQuery turns what the user typed into an fts5 query: every word must
// match, as a prefix, and nothing they type is read as query syntax.
func ftsQuery(text string) string {
	var terms []string
	for _, w := range strings.Fields(text) {
		w = strings.Trim(w, `"*()^:{}+-`)
		if w == "" {
			continue
		}
		terms = append(terms, `"`+strings.ReplaceAll(w, `"`, `""`)+`"*`)
	}
	return strings.Join(terms, " ")
}

const entryListColumns = `e.id, e.feed_id, e.guid, e.url, e.title, e.author, substr(e.search_text, 1, 280), e.image, e.enclosures,
e.published, e.updated, e.fetched_at, e.read, e.starred, e.read_later`

// QueryFeedEntries returns a page of entries, newest first, without their
// bodies.
func (d *DB) QueryFeedEntries(ctx context.Context, q FeedEntryQuery) ([]FeedEntry, error) {
	where, args := q.where()
	query := `SELECT ` + entryListColumns + ` FROM feed_entries e WHERE ` + where + ` ORDER BY e.published DESC, e.id DESC LIMIT ? OFFSET ?`
	args = append(args, normalizeLimit(q.Limit), max(q.Offset, 0))
	var out []FeedEntry
	err := d.queryRows(ctx, "feed entries", query, args, func(rows *sql.Rows) error {
		var (
			e                      FeedEntry
			enc, pub, upd, fetched string
			read, starred, later   int
		)
		if err := rows.Scan(&e.ID, &e.FeedID, &e.GUID, &e.URL, &e.Title, &e.Author, &e.Snippet, &e.Image, &enc,
			&pub, &upd, &fetched, &read, &starred, &later); err != nil {
			return err
		}
		if err := fillEntry(&e, enc, pub, upd, fetched, read, starred, later); err != nil {
			return err
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

func fillEntry(e *FeedEntry, enc, pub, upd, fetched string, read, starred, later int) error {
	e.Read, e.Starred, e.ReadLater = read != 0, starred != 0, later != 0
	if enc != "" {
		if err := json.Unmarshal([]byte(enc), &e.Enclosures); err != nil {
			e.Enclosures = nil
		}
	}
	var err error
	if e.Published, err = parseTime(pub); err != nil {
		return err
	}
	if e.Updated, err = parseTime(upd); err != nil {
		return err
	}
	e.FetchedAt, err = parseTime(fetched)
	return err
}

// GetFeedEntry returns one entry with its bodies, or ErrFeedEntryNotFound.
func (d *DB) GetFeedEntry(ctx context.Context, id int64) (*FeedEntry, error) {
	var (
		e                      FeedEntry
		enc, pub, upd, fetched string
		read, starred, later   int
	)
	err := d.sql.QueryRowContext(ctx, `
SELECT id, feed_id, guid, url, title, author, summary, content, search_text, image, enclosures,
  published, updated, fetched_at, read, starred, read_later
FROM feed_entries WHERE id = ?`, id).Scan(&e.ID, &e.FeedID, &e.GUID, &e.URL, &e.Title, &e.Author, &e.Summary, &e.Content,
		&e.SearchText, &e.Image, &enc, &pub, &upd, &fetched, &read, &starred, &later)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFeedEntryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get feed entry %d: %w", id, err)
	}
	if err := fillEntry(&e, enc, pub, upd, fetched, read, starred, later); err != nil {
		return nil, err
	}
	return &e, nil
}

// SetFeedEntriesRead marks entries read or unread and returns the ids whose
// state actually changed, which is what an undo has to put back.
func (d *DB) SetFeedEntriesRead(ctx context.Context, ids []int64, read bool) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	marks, args := inClause(ids)
	var changed []int64
	err := d.inTx(ctx, "mark feed entries", func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM feed_entries WHERE read = ? AND id IN (`+marks+`)`, append([]any{boolToInt(!read)}, args...)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			changed = append(changed, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(changed) == 0 {
			return nil
		}
		cm, cargs := inClause(changed)
		_, err = tx.ExecContext(ctx, `UPDATE feed_entries SET read = ? WHERE id IN (`+cm+`)`, append([]any{boolToInt(read)}, cargs...)...)
		return err
	})
	return changed, err
}

// MarkFeedEntriesRead marks every unread entry a query selects as read,
// ignoring its paging, and returns their ids.
func (d *DB) MarkFeedEntriesRead(ctx context.Context, q FeedEntryQuery) ([]int64, error) {
	q.UnreadOnly = true
	where, args := q.where()
	var ids []int64
	err := d.queryRows(ctx, "unread feed entries", `SELECT e.id FROM feed_entries e WHERE `+where, args, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return d.SetFeedEntriesRead(ctx, ids, true)
}

// SetFeedEntryStarred stars or unstars an entry.
func (d *DB) SetFeedEntryStarred(ctx context.Context, id int64, starred bool) error {
	return d.setEntryFlag(ctx, id, "starred", starred)
}

// SetFeedEntryReadLater puts an entry on the read-later list or takes it off.
func (d *DB) SetFeedEntryReadLater(ctx context.Context, id int64, later bool) error {
	return d.setEntryFlag(ctx, id, "read_later", later)
}

func (d *DB) setEntryFlag(ctx context.Context, id int64, column string, on bool) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE feed_entries SET `+column+` = ? WHERE id = ?`, boolToInt(on), id)
	if err != nil {
		return fmt.Errorf("storage: set feed entry %s %d: %w", column, id, err)
	}
	return requireOneRow(res, ErrFeedEntryNotFound)
}

// FeedCounts are the numbers beside the smart views.
type FeedCounts struct {
	Unread    int
	Today     int
	Starred   int
	ReadLater int
	// PerFeed is the unread count of each feed that has unread entries.
	PerFeed map[int64]int
}

// CountFeedEntries counts unread entries overall and per feed, unread entries
// published since todayStart, starred entries and the read-later list.
func (d *DB) CountFeedEntries(ctx context.Context, todayStart time.Time) (FeedCounts, error) {
	c := FeedCounts{PerFeed: map[int64]int{}}
	err := d.queryRows(ctx, "feed unread counts", `SELECT feed_id, COUNT(*) FROM feed_entries WHERE read = 0 GROUP BY feed_id`, nil, func(rows *sql.Rows) error {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		c.PerFeed[id] = n
		c.Unread += n
		return nil
	})
	if err != nil {
		return c, err
	}
	err = d.sql.QueryRowContext(ctx, `
SELECT
  (SELECT COUNT(*) FROM feed_entries WHERE read = 0 AND published >= ?),
  (SELECT COUNT(*) FROM feed_entries WHERE starred = 1),
  (SELECT COUNT(*) FROM feed_entries WHERE read_later = 1)`, formatTime(todayStart)).Scan(&c.Today, &c.Starred, &c.ReadLater)
	if err != nil {
		return c, fmt.Errorf("storage: feed counts: %w", err)
	}
	return c, nil
}

// prunedKeep is how long a pruned guid is remembered after its feed last
// listed it.
const prunedKeep = 30 * 24 * time.Hour

// PruneFeedEntries applies retention: entries published before now minus
// keepDays, and entries past the newest keepCount of their feed, are removed,
// except starred and read-later ones. 0 turns either rule off. Their guids are
// remembered so a feed still listing them does not bring them back. It
// returns how many entries went.
func (d *DB) PruneFeedEntries(ctx context.Context, keepDays, keepCount int, now time.Time) (int, error) {
	var conds []string
	var args []any
	if keepDays > 0 {
		conds = append(conds, "published < ?")
		args = append(args, formatTime(now.AddDate(0, 0, -keepDays)))
	}
	if keepCount > 0 {
		conds = append(conds, `id IN (SELECT id FROM (
  SELECT id, ROW_NUMBER() OVER (PARTITION BY feed_id ORDER BY published DESC, id DESC) AS rn FROM feed_entries
) WHERE rn > ?)`)
		args = append(args, keepCount)
	}
	removed := 0
	err := d.inTx(ctx, "prune feed entries", func(tx *sql.Tx) error {
		if len(conds) > 0 {
			where := "starred = 0 AND read_later = 0 AND (" + strings.Join(conds, " OR ") + ")"
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO feed_pruned (feed_id, guid, pruned_at)
SELECT feed_id, guid, ? FROM feed_entries WHERE `+where, append([]any{formatTime(now)}, args...)...); err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `DELETE FROM feed_entries WHERE `+where, args...)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			removed = int(n)
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM feed_pruned WHERE pruned_at < ?`, formatTime(now.Add(-prunedKeep)))
		return err
	})
	return removed, err
}

// inTx runs fn in a transaction, committing when it returns nil.
func (d *DB) inTx(ctx context.Context, what string, fn func(tx *sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return fmt.Errorf("storage: %s: %w", what, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit %s: %w", what, err)
	}
	return nil
}

func nullableFolder(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
