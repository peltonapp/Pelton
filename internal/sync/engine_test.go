package sync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestEngineStoresListStubsBeforeBodies(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"e1", "e2"}, listMeta: true}
	engine := NewEngine(adapter, db, nil)

	var stubAnnounceBeforeFetch bool
	engine.OnStored = func(_ storage.Folder, ids []int64) {
		if len(adapter.fetched) != 0 || len(ids) == 0 {
			return
		}
		msgs, err := db.ListMessages(ctx, folder.ID, 0)
		if err != nil {
			t.Errorf("list during stub announce: %v", err)
			return
		}
		if len(msgs) != 2 {
			t.Errorf("stub rows: got %d want 2", len(msgs))
			return
		}
		for _, m := range msgs {
			if m.Subject != "subj-"+m.RemoteID {
				t.Errorf("stub %q subject %q", m.RemoteID, m.Subject)
			}
			if m.BodyPlain != "preview-"+m.RemoteID {
				t.Errorf("stub %q preview %q", m.RemoteID, m.BodyPlain)
			}
		}
		stubAnnounceBeforeFetch = true
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !stubAnnounceBeforeFetch {
		t.Fatal("expected OnStored with list stubs before any Fetch")
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
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"old-a", "old-b"}, listMeta: true, stateToken: "s0"}, deltas: map[string]Delta{"s0": {Changed: []Header{metaHeader("new-1", 0)}, Cursor: "s1"}}}
	engine := NewEngine(adapter, db, nil)

	oldBatch := fetchBatch
	fetchBatch = 1
	t.Cleanup(func() { fetchBatch = oldBatch })

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
	found := false
	for _, m := range msgs {
		if m.RemoteID == "new-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("new-1 missing from cache after absorb")
	}
	fetchedNew := slices.Contains(adapter.fetched, "new-1")
	if !fetchedNew {
		t.Fatalf("fetched %v, want new-1 body ahead of remaining old mail", adapter.fetched)
	}
}

func TestEngineSkipsStubsWithoutListMeta(t *testing.T) {
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
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"a", "b"}}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	states, err := db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	var found bool
	for _, s := range states {
		if s.RemoteID == "b" {
			found = true
			if err := db.MarkFlagsPending(ctx, s.ID, storage.FlagSeen); err != nil {
				t.Fatalf("mark flags pending: %v", err)
			}
		}
	}
	if !found {
		t.Fatal("remote_id b was not cached")
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(adapter.flagPushes) != 1 || adapter.flagPushes[0] != "b" {
		t.Fatalf("flagPushes = %v, want [b]", adapter.flagPushes)
	}
}

func TestEnginePushesPendingDelete(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"a", "b"}}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	states, err := db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatalf("list states: %v", err)
	}
	var found bool
	for _, s := range states {
		if s.RemoteID == "a" {
			found = true
			if err := db.MarkDeletePending(ctx, s.ID); err != nil {
				t.Fatalf("mark delete pending: %v", err)
			}
		}
	}
	if !found {
		t.Fatal("remote_id a was not cached")
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(adapter.deleted) != 1 || adapter.deleted[0] != "a" {
		t.Fatalf("deleted = %v, want [a]", adapter.deleted)
	}
	states, err = db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatalf("list states after delete: %v", err)
	}
	for _, s := range states {
		if s.RemoteID == "a" {
			t.Fatal("remote_id a row still present after delete")
		}
	}
}

func TestEngineGenerationChangeDropsAndRefetches(t *testing.T) {
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

func TestSyncFolderStubsDoesNotFetchBodies(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2), listMeta: true}
	engine := NewEngine(adapter, db, nil)

	res, err := engine.SyncFolderStubs(ctx, folder, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.fetched) != 0 {
		t.Fatalf("fetched bodies %v", adapter.fetched)
	}
	if len(res.ToFetch) != 2 {
		t.Fatalf("ToFetch=%v", res.ToFetch)
	}
	if res.ToFetch[0] != "2" || res.ToFetch[1] != "1" {
		t.Fatalf("ToFetch=%v, want newest first [2 1]", res.ToFetch)
	}
	msgs, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("stub rows=%d, want 2", len(msgs))
	}
	for _, m := range msgs {
		if m.BodyComplete {
			t.Fatalf("stub %q left body_complete set", m.RemoteID)
		}
	}
}

