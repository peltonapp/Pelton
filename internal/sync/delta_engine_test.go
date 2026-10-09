package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// syncedOnce runs one full SyncFolder so the folder holds rows for ids and the
// adapter's stateToken becomes the stored cursor.
func syncedOnce(t *testing.T, db *storage.DB, folder storage.Folder, a Adapter) {
	t.Helper()
	if _, err := NewEngine(a, db, nil).SyncFolder(context.Background(), folder); err != nil {
		t.Fatalf("first sync: %v", err)
	}
}

func folderToken(t *testing.T, db *storage.DB, folder storage.Folder) storage.Folder {
	t.Helper()
	f, err := db.GetFolder(context.Background(), folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	return *f
}

func TestSyncFolderStubsUsesDeltaWhenCursorStored(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"b", "a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, adapter)
	listsBefore := adapter.listCalls

	adapter.deltas = map[string]Delta{"c1": {Changed: []Header{metaHeader("c", 0)}, Removed: []string{"a"}, Cursor: "c2"}}
	res, err := NewEngine(adapter, db, nil).SyncFolderStubs(ctx, folder, 0)
	if err != nil {
		t.Fatalf("delta sync: %v", err)
	}
	if adapter.listCalls != listsBefore {
		t.Fatal("delta sync must not list the folder in full")
	}
	has := remoteIDsPresent(t, db, folder.ID)
	if has["a"] || !has["b"] || !has["c"] {
		t.Fatalf("cached %v, want b and c", has)
	}
	if len(res.ToFetch) != 1 || res.ToFetch[0] != "c" {
		t.Fatalf("ToFetch = %v, want [c]", res.ToFetch)
	}
	if got := folderToken(t, db, folder).StateToken; got != "c2" {
		t.Fatalf("cursor = %q, want c2", got)
	}
}

func TestSyncFolderStubsFallsBackToFullOnNeedFullList(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, adapter)
	adapter.ids = []string{"b", "a"}
	adapter.stateToken = "c9"
	listsBefore, changesBefore := adapter.listCalls, adapter.changeCalls

	if _, err := NewEngine(adapter, db, nil).SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if adapter.changeCalls != changesBefore+1 || adapter.listCalls != listsBefore+1 {
		t.Fatalf("changeCalls=%d listCalls=%d, want one delta try then one full list", adapter.changeCalls-changesBefore, adapter.listCalls-listsBefore)
	}
	if got := folderToken(t, db, folder).StateToken; got != "c9" {
		t.Fatalf("cursor = %q, want c9 from the full list", got)
	}
}

func TestSyncFolderStubsDeltaErrorKeepsCursor(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, adapter)
	adapter.changesErr = errors.New("connection reset")
	listsBefore := adapter.listCalls

	if _, err := NewEngine(adapter, db, nil).SyncFolderStubs(ctx, folder, 0); err == nil {
		t.Fatal("want the delta error")
	}
	if adapter.listCalls != listsBefore {
		t.Fatal("a network error must not fall back to a full list")
	}
	if got := folderToken(t, db, folder).StateToken; got != "c1" {
		t.Fatalf("cursor = %q, want c1", got)
	}
}

func TestDeltaPendingFlagsUseServerFlagsFromEnsure(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"b", "a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, adapter)
	b, err := messageByRemote(t, db, folder.ID, "b")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkFlagsPending(ctx, b.ID, storage.FlagSeen); err != nil {
		t.Fatal(err)
	}
	// Another client flagged b; the delta reports b only because it was in ensure.
	adapter.deltas = map[string]Delta{"c1": {Changed: []Header{metaHeader("b", storage.FlagFlagged)}, Cursor: "c2"}}

	if _, err := NewEngine(adapter, db, nil).SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(adapter.lastEnsure) != 1 || adapter.lastEnsure[0] != "b" {
		t.Fatalf("ensure = %v, want [b]", adapter.lastEnsure)
	}
	if len(adapter.flagPushes) != 1 || adapter.flagPushes[0] != "b" {
		t.Fatalf("flag pushes = %v, want [b]", adapter.flagPushes)
	}
	got, err := messageByRemote(t, db, folder.ID, "b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags != storage.FlagSeen|storage.FlagFlagged {
		t.Fatalf("flags = %v, want seen|flagged (union, server flag kept)", got.Flags)
	}
}

