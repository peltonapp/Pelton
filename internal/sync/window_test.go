package sync

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func serversUpTo(n uint32) []ServerMessage {
	out := make([]ServerMessage, 0, n)
	for uid := uint32(1); uid <= n; uid++ {
		out = append(out, ServerMessage{UID: uid})
	}
	return out
}

func TestFloorForLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		servers []ServerMessage
		limit   int
		want    uint32
	}{
		{name: "no cap", servers: serversUpTo(100), limit: 0, want: 0},
		{name: "negative cap is no cap", servers: serversUpTo(100), limit: -1, want: 0},
		{name: "folder smaller than the cap", servers: serversUpTo(10), limit: 50, want: 0},
		{name: "folder exactly at the cap", servers: serversUpTo(50), limit: 50, want: 0},
		// uids 51..100 are the 50 newest, so the floor is 51.
		{name: "folder larger than the cap", servers: serversUpTo(100), limit: 50, want: 51},
		{name: "empty folder", servers: nil, limit: 50, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := floorForLimit(tt.servers, tt.limit); got != tt.want {
				t.Errorf("floorForLimit(limit=%d) = %d, want %d", tt.limit, got, tt.want)
			}
		})
	}
}

// the floor must count messages, not uid distance: imap uids are not contiguous
// once anything has ever been deleted, so arithmetic on the highest uid would
// admit the wrong number of messages.
func TestFloorForLimitWithGappyUIDs(t *testing.T) {
	t.Parallel()
	servers := []ServerMessage{{UID: 3}, {UID: 900}, {UID: 12}, {UID: 4000}, {UID: 77}}
	// the 2 newest are 4000 and 900, so the floor is 900.
	if got := floorForLimit(servers, 2); got != 900 {
		t.Fatalf("floorForLimit = %d, want 900", got)
	}
}

func TestLowerFloor(t *testing.T) {
	t.Parallel()
	servers := serversUpTo(100)
	tests := []struct {
		name    string
		current uint32
		batch   int
		want    uint32
	}{
		{name: "no floor stays no floor", current: 0, batch: 50, want: 0},
		// below 51 are uids 1..50; admitting 20 more puts the floor at 31.
		{name: "admits a batch", current: 51, batch: 20, want: 31},
		// exactly as many left as the batch: the rest all fit, so no floor remains.
		{name: "last batch clears the floor", current: 51, batch: 50, want: 0},
		{name: "batch larger than the remainder clears it", current: 51, batch: 500, want: 0},
		{name: "unlimited batch clears it", current: 51, batch: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lowerFloor(servers, tt.current, tt.batch); got != tt.want {
				t.Errorf("lowerFloor(current=%d, batch=%d) = %d, want %d",
					tt.current, tt.batch, got, tt.want)
			}
		})
	}
}

func TestNormalizeFloor(t *testing.T) {
	t.Parallel()
	// nothing below the floor is left on the server, so it holds nothing back.
	if got := normalizeFloor(serversUpTo(100), 1); got != 0 {
		t.Errorf("normalizeFloor with nothing below = %d, want 0", got)
	}
	if got := normalizeFloor(serversUpTo(100), 51); got != 51 {
		t.Errorf("normalizeFloor with messages below = %d, want 51", got)
	}
	if got := normalizeFloor(nil, 0); got != 0 {
		t.Errorf("normalizeFloor of no floor = %d, want 0", got)
	}
}

