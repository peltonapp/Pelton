package sync

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func serversUpTo(n uint32) []ServerMessage {
	out := make([]ServerMessage, 0, n)
	for uid := n; uid >= 1; uid-- {
		out = append(out, ServerMessage{RemoteID: strconv.FormatUint(uint64(uid), 10)})
	}
	return out
}

func TestFloorForLimit(t *testing.T) {
	tests := []struct {
		name    string
		servers []ServerMessage
		limit   int
		want    string
	}{
		{name: "no cap", servers: serversUpTo(100), limit: 0, want: ""},
		{name: "negative cap is no cap", servers: serversUpTo(100), limit: -1, want: ""},
		{name: "folder smaller than the cap", servers: serversUpTo(10), limit: 50, want: ""},
		{name: "folder exactly at the cap", servers: serversUpTo(50), limit: 50, want: ""},
		// newest-first 100..1; the 50 newest end at 51, so the floor is "51".
		{name: "folder larger than the cap", servers: serversUpTo(100), limit: 50, want: "51"},
		{name: "empty folder", servers: nil, limit: 50, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := floorForLimit(tt.servers, tt.limit); got != tt.want {
				t.Errorf("floorForLimit(limit=%d) = %q, want %q", tt.limit, got, tt.want)
			}
		})
	}
}

// the floor must count messages, not uid distance: imap uids are not contiguous
// once anything has ever been deleted, so arithmetic on the highest uid would
// admit the wrong number of messages.
func TestFloorForLimitWithGappyUIDs(t *testing.T) {
	servers := []ServerMessage{
		{RemoteID: "4000"}, {RemoteID: "900"}, {RemoteID: "77"}, {RemoteID: "12"}, {RemoteID: "3"},
	}
	// the 2 newest are 4000 and 900, so the floor is 900.
	if got := floorForLimit(servers, 2); got != "900" {
		t.Fatalf("floorForLimit = %q, want %q", got, "900")
	}
}

func TestLowerFloor(t *testing.T) {
	servers := serversUpTo(100)
	tests := []struct {
		name    string
		current string
		batch   int
		want    string
	}{
		{name: "no floor stays no floor", current: "", batch: 50, want: ""},
		// below 51 are 50..1; admitting 20 more puts the floor at 31.
		{name: "admits a batch", current: "51", batch: 20, want: "31"},
		// exactly as many left as the batch: the rest all fit, so no floor remains.
		{name: "last batch clears the floor", current: "51", batch: 50, want: ""},
		{name: "batch larger than the remainder clears it", current: "51", batch: 500, want: ""},
		{name: "unlimited batch clears it", current: "51", batch: 0, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lowerFloor(servers, tt.current, tt.batch); got != tt.want {
				t.Errorf("lowerFloor(current=%q, batch=%d) = %q, want %q",
					tt.current, tt.batch, got, tt.want)
			}
		})
	}
}

func TestNormalizeFloor(t *testing.T) {
	// nothing below the floor is left on the server, so it holds nothing back.
	if got := normalizeFloor(serversUpTo(100), "1"); got != "" {
		t.Errorf("normalizeFloor with nothing below = %q, want %q", got, "")
	}
	if got := normalizeFloor(serversUpTo(100), "51"); got != "51" {
		t.Errorf("normalizeFloor with messages below = %q, want %q", got, "51")
	}
	if got := normalizeFloor(nil, ""); got != "" {
		t.Errorf("normalizeFloor of no floor = %q, want %q", got, "")
	}
}

