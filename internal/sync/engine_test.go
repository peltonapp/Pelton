package sync

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// List stubs land and are announced once before any body fetch; the body fill
// then reuses those rows and announces once more.
func TestEngineStoresListStubsBeforeBodies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"e1", "e2"}, listMeta: true}
	engine := NewEngine(adapter, db, nil)

	var stubAnnounces, bodyAnnounces int
	engine.OnStored = func(_ storage.Folder, ids []int64) {
		if len(ids) == 0 {
			return
		}
		if len(adapter.fetched) != 0 {
			bodyAnnounces++
			return
		}
		stubAnnounces++
		msgs, err := db.ListMessages(ctx, folder.ID, 0)
		if err != nil || len(msgs) != 2 {
			t.Errorf("stub rows during announce: %d (err %v), want 2", len(msgs), err)
			return
		}
		for _, m := range msgs {
			if m.Subject != "subj-"+m.RemoteID || m.BodyPlain != "preview-"+m.RemoteID {
				t.Errorf("stub %q subject %q preview %q", m.RemoteID, m.Subject, m.BodyPlain)
			}
		}
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if stubAnnounces != 1 || bodyAnnounces != 1 {
		t.Fatalf("announces stub=%d body=%d, want 1 each (stubs before any Fetch)", stubAnnounces, bodyAnnounces)
	}
	if len(adapter.fetched) != 2 {
		t.Fatalf("fetched %v, want both bodies", adapter.fetched)
	}
	msgs, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list after sync: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("cached %d, want 2 (body fill must reuse stub rows)", len(msgs))
	}
	for _, m := range msgs {
		if m.Subject != "test" { // fakeAdapter.Fetch sets Subject: "test"
			t.Errorf("after body fill %q subject %q, want test", m.RemoteID, m.Subject)
		}
	}
}

// New mail that arrives while bodies are still downloading must appear as list
// stubs without waiting for the rest of the body campaign (the Pelton-vs-Apple
// Mail gap during a 22k-message backfill).
func TestEngineAbsorbsArrivalsDuringBodyFill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"old-a", "old-b"}, listMeta: true, stateToken: "s0"}, deltas: map[string]Delta{"s0": {Changed: []Header{metaHeader("new-1", 0)}, Cursor: "s1"}}}
	engine := NewEngine(adapter, db, nil)
	engine.FetchBatchSize = 1

	var releases int
	engine.ReleaseLock = func() { releases++ }

	sawNewStub := make(chan struct{}, 1)
	engine.OnStored = func(_ storage.Folder, ids []int64) {
		if len(ids) == 0 {
			return
		}
		msgs, err := db.ListMessages(ctx, folder.ID, 0)
		if err != nil {
			return
		}
		for _, m := range msgs {
			if m.RemoteID == "new-1" && m.Subject == "subj-new-1" {
				select {
				case sawNewStub <- struct{}{}:
				default:
				}
			}
		}
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if releases == 0 {
		t.Fatal("ReleaseLock never called between body batches")
	}
	select {
	case <-sawNewStub:
	default:
		t.Fatal("expected OnStored for new-1 stub before body fill finished")
	}
	msgs, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("cached %d, want 3 (mid-fetch arrival absorbed)", len(msgs))
	}
	if !slices.ContainsFunc(msgs, func(m storage.Message) bool { return m.RemoteID == "new-1" }) {
		t.Fatal("new-1 missing from cache after absorb")
	}
	if !slices.Contains(adapter.fetched, "new-1") {
		t.Fatalf("fetched %v, want new-1 body ahead of remaining old mail", adapter.fetched)
	}
}

func TestEngineSkipsStubsWithoutListMeta(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"a"}}
	engine := NewEngine(adapter, db, nil)

	announces := 0
	engine.OnStored = func(_ storage.Folder, _ []int64) { announces++ }

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	// IMAP-style headers: one announce from fetchNew only, not a stub pass.
	if announces != 1 {
		t.Fatalf("OnStored called %d times, want 1 (no stub announce)", announces)
	}
}

