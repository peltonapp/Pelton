package desktop

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// idleQueue waits until the account has no queued or running task.
func idleQueue(t *testing.T, rt *accountSync) {
	t.Helper()
	waitUntil(t, 10*time.Second, "queued work never ended", func() bool {
		st := rt.sched.Stats()
		return st.Fast == 0 && st.Normal == 0 && st.Running == 0
	})
}

// IDLE's new-mail check lists nothing in the fast lane for an Inbox without
// a cursor: that would be a full list. It queues a paged normal-lane list.
func TestNewMailWithoutCursorListsInTheNormalLane(t *testing.T) {
	client := &pagingIMAP{n: 2000}
	a, account, _, _ := pagingInbox(t, client)
	inbox := mustFolderID(t, a, account.ID, "INBOX")

	task := a.newMailTask(account, &imapWatch{})
	if err := task.Run(withTaskSession(a.ctx, account.ID, client), client); err != nil {
		t.Fatalf("new mail task: %v", err)
	}
	client.mu.Lock()
	listed := client.flagsCalls
	client.mu.Unlock()
	if listed != 0 {
		t.Fatalf("the fast-lane task listed the folder %d times, want none", listed)
	}
	// Only the list is waited for: the 2000 bodies after it are slow under -race.
	waitUntil(t, 30*time.Second, "the queued list never stored the folder", func() bool { return rowCount(t, a, inbox) == 2000 })
}

// The full check lists a page per unit, so other work runs between pages.
func TestFullCheckListsAPagePerUnit(t *testing.T) {
	client := &pagingIMAP{n: 2000}
	a, account, inbox, _ := pagingInbox(t, client)
	var mu sync.Mutex
	var pages []int
	client.onHeaders = func() {
		mu.Lock()
		pages = append(pages, rowCount(t, a, inbox.ID))
		mu.Unlock()
	}
	a.enqueueFullReconcile(a.ctx, account, []storage.Folder{inbox})
	waitUntil(t, 30*time.Second, "the check never stored the folder", func() bool { return rowCount(t, a, inbox.ID) == 2000 })
	mu.Lock()
	defer mu.Unlock()
	// Before each page: 0, 50, 550, 1050, 1550 rows stored.
	if len(pages) < 5 || pages[1] != 50 {
		t.Fatalf("rows before each header fetch %v, want pages of 50 then 500", pages)
	}
}

// "Load older" lists one small page in the fast lane; the rest of the batch
// follows a list page per normal-lane unit.
func TestLoadOlderListsOnlyTheFirstPageInTheFastLane(t *testing.T) {
	client := &pagingIMAP{n: 2000}
	a, account, inbox, rt := pagingInbox(t, client)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 600); err != nil {
		t.Fatal(err)
	}
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	idleQueue(t, rt)
	if n := rowCount(t, a, inbox.ID); n != 600 {
		t.Fatalf("%d stubs in the window, want 600", n)
	}

	var mu sync.Mutex
	var fastRows []int
	client.onHeaders = func() {
		mu.Lock()
		fastRows = append(fastRows, rowCount(t, a, inbox.ID))
		mu.Unlock()
	}
	if _, err := a.FetchOlderMessages(ListMessagesRequest{FolderID: inbox.ID}); err != nil {
		t.Fatalf("FetchOlderMessages: %v", err)
	}
	// Only the listing is waited for: the 600 bodies after it take long
	// under -race.
	waitUntil(t, 30*time.Second, "the older stubs were never stored", func() bool { return rowCount(t, a, inbox.ID) >= 1200 })
	time.Sleep(300 * time.Millisecond)
	if n := rowCount(t, a, inbox.ID); n != 1200 {
		t.Fatalf("%d stubs after load older, want 1200", n)
	}
	mu.Lock()
	defer mu.Unlock()
	// The fast-lane page stores 50, then 500 and 50 more in the normal lane.
	// (The fast-lane page fetches twice before it stores: UID+FLAGS of the
	// range below the floor, then the envelopes.)
	fastRows = slices.Compact(fastRows)
	if len(fastRows) < 3 || fastRows[0] != 600 || fastRows[1] != 650 {
		t.Fatalf("rows before each header fetch %v, want a first page of 50", fastRows)
	}
}