// A cancel while stub rows are being written must not persist the new state
// token. If it did, the next incremental list (nothing new since that
// token) would skip messages that never landed.
func TestSyncFolderStubsCancelDuringStubStoreKeepsStateToken(t *testing.T) {
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

func TestSyncFolderStubsReturnsToFetchWithoutListMeta(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1)}
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
	if len(res.ToFetch) != 1 || res.ToFetch[0] != "1" {
		t.Fatalf("ToFetch=%v, want [1]", res.ToFetch)
	}
	if announces != 0 {
		t.Fatalf("stub announces=%d, want 0 without list meta", announces)
	}
	msgs, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("cached %d rows, legacy headers must not store stubs", len(msgs))
	}
}

func TestSyncFolderAnnouncesListStubsOnce(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2), listMeta: true}
	engine := NewEngine(adapter, db, nil)

	var stubAnnounces, bodyAnnounces int
	engine.OnStored = func(_ storage.Folder, ids []int64) {
		if len(ids) == 0 {
			return
		}
		if len(adapter.fetched) == 0 {
			stubAnnounces++
			return
		}
		bodyAnnounces++
	}

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	if stubAnnounces != 1 {
		t.Fatalf("stub announces=%d, want 1", stubAnnounces)
	}
	if bodyAnnounces != 1 {
		t.Fatalf("body announces=%d, want 1", bodyAnnounces)
	}
	if len(adapter.fetched) != 2 {
		t.Fatalf("fetched %v, want both bodies", adapter.fetched)
	}
}

func TestSyncFolderSoftPausePreservesRemaining(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3), listMeta: true}
	old := fetchBatch
	fetchBatch = 1
	t.Cleanup(func() { fetchBatch = old })

	engine := NewEngine(adapter, db, nil)
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