func TestEngineFetchesNewIDAndReportsProgress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"a", "b"}}
	engine := NewEngine(adapter, db, nil)

	var progress []FolderProgress
	engine.OnProgress = func(p FolderProgress) {
		progress = append(progress, p)
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}

	if got := adapter.fetched; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("fetched %v, want [a b]", got)
	}
	sawTotal := false
	for _, p := range progress {
		if p.Total == 2 {
			sawTotal = true
			break
		}
	}
	if !sawTotal {
		t.Fatalf("OnProgress never saw Total==2, got %v", progress)
	}

	msgs, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("cached %d messages, want 2", len(msgs))
	}
	byID := map[string]storage.Message{}
	for _, m := range msgs {
		byID[m.RemoteID] = m
	}
	for _, id := range []string{"a", "b"} {
		m, ok := byID[id]
		if !ok {
			t.Fatalf("missing remote_id %q", id)
		}
		if m.UID != 0 {
			t.Errorf("remote_id %q uid = %d, want 0", id, m.UID)
		}
	}
}

func TestEnginePushesPendingFlags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"a", "b"}}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	markPendingSeen(t, db, folder, "b")

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(adapter.flagPushes) != 1 || adapter.flagPushes[0] != "b" {
		t.Fatalf("flagPushes = %v, want [b]", adapter.flagPushes)
	}
}

func TestEngineGenerationChangeDropsAndRefetches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"a"}, generation: "1"}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	adapter.ids = []string{"b"}
	adapter.generation = "2"
	adapter.fetched = nil
	fresh, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatalf("reload folder: %v", err)
	}
	res, err := engine.SyncFolder(ctx, *fresh)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !res.GenerationReset {
		t.Fatal("expected GenerationReset")
	}
	if len(adapter.fetched) != 1 || adapter.fetched[0] != "b" {
		t.Fatalf("fetched %v, want [b]", adapter.fetched)
	}
	states, err := db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	if len(states) != 1 || states[0].RemoteID != "b" {
		t.Fatalf("states = %v, want only b", states)
	}
}

// SyncFolderStubs never fetches bodies. With list meta it stores and announces
// stubs; legacy headers store nothing and leave every id in ToFetch.
func TestSyncFolderStubsDoesNotFetchBodies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		ids           []string
		listMeta      bool
		wantToFetch   []string // newest first
		wantStubs     int
		wantAnnounces int
	}{
		{"list meta", fakeIDs(1, 2), true, fakeIDs(2, 1), 2, 1},
		{"legacy headers", fakeIDs(1), false, fakeIDs(1), 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			adapter := &fakeAdapter{ids: tc.ids, listMeta: tc.listMeta}
			engine := NewEngine(adapter, db, nil)
			var announces int
			engine.OnStored = func(_ storage.Folder, ids []int64) {
				if len(ids) > 0 {
					announces++
				}
			}

			res, err := engine.SyncFolderStubs(ctx, folder, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(adapter.fetched) != 0 {
				t.Fatalf("fetched bodies %v", adapter.fetched)
			}
			if !slices.Equal(res.ToFetch, tc.wantToFetch) {
				t.Fatalf("ToFetch=%v, want %v", res.ToFetch, tc.wantToFetch)
			}
			if announces != tc.wantAnnounces {
				t.Fatalf("stub announces=%d, want %d", announces, tc.wantAnnounces)
			}
			msgs, err := db.ListMessages(ctx, folder.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(msgs) != tc.wantStubs {
				t.Fatalf("stub rows=%d, want %d", len(msgs), tc.wantStubs)
			}
			for _, m := range msgs {
				if m.BodyComplete {
					t.Fatalf("stub %q left body_complete set", m.RemoteID)
				}
			}
		})
	}
}

