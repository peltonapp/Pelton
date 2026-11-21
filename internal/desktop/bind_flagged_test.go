package desktop

import (
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// one starred gmail message arrives once per label it carries; the Flagged
// view has to show it once and let it go once it is unflagged (#487).
func TestFlaggedViewShowsAGmailMessageOnce(t *testing.T) {
	a := newTrashTestApp(t)
	accountID := seedAccount(t, a)
	var copies []int64
	for i, f := range []struct{ name, path string }{
		{"INBOX", "INBOX"},
		{"All Mail", "[Gmail]/All Mail"},
		{"Starred", "[Gmail]/Starred"},
		{"Important", "[Gmail]/Important"},
	} {
		folder := seedFolder(t, a, accountID, f.name, f.path, nil, 0)
		id, err := a.store.InsertMessage(a.ctx, &storage.Message{
			AccountID: accountID, FolderID: folder.ID, UID: uint32(i + 1),
			MessageID: "<starred@example.test>", Subject: "starred",
			Date: time.Now().UTC(), Flags: storage.FlagFlagged,
		})
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, id)
	}

	flagged := func() int {
		t.Helper()
		q, err := a.viewQuery(a.ctx, viewFlagged)
		if err != nil {
			t.Fatal(err)
		}
		n, err := a.store.CountMessages(a.ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := flagged(); n != 1 {
		t.Fatalf("Flagged shows %d messages, want 1", n)
	}

	if err := a.SetFlagged(copies[0], false); err != nil {
		t.Fatal(err)
	}
	if n := flagged(); n != 0 {
		t.Fatalf("Flagged still shows %d messages after unflagging", n)
	}

	if err := a.SetFlagged(copies[2], true); err != nil {
		t.Fatal(err)
	}
	if n := flagged(); n != 1 {
		t.Fatalf("Flagged shows %d messages after flagging again, want 1", n)
	}
}
