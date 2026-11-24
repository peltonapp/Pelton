package sync

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// olderAdapter is fakeAdapter plus OlderLister over its numeric ids. It
// returns at most max headers per call (0: n), and counts its calls.
type olderAdapter struct {
	*fakeAdapter
	max        int
	bottomless bool // never reports the bottom
	belowCalls int
	asked      []string
	genErr     bool
}

func (a *olderAdapter) ListBelow(_ context.Context, _ RemoteMailbox, below string, n int) (OlderPage, error) {
	a.belowCalls++
	a.asked = append(a.asked, below)
	if a.genErr {
		return OlderPage{}, ErrGenerationChanged
	}
	floor, err := strconv.ParseUint(below, 10, 32)
	if err != nil {
		return OlderPage{}, err
	}
	var uids []uint32
	for _, id := range a.ids {
		if u, _ := strconv.ParseUint(id, 10, 32); u < floor {
			uids = append(uids, uint32(u))
		}
	}
	slices.Sort(uids)
	slices.Reverse(uids)
	limit := n
	if a.max > 0 {
		limit = min(limit, a.max)
	}
	var page OlderPage
	for i, u := range uids {
		if i == limit {
			break
		}
		page.Headers = append(page.Headers, metaHeader(strconv.FormatUint(uint64(u), 10), 0))
	}
	page.Bottom = len(uids) <= limit && !a.bottomless
	return page, nil
}

// olderFolder is a folder of uids 1..total on the server whose window holds
// the newest window of them.
func olderFolder(t *testing.T, total, window int) (*storage.DB, storage.Folder, *olderAdapter) {
	t.Helper()
	db, folder := newSyncTestFolder(t)
	uids := make([]uint32, total)
	for i := range uids {
		uids[i] = uint32(i + 1)
	}
	adapter := &olderAdapter{fakeAdapter: &fakeAdapter{ids: fakeIDs(uids...), listMeta: true, stateToken: "s1"}}
	engine := NewEngine(adapter, db, nil)
	engine.InitialLimit = window
	if _, err := engine.SyncFolderStubs(context.Background(), folder, 0); err != nil {
		t.Fatalf("first list: %v", err)
	}
	adapter.listCalls = 0
	return db, folder, adapter
}

func floorOf(t *testing.T, db *storage.DB, folder storage.Folder) (string, uint32) {
	t.Helper()
	f, err := db.GetFolder(context.Background(), folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := db.FolderSyncFloorUID(context.Background(), folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f.SyncFloorID, uid
}

// The older page stores the n messages just below the floor and lowers the
// floor to the lowest of them, without a full list.
func TestSyncOlderPageStoresTheNextPageBelowTheFloor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, adapter := olderFolder(t, 2000, 50)
	if id, _ := floorOf(t, db, folder); id != "1951" {
		t.Fatalf("floor %q after the first list, want 1951", id)
	}
	res, err := NewEngine(adapter, db, nil).SyncOlderPage(ctx, folder, 50)
	if err != nil {
		t.Fatalf("older page: %v", err)
	}
	if adapter.listCalls != 0 || adapter.belowCalls != 1 || adapter.asked[0] != "1951" {
		t.Fatalf("%d full lists, %d older lists below %v; want none and one below 1951", adapter.listCalls, adapter.belowCalls, adapter.asked)
	}
	if len(res.ToFetch) != 50 || res.ToFetch[0] != "1950" || res.Admitted != 50 || !res.HasOlder {
		t.Fatalf("result %d ids from %v, admitted %d, older %v", len(res.ToFetch), res.ToFetch[:1], res.Admitted, res.HasOlder)
	}
	if n := countRows(t, db, folder.ID); n != 100 {
		t.Fatalf("%d rows, want 100", n)
	}
	if id, uid := floorOf(t, db, folder); id != "1901" || uid != 1901 {
		t.Fatalf("floor %q/%d, want 1901", id, uid)
	}
	if folderToken(t, db, folder).StateToken != "s1" {
		t.Fatal("the older page dropped the cursor: the window covers only stored rows")
	}
}

// A page that finds fewer than n (a bounded look) lowers the floor only to
// what it stored; one that reaches the bottom opens the whole folder.
func TestSyncOlderPageLowersOnlyToWhatItStored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, adapter := olderFolder(t, 100, 50)
	adapter.max, adapter.bottomless = 10, true
	res, err := NewEngine(adapter, db, nil).SyncOlderPage(ctx, folder, 50)
	if err != nil || res.Admitted != 10 || !res.HasOlder {
		t.Fatalf("admitted %d, older %v, err %v; want 10 and more older", res.Admitted, res.HasOlder, err)
	}
	if id, _ := floorOf(t, db, folder); id != "41" {
		t.Fatalf("floor %q, want 41", id)
	}

	adapter.max, adapter.bottomless = 0, false
	res, err = NewEngine(adapter, db, nil).SyncOlderPage(ctx, folder, 50)
	if err != nil || res.Admitted != 40 || res.HasOlder {
		t.Fatalf("admitted %d, older %v, err %v; want the last 40 and no floor", res.Admitted, res.HasOlder, err)
	}
	if id, uid := floorOf(t, db, folder); id != "" || uid != 0 {
		t.Fatalf("floor %q/%d, want none", id, uid)
	}
	if n := countRows(t, db, folder.ID); n != 100 {
		t.Fatalf("%d rows, want 100", n)
	}
}