// A cancel while stub rows are being written must not persist the new state
// token. If it did, the next incremental list (nothing new since that
// token) would skip messages that never landed.
func TestSyncFolderStubsCancelDuringStubStoreKeepsStateToken(t *testing.T) {
	t.Parallel()
	db, folder := newSyncTestFolder(t)
	bg := context.Background()
	if err := db.SetFolderStateToken(bg, folder.ID, "s0"); err != nil {
		t.Fatal(err)
	}

	// Caught up at s1: a cursor that advanced early sees no arrivals.
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: fakeIDs(1, 2), listMeta: true, stateToken: "s1"}, deltas: map[string]Delta{"s1": {Cursor: "s1"}}}
	engine := NewEngine(adapter, db, nil)

	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	// Stubs are stored newest-first, so "2" lands and "1" is the write we cancel.
	engine.beforeListStub = func(remoteID string) error {
		if remoteID == "1" {
			cancel()
		}
		return nil
	}

	_, err := engine.SyncFolderStubs(ctx, folder, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	got, err := db.GetFolder(bg, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StateToken != "s0" {
		t.Fatalf("state token %q, want s0", got.StateToken)
	}
	if has := remoteIDsPresent(t, db, folder.ID); has["1"] || !has["2"] {
		t.Fatalf("after cancel cached %v, want only 2", has)
	}

	engine.beforeListStub = nil
	if _, err := engine.SyncFolderStubs(bg, folder, 0); err != nil {
		t.Fatalf("retry: %v", err)
	}
	got, err = db.GetFolder(bg, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StateToken != "s1" {
		t.Fatalf("state token after retry %q, want s1", got.StateToken)
	}
	if has := remoteIDsPresent(t, db, folder.ID); !has["1"] || !has["2"] {
		t.Fatalf("after retry cached %v, want 1 and 2 (unstored mail must not be skipped)", has)
	}
}

func remoteIDsPresent(t *testing.T, db *storage.DB, folderID int64) map[string]bool {
	t.Helper()
	msgs, err := db.ListMessages(context.Background(), folderID, 0)
	if err != nil {
		t.Fatal(err)
	}
	has := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		has[m.RemoteID] = true
	}
	return has
}

func TestSyncFolderSoftPausePreservesRemaining(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3), listMeta: true}
	engine := NewEngine(adapter, db, nil)
	engine.FetchBatchSize = 1
	engine.PauseCheck = func() bool { return len(adapter.fetched) >= 1 }

	_, err := engine.SyncFolder(ctx, folder)
	if !errors.Is(err, ErrSoftPaused) {
		t.Fatalf("err=%v want ErrSoftPaused", err)
	}
	rest, ok := SoftPauseRemaining(err)
	if !ok || len(rest) != 2 || rest[0] != "2" || rest[1] != "1" {
		t.Fatalf("remaining %v ok=%v, want [2 1]", rest, ok)
	}
	if len(adapter.fetched) != 1 || adapter.fetched[0] != "3" {
		t.Fatalf("fetched %v, want [3]", adapter.fetched)
	}
}

func TestFetchBodiesSkipsAbsorbWhenDisabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	if err := db.SetFolderStateToken(ctx, folder.ID, "s0"); err != nil {
		t.Fatal(err)
	}
	folder.StateToken = "s0"
	seedLocalStub(t, db, folder, "a")
	seedLocalStub(t, db, folder, "b")
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{stateToken: "s0", listMeta: true}, deltas: map[string]Delta{"s0": {Changed: []Header{metaHeader("late", 0)}, Cursor: "s1"}}}
	engine := NewEngine(adapter, db, nil)
	engine.FetchBatchSize = 1
	engine.AbsorbArrivals = false
	if _, err := engine.FetchBodies(ctx, folder, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if adapter.changeCalls != 0 || adapter.listCalls != 0 {
		t.Fatalf("list calls during FetchBodies=%d/%d, want 0 with AbsorbArrivals=false", adapter.changeCalls, adapter.listCalls)
	}
	if len(adapter.fetched) != 2 {
		t.Fatalf("fetched %v, want both bodies", adapter.fetched)
	}
}

