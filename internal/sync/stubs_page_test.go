package sync

import (
	"context"
	"strconv"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// seenAdapter reports one message as \Seen on the server.
type seenAdapter struct {
	*fakeAdapter
	seen string
}

func (a *seenAdapter) ListMessages(ctx context.Context, box RemoteMailbox) ([]Header, string, string, error) {
	headers, token, gen, err := a.fakeAdapter.ListMessages(ctx, box)
	for i := range headers {
		if headers[i].RemoteID == a.seen {
			headers[i].Flags = storage.FlagSeen
		}
	}
	return headers, token, gen, err
}

func countRows(t *testing.T, db *storage.DB, folderID int64) int {
	t.Helper()
	return len(remoteIDsPresent(t, db, folderID))
}

func seenLocally(t *testing.T, db *storage.DB, folderID int64, remoteID string) bool {
	t.Helper()
	states, err := db.ListMessageStates(context.Background(), folderID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.RemoteID == remoteID {
			return s.Flags.Has(storage.FlagSeen)
		}
	}
	t.Fatalf("no row %s", remoteID)
	return false
}

// pagedFolder is a 2000-message folder, uids 1..2000, plus a cached
// message the server no longer has ("gone") and a cached 2000 the server
// reports \Seen.
func pagedFolder(t *testing.T) (*storage.DB, storage.Folder, *seenAdapter) {
	t.Helper()
	db, folder := newSyncTestFolder(t)
	uids := make([]uint32, 2000)
	for i := range uids {
		uids[i] = uint32(i + 1)
	}
	seedLocalStub(t, db, folder, "gone")
	seedCached(t, db, folder, 2000)
	return db, folder, &seenAdapter{fakeAdapter: &fakeAdapter{ids: fakeIDs(uids...), listMeta: true, stateToken: "s1"}, seen: "2000"}
}

// A 2000-message folder lists in pages: 50 first, then 500 at a time,
// newest first. Nothing is deleted, no server flag is adopted and the
// cursor stays empty until the call that finishes the folder.
func TestSyncFolderStubsPagePagesLargeFolder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, adapter := pagedFolder(t)
	engine := NewEngine(adapter, db, nil)
	p := &StubPager{First: 50, Page: 500}

	// 2 rows to start; 1999 missing: 50, 500, 500, 500, then 449 at the end.
	wantRows := []int{2 + 50, 2 + 550, 2 + 1050, 2 + 1550}
	for i, want := range wantRows {
		res, more, err := engine.SyncFolderStubsPage(ctx, folder, p)
		if err != nil || !more {
			t.Fatalf("call %d: more=%v err=%v, want another page", i, more, err)
		}
		if got := countRows(t, db, folder.ID); got != want {
			t.Fatalf("call %d: %d rows, want %d", i, got, want)
		}
		if i == 0 && (res.ToFetch[0] != "1999" || len(res.ToFetch) != 50) {
			t.Fatalf("first page %d ids from %s, want 50 from the newest", len(res.ToFetch), res.ToFetch[0])
		}
		if !remoteIDsPresent(t, db, folder.ID)["gone"] {
			t.Fatalf("call %d: a partial list deleted a row", i)
		}
		if seenLocally(t, db, folder.ID, "2000") {
			t.Fatalf("call %d: a partial list adopted server flags", i)
		}
		if tok := folderToken(t, db, folder).StateToken; tok != "" {
			t.Fatalf("call %d: cursor %q stored before the folder was listed", i, tok)
		}
	}
	res, more, err := engine.SyncFolderStubsPage(ctx, folder, p)
	if err != nil || more {
		t.Fatalf("last call: more=%v err=%v", more, err)
	}
	has := remoteIDsPresent(t, db, folder.ID)
	if len(has) != 2000 || has["gone"] {
		t.Fatalf("%d rows (gone kept: %v), want the 2000 server messages", len(has), has["gone"])
	}
	if res.Deleted != 1 || !seenLocally(t, db, folder.ID, "2000") {
		t.Fatalf("deleted %d, seen %v: the last call must reconcile", res.Deleted, seenLocally(t, db, folder.ID, "2000"))
	}
	if tok := folderToken(t, db, folder).StateToken; tok != "s1" {
		t.Fatalf("cursor %q, want s1 after the full listing", tok)
	}
	// one list for the pages, one fresh list for the reconcile.
	if adapter.listCalls != 2 {
		t.Fatalf("%d full lists, want 2", adapter.listCalls)
	}
	if p.Pages() != 0 || p.snap != nil {
		t.Fatal("pager not reset after the folder was listed")
	}
}