// Raising the message limit lists the extra mail like "load older": a
// short first page (FirstListPage), then full pages, so the first older
// stubs show quickly.
func TestRaisedLimitListsAShortFirstPage(t *testing.T) {
	client := &pagingIMAP{n: 2000}
	a, account, inbox, rt := pagingInbox(t, client)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 600); err != nil {
		t.Fatal(err)
	}
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	idleQueue(t, rt)
	if n := rowCount(t, a, inbox.ID); n != 600 {
		t.Fatalf("%d stubs in the window, want 600", n)
	}

	var mu sync.Mutex
	var rows []int
	client.onHeaders = func() {
		mu.Lock()
		rows = append(rows, rowCount(t, a, inbox.ID))
		mu.Unlock()
	}
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 1200); err != nil {
		t.Fatal(err)
	}
	a.enqueueSyncLimitDelta(600, 1200)
	// Only the listing is waited for: the 600 bodies after it take long
	// under -race.
	waitUntil(t, 30*time.Second, "the older stubs were never stored", func() bool { return rowCount(t, a, inbox.ID) >= 1200 })
	time.Sleep(300 * time.Millisecond)
	if n := rowCount(t, a, inbox.ID); n != 1200 {
		t.Fatalf("%d stubs after raising the limit, want 1200", n)
	}
	mu.Lock()
	defer mu.Unlock()
	// The first page stores 50, then 500 and 50 more.
	if len(rows) < 3 || rows[0] != 600 || rows[1] != 650 {
		t.Fatalf("rows before each header fetch %v, want a first page of 50", rows)
	}
}

// Opening a message larger than the chunk size fetches it in chunks in the
// normal lane, never whole in the fast lane.
func TestOpenedLargeMessageComesInChunks(t *testing.T) {
	const size = 12 << 20
	client := &pagingIMAP{n: 1, sources: map[imap.UID][]byte{1: bigSource(size)}}
	a, _, inbox, _ := pagingInbox(t, client)
	seedStub(t, a, inbox, 1, size)
	id, err := a.store.MessageIDByRemoteID(a.ctx, inbox.ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.fetchMessageBodyOnDemand(mustGetMessage(t, a.store, id)); err != nil {
		t.Fatalf("open: %v", err)
	}
	ops := client.opsSnapshot()
	if len(ops) != 3 || !strings.HasPrefix(ops[0], "chunk ") {
		t.Fatalf("ops %v, want three chunks", ops)
	}
	if m := mustGetMessage(t, a.store, id); !m.BodyComplete {
		t.Fatal("the opened message has no body")
	}
}

// Loading older mail or raising the limit lists the folder a fixed number of
// times however much it admits: one server list serves every page, and one
// more reconciles at the end. Listing in full once per page made "All" on a
// big folder cost a full list per 500 messages.
func TestOlderMailCostsAFixedNumberOfFullLists(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int // the setting when older mail is asked for; 0 is All
		raise bool
		want  int
	}{
		{"load older, All", 0, false, 5000},
		{"load older, 2000", 2000, false, 3000},
		{"limit raised to All", 0, true, 5000},
		{"limit raised to 3000", 3000, true, 3000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &pagingIMAP{n: 5000}
			a, account, inbox, rt := pagingInbox(t, client)
			if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 1000); err != nil {
				t.Fatal(err)
			}
			inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
				_, err := listWhole(ctx, a, account, inbox)
				return err
			})
			idleQueue(t, rt)
			if n := rowCount(t, a, inbox.ID); n != 1000 {
				t.Fatalf("%d stubs in the window, want 1000", n)
			}
			if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, tc.limit); err != nil {
				t.Fatal(err)
			}
			client.mu.Lock()
			client.flagsCalls = 0
			client.mu.Unlock()
			if tc.raise {
				a.enqueueSyncLimitDelta(1000, tc.limit)
			} else if _, err := a.FetchOlderMessages(ListMessagesRequest{FolderID: inbox.ID}); err != nil {
				t.Fatalf("FetchOlderMessages: %v", err)
			}
			// The bodies of what was admitted follow and take long under
			// -race; only the listing is waited for: the pages, then the
			// last unit's fresh list.
			lists := func() int {
				client.mu.Lock()
				defer client.mu.Unlock()
				return client.flagsCalls
			}
			waitUntil(t, 60*time.Second, "older stubs never stored", func() bool { return rowCount(t, a, inbox.ID) >= tc.want && lists() >= 2 })
			time.Sleep(300 * time.Millisecond)
			if n := rowCount(t, a, inbox.ID); n != tc.want {
				t.Fatalf("%d stubs, want %d", n, tc.want)
			}
			if n := lists(); n > 2 {
				t.Fatalf("%d full lists, want at most 2", n)
			}
		})
	}
}