// FetchBodies checks PauseCheck only between batches of Engine.FetchBatchSize:
// a pause never cuts the batch in flight, and nothing pauses once all is fetched.
func TestFetchBodiesSoftPausesBetweenBatches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		batch         int
		ids           []string
		pause         func(a *fakeAdapter) bool
		wantFetched   []string
		wantRemaining []string // nil: no pause, FetchBodies returns nil
	}{
		{"engine batch size", 10, newestIDs(25), func(a *fakeAdapter) bool { return a.commands >= 1 }, newestIDs(25)[:10], newestIDs(15)},
		{"after one batch", 1, newestIDs(3), func(a *fakeAdapter) bool { return len(a.fetched) >= 1 }, fakeIDs(3), fakeIDs(2, 1)},
		{"finishes in-flight batch", 2, newestIDs(3), func(a *fakeAdapter) bool { return len(a.fetched) >= 1 }, fakeIDs(3, 2), fakeIDs(1)},
		{"not after final batch", 1, newestIDs(1), func(*fakeAdapter) bool { return true }, fakeIDs(1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, folder := newSyncTestFolder(t)
			adapter := &fakeAdapter{}
			engine := NewEngine(adapter, db, nil)
			engine.FetchBatchSize = tc.batch
			engine.PauseCheck = func() bool { return tc.pause(adapter) }

			_, err := engine.FetchBodies(context.Background(), folder, tc.ids)
			if tc.wantRemaining == nil {
				if err != nil {
					t.Fatalf("err=%v, want nil when nothing remains", err)
				}
			} else if !errors.Is(err, ErrSoftPaused) {
				t.Fatalf("err=%v want ErrSoftPaused", err)
			}
			if adapter.commands != 1 {
				t.Fatalf("Fetch commands=%d want 1", adapter.commands)
			}
			if !slices.Equal(adapter.fetched, tc.wantFetched) {
				t.Fatalf("fetched %v, want the whole first batch %v", adapter.fetched, tc.wantFetched)
			}
			if rest, ok := SoftPauseRemaining(err); tc.wantRemaining != nil && (!ok || !slices.Equal(rest, tc.wantRemaining)) {
				t.Fatalf("remaining %v ok=%v, want %v", rest, ok, tc.wantRemaining)
			}
		})
	}
}

// newestIDs returns "n" down to "1", the newest-first order bodies are fetched in.
func newestIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strconv.Itoa(n - i)
	}
	return out
}

func TestFetchBodiesSkipsIDsThatDoNotNeedBody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	if _, err := db.InsertMessageWithAttachments(ctx, &storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: "m1",
		Subject: "kept", BodyPlain: "already",
	}, nil); err != nil {
		t.Fatal(err)
	}
	seedLocalStub(t, db, folder, "m2")
	adapter := &fakeAdapter{}
	engine := NewEngine(adapter, db, nil)
	if _, err := engine.FetchBodies(ctx, folder, []string{"m1", "m2"}); err != nil {
		t.Fatal(err)
	}
	if len(adapter.fetched) != 1 || adapter.fetched[0] != "m2" {
		t.Fatalf("fetched %v, want [m2]", adapter.fetched)
	}
}