func TestWindowFloor(t *testing.T) {
	servers := serversUpTo(100)
	tests := []struct {
		name     string
		state    FolderSyncState
		cached   int
		backfill int
		limit    int
		want     string
	}{
		{
			name:  "first sync caps a large folder",
			limit: 50,
			want:  "51",
		},
		{
			// a folder cached by a version that had no window must stay uncapped:
			// capping it now would make already-cached mail look out of window.
			name:   "existing full cache is left alone",
			state:  FolderSyncState{SyncInitialized: true, LastSeenUID: 100},
			cached: 100,
			limit:  50,
			want:   "",
		},
		{
			// migration 0037 leaves sync_initialized=0 on existing folders; local
			// messages mean this is not a first sync and must not re-floor.
			name:   "upgraded folder with local messages is not re-floored",
			state:  FolderSyncState{SyncInitialized: false, LastSeenUID: 100},
			cached: 100,
			limit:  50,
			want:   "",
		},
		{
			name:  "upgraded folder with last_seen_uid only is not re-floored",
			state: FolderSyncState{SyncInitialized: false, LastSeenUID: 100},
			limit: 50,
			want:  "",
		},
		{
			name:  "upgraded folder with sync_floor_uid only is not re-floored",
			state: FolderSyncState{SyncInitialized: false, SyncFloorUID: 51, SyncFloorID: "51"},
			limit: 50,
			want:  "51",
		},
		{
			name:   "established floor is kept",
			state:  FolderSyncState{SyncInitialized: true, LastSeenUID: 100, SyncFloorID: "51"},
			cached: 50,
			limit:  50,
			want:   "51",
		},
		{
			name:     "backfill lowers the floor",
			state:    FolderSyncState{SyncInitialized: true, LastSeenUID: 100, SyncFloorID: "51"},
			cached:   50,
			backfill: 20,
			limit:    50,
			want:     "31",
		},
		{
			// every message the folder ever had was deleted on the server, so there
			// is nothing to hold back and the ui must stop offering "load older".
			name:   "floor with nothing below it is cleared",
			state:  FolderSyncState{SyncInitialized: true, LastSeenUID: 100, SyncFloorID: "1"},
			cached: 100,
			limit:  50,
			want:   "",
		},
		{
			name:  "no cap configured means no floor",
			limit: 0,
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engine{InitialLimit: tt.limit}
			if got := e.windowFloor(tt.state, servers, tt.cached, tt.backfill); got != tt.want {
				t.Errorf("windowFloor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPlanRespectsFloor(t *testing.T) {
	servers := []ServerMessage{
		*server("5", 0), *server("4", 0), *server("3", 0), *server("2", 0), *server("1", 0),
	}

	plan := BuildPlan(nil, servers, "3")

	if len(plan) != 3 {
		t.Fatalf("plan length = %d, want 3 (ids below the floor must not be fetched)", len(plan))
	}
	wantIDs := []string{"5", "4", "3"}
	for i, d := range plan {
		if d.RemoteID != wantIDs[i] {
			t.Errorf("plan[%d].RemoteID = %q, want %q", i, d.RemoteID, wantIDs[i])
		}
		if d.Action != ActionFetchNew {
			t.Errorf("id %q action = %v, want ActionFetchNew", d.RemoteID, d.Action)
		}
	}

	// a cached message below the floor still reconciles.
	locals := []LocalMessage{*local("1", 0, false, false)}
	plan = BuildPlan(locals, servers, "3")
	foundCached := false
	for _, d := range plan {
		if d.RemoteID == "1" {
			foundCached = true
			if d.Action != ActionNone {
				t.Errorf("cached id below floor action = %v, want ActionNone", d.Action)
			}
		}
		if d.RemoteID == "2" {
			t.Errorf("uncached id %q below the floor was planned", d.RemoteID)
		}
	}
	if !foundCached {
		t.Fatal("cached id below the floor was not planned")
	}
}

// a cached message below the floor still reconciles: it is mail the user
// already has, so its flags must keep syncing and a server-side delete must
// still remove it. Skipping it because of the floor would freeze it forever.
func TestBuildPlanKeepsCachedMessagesBelowFloor(t *testing.T) {
	locals := []LocalMessage{
		*local("1", 0, false, false),
		*local("2", storage.FlagSeen, false, false),
	}
	servers := []ServerMessage{*server("1", storage.FlagSeen)}

	plan := BuildPlan(locals, servers, "50")

	if len(plan) != 2 {
		t.Fatalf("plan length = %d, want 2", len(plan))
	}
	if plan[0].RemoteID != "1" || plan[0].Action != ActionAdoptServerFlags {
		t.Errorf("id 1 = %v, want ActionAdoptServerFlags", plan[0].Action)
	}
	if plan[1].RemoteID != "2" || plan[1].Action != ActionDeleteLocal {
		t.Errorf("id 2 = %v, want ActionDeleteLocal", plan[1].Action)
	}
}

// fakeAdapter serves a fixed set of remote ids and records fetch / push traffic.
type fakeAdapter struct {
	ids            []string
	fetched        []string
	generation     string
	deleted        []string
	moved          []string
	movedTo        string
	flagPushes     []string
	commands       int
	fetchErr       error
	fetchFailAfter int // when > 0 with fetchErr, return successful Fetched for the first N then error
	// listMeta, when true, fills Header envelope fields in ListMessages.
	listMeta bool
	// stateToken is returned from ListMessages. Empty mimics IMAP without CONDSTORE.
	stateToken string
	// onFetch, when set, runs for each remote id before it is appended to fetched.
	onFetch func(id string)
	// listCalls counts ListMessages invocations (tests).
	listCalls int
}

func (a *fakeAdapter) Addr() string { return "imap.example.com:993" }

func (a *fakeAdapter) ListMailboxes(context.Context) ([]Mailbox, error) {
	gen := a.generation
	if gen == "" {
		gen = "1"
	}
	return []Mailbox{{RemoteID: "INBOX", Name: "INBOX", Generation: gen, Selectable: true}}, nil
}

func (a *fakeAdapter) ListMessages(_ context.Context, _ RemoteMailbox) ([]Header, string, string, error) {
	a.listCalls++
	ids := append([]string(nil), a.ids...)
	if allUint32IDs(ids) {
		sort.Slice(ids, func(i, j int) bool {
			ni, _ := strconv.ParseUint(ids[i], 10, 32)
			nj, _ := strconv.ParseUint(ids[j], 10, 32)
			return ni > nj
		})
	}
	out := make([]Header, 0, len(ids))
	for _, id := range ids {
		h := Header{RemoteID: id}
		if n, err := strconv.ParseUint(id, 10, 32); err == nil {
			h.LegacyUID = uint32(n)
		}
		if a.listMeta {
			h.HasListMeta = true
			h.Subject = "subj-" + id
			h.From = "ada@ex"
			h.FromName = "Ada"
			h.Preview = "preview-" + id
		}
		out = append(out, h)
	}
	gen := a.generation
	if gen == "" {
		gen = "1"
	}
	return out, a.stateToken, gen, nil
}

func (a *fakeAdapter) Fetch(ctx context.Context, _ string, remoteIDs []string) ([]Fetched, error) {
	a.commands++
	out := make([]Fetched, 0, len(remoteIDs))
	for i, id := range remoteIDs {
		if a.fetchErr != nil && a.fetchFailAfter > 0 && i >= a.fetchFailAfter {
			return out, a.fetchErr
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if a.onFetch != nil {
			a.onFetch(id)
		}
		a.fetched = append(a.fetched, id)
		f := Fetched{RemoteID: id, Subject: "test"}
		if n, err := strconv.ParseUint(id, 10, 32); err == nil {
			f.LegacyUID = uint32(n)
		}
		out = append(out, f)
	}
	if a.fetchErr != nil && a.fetchFailAfter == 0 {
		return out, a.fetchErr
	}
	return out, nil
}

func (a *fakeAdapter) SetFlags(_ context.Context, _, remoteID string, _ storage.Flag) error {
	a.flagPushes = append(a.flagPushes, remoteID)
	return nil
}

func (a *fakeAdapter) Move(_ context.Context, _ string, remoteIDs []string, destMailboxID string) error {
	a.moved = append(a.moved, remoteIDs...)
	a.movedTo = destMailboxID
	return nil
}

func (a *fakeAdapter) Delete(_ context.Context, _ string, remoteIDs []string) error {
	a.deleted = append(a.deleted, remoteIDs...)
	return nil
}

func (a *fakeAdapter) CreateMailbox(context.Context, string, string) (Mailbox, error) {
	return Mailbox{}, nil
}
func (a *fakeAdapter) RenameMailbox(context.Context, string, string) error { return nil }
func (a *fakeAdapter) DeleteMailbox(context.Context, string) error         { return nil }

func allUint32IDs(ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if _, err := strconv.ParseUint(id, 10, 32); err != nil {
			return false
		}
	}
	return true
}

func fakeIDs(uids ...uint32) []string {
	out := make([]string, len(uids))
	for i, uid := range uids {
		out[i] = strconv.FormatUint(uint64(uid), 10)
	}
	return out
}

func newSyncTestFolder(t *testing.T) (*storage.DB, storage.Folder) {
	t.Helper()
	ctx := context.Background()

	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	return db, folder
}

// the reported bug: a first sync of a big mailbox downloaded the oldest message
// first and the user waited hours for recent mail. Newest uid must come first.
func TestSyncFolderFetchesNewestFirst(t *testing.T) {
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5)}
	engine := NewEngine(adapter, db, nil)

	if _, err := engine.SyncFolder(context.Background(), folder); err != nil {
		t.Fatalf("sync: %v", err)
	}

	want := []string{"5", "4", "3", "2", "1"}
	if len(adapter.fetched) != len(want) {
		t.Fatalf("fetched %v, want %v", adapter.fetched, want)
	}
	for i, id := range want {
		if adapter.fetched[i] != id {
			t.Fatalf("fetch order = %v, want %v (newest first)", adapter.fetched, want)
		}
	}
}

func TestSyncFolderCapsFirstSyncAndBackfills(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)}
	engine := NewEngine(adapter, db, nil)
	engine.InitialLimit = 3

	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.New != 3 {
		t.Fatalf("first sync fetched %d, want 3", res.New)
	}
	if !res.HasOlder {
		t.Fatal("first sync of a capped folder should report older messages")
	}
	if got := adapter.fetched; got[0] != "10" || got[2] != "8" {
		t.Fatalf("first sync fetched %v, want the 3 newest newest-first", got)
	}

	// a second ordinary sync must not re-fetch the held-back messages, and must
	// not treat them as new mail either.
	adapter.fetched = nil
	res, err = engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if res.New != 0 || len(adapter.fetched) != 0 {
		t.Fatalf("resync fetched %v (new=%d), want nothing", adapter.fetched, res.New)
	}

	// backfilling admits the next batch, again newest first.
	adapter.fetched = nil
	res, err = engine.BackfillFolder(ctx, folder, 4)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.New != 4 {
		t.Fatalf("backfill fetched %d, want 4", res.New)
	}
	if got := adapter.fetched; len(got) != 4 || got[0] != "7" || got[3] != "4" {
		t.Fatalf("backfill fetched %v, want 7,6,5,4", got)
	}
	if !res.HasOlder {
		t.Fatal("three messages still remain below the window")
	}

	// the final backfill clears the floor and reports nothing older left.
	adapter.fetched = nil
	res, err = engine.BackfillFolder(ctx, folder, 4)
	if err != nil {
		t.Fatalf("final backfill: %v", err)
	}
	if res.New != 3 {
		t.Fatalf("final backfill fetched %d, want the remaining 3", res.New)
	}
	if res.HasOlder {
		t.Fatal("folder is fully cached, HasOlder should be false")
	}

	floor, err := db.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		t.Fatalf("read floor: %v", err)
	}
	if floor != 0 {
		t.Fatalf("floor = %d, want 0 once the folder is fully cached", floor)
	}
}