func TestWindowFloor(t *testing.T) {
	t.Parallel()
	servers := serversUpTo(100)
	tests := []struct {
		name     string
		state    FolderSyncState
		cached   int
		backfill int
		limit    int
		want     uint32
	}{
		{
			name:  "first sync caps a large folder",
			limit: 50,
			want:  51,
		},
		{
			// a folder cached by a version that had no window must stay uncapped:
			// capping it now would make already-cached mail look out of window.
			name:   "existing full cache is left alone",
			state:  FolderSyncState{LastSeenUID: 100},
			cached: 100,
			limit:  50,
			want:   0,
		},
		{
			name:   "established floor is kept",
			state:  FolderSyncState{LastSeenUID: 100, SyncFloorUID: 51},
			cached: 50,
			limit:  50,
			want:   51,
		},
		{
			name:     "backfill lowers the floor",
			state:    FolderSyncState{LastSeenUID: 100, SyncFloorUID: 51},
			cached:   50,
			backfill: 20,
			limit:    50,
			want:     31,
		},
		{
			// every message the folder ever had was deleted on the server, so there
			// is nothing to hold back and the ui must stop offering "load older".
			name:   "floor with nothing below it is cleared",
			state:  FolderSyncState{LastSeenUID: 100, SyncFloorUID: 1},
			cached: 100,
			limit:  50,
			want:   0,
		},
		{
			name:  "no cap configured means no floor",
			limit: 0,
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engine{InitialLimit: tt.limit}
			if got := e.windowFloor(tt.state, servers, tt.cached, tt.backfill); got != tt.want {
				t.Errorf("windowFloor = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBuildPlanRespectsFloor(t *testing.T) {
	t.Parallel()
	servers := []ServerMessage{*server(1, 0), *server(2, 0), *server(3, 0), *server(4, 0)}

	plan := BuildPlan(nil, servers, 3)

	if len(plan) != 2 {
		t.Fatalf("plan length = %d, want 2 (uids below the floor must not be fetched)", len(plan))
	}
	for _, d := range plan {
		if d.UID < 3 {
			t.Errorf("planned uid %d below the floor", d.UID)
		}
		if d.Action != ActionFetchNew {
			t.Errorf("uid %d action = %v, want ActionFetchNew", d.UID, d.Action)
		}
	}
}

// a cached message below the floor still reconciles: it is mail the user
// already has, so its flags must keep syncing and a server-side delete must
// still remove it. Skipping it because of the floor would freeze it forever.
func TestBuildPlanKeepsCachedMessagesBelowFloor(t *testing.T) {
	t.Parallel()
	locals := []LocalMessage{
		*local(1, 0, false, false),
		*local(2, storage.FlagSeen, false, false),
	}
	servers := []ServerMessage{*server(1, storage.FlagSeen)}

	plan := BuildPlan(locals, servers, 50)

	if len(plan) != 2 {
		t.Fatalf("plan length = %d, want 2", len(plan))
	}
	if plan[0].UID != 1 || plan[0].Action != ActionAdoptServerFlags {
		t.Errorf("uid 1 = %v, want ActionAdoptServerFlags", plan[0].Action)
	}
	if plan[1].UID != 2 || plan[1].Action != ActionDeleteLocal {
		t.Errorf("uid 2 = %v, want ActionDeleteLocal", plan[1].Action)
	}
}

// fakeClient serves a fixed set of uids and records the order bodies were
// fetched in, which is the whole point of #175.
type fakeClient struct {
	uids    []uint32
	fetched []uint32
	// uidValidity defaults to 1; a test changes it to simulate a server-side
	// mailbox reset.
	uidValidity uint32
	// deleted records every uid handed to DeleteMessages, so a test can assert
	// that a sync only ever asks to delete what the user deleted. moved and
	// movedTo do the same for the move-to-trash path.
	deleted []imap.UID
	moved   []imap.UID
	movedTo string
	// commands counts fetch commands rather than messages, which is what a
	// round trip to the server costs.
	commands int
}

func (c *fakeClient) Select(string) (*pimap.Mailbox, error) {
	validity := c.uidValidity
	if validity == 0 {
		validity = 1
	}
	return &pimap.Mailbox{UIDValidity: validity, NumMessages: uint32(len(c.uids))}, nil
}

func (c *fakeClient) FetchAllFlags() ([]pimap.MessageHeader, error) {
	out := make([]pimap.MessageHeader, 0, len(c.uids))
	for _, uid := range c.uids {
		out = append(out, pimap.MessageHeader{UID: imap.UID(uid)})
	}
	return out, nil
}

func (c *fakeClient) FetchMessage(uid imap.UID) (*pimap.Message, error) {
	c.fetched = append(c.fetched, uint32(uid))
	return &pimap.Message{UID: uid, Subject: "test"}, nil
}

func (c *fakeClient) Addr() string { return "imap.example.com:993" }

func (c *fakeClient) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.commands++
	for _, uid := range uids {
		msg, err := c.FetchMessage(uid)
		if err != nil {
			if fnErr := fn(uid, nil, err); fnErr != nil {
				return fnErr
			}
			continue
		}
		if fnErr := fn(uid, msg, nil); fnErr != nil {
			return fnErr
		}
	}
	return nil
}

func (c *fakeClient) AddFlags(imap.UID, ...imap.Flag) error { return nil }
func (c *fakeClient) DeleteMessages(uids ...imap.UID) error {
	c.deleted = append(c.deleted, uids...)
	return nil
}

func (c *fakeClient) MoveMessages(uids []imap.UID, mailbox string) error {
	c.moved = append(c.moved, uids...)
	c.movedTo = mailbox
	return nil
}

func newSyncTestFolder(t *testing.T) (*storage.DB, storage.Folder) {
	t.Helper()
	ctx := context.Background()

	db, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
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
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, &folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	return db, folder
}

// the reported bug: a first sync of a big mailbox downloaded the oldest message
// first and the user waited hours for recent mail. Newest uid must come first.
func TestSyncFolderFetchesNewestFirst(t *testing.T) {
	t.Parallel()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3, 4, 5}}
	engine := NewEngine(client, db, nil)

	if _, err := engine.SyncFolder(context.Background(), folder); err != nil {
		t.Fatalf("sync: %v", err)
	}

	want := []uint32{5, 4, 3, 2, 1}
	if len(client.fetched) != len(want) {
		t.Fatalf("fetched %v, want %v", client.fetched, want)
	}
	for i, uid := range want {
		if client.fetched[i] != uid {
			t.Fatalf("fetch order = %v, want %v (newest first)", client.fetched, want)
		}
	}
}

func TestSyncFolderCapsFirstSyncAndBackfills(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}
	engine := NewEngine(client, db, nil)
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
	if got := client.fetched; got[0] != 10 || got[2] != 8 {
		t.Fatalf("first sync fetched %v, want the 3 newest newest-first", got)
	}

	// a second ordinary sync must not re-fetch the held-back messages, and must
	// not treat them as new mail either.
	client.fetched = nil
	res, err = engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	if res.New != 0 || len(client.fetched) != 0 {
		t.Fatalf("resync fetched %v (new=%d), want nothing", client.fetched, res.New)
	}

	// backfilling admits the next batch, again newest first.
	client.fetched = nil
	res, err = engine.BackfillFolder(ctx, folder, 4)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if res.New != 4 {
		t.Fatalf("backfill fetched %d, want 4", res.New)
	}
	if got := client.fetched; len(got) != 4 || got[0] != 7 || got[3] != 4 {
		t.Fatalf("backfill fetched %v, want 7,6,5,4", got)
	}
	if !res.HasOlder {
		t.Fatal("three messages still remain below the window")
	}

	// the final backfill clears the floor and reports nothing older left.
	client.fetched = nil
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

// a UIDVALIDITY reset drops the cache, so the folder is a first sync again and
// its floor must be recomputed rather than left pointing at uids from the old
// generation. The stale floor has to actually reach the database, not just the
// in-memory state.
func TestSyncFolderResetsFloorOnUIDValidityChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}
	engine := NewEngine(client, db, nil)
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

	// the server resets the mailbox and hands out a small uid set again. the old
	// floor of 8 would sit above every new uid and hide the whole folder.
	client.uids = []uint32{1, 2}
	client.uidValidity = 2
	// re-read the row so the sync sees the UIDVALIDITY the first sync stored,
	// the way a real caller listing folders would.
	fresh, err := db.GetFolder(ctx, folder.ID)
	if err != nil {
		t.Fatalf("reload folder: %v", err)
	}
	uncapped := NewEngine(client, db, nil)
	res, err := uncapped.SyncFolder(ctx, *fresh)
	if err != nil {
		t.Fatalf("resync after uidvalidity change: %v", err)
	}
	if !res.UIDValidityReset {
		t.Fatal("expected a uidvalidity reset")
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
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	client := &fakeClient{uids: []uint32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}

	full := NewEngine(client, db, nil)
	if _, err := full.SyncFolder(ctx, folder); err != nil {
		t.Fatalf("initial full sync: %v", err)
	}

	capped := NewEngine(client, db, nil)
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
