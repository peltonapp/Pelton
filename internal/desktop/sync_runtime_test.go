package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// batchIMAP serves every UID's body except the ones in gone, and records
// each FETCH and the scheduler's queue at that moment.
type batchIMAP struct {
	fakeIMAP
	gone    map[imap.UID]bool
	queued  func() int
	mu      sync.Mutex
	fetches [][]imap.UID
	maxQ    int
}

func (c *batchIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	return &pimap.Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: 100, NumMessages: 23}, nil
}

func (c *batchIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.mu.Lock()
	c.fetches = append(c.fetches, append([]imap.UID(nil), uids...))
	c.maxQ = max(c.maxQ, c.queued())
	c.mu.Unlock()
	for _, uid := range uids {
		if c.gone[uid] {
			continue
		}
		if err := fn(uid, &pimap.Message{UID: uid, Subject: "s", Text: "body"}, nil); err != nil {
			return err
		}
	}
	return nil
}

// A folder's missing bodies come in batches from one generator task: the
// queue never holds a second body task for the folder, the first batch is
// the default count, and a message the server no longer has is skipped
// without failing the folder.
func TestBodyGeneratorFillsFolderInBatches(t *testing.T) {
	a, account, folders := reconcileTestApp(t, storage.Folder{Name: "INBOX", IMAPPath: "INBOX", RemoteID: "INBOX"})
	inbox := folders[0]
	inbox.ID = mustFolderID(t, a, account.ID, "INBOX")
	for uid := 1; uid <= 23; uid++ {
		if _, err := a.store.UpsertMessageListMeta(a.ctx, &storage.Message{
			AccountID: account.ID, FolderID: inbox.ID, UID: uint32(uid), RemoteID: strconv.Itoa(uid),
			Date: time.Unix(int64(uid)*60, 0),
		}); err != nil {
			t.Fatal(err)
		}
	}
	rt := mustAccountSync(t, a, account.ID)
	client := &batchIMAP{gone: map[imap.UID]bool{20: true}, queued: func() int { return rt.sched.Stats().Normal }}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }

	done := make(chan error, 1)
	a.enqueueBodies(a.ctx, account, inbox, syncsched.ClassAuto, false, func(err error) { done <- err })
	a.enqueueBodies(a.ctx, account, inbox, syncsched.ClassAuto, false, nil) // a second campaign joins
	if err := recv(t, done, 5*time.Second, "body generator never finished"); err != nil {
		t.Fatalf("body generator: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.fetches) < 3 || len(client.fetches[0]) != syncsched.DefaultBodyCount {
		t.Fatalf("fetches %v, want batches starting at %d", client.fetches, syncsched.DefaultBodyCount)
	}
	if client.fetches[0][0] != 23 {
		t.Fatalf("first batch %v, want newest first", client.fetches[0])
	}
	if client.maxQ > 1 {
		t.Fatalf("normal lane held %d tasks during the campaign, want at most 1", client.maxQ)
	}
	need, err := a.store.RemoteIDsNeedingBodyNewest(a.ctx, inbox.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(need) != 1 || need[0] != "20" {
		t.Fatalf("still missing %v, want only the vanished 20", need)
	}
}

func mustFolderID(t *testing.T, a *App, accountID int64, path string) int64 {
	t.Helper()
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range folders {
		if f.IMAPPath == path {
			return f.ID
		}
	}
	t.Fatalf("no folder %s", path)
	return 0
}

// A body batch that fails on the connection goes back to the tail with its
// session closed, so the next unit (often the user's move) dials a fresh one
// instead of failing on the dead socket. A server NO keeps the session.
func TestBodyBatchFailureClosesABrokenSession(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		wantDials int32
	}{
		{"connection lost", io.ErrUnexpectedEOF, 2},
		{"server said no", &imap.Error{Type: imap.StatusResponseTypeNo, Text: "try later"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dials atomic.Int32
			p := pool.New(1)
			s := syncsched.New(context.Background(), pool.NewSessions(p, func(context.Context) (mailClient, error) {
				dials.Add(1)
				return &fakeIMAP{}, nil
			}, 0), syncsched.Options{Broken: imapSessionBroken})
			t.Cleanup(func() { _ = s.Stop(context.Background()) })
			runs := 0
			done := make(chan error, 1)
			s.Enqueue(bodyTask(storage.Folder{ID: 1}, syncsched.ClassAuto, nil, func(context.Context, mailClient) error {
				runs++
				if runs == 1 {
					return tc.err
				}
				return nil
			}, func(err error) { done <- err }))
			if err := recv(t, done, 2*time.Second, "body task never ended"); err != nil {
				t.Fatalf("body task: %v", err)
			}
			if got := dials.Load(); got != tc.wantDials {
				t.Fatalf("%d dials, want %d", got, tc.wantDials)
			}
		})
	}
}

// Which task errors close the session: a tagged NO/BAD and a folder whose
// UIDVALIDITY changed leave it fit for the next task; anything else may
// have left it mid-command.
func TestImapSessionBroken(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"server NO", &imap.Error{Type: imap.StatusResponseTypeNo, Text: "no"}, false},
		{"generation changed", fmt.Errorf("fetch: %w", psync.ErrGenerationChanged), false},
		{"connection lost", io.ErrUnexpectedEOF, true},
	} {
		if got := imapSessionBroken(tc.err); got != tc.want {
			t.Errorf("%s: broken = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Copies of one body generator that fail together on one blip (a dropped
// link fails every session at once) spend one retry, not one each: the
// generator goes on, and Done comes with nil. A failure that persists still
// ends it after syncStepAttempts rounds.
func TestBodyCopiesFailingTogetherSpendOneRetry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		persist bool
	}{{"one blip", false}, {"persistent", true}} {
		t.Run(tc.name, func(t *testing.T) {
			p := pool.New(3)
			s := syncsched.New(context.Background(), pool.NewSessions(p, func(context.Context) (mailClient, error) {
				return &fakeIMAP{}, nil
			}, 0), syncsched.Options{Broken: imapSessionBroken})
			t.Cleanup(func() { _ = s.Stop(context.Background()) })
			var calls, arrived, failed atomic.Int32
			done := make(chan error, 1)
			s.Enqueue(bodyTask(storage.Folder{ID: 1}, syncsched.ClassAuto, func(int) int { return 3 }, func(context.Context, mailClient) error {
				n := calls.Add(1)
				if n <= 3 {
					// The three copies are in flight at once when the link drops.
					arrived.Add(1)
					for end := time.Now().Add(5 * time.Second); arrived.Load() < 3 && time.Now().Before(end); {
						time.Sleep(time.Millisecond)
					}
					failed.Add(1)
					return io.ErrUnexpectedEOF
				}
				// A retry dials after the blip: every copy of it has failed
				// (and been counted) by then.
				for end := time.Now().Add(5 * time.Second); failed.Load() < 3 && time.Now().Before(end); {
					time.Sleep(time.Millisecond)
				}
				time.Sleep(20 * time.Millisecond)
				if tc.persist && n < 100 {
					return io.ErrUnexpectedEOF
				}
				return nil
			}, func(err error) { done <- err }))
			err := recv(t, done, 10*time.Second, "body task never ended")
			if arrived.Load() != 3 {
				t.Fatalf("%d copies failed together, want 3", arrived.Load())
			}
			if tc.persist {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("Done %v, want the persistent failure", err)
				}
				if n := calls.Load(); n >= 100 {
					t.Fatalf("%d units, want the generator to give up", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("Done %v after %d units, want nil: one blip is one failure", err, calls.Load())
			}
		})
	}
}
