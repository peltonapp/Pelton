package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func addTestFeed(t *testing.T, db *DB, url string, folder int64) *Feed {
	t.Helper()
	f := &Feed{URL: url, Title: "Feed " + url, FolderID: folder}
	if err := db.CreateFeed(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	return f
}

func entryTitles(list []FeedEntry) []string {
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.Title
	}
	return out
}

func TestFeedFoldersAndFeeds(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	tech, err := db.CreateFeedFolder(ctx, "Tech")
	if err != nil {
		t.Fatal(err)
	}
	news, err := db.CreateFeedFolder(ctx, "News")
	if err != nil {
		t.Fatal(err)
	}
	if tech.Position != 0 || news.Position != 1 {
		t.Fatalf("positions = %d %d", tech.Position, news.Position)
	}
	a := addTestFeed(t, db, "https://a.example/feed", tech.ID)
	b := addTestFeed(t, db, "https://b.example/feed", tech.ID)
	c := addTestFeed(t, db, "https://c.example/feed", 0)
	if a.Position != 0 || b.Position != 1 || c.Position != 0 || a.FolderID != tech.ID {
		t.Fatalf("feed positions = %d %d %d", a.Position, b.Position, c.Position)
	}
	if err := db.CreateFeed(ctx, &Feed{URL: a.URL}); !errors.Is(err, ErrFeedExists) {
		t.Errorf("duplicate = %v", err)
	}

	if err := db.MoveFeeds(ctx, news.ID, []int64{b.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetFeed(ctx, b.ID)
	if got.FolderID != news.ID || got.Position != 0 {
		t.Errorf("moved = %+v", got)
	}
	if err := db.ReorderFeedFolders(ctx, []int64{news.ID, tech.ID}); err != nil {
		t.Fatal(err)
	}
	folders, _ := db.ListFeedFolders(ctx)
	if folders[0].ID != news.ID {
		t.Errorf("folders = %+v", folders)
	}

	if err := db.UpdateFeedSettings(ctx, a.ID, FeedSettings{CustomTitle: "Mine", FolderID: 0, IntervalMinutes: 60, LoadImages: true, Notify: true}); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetFeed(ctx, a.ID)
	if got.DisplayTitle() != "Mine" || got.FolderID != 0 || got.Position != 1 || !got.LoadImages || !got.Notify || got.IntervalMinutes != 60 {
		t.Errorf("settings = %+v", got)
	}

	if err := db.DeleteFeedFolder(ctx, news.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetFeed(ctx, b.ID)
	if got.FolderID != 0 {
		t.Errorf("feed of a deleted folder = %+v", got)
	}

	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	if err := db.RecordFeedFetch(ctx, c.ID, FeedFetch{At: now, Next: now.Add(time.Hour), Err: "timeout"}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordFeedFetch(ctx, a.ID, FeedFetch{At: now, Next: now.Add(time.Hour), ETag: `"x"`}); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetFeed(ctx, c.ID)
	if got.LastError != "timeout" || got.ErrorCount != 1 || !got.LastSuccessAt.IsZero() {
		t.Errorf("failure = %+v", got)
	}
	due, err := db.DueFeeds(ctx, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].ID != b.ID {
		t.Errorf("due = %+v", due)
	}

	if err := db.DeleteFeed(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetFeed(ctx, a.ID); !errors.Is(err, ErrFeedNotFound) {
		t.Errorf("deleted feed = %v", err)
	}
}

func TestFeedEntryDedupe(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	f := addTestFeed(t, db, "https://a.example/feed", 0)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

	first := []FeedEntry{
		{GUID: "1", URL: "https://a.example/1", Title: "One", Published: now.Add(-2 * time.Hour), SearchText: "alpha"},
		{GUID: "2", URL: "https://a.example/2", Title: "Two", SearchText: "beta"},
	}
	added, err := db.UpsertFeedEntries(ctx, f.ID, first, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("added = %v", added)
	}
	if _, err := db.SetFeedEntriesRead(ctx, added[:1], true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetFeedEntryStarred(ctx, added[0], true); err != nil {
		t.Fatal(err)
	}

	second := []FeedEntry{
		{GUID: "1", URL: "https://a.example/1", Title: "One, edited", Published: now.Add(-2 * time.Hour), SearchText: "alpha gamma"},
		{GUID: "two-new-id", URL: "https://a.example/2", Title: "Two", SearchText: "beta"},
		{GUID: "3", Title: "Three", SearchText: "delta"},
	}
	added, err = db.UpsertFeedEntries(ctx, f.ID, second, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Fatalf("second fetch added %v, want only the third entry", added)
	}
	list, err := db.QueryFeedEntries(ctx, FeedEntryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("entries = %v", entryTitles(list))
	}
	var one *FeedEntry
	for i := range list {
		if list[i].GUID == "1" {
			one = &list[i]
		}
	}
	if one == nil || one.Title != "One, edited" || !one.Read || !one.Starred {
		t.Errorf("updated entry lost its state: %+v", one)
	}
	if list[2].Title != "One, edited" {
		t.Errorf("order = %v", entryTitles(list))
	}

	hits, err := db.QueryFeedEntries(ctx, FeedEntryQuery{Search: "gam"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].GUID != "1" {
		t.Errorf("search after update = %v", entryTitles(hits))
	}
	if _, err := db.QueryFeedEntries(ctx, FeedEntryQuery{Search: `"bad (syntax* - OR`}); err != nil {
		t.Errorf("query syntax leaked into fts: %v", err)
	}
}

func TestFeedEntryQueriesAndCounts(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	a := addTestFeed(t, db, "https://a.example/feed", 0)
	b := addTestFeed(t, db, "https://b.example/feed", 0)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	ids, _ := db.UpsertFeedEntries(ctx, a.ID, []FeedEntry{
		{GUID: "a1", Title: "A1", Published: now.Add(-time.Hour)},
		{GUID: "a2", Title: "A2", Published: now.Add(-48 * time.Hour)},
	}, now)
	bIDs, _ := db.UpsertFeedEntries(ctx, b.ID, []FeedEntry{{GUID: "b1", Title: "B1", Published: now.Add(-2 * time.Hour)}}, now)
	_ = db.SetFeedEntryReadLater(ctx, ids[1], true)
	_ = db.SetFeedEntryStarred(ctx, bIDs[0], true)

	today := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	c, err := db.CountFeedEntries(ctx, today)
	if err != nil {
		t.Fatal(err)
	}
	if c.Unread != 3 || c.Today != 2 || c.Starred != 1 || c.ReadLater != 1 || c.PerFeed[a.ID] != 2 || c.PerFeed[b.ID] != 1 {
		t.Errorf("counts = %+v", c)
	}

	cases := []struct {
		q    FeedEntryQuery
		want string
	}{
		{FeedEntryQuery{FeedIDs: []int64{a.ID}}, "[A1 A2]"},
		{FeedEntryQuery{FeedIDs: []int64{}}, "[]"},
		{FeedEntryQuery{Since: today}, "[A1 B1]"},
		{FeedEntryQuery{Starred: true}, "[B1]"},
		{FeedEntryQuery{ReadLater: true}, "[A2]"},
		{FeedEntryQuery{Limit: 1, Offset: 1}, "[B1]"},
	}
	for _, tc := range cases {
		got, err := db.QueryFeedEntries(ctx, tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if s := fmt.Sprint(entryTitles(got)); s != tc.want {
			t.Errorf("query %+v = %s, want %s", tc.q, s, tc.want)
		}
	}

	marked, err := db.MarkFeedEntriesRead(ctx, FeedEntryQuery{FeedIDs: []int64{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(marked) != 2 {
		t.Errorf("marked = %v", marked)
	}
	again, _ := db.SetFeedEntriesRead(ctx, marked, true)
	if len(again) != 0 {
		t.Errorf("already read entries reported as changed: %v", again)
	}
	undone, _ := db.SetFeedEntriesRead(ctx, marked, false)
	if len(undone) != 2 {
		t.Errorf("undo = %v", undone)
	}
}

func TestFeedRetention(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	f := addTestFeed(t, db, "https://a.example/feed", 0)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	var entries []FeedEntry
	for i := range 6 {
		entries = append(entries, FeedEntry{GUID: fmt.Sprint(i), Title: fmt.Sprint("E", i), Published: now.AddDate(0, 0, -i*10)})
	}
	ids, err := db.UpsertFeedEntries(ctx, f.ID, entries, now)
	if err != nil {
		t.Fatal(err)
	}
	// E5 is 50 days old but starred, E4 is 40 days old and on read later.
	_ = db.SetFeedEntryStarred(ctx, ids[5], true)
	_ = db.SetFeedEntryReadLater(ctx, ids[4], true)

	removed, err := db.PruneFeedEntries(ctx, 25, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("by age removed %d, want 1 (E3)", removed)
	}
	removed, err = db.PruneFeedEntries(ctx, 0, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("by count removed %d, want 1 (E2)", removed)
	}
	list, _ := db.QueryFeedEntries(ctx, FeedEntryQuery{})
	if s := fmt.Sprint(entryTitles(list)); s != "[E0 E1 E4 E5]" {
		t.Errorf("kept = %s", s)
	}

	// the feed still lists every entry: the pruned ones must not come back.
	added, err := db.UpsertFeedEntries(ctx, f.ID, entries, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Errorf("pruned entries came back: %v", added)
	}

	// a tombstone the feed stopped listing is forgotten after a while.
	if _, err := db.PruneFeedEntries(ctx, 0, 0, now.Add(prunedKeep+2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = db.sql.QueryRow(`SELECT COUNT(*) FROM feed_pruned`).Scan(&left)
	if left != 0 {
		t.Errorf("tombstones left = %d", left)
	}
}