// A row that becomes body-complete before it is stored must not fail the
// batch. The rest of the ids in that batch still land.
func TestFetchBodiesBodyCompleteDoesNotFailBatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	for _, id := range []string{"m1", "m2"} {
		seedLocalStub(t, db, folder, id)
	}
	adapter := &fakeAdapter{}
	adapter.onFetch = func(id string) {
		if id != "m1" {
			return
		}
		if _, err := db.InsertMessageWithAttachments(ctx, &storage.Message{
			AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: "m1",
			Subject: "kept", BodyPlain: "already",
		}, nil); err != nil {
			t.Errorf("pre-fill m1: %v", err)
		}
	}
	engine := NewEngine(adapter, db, nil)
	if _, err := engine.FetchBodies(ctx, folder, []string{"m1", "m2"}); err != nil {
		t.Fatalf("complete row must not fail the batch: %v", err)
	}
	m1, err := messageByRemote(t, db, folder.ID, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if !m1.BodyComplete || m1.BodyPlain != "already" {
		t.Fatalf("m1 = complete:%v plain:%q, want already-stored body", m1.BodyComplete, m1.BodyPlain)
	}
	m2, err := messageByRemote(t, db, folder.ID, "m2")
	if err != nil {
		t.Fatal(err)
	}
	if !m2.BodyComplete {
		t.Fatal("m2 was not stored after the complete id was skipped")
	}
}

// The first attempt stores one id and fails before the tail. The next
// FetchBodies of the original list fetches only the incomplete ids.
func TestFetchBodiesSecondAttemptSkipsStoredIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	for _, id := range []string{"m1", "m2", "m3"} {
		seedLocalStub(t, db, folder, id)
	}
	adapter := &fakeAdapter{}
	adapter.onFetch = func(id string) {
		if id == "m1" {
			adapter.fetchErr = errors.New("boom")
		}
	}
	engine := NewEngine(adapter, db, nil)
	engine.FetchBatchSize = 1
	if _, err := engine.FetchBodies(ctx, folder, []string{"m1", "m2", "m3"}); err == nil {
		t.Fatal("expected the first attempt to fail after storing m1")
	}
	if len(adapter.fetched) != 1 || adapter.fetched[0] != "m1" {
		t.Fatalf("first attempt fetched %v, want [m1]", adapter.fetched)
	}
	adapter.fetchErr = nil
	adapter.onFetch = nil
	if _, err := engine.FetchBodies(ctx, folder, []string{"m1", "m2", "m3"}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(adapter.fetched) != 3 || adapter.fetched[1] != "m2" || adapter.fetched[2] != "m3" {
		t.Fatalf("retry fetched %v, want [m1 m2 m3]", adapter.fetched)
	}
	for _, id := range []string{"m1", "m2", "m3"} {
		m, err := messageByRemote(t, db, folder.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		if !m.BodyComplete {
			t.Fatalf("%s body_complete=0", id)
		}
	}
}

func messageByRemote(t *testing.T, db *storage.DB, folderID int64, remoteID string) (*storage.Message, error) {
	t.Helper()
	msgs, err := db.ListMessages(context.Background(), folderID, 0)
	if err != nil {
		return nil, err
	}
	for i := range msgs {
		if msgs[i].RemoteID == remoteID {
			return &msgs[i], nil
		}
	}
	return nil, errors.New("missing " + remoteID)
}

// A message moved out of the folder while bodies download must not be fetched
// back in as a ghost row: the absorbed delta removes it from the queue.
func TestEngineAbsorbDropsRemovedIDsFromBodyQueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"old-a", "old-b"}, listMeta: true, stateToken: "s0"}, deltas: map[string]Delta{"s0": {Removed: []string{"old-b"}, Cursor: "s1"}}}
	engine := NewEngine(adapter, db, nil)
	engine.FetchBatchSize = 1

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	for _, id := range adapter.fetched {
		if id == "old-b" {
			t.Fatalf("fetched %v, old-b was removed and must not be fetched", adapter.fetched)
		}
	}
	if has := remoteIDsPresent(t, db, folder.ID); has["old-b"] || !has["old-a"] {
		t.Fatalf("cached %v, want only old-a", has)
	}
}