func TestDeltaPendingDeleteIsPushed(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, adapter)
	a, err := messageByRemote(t, db, folder.ID, "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkDeletePending(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	adapter.deltas = map[string]Delta{"c1": {Changed: []Header{metaHeader("a", 0)}, Cursor: "c2"}}

	if _, err := NewEngine(adapter, db, nil).SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(adapter.deleted) != 1 || adapter.deleted[0] != "a" {
		t.Fatalf("deleted = %v, want [a]", adapter.deleted)
	}
}

func TestDeltaStubFailureKeepsCursor(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, adapter)
	adapter.deltas = map[string]Delta{"c1": {Changed: []Header{metaHeader("c", 0), metaHeader("d", 0)}, Cursor: "c2"}}
	engine := NewEngine(adapter, db, nil)
	engine.beforeListStub = func(remoteID string) error {
		if remoteID == "d" {
			return errors.New("FOREIGN KEY constraint failed")
		}
		return nil
	}

	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err == nil {
		t.Fatal("want an error for the stub that did not store")
	}
	if got := folderToken(t, db, folder).StateToken; got != "c1" {
		t.Fatalf("cursor = %q, want c1 so the next delta reports d again", got)
	}
	engine.beforeListStub = nil
	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if has := remoteIDsPresent(t, db, folder.ID); !has["c"] || !has["d"] {
		t.Fatalf("after retry cached %v, want c and d", has)
	}
}

func TestFullListStubFailureKeepsCursor(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: []string{"b", "a"}, listMeta: true, stateToken: "c1"}
	engine := NewEngine(adapter, db, nil)
	engine.beforeListStub = func(remoteID string) error {
		if remoteID == "a" {
			return errors.New("FOREIGN KEY constraint failed")
		}
		return nil
	}
	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err == nil {
		t.Fatal("want an error for the stub that did not store")
	}
	f := folderToken(t, db, folder)
	if f.StateToken != "" {
		t.Fatalf("cursor = %q, want none saved", f.StateToken)
	}
	if !f.LastFullSyncAt.IsZero() {
		t.Fatal("a full list with a failed stub must not count as a full reconcile")
	}
}

func TestForceFullIgnoresCursorAndStampsFullSync(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, adapter)
	adapter.deltas = map[string]Delta{"c1": {Cursor: "c1", Unchanged: true}}
	changesBefore := adapter.changeCalls

	engine := NewEngine(adapter, db, nil)
	engine.ForceFull = true
	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatalf("full reconcile: %v", err)
	}
	if adapter.changeCalls != changesBefore {
		t.Fatal("ForceFull must not ask for a delta")
	}
	if folderToken(t, db, folder).LastFullSyncAt.IsZero() {
		t.Fatal("a successful full list must stamp last_full_sync_at")
	}
}

func TestFullListPagePauseOnlyForForceFull(t *testing.T) {
	for _, force := range []bool{false, true} {
		ctx := context.Background()
		db, folder := newSyncTestFolder(t)
		adapter := &pagedFakeAdapter{
			fakeAdapter: &fakeAdapter{ids: []string{"e4", "e3", "e2", "e1"}, listMeta: true, stateToken: "s1"},
			pageSize:    2,
		}
		engine := NewEngine(adapter, db, nil)
		engine.FullList = true
		engine.ForceFull = force
		engine.PauseCheck = func() bool { return true }

		_, err := engine.SyncFolderStubs(ctx, folder, 0)
		f := folderToken(t, db, folder)
		if !force {
			if err != nil {
				t.Fatalf("first full list must not yield: %v", err)
			}
			continue
		}
		if !errors.Is(err, ErrSoftPaused) {
			t.Fatalf("ForceFull err = %v, want ErrSoftPaused", err)
		}
		if f.StateToken != "" || !f.LastFullSyncAt.IsZero() {
			t.Fatalf("paused reconcile wrote cursor %q / full sync %v", f.StateToken, f.LastFullSyncAt)
		}
		if has := remoteIDsPresent(t, db, folder.ID); !has["e4"] || !has["e3"] {
			t.Fatalf("page stubs %v, want the first page kept", has)
		}
	}
}

