package desktop

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// inboxIMAP is an INBOX whose uids can grow between lists. It is safe for
// the several sessions of one account. onList runs after each full list.
type inboxIMAP struct {
	fakeIMAP
	mu     sync.Mutex
	uids   []imap.UID
	onList func()
}

func (c *inboxIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := imap.UID(1)
	if n := len(c.uids); n > 0 {
		next = c.uids[n-1] + 1
	}
	return &pimap.Mailbox{Name: mailbox, UIDValidity: 1, UIDNext: next, NumMessages: uint32(len(c.uids))}, nil
}

func (c *inboxIMAP) FetchAllFlags() ([]pimap.MessageHeader, error) {
	c.mu.Lock()
	out := make([]pimap.MessageHeader, 0, len(c.uids))
	for _, uid := range c.uids {
		out = append(out, pimap.MessageHeader{UID: uid})
	}
	hook := c.onList
	c.onList = nil
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
	return out, nil
}

func (c *inboxIMAP) FetchHeaders(uids []imap.UID, _ *imap.FetchOptions) ([]pimap.MessageHeader, error) {
	out := make([]pimap.MessageHeader, 0, len(uids))
	for _, uid := range uids {
		out = append(out, pimap.MessageHeader{UID: uid, Subject: "hi", From: "ada@example.com", Date: time.Now(), HasEnvelope: true})
	}
	return out, nil
}

func (c *inboxIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	for _, uid := range uids {
		if err := fn(uid, &pimap.Message{UID: uid, Subject: "hi", From: "ada@example.com", Date: time.Now(), Text: "body"}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *inboxIMAP) SearchAllUIDs() ([]imap.UID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.uids), nil
}

func (c *inboxIMAP) add(uid imap.UID) {
	c.mu.Lock()
	c.uids = append(c.uids, uid)
	c.mu.Unlock()
}

// listWhole lists folder's window in one call on the task's session, the way
// a sync's last page does, and returns the ids that need a body.
func listWhole(ctx context.Context, a *App, account storage.Account, folder storage.Folder) ([]string, error) {
	ids, _, err := a.imapListPage(ctx, account, folder, &psync.StubPager{})
	return ids, err
}

// notifyRig is an INBOX account with notifications on, recording the
// message ids notified.
func notifyRig(t *testing.T, client *inboxIMAP) (*App, storage.Account, storage.Folder, chan int64) {
	t.Helper()
	a, account, inbox := reconcileInbox(t, client)
	if err := a.store.SetBool(a.ctx, settingNotifyNewMail, true); err != nil {
		t.Fatal(err)
	}
	notified := make(chan int64, 16)
	a.notificationForTest = func(n notification) { notified <- n.messageID }
	return a, account, inbox, notified
}

// inTask runs fn as a normal-lane task of the account and waits for it.
func inTask(t *testing.T, a *App, account storage.Account, folderID int64, fn func(ctx context.Context) error) {
	t.Helper()
	if err := a.runPooled(a.ctx, account, poolTask{Kind: 0, FolderID: folderID, Run: func(ctx context.Context, _ mailClient) error {
		return fn(ctx)
	}}); err != nil {
		t.Fatalf("task: %v", err)
	}
}

// Only mail new in the list that found it notifies. A backfill and a stub
// left without its body by an earlier run never do.
func TestOnlyFreshMailNotifies(t *testing.T) {
	for _, tc := range []struct {
		name string
		// stub stores uid 5 as a body-less stub before the list.
		stub     bool
		backfill int
		want     bool
	}{
		{name: "new in the list", want: true},
		{name: "old stub refilled", stub: true},
		{name: "backfill page", backfill: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &inboxIMAP{uids: []imap.UID{5}}
			a, account, inbox, notified := notifyRig(t, client)
			if tc.stub {
				if _, err := a.store.UpsertMessageListMeta(a.ctx, &storage.Message{
					AccountID: account.ID, FolderID: inbox.ID, UID: 5, Subject: "hi", Date: time.Now(),
				}); err != nil {
					t.Fatal(err)
				}
			}
			inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
				var ids []string
				var err error
				if tc.backfill > 0 {
					ids, _, err = a.imapOlderStep(ctx, account, inbox, newOlderList(tc.backfill, 50, 500))
				} else {
					ids, err = listWhole(ctx, a, account, inbox)
				}
				if err != nil {
					return err
				}
				if !slices.Contains(ids, "5") {
					t.Errorf("list returned %v, want uid 5 to fetch", ids)
				}
				return a.execIMAPBodies(ctx, account, inbox, ids, false)
			})
			if tc.want {
				recv(t, notified, 2*time.Second, "new mail raised no notification")
				return
			}
			noRecv(t, notified, 200*time.Millisecond, "old mail raised a notification")
		})
	}
}

