package desktop

import (
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// the unified Unread view lists the unread mail of every folder but trash and
// junk, once per message, and lets a message go once it is read.
func TestUnreadViewListsUnreadMailOnce(t *testing.T) {
	a := newTrashTestApp(t)
	accountID := seedAccount(t, a)
	inbox := seedFolder(t, a, accountID, "INBOX", "INBOX", nil, 0)
	all := seedFolder(t, a, accountID, "All Mail", "[Gmail]/All Mail", nil, 0)
	trash := seedFolder(t, a, accountID, "Trash", "[Gmail]/Trash", []string{"\\Trash"}, 0)

	insert := func(folderID int64, uid uint32, messageID string, flags storage.Flag) int64 {
		t.Helper()
		id, err := a.store.InsertMessage(a.ctx, &storage.Message{
			AccountID: accountID, FolderID: folderID, UID: uid,
			MessageID: messageID, Subject: messageID,
			Date: time.Now().UTC(), Flags: flags,
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	fresh := insert(inbox.ID, 1, "<fresh@example.test>", 0)
	insert(all.ID, 2, "<fresh@example.test>", 0)
	insert(inbox.ID, 3, "<read@example.test>", storage.FlagSeen)
	insert(trash.ID, 4, "<binned@example.test>", 0)

	unread := func() int {
		t.Helper()
		q, err := a.viewQuery(a.ctx, viewUnread)
		if err != nil {
			t.Fatal(err)
		}
		n, err := a.store.CountMessages(a.ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := unread(); n != 1 {
		t.Fatalf("Unread shows %d messages, want 1", n)
	}

	if err := a.SetSeen(fresh, true); err != nil {
		t.Fatal(err)
	}
	if n := unread(); n != 0 {
		t.Fatalf("Unread still shows %d messages after reading", n)
	}
}
