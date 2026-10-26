package desktop

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// A message that cannot be read is a soft error: the rest of the batch is
// stored, the folder's body work does not fail, and background fetches try
// it unreadableTries times, then leave it until a manual Sync.
func TestUnreadableMessageIsASoftError(t *testing.T) {
	client := &badMessageIMAP{inboxIMAP: inboxIMAP{uids: []imap.UID{5, 6}}, bad: 6}
	a, account, inbox := reconcileInbox(t, client)
	for _, uid := range []uint32{5, 6} {
		if _, err := a.store.UpsertMessageListMeta(a.ctx, &storage.Message{
			AccountID: account.ID, FolderID: inbox.ID, UID: uid, Subject: "hi", Date: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	bodies := func() {
		t.Helper()
		done := make(chan error, 1)
		a.enqueueBodies(a.ctx, account, inbox, syncsched.ClassAuto, false, func(err error) { done <- err })
		if err := recv(t, done, 5*time.Second, "body generator never ended"); err != nil {
			t.Fatalf("body generator failed on an unreadable message: %v", err)
		}
	}
	for try := 1; try <= unreadableTries+1; try++ {
		bodies()
		if want := int32(min(try, unreadableTries)); client.attempts.Load() != want {
			t.Fatalf("after run %d the bad message was tried %d times, want %d", try, client.attempts.Load(), want)
		}
	}
	if need, err := a.store.RemoteIDsNeedingBody(a.ctx, inbox.ID, []string{"5", "6"}); err != nil || len(need) != 1 || need[0] != "6" {
		t.Fatalf("still need %v (%v), want only the bad message", need, err)
	}

	// The Sync returns after the lists; its bodies go on in the background
	// until a unit finds nothing left to fetch (the bad message skipped).
	finished := make(chan struct{}, 1)
	onBodiesFinished = func(id int64) {
		if id == inbox.ID {
			select {
			case finished <- struct{}{}:
			default:
			}
		}
	}
	t.Cleanup(func() { onBodiesFinished = nil })
	if err := a.runIMAPManualSync(a.ctx, account, true); err != nil {
		t.Fatalf("manual sync: %v", err)
	}
	recv(t, finished, 5*time.Second, "manual Sync's bodies never finished")
	if client.attempts.Load() != unreadableTries+1 {
		t.Fatalf("manual Sync tried the bad message %d times in all, want one more try", client.attempts.Load())
	}
}

// badMessageIMAP is inboxIMAP where one uid never parses.
type badMessageIMAP struct {
	inboxIMAP
	bad      imap.UID
	attempts atomic.Int32
}

func (c *badMessageIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	var good []imap.UID
	for _, uid := range uids {
		if uid == c.bad {
			c.attempts.Add(1)
			if err := fn(uid, nil, errors.New("malformed MIME")); err != nil {
				return err
			}
			continue
		}
		good = append(good, uid)
	}
	return c.inboxIMAP.FetchMessages(good, fn)
}