func TestSyncFolderStubsPageSizes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		limit  int // InitialLimit: the window of a first sync
		first  int
		page   int
		counts []int // stubs stored by each call
	}{
		{"default window of 100", 100, 50, 500, []int{50, 50}},
		{"window under one page", 40, 50, 500, []int{40}},
		{"unpaged", 1200, 0, 0, []int{1200}},
		{"window of 1200", 1200, 50, 500, []int{50, 500, 500, 150}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			uids := make([]uint32, 2000)
			for i := range uids {
				uids[i] = uint32(i + 1)
			}
			engine := NewEngine(&fakeAdapter{ids: fakeIDs(uids...), listMeta: true}, db, nil)
			engine.InitialLimit = tc.limit
			p := &StubPager{First: tc.first, Page: tc.page}
			var got []int
			for range 10 {
				before := countRows(t, db, folder.ID)
				_, more, err := engine.SyncFolderStubsPage(ctx, folder, p)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, countRows(t, db, folder.ID)-before)
				if !more {
					break
				}
			}
			if len(got) != len(tc.counts) {
				t.Fatalf("stored %v, want %v", got, tc.counts)
			}
			for i := range got {
				if got[i] != tc.counts[i] {
					t.Fatalf("stored %v, want %v", got, tc.counts)
				}
			}
			// the newest stay in the window, the rest wait below the floor.
			has := remoteIDsPresent(t, db, folder.ID)
			if !has["2000"] || has[strconv.Itoa(2000-tc.limit)] {
				t.Fatalf("window holds the wrong end of the folder")
			}
		})
	}
}

// A restart drops the pager. The next listing starts from the newest again,
// skips what is stored, keeps the first sync's window and still deletes
// only at the end.
func TestSyncFolderStubsPageRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	uids := make([]uint32, 2000)
	for i := range uids {
		uids[i] = uint32(i + 1)
	}
	adapter := &fakeAdapter{ids: fakeIDs(uids...), listMeta: true, stateToken: "s1"}
	engine := NewEngine(adapter, db, nil)
	engine.InitialLimit = 1200

	p := &StubPager{First: 50, Page: 500}
	for range 2 {
		if _, more, err := engine.SyncFolderStubsPage(ctx, folder, p); err != nil || !more {
			t.Fatalf("more=%v err=%v", more, err)
		}
	}
	if got := countRows(t, db, folder.ID); got != 550 {
		t.Fatalf("%d rows before the restart, want 550", got)
	}
	seedLocalStub(t, db, folder, "gone")

	// After the restart: a new engine and pager, and the folder row as the
	// caller had it before the first sync.
	engine = NewEngine(adapter, db, nil)
	engine.InitialLimit = 1200
	p = &StubPager{First: 50, Page: 500}
	var calls int
	for more := true; more; calls++ {
		var err error
		if _, more, err = engine.SyncFolderStubsPage(ctx, folder, p); err != nil {
			t.Fatal(err)
		}
		if more && !remoteIDsPresent(t, db, folder.ID)["gone"] {
			t.Fatal("a partial list deleted a row")
		}
	}
	has := remoteIDsPresent(t, db, folder.ID)
	// 1200 newest: 801..2000, nothing below the floor, gone deleted.
	if len(has) != 1200 || !has["801"] || has["800"] || has["gone"] {
		t.Fatalf("%d rows after the restart, want the 1200 newest", len(has))
	}
	// 650 left of the window: 50, 500, then 100 in the reconcile.
	if calls != 3 {
		t.Fatalf("%d calls after the restart, want 3", calls)
	}
}

// A folder with a cursor takes the delta in one call.
func TestSyncFolderStubsPageDeltaInOneCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, adapter := syncedDelta(t, "b", "a")
	listsBefore := adapter.listCalls
	adapter.deltas = map[string]Delta{"c1": {Changed: []Header{metaHeader("c", 0), metaHeader("d", 0)}, Cursor: "c2"}}

	res, more, err := NewEngine(adapter, db, nil).SyncFolderStubsPage(ctx, folder, &StubPager{First: 1, Page: 1})
	if err != nil || more {
		t.Fatalf("more=%v err=%v", more, err)
	}
	if adapter.listCalls != listsBefore || len(res.ToFetch) != 2 {
		t.Fatalf("lists %d, ToFetch %v: want the delta, unpaged", adapter.listCalls-listsBefore, res.ToFetch)
	}
	if got := folderToken(t, db, folder).StateToken; got != "c2" {
		t.Fatalf("cursor %q, want c2", got)
	}
}

// A paged list that finds a page or fewer missing reconciles on the list it
// took: one full list, not a page and then a fresh list.
func TestSyncFolderStubsPageShortListReconcilesAtOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedLocalStub(t, db, folder, "gone")
	seedCached(t, db, folder, 30)
	adapter := &seenAdapter{fakeAdapter: &fakeAdapter{ids: fakeIDs(10, 20, 30), listMeta: true, stateToken: "s1"}, seen: "30"}
	engine := NewEngine(adapter, db, nil)
	p := &StubPager{First: 50, Page: 500}

	res, more, err := engine.SyncFolderStubsPage(ctx, folder, p)
	if err != nil || more {
		t.Fatalf("more=%v err=%v, want the folder listed in one call", more, err)
	}
	if len(res.ToFetch) != 2 || remoteIDsPresent(t, db, folder.ID)["gone"] || !seenLocally(t, db, folder.ID, "30") {
		t.Fatalf("fetched %v: the call did not reconcile", res.ToFetch)
	}
	if adapter.listCalls != 1 {
		t.Fatalf("%d full lists, want 1", adapter.listCalls)
	}
}
