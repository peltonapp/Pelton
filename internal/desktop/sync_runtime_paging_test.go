package desktop

import (
	"bytes"
	"context"
	"fmt"
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

// pagingIMAP is a mailbox of uids 1..n. Envelopes carry sizes[uid] (1 KB
// by default), FetchPartial serves sources[uid], and ops records each body
// command in order.
type pagingIMAP struct {
	fakeIMAP
	n       int
	sizes   map[imap.UID]int64
	sources map[imap.UID][]byte
	// onHeaders and onPartial run on every FetchHeaders and FetchPartial.
	onHeaders func()
	onPartial func(offset int64)

	mu         sync.Mutex
	ops        []string
	flagsCalls int
}

func (c *pagingIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	return &pimap.Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: imap.UID(c.n + 1), NumMessages: uint32(c.n)}, nil
}

func (c *pagingIMAP) FetchAllFlags() ([]pimap.MessageHeader, error) {
	c.mu.Lock()
	c.flagsCalls++
	c.mu.Unlock()
	out := make([]pimap.MessageHeader, c.n)
	for i := range out {
		out[i] = pimap.MessageHeader{UID: imap.UID(i + 1)}
	}
	return out, nil
}

func (c *pagingIMAP) SearchAllUIDs() ([]imap.UID, error) {
	out := make([]imap.UID, c.n)
	for i := range out {
		out[i] = imap.UID(i + 1)
	}
	return out, nil
}

func (c *pagingIMAP) FetchHeaders(uids []imap.UID, _ *imap.FetchOptions) ([]pimap.MessageHeader, error) {
	if c.onHeaders != nil {
		c.onHeaders()
	}
	out := make([]pimap.MessageHeader, 0, len(uids))
	for _, uid := range uids {
		size := c.sizes[uid]
		if size == 0 {
			size = 1 << 10
		}
		out = append(out, pimap.MessageHeader{
			UID: uid, HasEnvelope: true, Subject: "s" + strconv.Itoa(int(uid)),
			Date: time.Unix(int64(uid)*60, 0), Size: size,
		})
	}
	return out, nil
}

func (c *pagingIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.mu.Lock()
	c.ops = append(c.ops, "fetch "+uidList(uids))
	c.mu.Unlock()
	for _, uid := range uids {
		if err := fn(uid, &pimap.Message{UID: uid, Subject: "s", Text: "body"}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *pagingIMAP) FetchPartial(uid imap.UID, offset, length int64) (*pimap.Message, error) {
	c.mu.Lock()
	c.ops = append(c.ops, fmt.Sprintf("chunk %d@%d", uid, offset))
	c.mu.Unlock()
	if c.onPartial != nil {
		c.onPartial(offset)
	}
	src, ok := c.sources[uid]
	if !ok {
		return nil, fmt.Errorf("uid %d: %w", uid, pimap.ErrNoMessage)
	}
	start := min(offset, int64(len(src)))
	return &pimap.Message{UID: uid, Subject: "big", Raw: src[start:min(start+length, int64(len(src)))]}, nil
}

func (c *pagingIMAP) opsSnapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.ops)
}

func uidList(uids []imap.UID) string {
	parts := make([]string, len(uids))
	for i, uid := range uids {
		parts[i] = strconv.Itoa(int(uid))
	}
	return strings.Join(parts, ",")
}

// bigSource is a plain-text message of exactly size bytes.
func bigSource(size int) []byte {
	head := "From: ada@example.com\r\nSubject: big\r\nContent-Type: text/plain\r\n\r\n"
	line := strings.Repeat("x", 76) + "\r\n"
	var b bytes.Buffer
	b.WriteString(head)
	for b.Len()+len(line) <= size {
		b.WriteString(line)
	}
	b.WriteString(strings.Repeat("y", size-b.Len()))
	return b.Bytes()
}

// pagingInbox is an account with an INBOX on c, one session (N=1) and an
// unlimited sync window.
func pagingInbox(t *testing.T, c *pagingIMAP) (*App, storage.Account, storage.Folder, *accountSync) {
	t.Helper()
	a, account, inbox := reconcileInbox(t, c)
	inbox.ID = mustFolderID(t, a, account.ID, "INBOX")
	setParallel(t, a, 1)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 0); err != nil {
		t.Fatal(err)
	}
	return a, account, inbox, mustAccountSync(t, a, account.ID)
}

// rowCount is how many rows the folder has, the ones ListMessages returns.
// It counts rather than lists: tests poll it while thousands of stubs are
// stored, and reading every row on each poll was most of their time.
func rowCount(t *testing.T, a *App, folderID int64) int {
	t.Helper()
	n, _, err := a.store.FolderCounts(a.ctx, folderID)
	if err != nil {
		t.Error(err)
	}
	return n
}