// a generation reset drops the cache, so the folder is a first sync again and
// its floor must be recomputed rather than left pointing at ids from the old
// generation. The stale floor has to actually reach the database, not just the
// in-memory state.
func TestSyncFolderResetsFloorOnUIDValidityChange(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)}
	engine := NewEngine(adapter, db, nil)
	engine.InitialLimit = 3

	if _, err := engine.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	floor, err := db.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		t.Fatalf("read floor: %v", err)
	}
	if floor != 8 {
		t.Fatalf("floor after first sync = %d, want 8", floor)
	}

	// the server resets the mailbox and hands out a small id set again. the old
	// floor of 8 would sit above every new id and hide the whole folder.
	adapter.ids = fakeIDs(1, 2)
	adapter.generation = "2"
	// re-read the row so the sync sees the generation the first sync stored,
	// the way a real caller listing folders would.
	fresh, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatalf("reload folder: %v", err)
	}
	uncapped := NewEngine(adapter, db, nil)
	res, err := uncapped.SyncFolder(ctx, *fresh)
	if err != nil {
		t.Fatalf("resync after generation change: %v", err)
	}
	if !res.GenerationReset {
		t.Fatal("expected a generation reset")
	}
	if res.New != 2 {
		t.Fatalf("refetched %d, want both messages", res.New)
	}
	floor, err = db.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		t.Fatalf("read floor: %v", err)
	}
	if floor != 0 {
		t.Fatalf("floor after reset = %d, want 0", floor)
	}
}

