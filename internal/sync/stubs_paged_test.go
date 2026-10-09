package sync

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// pagedFakeAdapter is an adapter that hands the engine its list in
// pages. afterPage runs synchronously after each onPage call, before
// ListMessagesPaged returns, so a test can inspect the store mid-list.
type pagedFakeAdapter struct {
	*fakeAdapter
	pageSize   int
	pagedCalls int
	// failAfterPages, when > 0, makes ListMessagesPaged return listErr after
	// delivering that many pages.
	failAfterPages int
	listErr        error
	afterPage      func(n int)
}

func (a *pagedFakeAdapter) ListMessagesPaged(ctx context.Context, box RemoteMailbox, onPage func([]Header) error) ([]Header, string, string, error) {
	a.pagedCalls++
	headers, token, _, err := a.fakeAdapter.ListMessages(ctx, box)
	if err != nil {
		return nil, "", "", err
	}
	pages := 0
	for start := 0; start < len(headers); start += a.pageSize {
		end := min(start+a.pageSize, len(headers))
		if err := onPage(headers[start:end]); err != nil {
			return nil, "", "", err
		}
		pages++
		if a.afterPage != nil {
			a.afterPage(pages)
		}
		if a.failAfterPages > 0 && pages >= a.failAfterPages {
			return nil, "", "", a.listErr
		}
	}
	// a paged adapter may report no generation.
	return headers, token, "", nil
}

func seedLocalStub(t *testing.T, db *storage.DB, folder storage.Folder, remoteID string) {
	t.Helper()
	if _, err := db.UpsertMessageListMeta(context.Background(), &storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, RemoteID: remoteID, Subject: "local-" + remoteID,
	}); err != nil {
		t.Fatalf("seed %q: %v", remoteID, err)
	}
}