// IDLE's new-mail check stays small: when more than a page arrived since
// the last list it lists nothing in the fast lane and queues a paged list in
// the normal lane, which a delta (one call however big) would not be.
func TestNewMailBurstListsInTheNormalLane(t *testing.T) {
	client := &pagingIMAP{n: 2000}
	a, account, inbox, _ := pagingInbox(t, client)
	if err := a.store.SetFolderStateToken(a.ctx, inbox.ID, "cursor"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetFolderLastSeenUID(a.ctx, inbox.ID, 100); err != nil {
		t.Fatal(err)
	}
	task := a.newMailTask(account, &imapWatch{})
	if err := task.Run(withTaskSession(a.ctx, account.ID, client), client); err != nil {
		t.Fatalf("new mail task: %v", err)
	}
	client.mu.Lock()
	listed := client.flagsCalls
	client.mu.Unlock()
	if listed != 0 {
		t.Fatalf("the fast-lane task listed the folder %d times, want none", listed)
	}
	waitUntil(t, 30*time.Second, "the queued list never stored the burst", func() bool { return rowCount(t, a, inbox.ID) == 2000 })
}

// gapIMAP is an inboxIMAP with CONDSTORE whose UIDNEXT stays put when the
// newest mail is deleted, as a server's does. fullLists counts the full
// lists.
type gapIMAP struct {
	inboxIMAP
	next      imap.UID
	fullLists int
	// failLists is how many of the next full lists fail.
	failLists int
}

func (c *gapIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &pimap.Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: c.next, NumMessages: uint32(len(c.uids)), HighestModSeq: 1}, nil
}

func (c *gapIMAP) FetchAllFlags() ([]pimap.MessageHeader, error) {
	c.mu.Lock()
	c.fullLists++
	if c.failLists > 0 {
		c.failLists--
		c.mu.Unlock()
		return nil, errors.New("connection reset by peer")
	}
	c.mu.Unlock()
	return c.inboxIMAP.FetchAllFlags()
}

func (c *gapIMAP) lists() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fullLists
}

// After the newest messages were deleted, the highest UID seen is far below
// UIDNEXT though no mail came. IDLE's new-mail check compares UIDNEXT with
// the one its last clean list saw, so it does not read every wake as a
// burst and list the folder in full.
func TestDeletingTheNewestMailIsNoNewMailBurst(t *testing.T) {
	client := &gapIMAP{next: 201}
	for uid := imap.UID(1); uid <= 200; uid++ {
		client.uids = append(client.uids, uid)
	}
	a, account, inbox := reconcileInbox(t, client)
	rt := mustAccountSync(t, a, account.ID)
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	fresh, err := a.store.GetFolder(a.ctx, inbox.ID)
	if err != nil {
		t.Fatal(err)
	}
	inbox = *fresh
	if inbox.StateToken == "" {
		t.Fatal("the first list left no cursor")
	}
	check := func() {
		t.Helper()
		task := a.newMailTask(account, &imapWatch{})
		if err := task.Run(withTaskSession(a.ctx, account.ID, client), client); err != nil {
			t.Fatalf("new mail task: %v", err)
		}
		idleQueue(t, rt)
	}
	check()

	// The newest 60 go; a full check lists the folder and sees 140 as the
	// highest UID.
	client.mu.Lock()
	client.uids = client.uids[:140]
	client.mu.Unlock()
	if err := a.execFullReconcile(a.ctx, account, inbox); err != nil {
		t.Fatalf("full check: %v", err)
	}
	idleQueue(t, rt)
	if seen, err := a.store.FolderLastSeenUID(a.ctx, inbox.ID); err != nil || seen != 140 {
		t.Fatalf("highest UID seen %d (%v), want 140", seen, err)
	}
	before := client.lists()
	check()
	check()
	if n := client.lists() - before; n != 0 {
		t.Fatalf("new-mail checks after the delete listed the folder in full %d times, want none", n)
	}
}

// "Load older" lowers the window and lists what that admitted. When the
// listing fails after the window moved, nothing is missing for good: the
// folder's next list stores the admitted mail, though the folder had a
// cursor and its next list would have been a delta that never sees it.
func TestFailedOlderListingLeavesNoMailMissing(t *testing.T) {
	client := &gapIMAP{next: 101}
	for uid := imap.UID(1); uid <= 100; uid++ {
		client.uids = append(client.uids, uid)
	}
	a, account, inbox := reconcileInbox(t, client)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 50); err != nil {
		t.Fatal(err)
	}
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	if n := rowCount(t, a, inbox.ID); n != 50 {
		t.Fatalf("%d stubs in the window, want 50", n)
	}
	if f, err := a.store.GetFolder(a.ctx, inbox.ID); err != nil || f.StateToken == "" {
		t.Fatalf("the first list left no cursor (%v)", err)
	}

	client.mu.Lock()
	client.failLists = 1
	client.mu.Unlock()
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		if _, _, err := a.imapOlderStep(ctx, account, inbox, newOlderList(20, 50, 500)); err == nil {
			t.Error("the older listing did not fail")
		}
		return nil
	})
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	if n := rowCount(t, a, inbox.ID); n != 70 {
		t.Fatalf("%d stubs after the next list, want the 70 the lowered window holds", n)
	}
}