// A list task over a 2000-message folder runs one page per unit (50, then
// 500s) and goes back to the tail between them, so a fast-lane task queued
// during the first page runs before the second, even at N=1.
func TestListTaskPagesAndLetsFastLaneIn(t *testing.T) {
	client := &pagingIMAP{n: 2000}
	a, account, inbox, rt := pagingInbox(t, client)

	fastRows := make(chan int, 1)
	var once sync.Once
	client.onHeaders = func() {
		once.Do(func() {
			rt.sched.Enqueue(imapTask{Kind: syncsched.KindSource, Run: func(context.Context, mailClient) error {
				fastRows <- rowCount(t, a, inbox.ID)
				return nil
			}})
		})
	}
	step := a.imapPlanStep(account, false)
	units := 0
	done := make(chan error, 1)
	rt.sched.Enqueue(imapTask{
		Kind: syncsched.KindListStubs, FolderID: inbox.ID, Key: folderKey(syncsched.KindListStubs, inbox.ID),
		Run: func(ctx context.Context, c mailClient) error {
			units++
			return step(ctx, c, inbox, syncsched.KindListStubs)
		},
		Done: func(err error) { done <- err },
	})
	// The bound only catches a wedged task. Storing 2000 stubs a row at a
	// time takes 8-10 s under -race, which used to hit a 10 s bound when the
	// test ran on its own or on a busy machine. The order checked below does
	// not depend on timing: the fast task is queued during the first page,
	// and the scheduler runs it before the second.
	if err := recv(t, done, 2*time.Minute, "list task never ended"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := recv(t, fastRows, 10*time.Second, "fast task never ran"); got != 50 {
		t.Fatalf("fast task saw %d stubs, want it right after the first page of 50", got)
	}
	// 50, 500, 500, 500, then the last 450 with the reconcile.
	if units != 5 || rowCount(t, a, inbox.ID) != 2000 {
		t.Fatalf("%d units, %d rows; want 5 units and 2000 rows", units, rowCount(t, a, inbox.ID))
	}
	if client.flagsCalls != 2 {
		t.Fatalf("%d full lists, want 2 (the pages and the reconcile)", client.flagsCalls)
	}
}

// A 30 MB message comes in 5 MB chunks, one per unit of the body task, so a
// fast-lane task queued during the first chunk runs before the second.
func TestBodyChunksLetFastLaneIn(t *testing.T) {
	const size = 30 << 20
	client := &pagingIMAP{n: 1, sources: map[imap.UID][]byte{1: bigSource(size)}}
	a, account, inbox, rt := pagingInbox(t, client)
	seedStub(t, a, inbox, 1, size)

	fastAfter := make(chan int, 1)
	client.onPartial = func(offset int64) {
		if offset != 0 {
			return
		}
		rt.sched.Enqueue(imapTask{Kind: syncsched.KindSource, Run: func(context.Context, mailClient) error {
			fastAfter <- len(client.opsSnapshot())
			return nil
		}})
	}
	done := make(chan error, 1)
	a.enqueueBodies(a.ctx, account, inbox, syncsched.ClassAuto, false, func(err error) { done <- err })
	if err := recv(t, done, 20*time.Second, "body task never ended"); err != nil {
		t.Fatalf("bodies: %v", err)
	}
	if got := recv(t, fastAfter, time.Second, "fast task never ran"); got != 1 {
		t.Fatalf("fast task ran after %d chunks, want 1", got)
	}
	want := []string{"chunk 1@0", "chunk 1@5242880", "chunk 1@10485760", "chunk 1@15728640", "chunk 1@20971520", "chunk 1@26214400", "chunk 1@31457280"}
	if ops := client.opsSnapshot(); !slices.Equal(ops, want) {
		t.Fatalf("ops %v, want %v", ops, want)
	}
	need, err := a.store.RemoteIDsNeedingBody(a.ctx, inbox.ID, []string{"1"})
	if err != nil || len(need) != 0 {
		t.Fatalf("body still missing (%v, %v)", need, err)
	}
}

// seedStub stores an incomplete stub of size bytes, dated by its uid.
func seedStub(t *testing.T, a *App, folder storage.Folder, uid int, size int64) {
	t.Helper()
	if _, err := a.store.UpsertMessageListMeta(a.ctx, &storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, UID: uint32(uid), RemoteID: strconv.Itoa(uid),
		Date: time.Unix(int64(uid)*60, 0), SizeBytes: size,
	}); err != nil {
		t.Fatal(err)
	}
}