func sortedPresent(t *testing.T, db *storage.DB, folderID int64) []string {
	t.Helper()
	var out []string
	for id := range remoteIDsPresent(t, db, folderID) {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// A full paged list must store stubs as each page arrives, not only after the
// whole mailbox is listed. Reconcile still runs once at the end and owns
// deletes.
func TestSyncFolderStubsPagedStoresStubsPerPage(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedLocalStub(t, db, folder, "gone")

	adapter := &pagedFakeAdapter{
		fakeAdapter: &fakeAdapter{ids: []string{"e6", "e5", "e4", "e3", "e2", "e1"}, listMeta: true, stateToken: "s1"},
		pageSize:    2,
	}
	engine := NewEngine(adapter, db, nil)
	engine.FullList = true
	var announced [][]int64
	engine.OnStored = func(_ storage.Folder, ids []int64) { announced = append(announced, ids) }

	var afterPage1 []string
	adapter.afterPage = func(n int) {
		if n == 1 {
			afterPage1 = sortedPresent(t, db, folder.ID)
		}
	}

	res, err := engine.SyncFolderStubs(ctx, folder, 0)
	if err != nil {
		t.Fatalf("SyncFolderStubs: %v", err)
	}
	if adapter.pagedCalls != 1 || adapter.listCalls != 1 {
		t.Fatalf("paged calls=%d list calls=%d, want one paged list", adapter.pagedCalls, adapter.listCalls)
	}
	if want := []string{"e5", "e6", "gone"}; !slices.Equal(afterPage1, want) {
		t.Fatalf("after page 1 cached %v, want %v (page stubs stored, nothing deleted yet)", afterPage1, want)
	}
	if got, want := sortedPresent(t, db, folder.ID), []string{"e1", "e2", "e3", "e4", "e5", "e6"}; !slices.Equal(got, want) {
		t.Fatalf("after list cached %v, want %v (reconcile deletes local-only row)", got, want)
	}
	if res.Deleted != 1 {
		t.Fatalf("Deleted=%d, want 1", res.Deleted)
	}
	if len(announced) != 3 {
		t.Fatalf("announces=%d (%v), want one per page", len(announced), announced)
	}
	for i, ids := range announced {
		if len(ids) != 2 {
			t.Fatalf("announce %d ids=%v, want 2", i, ids)
		}
	}
	if want := []string{"e6", "e5", "e4", "e3", "e2", "e1"}; !slices.Equal(res.ToFetch, want) {
		t.Fatalf("ToFetch=%v, want %v newest first", res.ToFetch, want)
	}
	got, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StateToken != "s1" {
		t.Fatalf("state token %q, want s1", got.StateToken)
	}
}

// Review Focus 2: page-1 stubs stored, then the full list fails. The stubs are
// real server mail and stay; nothing is deleted on a partial snapshot and the
// cursor does not advance. The next successful list deletes what the server
// no longer has.
func TestSyncFolderStubsPagedFailureKeepsStubs(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedLocalStub(t, db, folder, "gone")

	listErr := errors.New("Email/get chunk 2 failed")
	adapter := &pagedFakeAdapter{
		fakeAdapter:    &fakeAdapter{ids: []string{"e4", "e3", "e2", "e1"}, listMeta: true, stateToken: "s1"},
		pageSize:       2,
		failAfterPages: 1,
		listErr:        listErr,
	}
	engine := NewEngine(adapter, db, nil)
	engine.FullList = true

	_, err := engine.SyncFolderStubs(ctx, folder, 0)
	if !errors.Is(err, listErr) {
		t.Fatalf("err=%v, want list error", err)
	}
	if got, want := sortedPresent(t, db, folder.ID), []string{"e3", "e4", "gone"}; !slices.Equal(got, want) {
		t.Fatalf("after failed list cached %v, want %v", got, want)
	}
	f, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.StateToken != "" {
		t.Fatalf("state token %q advanced on a failed list", f.StateToken)
	}

	// e3 was deleted on the server meanwhile.
	adapter.failAfterPages = 0
	adapter.ids = []string{"e4", "e2", "e1"}
	adapter.stateToken = "s2"
	res, err := engine.SyncFolderStubs(ctx, folder, 0)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got, want := sortedPresent(t, db, folder.ID), []string{"e1", "e2", "e4"}; !slices.Equal(got, want) {
		t.Fatalf("after retry cached %v, want %v (deleted mail must not stay)", got, want)
	}
	if res.Deleted != 2 {
		t.Fatalf("Deleted=%d, want 2 (e3 and gone)", res.Deleted)
	}
}

// Without FullList (IMAP-style windows) the engine must not
// page-insert: rows below the folder's sync floor would land early.
func TestSyncFolderStubsPagedOnlyWithFullList(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &pagedFakeAdapter{
		fakeAdapter: &fakeAdapter{ids: fakeIDs(1, 2, 3), listMeta: true},
		pageSize:    1,
	}
	engine := NewEngine(adapter, db, nil)
	engine.InitialLimit = 1

	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatal(err)
	}
	if adapter.pagedCalls != 0 {
		t.Fatalf("paged calls=%d, want 0 without FullList", adapter.pagedCalls)
	}
	if got := sortedPresent(t, db, folder.ID); !slices.Equal(got, []string{"3"}) {
		t.Fatalf("cached %v, want only the newest above the floor", got)
	}
}

// Scroll backfill can run with FullList set, but a backfill lowers the floor
// by one page instead of clearing it. Page inserts there would store rows
// below the new floor, out of step with the stored floor and HasOlder.
func TestSyncFolderStubsBackfillDoesNotPageInsert(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &pagedFakeAdapter{
		fakeAdapter: &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5), listMeta: true},
		pageSize:    1,
	}
	engine := NewEngine(adapter, db, nil)
	engine.InitialLimit = 2
	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatal(err)
	}

	engine.FullList = true
	res, err := engine.SyncFolderStubs(ctx, folder, 1)
	if err != nil {
		t.Fatal(err)
	}
	if adapter.pagedCalls != 0 {
		t.Fatalf("paged calls=%d, want 0 for a backfill", adapter.pagedCalls)
	}
	if got := sortedPresent(t, db, folder.ID); !slices.Equal(got, []string{"3", "4", "5"}) {
		t.Fatalf("cached %v, want 3,4,5 (one page below the old floor)", got)
	}
	if !res.HasOlder {
		t.Fatal("HasOlder=false, want true with 1 and 2 still on the server")
	}
}

// A page that repeats ids already cached must not rewrite their rows: page
// inserts are insert-only, flags stay reconcile's job.
func TestSyncFolderStubsPagedSkipsKnownIDs(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedLocalStub(t, db, folder, "e2")

	adapter := &pagedFakeAdapter{
		fakeAdapter: &fakeAdapter{ids: []string{"e2", "e1"}, listMeta: true},
		pageSize:    2,
	}
	engine := NewEngine(adapter, db, nil)
	engine.FullList = true
	var subjectDuringList string
	adapter.afterPage = func(int) {
		m, err := messageByRemote(t, db, folder.ID, "e2")
		if err == nil && m != nil {
			subjectDuringList = m.Subject
		}
	}
	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatal(err)
	}
	if subjectDuringList != "local-e2" {
		t.Fatalf("known row subject during list %q, want untouched local-e2", subjectDuringList)
	}
}