// "Load older" with a batch of at most a page (or a small folder) keeps its
// fast-lane unit to one insert-only page too. The full reconcile (deletes,
// server flags, the cursor) is left to a normal-lane unit.
func TestLoadOlderFastLaneUnitNeverReconciles(t *testing.T) {
	client := &gapIMAP{next: 101}
	for uid := imap.UID(1); uid <= 100; uid++ {
		client.uids = append(client.uids, uid)
	}
	a, account, inbox := reconcileInbox(t, client)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 50); err != nil {
		t.Fatal(err)
	}
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	// Uid 100 goes from the server: only a reconcile drops its row.
	client.mu.Lock()
	client.uids = client.uids[:99]
	client.mu.Unlock()

	var more bool
	err := a.runNowKey(a.ctx, account, syncsched.KindLoadOlder, inbox.ID, folderKey(syncsched.KindLoadOlder, inbox.ID),
		func(ctx context.Context, _ mailClient) error {
			var err error
			older := newOlderList(50, 50, 500)
			older.fast = true
			_, more, err = a.imapOlderStep(ctx, account, inbox, older)
			return err
		})
	if err != nil {
		t.Fatalf("load older: %v", err)
	}
	if _, err := a.store.MessageIDByRemoteID(a.ctx, inbox.ID, "100"); err != nil || !more {
		t.Fatalf("the fast-lane unit reconciled (more=%v, row of the deleted uid: %v)", more, err)
	}
	if n := rowCount(t, a, inbox.ID); n != 100 {
		t.Fatalf("%d stubs after the fast-lane unit, want its page of 50 stored", n)
	}
}

// The user waits for an opened large message, so its chunks do not queue
// behind a listing of its folder (a manual Sync of a big folder, say).
func TestOpenedLargeMessageDoesNotWaitForTheFoldersList(t *testing.T) {
	const size = 6 << 20
	client := &pagingIMAP{n: 1, sources: map[imap.UID][]byte{1: bigSource(size)}}
	a, account, inbox, rt := pagingInbox(t, client)
	setParallel(t, a, 2)
	a.reconfigureAccountPool(account.ID)
	seedStub(t, a, inbox, 1, size)
	id, err := a.store.MessageIDByRemoteID(a.ctx, inbox.ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	recv(t, signalJob(rt.sched, syncsched.KindListStubs, inbox.ID, release), 2*time.Second, "list never started")
	opened := make(chan error, 1)
	go func() { opened <- a.fetchMessageBodyOnDemand(mustGetMessage(t, a.store, id)) }()
	if err := recv(t, opened, 5*time.Second, "the opened message waited for the folder's list"); err != nil {
		t.Fatalf("open: %v", err)
	}
}

// Opening a stub fills it in place: the pane hears mail:updated, and the
// list is not told about new mail (mail:new reloads it, and the old ui
// jumps), small message or large.
func TestOpeningAStubSendsNoNewMail(t *testing.T) {
	for _, size := range []int64{1 << 10, 6 << 20} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) { openStubQuietly(t, size) })
	}
}

func openStubQuietly(t *testing.T, size int64) {
	client := &pagingIMAP{n: 1, sources: map[imap.UID][]byte{1: bigSource(int(size))}}
	a, _, inbox, _ := pagingInbox(t, client)
	seedStub(t, a, inbox, 1, size)
	id, err := a.store.MessageIDByRemoteID(a.ctx, inbox.ID, "1")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	news := 0
	a.emitForTest = func(name string, _ any) {
		if name == EventMailNew {
			mu.Lock()
			news++
			mu.Unlock()
		}
	}
	if err := a.fetchMessageBodyOnDemand(mustGetMessage(t, a.store, id)); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !mustGetMessage(t, a.store, id).BodyComplete {
		t.Fatalf("size %d: the opened message has no body", size)
	}
	mu.Lock()
	if news != 0 {
		t.Errorf("size %d: %d mail:new events for an opened message, want none", size, news)
	}
	mu.Unlock()
}

