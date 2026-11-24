package sync

import (
	"context"
	gosync "sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// stubTx is one list-stub write transaction afterStubTx saw.
type stubTx struct {
	rows int
	held time.Duration
}

// recordStubTxs makes engine record its list-stub transactions.
func recordStubTxs(engine *Engine) func() []stubTx {
	var mu gosync.Mutex
	var txs []stubTx
	engine.afterStubTx = func(rows int, held time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		txs = append(txs, stubTx{rows, held})
	}
	return func() []stubTx {
		mu.Lock()
		defer mu.Unlock()
		return append([]stubTx(nil), txs...)
	}
}

// A 500-stub page and the 50-stub first page store in chunks: no
// transaction takes more than stubTxRows stubs, and a page needs no more
// transactions than its chunks unless the time budget cut one short.
func TestSyncFolderStubsPageStoresAPageInChunks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder, adapter := pagedFolder(t)
	engine := NewEngine(adapter, db, nil)
	txs := recordStubTxs(engine)
	p := &StubPager{First: 50, Page: 500}

	before := 0
	for i, page := range []int{50, 500} {
		if _, more, err := engine.SyncFolderStubsPage(ctx, folder, p); err != nil || !more {
			t.Fatalf("call %d: more=%v err=%v", i, more, err)
		}
		got := txs()[before:]
		before += len(got)
		rows, cut := 0, false
		for _, tx := range got {
			if tx.rows > stubTxRows {
				t.Fatalf("call %d: a transaction took %d stubs, more than %d", i, tx.rows, stubTxRows)
			}
			rows += tx.rows
			cut = cut || tx.held >= stubTxBudget
		}
		if rows != page {
			t.Fatalf("call %d: transactions took %d stubs, want the page's %d", i, rows, page)
		}
		if chunks := (page + stubTxRows - 1) / stubTxRows; len(got) != chunks && !cut {
			t.Fatalf("call %d: %d transactions for %d stubs, want %d", i, len(got), page, chunks)
		}
		t.Logf("page of %d: %v", page, got)
	}
}

// A user's single-row write during a long listing goes in between two
// chunks instead of waiting for the whole listing.
func TestListingLetsAUserWriteInBetweenChunks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedCached(t, db, folder, 9999)
	userRow := cachedID(t, db, folder, "9999")
	const listed = 5000
	uids := make([]uint32, listed)
	for i := range uids {
		uids[i] = uint32(i + 1)
	}
	adapter := &fakeAdapter{ids: append(fakeIDs(uids...), "9999"), listMeta: true, stateToken: "s1"}
	engine := NewEngine(adapter, db, nil)

	var mu gosync.Mutex
	txs, landedAfter := 0, -1
	userDone := make(chan error, 1)
	engine.afterStubTx = func(int, time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		txs++
		if txs != 1 {
			return
		}
		go func() {
			err := db.SetFlagColor(ctx, userRow, 3)
			mu.Lock()
			landedAfter = txs
			mu.Unlock()
			userDone <- err
		}()
	}

	started := time.Now()
	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatal(err)
	}
	took := time.Since(started)
	if err := <-userDone; err != nil {
		t.Fatalf("user write: %v", err)
	}
	if got := countRows(t, db, folder.ID); got != listed+1 {
		t.Fatalf("%d rows, want %d", got, listed+1)
	}
	if landedAfter < 1 || landedAfter >= txs {
		t.Fatalf("user write landed after chunk %d of %d, want before the listing's last", landedAfter, txs)
	}
	t.Logf("%d stubs in %d transactions, %v; the user write landed after chunk %d", listed, txs, took, landedAfter)
}

// cachedID is the row id of remoteID in folder.
func cachedID(t *testing.T, db *storage.DB, folder storage.Folder, remoteID string) int64 {
	t.Helper()
	states, err := db.ListMessageStates(context.Background(), folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.RemoteID == remoteID {
			return s.ID
		}
	}
	t.Fatalf("no row %s", remoteID)
	return 0
}

// The gap after a transaction outlasts the busy handler's sleep a writer
// queued behind it can be in.
func TestStubTxGapOutlastsTheBusyHandlerStep(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		held time.Duration
		want time.Duration
	}{
		{0, 2 * time.Millisecond},
		{2 * time.Millisecond, 3 * time.Millisecond},
		{8 * time.Millisecond, 11 * time.Millisecond},
		{25 * time.Millisecond, 16 * time.Millisecond},
		{40 * time.Millisecond, 21 * time.Millisecond},
	} {
		if got := stubTxGap(tc.held); got != tc.want {
			t.Errorf("stubTxGap(%v) = %v, want %v", tc.held, got, tc.want)
		}
	}
}