// Body batches keep to the byte budget by the stubs' sizes, and a message
// over the chunk size comes alone, in chunks.
func TestBodyBatchesUseStubSizes(t *testing.T) {
	const kb, mb = 1 << 10, 1 << 20
	for _, tc := range []struct {
		name  string
		sizes []int64 // uid i+1 has sizes[i]
		want  []string
	}{
		{"small mail, count first", []int64{kb, kb, kb, kb, kb, kb, kb}, []string{"fetch 7,6,5,4,3", "fetch 2,1"}},
		// 20 MB budget: four of 4.5 MB fit, the fifth would not.
		{"byte budget first", []int64{4.5 * mb, 4.5 * mb, 4.5 * mb, 4.5 * mb, 4.5 * mb, 4.5 * mb}, []string{"fetch 6,5,4,3", "fetch 2,1"}},
		{"large one alone, in chunks", []int64{kb, 6 * mb, kb}, []string{"fetch 3", "chunk 2@0", "chunk 2@5242880", "fetch 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &pagingIMAP{n: len(tc.sizes), sources: map[imap.UID][]byte{}}
			a, account, inbox, _ := pagingInbox(t, client)
			for i, size := range tc.sizes {
				seedStub(t, a, inbox, i+1, size)
				if size > 5*mb {
					client.sources[imap.UID(i+1)] = bigSource(int(size))
				}
			}
			done := make(chan error, 1)
			a.enqueueBodies(a.ctx, account, inbox, syncsched.ClassAuto, false, func(err error) { done <- err })
			if err := recv(t, done, 10*time.Second, "body task never ended"); err != nil {
				t.Fatalf("bodies: %v", err)
			}
			if ops := client.opsSnapshot(); !slices.Equal(ops, tc.want) {
				t.Fatalf("ops %v, want %v", ops, tc.want)
			}
		})
	}
}

// After a restart mid-chunk (the runtime and its memory are gone) the
// message is still a stub and is fetched again from byte 0.
func TestBodyChunksRestartFromZero(t *testing.T) {
	const size = 12 << 20
	client := &pagingIMAP{n: 1, sources: map[imap.UID][]byte{1: bigSource(size)}}
	a, account, inbox, rt := pagingInbox(t, client)
	seedStub(t, a, inbox, 1, size)

	firstChunk := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	client.onPartial = func(int64) {
		once.Do(func() {
			close(firstChunk)
			<-release
		})
	}
	a.enqueueBodies(a.ctx, account, inbox, syncsched.ClassAuto, false, nil)
	recv(t, firstChunk, 5*time.Second, "no chunk fetched")
	stopped := make(chan struct{})
	go func() {
		a.stopAccountScheduler(account.ID, false)
		close(stopped)
	}()
	// The chunk ends only once the stop has begun: a chunk that ended
	// first would send the task back to its lane and the rest of the body
	// would come in before the stop.
	waitUntil(t, 5*time.Second, "the stop never began", rt.sched.Stopped)
	close(release)
	recv(t, stopped, 10*time.Second, "scheduler did not stop")
	need, err := a.store.RemoteIDsNeedingBody(a.ctx, inbox.ID, []string{"1"})
	if err != nil || len(need) != 1 {
		t.Fatalf("after the stop: need %v, %v; want the stub still incomplete", need, err)
	}

	done := make(chan error, 1)
	a.enqueueBodies(a.ctx, account, inbox, syncsched.ClassAuto, false, func(err error) { done <- err })
	if err := recv(t, done, 10*time.Second, "body task never ended"); err != nil {
		t.Fatalf("bodies: %v", err)
	}
	ops := client.opsSnapshot()
	if len(ops) < 2 || ops[0] != "chunk 1@0" || ops[len(ops)-3] != "chunk 1@0" {
		t.Fatalf("ops %v, want the second run to start at byte 0", ops)
	}
	if need, err := a.store.RemoteIDsNeedingBody(a.ctx, inbox.ID, []string{"1"}); err != nil || len(need) != 0 {
		t.Fatalf("body still missing (%v, %v)", need, err)
	}
}

// A chunked body fetch that finds the folder's UIDVALIDITY changed ends the
// folder's body work at once: its stored ids may name other mail now, and
// its next list resets it. It is no failed batch to retry.
func TestChunkedFetchStopsOnAGenerationChange(t *testing.T) {
	const size = 12 << 20
	client := &pagingIMAP{n: 1, sources: map[imap.UID][]byte{1: bigSource(size)}}
	a, account, inbox, _ := pagingInbox(t, client)
	if err := a.store.SetFolderUIDValidity(a.ctx, inbox.ID, 2); err != nil { // the server says 1
		t.Fatal(err)
	}
	inbox.UIDValidity = 2
	seedStub(t, a, inbox, 1, size)
	done := make(chan error, 1)
	a.enqueueBodies(a.ctx, account, inbox, syncsched.ClassAuto, false, func(err error) { done <- err })
	if err := recv(t, done, 5*time.Second, "body work never ended"); err != nil {
		t.Fatalf("body work: %v", err)
	}
	if ops := client.opsSnapshot(); len(ops) != 0 {
		t.Fatalf("ops %v, want nothing fetched from a changed folder", ops)
	}
}