// A stub whose body fetch failed (dropped connection, quit) is listed as
// cached, so only the stub refill brings its body in on the next sync, on the
// full-list path and the delta path alike.
func TestSyncFolderRefillsStubsWithoutBody(t *testing.T) {
	t.Parallel()
	for _, condstore := range []bool{false, true} {
		t.Run(map[bool]string{false: "full list", true: "condstore delta"}[condstore], func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			inner := &fakeAdapter{ids: fakeIDs(1, 2, 3, 4), listMeta: true}
			var adapter Adapter = inner
			if condstore {
				inner.stateToken = "c1"
				adapter = &deltaAdapter{fakeAdapter: inner, deltas: map[string]Delta{"c1": {Cursor: "c1", Unchanged: true}}}
			}
			inner.onFetch = func(id string) {
				if id == "4" {
					inner.fetchErr = errors.New("connection reset")
				}
			}
			engine := NewEngine(adapter, db, nil)
			engine.FetchBatchSize = 1
			if _, err := engine.SyncFolder(ctx, folder); err == nil {
				t.Fatal("first sync: want the dropped connection reported")
			}

			inner.fetchErr, inner.onFetch, inner.fetched = nil, nil, nil
			res, err := engine.SyncFolder(ctx, folder)
			if err != nil {
				t.Fatalf("second sync: %v", err)
			}
			if want := []string{"3", "2", "1"}; !slices.Equal(inner.fetched, want) {
				t.Fatalf("second sync fetched %v, want %v", inner.fetched, want)
			}
			if res.New != 3 {
				t.Fatalf("new = %d, want 3", res.New)
			}
			for _, id := range fakeIDs(1, 2, 3, 4) {
				if m, err := messageByRemote(t, db, folder.ID, id); err != nil || !m.BodyComplete {
					t.Fatalf("%s not complete: %+v %v", id, m, err)
				}
			}
		})
	}
}

// unreadableAdapter is fakeAdapter whose bodies for bad ids do not parse.
type unreadableAdapter struct {
	*fakeAdapter
	bad map[string]bool
}

func (a *unreadableAdapter) Fetch(ctx context.Context, box string, remoteIDs []string) ([]Fetched, error) {
	var good, bad []string
	for _, id := range remoteIDs {
		if a.bad[id] {
			bad = append(bad, id)
		} else {
			good = append(good, id)
		}
	}
	out, err := a.fakeAdapter.Fetch(ctx, box, good)
	if err == nil && len(bad) > 0 {
		err = &MessageError{RemoteIDs: bad, Err: errors.New("malformed MIME")}
	}
	return out, err
}

// One message that does not parse must not stop the rest of the folder: it
// is reported, stays a stub, and the next list hands it out again.
func TestFetchBodiesContinuesPastUnreadableMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	ids := []string{"m1", "m2", "m3", "m4"}
	for _, id := range ids {
		seedLocalStub(t, db, folder, id)
	}
	adapter := &unreadableAdapter{fakeAdapter: &fakeAdapter{ids: ids, listMeta: true}, bad: map[string]bool{"m2": true}}
	engine := NewEngine(adapter, db, nil)
	engine.FetchBatchSize = 1

	res, err := engine.FetchBodies(ctx, folder, ids)
	var me *MessageError
	if !errors.As(err, &me) || !slices.Equal(me.RemoteIDs, []string{"m2"}) {
		t.Fatalf("err = %v, want a MessageError for m2", err)
	}
	if want := []string{"m1", "m3", "m4"}; !slices.Equal(adapter.fetched, want) || res.New != 3 {
		t.Fatalf("fetched %v (new %d), want %v", adapter.fetched, res.New, want)
	}
	if m, err := messageByRemote(t, db, folder.ID, "m2"); err != nil || m.BodyComplete {
		t.Fatalf("m2 = %+v %v, want an incomplete stub", m, err)
	}

	stubs, err := engine.SyncFolderStubs(ctx, folder, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(stubs.ToFetch, []string{"m2"}) {
		t.Fatalf("next list ToFetch = %v, want [m2]", stubs.ToFetch)
	}
}