// a folder already cached in full by a version without the sync window must not
// be capped retroactively: its messages are all in the cache, and a floor would
// make the ui claim there is older mail to fetch when there is not.
func TestSyncFolderDoesNotCapAnExistingCache(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)}

	full := NewEngine(adapter, db, nil)
	if _, err := full.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial full sync: %v", err)
	}

	capped := NewEngine(adapter, db, nil)
	capped.InitialLimit = 3
	res, err := capped.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if res.HasOlder {
		t.Fatal("a fully cached folder must not gain a sync floor")
	}
	floor, err := db.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		t.Fatalf("read floor: %v", err)
	}
	if floor != 0 {
		t.Fatalf("floor = %d, want 0", floor)
	}
}

// After migration 0037, existing folders keep sync_initialized=0. A folder that
// already has local messages must not be treated as a first sync and re-capped
// by InitialLimit.
func TestSyncFolderDoesNotReFloorUpgradedFolder(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)

	for uid := uint32(1); uid <= 10; uid++ {
		id := strconv.FormatUint(uint64(uid), 10)
		if _, err := db.InsertMessage(ctx, &storage.Message{
			AccountID: folder.AccountID,
			FolderID:  folder.ID,
			UID:       uid,
			RemoteID:  id,
			Subject:   "seed",
		}); err != nil {
			t.Fatalf("seed message %s: %v", id, err)
		}
	}
	if err := db.SetFolderLastSeenUID(ctx, folder.ID, 10); err != nil {
		t.Fatalf("set last_seen_uid: %v", err)
	}
	fresh, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatalf("reload folder: %v", err)
	}
	if fresh.SyncInitialized {
		t.Fatal("seeded folder should have sync_initialized=0 (post-migration default)")
	}

	adapter := &fakeAdapter{ids: fakeIDs(1, 2, 3, 4, 5, 6, 7, 8, 9, 10)}
	engine := NewEngine(adapter, db, nil)
	engine.InitialLimit = 3

	res, err := engine.SyncFolder(ctx, *fresh)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.HasOlder {
		t.Fatal("upgraded folder with existing messages must not gain an InitialLimit floor")
	}
	if len(adapter.fetched) != 0 {
		t.Fatalf("fetched %v, want nothing (all messages already local)", adapter.fetched)
	}
	floor, err := db.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		t.Fatalf("read floor: %v", err)
	}
	if floor != 0 {
		t.Fatalf("floor = %d, want 0", floor)
	}
	after, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatalf("reload after sync: %v", err)
	}
	if !after.SyncInitialized {
		t.Fatal("sync should mark the upgraded folder initialized")
	}
}