// A list asked for while "load older" holds the folder is folded into the
// older listing, which lists once more. Mail that arrived meanwhile is new
// and notifies, though the listing it came in is old mail; the older mail
// it admitted does not.
func TestNewMailFoldedIntoAnOlderListingNotifies(t *testing.T) {
	client := &inboxIMAP{uids: []imap.UID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}
	a, account, inbox, notified := notifyRig(t, client)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 5); err != nil {
		t.Fatal(err)
	}
	rt := mustAccountSync(t, a, account.ID)
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		ids, err := listWhole(ctx, a, account, inbox)
		// The window's mail is not what this test is about.
		rt.takeFresh(inbox.ID, ids)
		return err
	})
	client.mu.Lock()
	client.onList = func() {
		// While the older listing lists, a push asks for the folder and mail
		// 11 arrives.
		if rt.shared.claimFolderList(inbox.ID) != 0 {
			t.Error("the older listing did not hold the folder")
		}
		client.add(11)
	}
	client.mu.Unlock()
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		older := newOlderList(5, 50, 500)
		var all []string
		for {
			ids, more, err := a.imapOlderStep(ctx, account, inbox, older)
			if err != nil {
				return err
			}
			all = append(all, ids...)
			if !more {
				break
			}
		}
		if !slices.Contains(all, "11") {
			t.Errorf("the older listing returned %v, want uid 11 to fetch", all)
		}
		return a.execIMAPBodies(ctx, account, inbox, all, true)
	})
	id := recv(t, notified, 2*time.Second, "mail found by the folded list raised no notification")
	if m := mustGetMessage(t, a.store, id); m.UID != 11 {
		t.Fatalf("notified uid %d, want 11", m.UID)
	}
	noRecv(t, notified, 200*time.Millisecond, "older mail raised a notification")
}

// refusingFlagsIMAP is inboxIMAP on a server that refuses every flag change.
type refusingFlagsIMAP struct{ inboxIMAP }

func (*refusingFlagsIMAP) AddFlags(imap.UID, ...imap.Flag) error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "read-only mailbox"}
}

// A server refusing to take a local flag change is not a failed list: the
// change stays pending for the next sync, and the list's result stands.
func TestRefusedFlagPushIsNotAFailedList(t *testing.T) {
	client := &refusingFlagsIMAP{inboxIMAP{uids: []imap.UID{5}}}
	a, account, inbox := reconcileInbox(t, client)
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		_, err := listWhole(ctx, a, account, inbox)
		return err
	})
	id, err := a.store.MessageIDByRemoteID(a.ctx, inbox.ID, "5")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.MarkFlagsPending(a.ctx, id, storage.FlagSeen); err != nil {
		t.Fatal(err)
	}
	inTask(t, a, account, inbox.ID, func(ctx context.Context) error {
		if _, err := listWhole(ctx, a, account, inbox); err != nil {
			t.Errorf("list with a refused flag push failed: %v", err)
		}
		return nil
	})
}