func TestListMetaOnlyForMessagesBecomingStubs(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &metaAdapter{fakeAdapter: &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5)}}
	engine := NewEngine(adapter, db, nil)
	engine.InitialLimit = 2

	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(adapter.metaAsked) != 1 || len(adapter.metaAsked[0]) != 2 || adapter.metaAsked[0][0] != "5" || adapter.metaAsked[0][1] != "4" {
		t.Fatalf("list meta asked for %v, want [[5 4]] (only the window)", adapter.metaAsked)
	}
	if has := remoteIDsPresent(t, db, folder.ID); !has["5"] || !has["4"] || has["3"] {
		t.Fatalf("stubs %v, want 5 and 4", has)
	}
}

// rejectingAdapter is a server that refuses every flag change, like IMAP
// STORE on a read-only mailbox.
type rejectingAdapter struct{ *deltaAdapter }

func (rejectingAdapter) SetFlags(context.Context, string, string, storage.Flag) error {
	return errors.New("NO [READONLY] mailbox is read-only")
}

// vanishingAdapter removes one cached row right after the engine read its
// local snapshot, so the plan's local delete of that row fails.
type vanishingAdapter struct {
	*deltaAdapter
	t      *testing.T
	db     *storage.DB
	folder storage.Folder
	gone   string
}

func (a *vanishingAdapter) vanish(ctx context.Context) {
	a.t.Helper()
	m, err := messageByRemote(a.t, a.db, a.folder.ID, a.gone)
	if err != nil {
		a.t.Fatal(err)
	}
	if err := a.db.DeleteCachedMessage(ctx, a.folder.AccountID, m.ID); err != nil {
		a.t.Fatal(err)
	}
}

func (a *vanishingAdapter) ListChanges(ctx context.Context, box RemoteMailbox, ensure []string) (Delta, error) {
	a.vanish(ctx)
	return a.deltaAdapter.ListChanges(ctx, box, ensure)
}

func (a *vanishingAdapter) ListMessages(ctx context.Context, box RemoteMailbox) ([]Header, string, string, error) {
	a.vanish(ctx)
	return a.deltaAdapter.ListMessages(ctx, box)
}

// oldStamp backdates the folder's last full sync so a test can tell whether
// this run stamped it again.
var oldStamp = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

func markPendingSeen(t *testing.T, db *storage.DB, folder storage.Folder, remoteID string) {
	t.Helper()
	m, err := messageByRemote(t, db, folder.ID, remoteID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkFlagsPending(context.Background(), m.ID, storage.FlagSeen); err != nil {
		t.Fatal(err)
	}
}

// A server that refuses a flag push must not freeze the folder: the pending
// marker stays on the row and the next delta re-reads it through ensure.
func TestDeltaPushRejectionAdvancesCursor(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	inner := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, inner)
	markPendingSeen(t, db, folder, "a")
	inner.deltas = map[string]Delta{"c1": {Changed: []Header{metaHeader("a", 0)}, Cursor: "c2"}}

	if _, err := NewEngine(rejectingAdapter{inner}, db, nil).SyncFolderStubs(ctx, folder, 0); err == nil {
		t.Fatal("want the push rejection reported")
	}
	if got := folderToken(t, db, folder).StateToken; got != "c2" {
		t.Fatalf("cursor = %q, want c2", got)
	}
	states, err := db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !states[0].PendingFlags {
		t.Fatalf("states = %+v, want a with its pending marker kept", states)
	}
}

func TestForceFullPushRejectionStampsFullSync(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	inner := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, inner)
	if err := db.SetFolderFullSyncAt(ctx, folder.ID, oldStamp); err != nil {
		t.Fatal(err)
	}
	markPendingSeen(t, db, folder, "a")
	inner.stateToken = "c2"

	engine := NewEngine(rejectingAdapter{inner}, db, nil)
	engine.ForceFull = true
	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err == nil {
		t.Fatal("want the push rejection reported")
	}
	f := folderToken(t, db, folder)
	if f.StateToken != "c2" {
		t.Fatalf("cursor = %q, want c2", f.StateToken)
	}
	if !f.LastFullSyncAt.After(oldStamp) {
		t.Fatalf("last full sync = %v, want it stamped by this run", f.LastFullSyncAt)
	}
}