// A push can cap Inbox at X headers and store a state token before the stub
// campaign runs. FullList must not ask for changes since that token, or the
// folder never grows beyond the push window while other mailboxes list fully.
func TestSyncFolderStubsFullListIgnoresStateToken(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)

	for _, uid := range []uint32{105, 104} {
		id := strconv.FormatUint(uint64(uid), 10)
		if _, err := db.InsertMessage(ctx, &storage.Message{
			AccountID: folder.AccountID,
			FolderID:  folder.ID,
			UID:       uid,
			RemoteID:  id,
			Subject:   "push",
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	if err := db.SetFolderStateToken(ctx, folder.ID, "s-push"); err != nil {
		t.Fatalf("set state token: %v", err)
	}
	if err := db.SetFolderSyncWindow(ctx, folder.ID, "104", true); err != nil {
		t.Fatalf("set sync window: %v", err)
	}

	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: fakeIDs(105, 104, 103, 102, 101), listMeta: true, stateToken: "s-after-full"}, deltas: map[string]Delta{"s-push": {Changed: []Header{metaHeader("105", 0), metaHeader("104", 0)}, Cursor: "s-push"}}}
	engine := NewEngine(adapter, db, nil)
	engine.FullList = true
	engine.InitialLimit = 0

	res, err := engine.SyncFolderStubs(ctx, folder, 0)
	if err != nil {
		t.Fatalf("SyncFolderStubs: %v", err)
	}
	if res.HasOlder {
		t.Fatal("full stub list should clear the floor")
	}
	msgs, err := db.ListMessages(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("cached %d stubs, want 5 (full mailbox)", len(msgs))
	}
	opened, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatalf("reload folder: %v", err)
	}
	if opened.SyncFloorID != "" {
		t.Fatalf("floor = %q, want empty", opened.SyncFloorID)
	}
	if opened.StateToken != "s-after-full" {
		t.Fatalf("state token = %q, want s-after-full from full list", opened.StateToken)
	}
	if adapter.changeCalls != 0 {
		t.Fatalf("ListChanges called %d times, want 0 on a floored FullList folder", adapter.changeCalls)
	}
}