// A store that fails stops the page there: the floor goes down only over
// rows that exist.
func TestSyncOlderPageStopsTheFloorAtAFailedStub(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, adapter := olderFolder(t, 100, 50)
	engine := NewEngine(adapter, db, nil)
	engine.beforeListStub = func(rid string) error {
		if rid == "45" {
			return errors.New("disk full")
		}
		return nil
	}
	res, err := engine.SyncOlderPage(ctx, folder, 50)
	if err == nil || res.Admitted != 5 {
		t.Fatalf("admitted %d, err %v; want 5 and an error", res.Admitted, err)
	}
	if id, _ := floorOf(t, db, folder); id != "46" {
		t.Fatalf("floor %q, want 46", id)
	}
}

// Without OlderLister, or after a generation change, the page cannot run
// and says so; a folder without a floor has nothing below its window.
func TestSyncOlderPageNeedsAFullList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, adapter := olderFolder(t, 100, 50)
	if _, err := NewEngine(adapter.fakeAdapter, db, nil).SyncOlderPage(ctx, folder, 50); !errors.Is(err, ErrNeedFullList) {
		t.Fatalf("no OlderLister: %v, want ErrNeedFullList", err)
	}
	adapter.genErr = true
	if _, err := NewEngine(adapter, db, nil).SyncOlderPage(ctx, folder, 50); !errors.Is(err, ErrNeedFullList) {
		t.Fatalf("generation changed: %v, want ErrNeedFullList", err)
	}

	db, folder, adapter = olderFolder(t, 10, 50)
	res, err := NewEngine(adapter, db, nil).SyncOlderPage(ctx, folder, 50)
	if err != nil || adapter.belowCalls != 0 || res.Admitted != 0 || res.HasOlder {
		t.Fatalf("no floor: %+v, %d calls, err %v; want nothing done", res, adapter.belowCalls, err)
	}
}

// countingStates counts the message-state rows the engine reads.
type countingStates struct {
	*storage.DB
	rows int
}

func (c *countingStates) ListMessageStates(ctx context.Context, folderID int64) ([]storage.MessageState, error) {
	s, err := c.DB.ListMessageStates(ctx, folderID)
	c.rows += len(s)
	return s, err
}

func (c *countingStates) MessageStatesByRemoteID(ctx context.Context, folderID int64, ids []string) ([]storage.MessageState, error) {
	s, err := c.DB.MessageStatesByRemoteID(ctx, folderID, ids)
	c.rows += len(s)
	return s, err
}

// The fast older page reads local rows only for its page of server ids, not
// the folder's whole cache: with 5000 cached rows a page of 50 reads at most
// 50. A row already cached below the floor is admitted, not stored again.
func TestSyncOlderPageReadsOnlyItsPageOfLocalRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, adapter := olderFolder(t, 5100, 5000)
	if id, _ := floorOf(t, db, folder); id != "101" {
		t.Fatalf("floor %q after the first list, want 101", id)
	}
	if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: folder.AccountID, FolderID: folder.ID,
		UID: 90, RemoteID: "90", Subject: "cached"}); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(adapter, db, nil)
	counter := &countingStates{DB: db}
	engine.states = counter
	res, err := engine.SyncOlderPage(ctx, folder, 50)
	if err != nil {
		t.Fatalf("older page: %v", err)
	}
	if counter.rows > 50 {
		t.Fatalf("read %d local rows for a page of 50 in a folder of %d", counter.rows, 5001)
	}
	if res.Admitted != 50 || len(res.ToFetch) != 49 || slices.Contains(res.ToFetch, "90") {
		t.Fatalf("admitted %d, stored %d (90 among them: %v); want 50 admitted, 49 stored without 90",
			res.Admitted, len(res.ToFetch), slices.Contains(res.ToFetch, "90"))
	}
	if n := countRows(t, db, folder.ID); n != 5050 {
		t.Fatalf("%d rows, want 5050", n)
	}
	if id, _ := floorOf(t, db, folder); id != "51" {
		t.Fatalf("floor %q, want 51", id)
	}
}