// probedIMAP is a pagingIMAP that answers a UID FETCH only for the UIDs it
// holds, as a server does, and counts SEARCH ALL commands and the rows its
// FETCHes return.
type probedIMAP struct {
	*pagingIMAP
	searches, rows int
}

func (c *probedIMAP) SearchAllUIDs() ([]imap.UID, error) {
	c.mu.Lock()
	c.searches++
	c.mu.Unlock()
	return c.pagingIMAP.SearchAllUIDs()
}

func (c *probedIMAP) FetchHeaders(uids []imap.UID, opts *imap.FetchOptions) ([]pimap.MessageHeader, error) {
	held := make([]imap.UID, 0, len(uids))
	for _, uid := range uids {
		if uid >= 1 && int(uid) <= c.n {
			held = append(held, uid)
		}
	}
	out, err := c.pagingIMAP.FetchHeaders(held, opts)
	c.mu.Lock()
	c.rows += len(out)
	c.mu.Unlock()
	return out, err
}

// counts returns the full lists (FETCH of every flag), SEARCH ALLs and rows
// since the last call.
func (c *probedIMAP) counts() (lists, searches, rows int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lists, searches, rows = c.flagsCalls, c.searches, c.rows
	c.flagsCalls, c.searches, c.rows = 0, 0, 0
	return lists, searches, rows
}

// "Load older"'s fast-lane unit costs the same in any folder: it fetches
// the UIDs just below the window floor and their envelopes, never every
// UID (SEARCH ALL) or every flag (a full list) of the folder. The normal
// lane then admits the rest of the batch.
func TestLoadOlderFastLaneUnitIsBounded(t *testing.T) {
	client := &probedIMAP{pagingIMAP: &pagingIMAP{n: 2000}}
	a, account, inbox := reconcileInbox(t, client)
	inbox.ID = mustFolderID(t, a, account.ID, "INBOX")
	setParallel(t, a, 1)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 50); err != nil {
		t.Fatal(err)
	}
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	if n := rowCount(t, a, inbox.ID); n != 50 {
		t.Fatalf("%d stubs in the window, want 50", n)
	}
	client.counts()

	older := newOlderList(600, 50, 500)
	older.fast = true
	var ids []string
	var more bool
	err := a.runNowKey(a.ctx, account, syncsched.KindLoadOlder, inbox.ID, folderKey(syncsched.KindLoadOlder, inbox.ID),
		func(ctx context.Context, _ mailClient) error {
			var err error
			ids, more, err = a.imapOlderStep(ctx, account, inbox, older)
			return err
		})
	if err != nil {
		t.Fatalf("load older: %v", err)
	}
	lists, searches, rows := client.counts()
	if lists != 0 || searches != 0 {
		t.Fatalf("the fast-lane unit ran %d full lists and %d SEARCH ALLs, want none", lists, searches)
	}
	if rows > 3*50 {
		t.Fatalf("the fast-lane unit fetched %d rows, want at most %d", rows, 3*50)
	}
	if n := rowCount(t, a, inbox.ID); n != 100 || len(ids) != 50 || ids[0] != "1950" || !more {
		t.Fatalf("%d stubs, %d ids to fetch, more %v; want its page of 50 below the floor and more", n, len(ids), more)
	}

	// The normal lane admits the other 550, a page per unit.
	for more {
		inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
			var err error
			_, more, err = a.imapOlderStep(ctx, account, inbox, older)
			return err
		})
	}
	if n := rowCount(t, a, inbox.ID); n != 650 {
		t.Fatalf("%d stubs after the listing, want 650", n)
	}
}

// Raising the limit by less than a page lists the folder once: the list that
// finds the admitted mail reconciles at once, with no fast-lane page first.
func TestRaisedLimitShortAdmissionListsOnce(t *testing.T) {
	client := &pagingIMAP{n: 2000}
	a, account, inbox, rt := pagingInbox(t, client)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 600); err != nil {
		t.Fatal(err)
	}
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	idleQueue(t, rt)
	client.mu.Lock()
	client.flagsCalls = 0
	client.mu.Unlock()
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 602); err != nil {
		t.Fatal(err)
	}
	a.enqueueSyncLimitDelta(600, 602)
	waitUntil(t, 30*time.Second, "the older stubs were never stored", func() bool { return rowCount(t, a, inbox.ID) >= 602 })
	idleQueue(t, rt)
	if n := rowCount(t, a, inbox.ID); n != 602 {
		t.Fatalf("%d stubs, want 602", n)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.flagsCalls != 1 {
		t.Fatalf("%d full lists, want 1", client.flagsCalls)
	}
}