// A folder whose stub list is already complete must not be re-listed from scratch
// on the next FullList pass; incremental Email/changes is enough.
func TestSyncFolderStubsFullListKeepsTokenWhenComplete(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)

	if err := db.SetFolderStateToken(ctx, folder.ID, "s-done"); err != nil {
		t.Fatalf("set token: %v", err)
	}
	if err := db.SetFolderSyncWindow(ctx, folder.ID, "", true); err != nil {
		t.Fatalf("set window: %v", err)
	}
	for _, uid := range []uint32{3, 2, 1} {
		id := strconv.FormatUint(uint64(uid), 10)
		if _, err := db.InsertMessage(ctx, &storage.Message{
			AccountID: folder.AccountID,
			FolderID:  folder.ID,
			UID:       uid,
			RemoteID:  id,
			Subject:   "cached",
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	adapter := &deltaAdapter{fakeAdapter: &fakeAdapter{ids: fakeIDs(3, 2, 1), stateToken: "s-done"}, deltas: map[string]Delta{"s-done": {Cursor: "s-done", Unchanged: true}}}
	engine := NewEngine(adapter, db, nil)
	engine.FullList = true

	if _, err := engine.SyncFolderStubs(ctx, folder, 0); err != nil {
		t.Fatalf("SyncFolderStubs: %v", err)
	}
	if adapter.changeCalls != 1 || adapter.listCalls != 0 {
		t.Fatalf("changeCalls=%d listCalls=%d, want 1 and 0", adapter.changeCalls, adapter.listCalls)
	}
}

// deltaAdapter is fakeAdapter plus DeltaLister. deltas maps a stored cursor to
// what ListChanges returns; a cursor with no entry gets ErrNeedFullList.
type deltaAdapter struct {
	*fakeAdapter
	deltas      map[string]Delta
	changesErr  error
	changeCalls int
	lastEnsure  []string
	// onChanges, when set, runs inside ListChanges after the engine has read
	// its local snapshot, so a test can change a row mid-sync.
	onChanges func()
}

func (a *deltaAdapter) ListChanges(_ context.Context, box RemoteMailbox, ensure []string) (Delta, error) {
	a.changeCalls++
	a.lastEnsure = append([]string(nil), ensure...)
	if a.onChanges != nil {
		a.onChanges()
	}
	if a.changesErr != nil {
		return Delta{}, a.changesErr
	}
	d, ok := a.deltas[box.StateToken]
	if !ok {
		return Delta{}, ErrNeedFullList
	}
	return d, nil
}

// metaHeader is a header with list metadata.
func metaHeader(id string, flags storage.Flag) Header {
	h := Header{RemoteID: id, Flags: flags, HasListMeta: true, Subject: "subj-" + id, From: "ada@ex", Preview: "preview-" + id}
	if n, err := strconv.ParseUint(id, 10, 32); err == nil {
		h.LegacyUID = uint32(n)
	}
	return h
}

// metaAdapter is fakeAdapter plus ListMetaFetcher; it records which ids were asked for.
type metaAdapter struct {
	*fakeAdapter
	metaAsked [][]string
}

func (a *metaAdapter) FetchListMeta(_ context.Context, _ RemoteMailbox, ids []string) ([]Header, error) {
	a.metaAsked = append(a.metaAsked, append([]string(nil), ids...))
	out := make([]Header, 0, len(ids))
	for _, id := range ids {
		out = append(out, metaHeader(id, 0))
	}
	return out, nil
}