func TestReconcileAndStoreStubsAnnouncesWithoutFetch(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{}
	engine := NewEngine(adapter, db, nil)

	var announced []int64
	engine.OnStored = func(_ storage.Folder, ids []int64) {
		announced = append(announced, ids...)
	}

	ids := engine.ReconcileAndStoreStubs(ctx, folder, []string{"1", "skip"}, map[string]Header{
		"1": {RemoteID: "1", HasListMeta: true, Subject: "Hi", Preview: "prev"},
	})
	if len(ids) != 1 {
		t.Fatalf("stored %v, want one stub", ids)
	}
	if len(announced) != 1 || announced[0] != ids[0] {
		t.Fatalf("announced %v, want %v", announced, ids)
	}
	if len(adapter.fetched) != 0 {
		t.Fatalf("fetched %v, stub path must not fetch bodies", adapter.fetched)
	}
	m, err := db.GetMessage(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.BodyComplete {
		t.Fatal("stub must leave body_complete=0")
	}
	if m.Subject != "Hi" || m.BodyPlain != "prev" {
		t.Fatalf("stub subject=%q preview=%q", m.Subject, m.BodyPlain)
	}
}

func TestFetchBodiesSkipsAbsorbWhenDisabled(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	if err := db.SetFolderStateToken(ctx, folder.ID, "s0"); err != nil {
		t.Fatal(err)
	}
	folder.StateToken = "s0"
	for _, id := range []string{"a", "b"} {
		if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{
			AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{stateToken: "s0", listMeta: true}, deltas: map[string]Delta{"s0": {Changed: []Header{metaHeader("late", 0)}, Cursor: "s1"}}}
	old := fetchBatch
	fetchBatch = 1
	t.Cleanup(func() { fetchBatch = old })

	engine := NewEngine(adapter, db, nil)
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

func TestFetchBodiesRespectsEngineFetchBatchSize(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{}
	ids := make([]string, 25)
	for i := range ids {
		ids[i] = fmt.Sprintf("m%d", 25-i)
	}

	engine := NewEngine(adapter, db, nil)
	engine.FetchBatchSize = 10
	engine.PauseCheck = func() bool { return adapter.commands >= 1 }

	_, err := engine.FetchBodies(ctx, folder, ids)
	if !errors.Is(err, ErrSoftPaused) {
		t.Fatalf("err=%v want ErrSoftPaused after first batch", err)
	}
	if adapter.commands != 1 {
		t.Fatalf("Fetch commands=%d want 1", adapter.commands)
	}
	if len(adapter.fetched) != 10 {
		t.Fatalf("fetched %d ids, want 10 in first batch", len(adapter.fetched))
	}
}

func TestFetchBodiesSoftPausesAfterBatch(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{}
	old := fetchBatch
	fetchBatch = 1
	defer func() { fetchBatch = old }()

	engine := NewEngine(adapter, db, nil)
	paused := false
	engine.PauseCheck = func() bool { return !paused && len(adapter.fetched) >= 1 }

	_, err := engine.FetchBodies(ctx, folder, []string{"3", "2", "1"})
	if !errors.Is(err, ErrSoftPaused) {
		t.Fatalf("err=%v want ErrSoftPaused", err)
	}
	if len(adapter.fetched) != 1 {
		t.Fatalf("fetched %d want 1 (one batch)", len(adapter.fetched))
	}
	if adapter.fetched[0] != "3" {
		t.Fatalf("fetched %v, want the first id only", adapter.fetched)
	}
	rest, ok := SoftPauseRemaining(err)
	if !ok || len(rest) != 2 || rest[0] != "2" || rest[1] != "1" {
		t.Fatalf("remaining %v ok=%v, want [2 1]", rest, ok)
	}
}

// Pause requested mid-batch must not cut the fetch already started.
func TestFetchBodiesSoftPauseFinishesInFlightBatch(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{}
	old := fetchBatch
	fetchBatch = 2
	defer func() { fetchBatch = old }()

	engine := NewEngine(adapter, db, nil)
	engine.PauseCheck = func() bool { return len(adapter.fetched) >= 1 }

	_, err := engine.FetchBodies(ctx, folder, []string{"3", "2", "1"})
	if !errors.Is(err, ErrSoftPaused) {
		t.Fatalf("err=%v want ErrSoftPaused", err)
	}
	if len(adapter.fetched) != 2 || adapter.fetched[0] != "3" || adapter.fetched[1] != "2" {
		t.Fatalf("fetched %v, want the whole in-flight batch [3 2]", adapter.fetched)
	}
	rest, ok := SoftPauseRemaining(err)
	if !ok || len(rest) != 1 || rest[0] != "1" {
		t.Fatalf("remaining %v ok=%v, want [1]", rest, ok)
	}
}

func TestFetchBodiesDoesNotPauseAfterFinalBatch(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{}
	old := fetchBatch
	fetchBatch = 1
	defer func() { fetchBatch = old }()

	engine := NewEngine(adapter, db, nil)
	engine.PauseCheck = func() bool { return true }

	if _, err := engine.FetchBodies(ctx, folder, []string{"1"}); err != nil {
		t.Fatalf("err=%v, want nil when nothing remains", err)
	}
	if len(adapter.fetched) != 1 || adapter.fetched[0] != "1" {
		t.Fatalf("fetched %v, want [1]", adapter.fetched)
	}
}

func TestFetchBodiesSkipsIDsThatDoNotNeedBody(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	if _, err := db.InsertMessageWithAttachments(ctx, &storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: "m1",
		Subject: "kept", BodyPlain: "already",
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: "m2",
	}); err != nil {
		t.Fatal(err)
	}
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
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	for _, id := range []string{"m1", "m2"} {
		if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{
			AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: id,
		}); err != nil {
			t.Fatal(err)
		}
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
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	for _, id := range []string{"m1", "m2", "m3"} {
		if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{
			AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &fakeAdapter{}
	old := fetchBatch
	fetchBatch = 1
	t.Cleanup(func() { fetchBatch = old })
	adapter.onFetch = func(id string) {
		if id == "m1" {
			adapter.fetchErr = errors.New("boom")
		}
	}
	engine := NewEngine(adapter, db, nil)
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
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"old-a", "old-b"}, listMeta: true, stateToken: "s0"}, deltas: map[string]Delta{"s0": {Removed: []string{"old-b"}, Cursor: "s1"}}}
	engine := NewEngine(adapter, db, nil)

	oldBatch := fetchBatch
	fetchBatch = 1
	t.Cleanup(func() { fetchBatch = oldBatch })

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