// A local write that failed (here a local delete) still holds the cursor and
// the full-sync stamp, on both paths.
func TestLocalDeleteFailureHoldsCursorAndStamp(t *testing.T) {
	for _, force := range []bool{false, true} {
		ctx := context.Background()
		db, folder := newSyncTestFolder(t)
		inner := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"b", "a"}, listMeta: true, stateToken: "c1"}}
		syncedOnce(t, db, folder, inner)
		if err := db.SetFolderFullSyncAt(ctx, folder.ID, oldStamp); err != nil {
			t.Fatal(err)
		}
		inner.ids = []string{"b"}
		inner.stateToken = "c2"
		inner.deltas = map[string]Delta{"c1": {Removed: []string{"a"}, Cursor: "c2"}}

		engine := NewEngine(&vanishingAdapter{deltaAdapter: inner, t: t, db: db, folder: folder, gone: "a"}, db, nil)
		engine.ForceFull = force
		if _, err := engine.SyncFolderStubs(ctx, folder, 0); err == nil {
			t.Fatalf("force=%v: want the failed local delete reported", force)
		}
		f := folderToken(t, db, folder)
		if f.StateToken != "c1" {
			t.Fatalf("force=%v: cursor = %q, want c1", force, f.StateToken)
		}
		if !f.LastFullSyncAt.Equal(oldStamp) {
			t.Fatalf("force=%v: last full sync = %v, want it left at %v", force, f.LastFullSyncAt, oldStamp)
		}
	}
}

// The user marks a message read while a sync is between reading its local
// snapshot and applying the server's flags. The server's older flags must not
// replace the pending local change; the next sync pushes it.
func TestAdoptDoesNotOverwriteFlagsMarkedDuringSync(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
	syncedOnce(t, db, folder, adapter)
	a, err := messageByRemote(t, db, folder.ID, "a")
	if err != nil {
		t.Fatal(err)
	}
	// Another client flagged it; the delta reports that.
	adapter.deltas = map[string]Delta{"c1": {Changed: []Header{metaHeader("a", storage.FlagFlagged)}, Cursor: "c2"}}
	adapter.onChanges = func() {
		if err := db.MarkFlagsPending(ctx, a.ID, storage.FlagSeen); err != nil {
			t.Error(err)
		}
	}

	if _, err := NewEngine(adapter, db, nil).SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := messageByRemote(t, db, folder.ID, "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags&storage.FlagSeen == 0 {
		t.Fatalf("flags = %v, the read mark made during the sync was lost", got.Flags)
	}
	states, err := db.ListMessageStates(ctx, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !states[0].PendingFlags {
		t.Fatalf("states = %+v, want the change still pending for the next push", states)
	}
}

// A flag change the user makes after the sync read its snapshot must survive
// the push and keep its pending marker, so the next sync pushes it.
func TestPushDoesNotOverwriteFlagsMarkedDuringSync(t *testing.T) {
	for _, tc := range []struct {
		name   string
		server storage.Flag // what the delta reports for the row
	}{
		{"push", storage.FlagFlagged},       // merged Seen|Flagged differs from the server: push
		{"clear pending", storage.FlagSeen}, // server already has Seen: clear pending
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: []string{"a"}, listMeta: true, stateToken: "c1"}}
			syncedOnce(t, db, folder, adapter)
			markPendingSeen(t, db, folder, "a")
			a, err := messageByRemote(t, db, folder.ID, "a")
			if err != nil {
				t.Fatal(err)
			}
			adapter.deltas = map[string]Delta{"c1": {Changed: []Header{metaHeader("a", tc.server)}, Cursor: "c2"}}
			// The user marks it unread again while the sync runs.
			adapter.onChanges = func() {
				if err := db.MarkFlagsPending(ctx, a.ID, 0); err != nil {
					t.Error(err)
				}
			}

			if _, err := NewEngine(adapter, db, nil).SyncFolderStubs(ctx, folder, 0); err != nil {
				t.Fatalf("sync: %v", err)
			}
			states, err := db.ListMessageStates(ctx, folder.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(states) != 1 || states[0].Flags != 0 || !states[0].PendingFlags {
				t.Fatalf("states = %+v, want the unread mark kept and still pending", states)
			}
		})
	}
}
