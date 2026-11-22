package desktop

import (
	"fmt"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/storage"
)

// A database upgraded from a release without the sync window holds more mail
// per folder than the window fetches. The window only limits what comes from
// the server: after a sync every stored message is still in the list, and
// search finds the old ones.
func TestUpgradedMailOutsideTheWindowStaysListedAndSearchable(t *testing.T) {
	const stored = 300
	client := &pagingIMAP{n: stored}
	a, account, inbox := reconcileInbox(t, client)
	inbox.ID = mustFolderID(t, a, account.ID, "INBOX")
	setParallel(t, a, 1)
	if err := a.store.SetInt(a.ctx, settingSyncMessageLimit, 100); err != nil {
		t.Fatal(err)
	}
	if err := credentials.Store(account.ID, credentials.Secret{Method: credentials.MethodPassword, Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credentials.Delete(account.ID) })

	// what migration 0041 leaves behind: complete rows, the folder's
	// UIDVALIDITY and last seen uid as the old build stored them, no floor.
	for uid := 1; uid <= stored; uid++ {
		if _, err := a.store.InsertMessage(a.ctx, &storage.Message{
			AccountID: account.ID, FolderID: inbox.ID, UID: uint32(uid),
			Subject: fmt.Sprintf("s%d", uid), BodyPlain: fmt.Sprintf("stored body %d", uid),
			Date: time.Unix(int64(uid)*60, 0), BodyComplete: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.store.SetFolderUIDValidity(a.ctx, inbox.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetFolderLastSeenUID(a.ctx, inbox.ID, stored); err != nil {
		t.Fatal(err)
	}
	idx, err := openSearchIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	a.index = idx

	if err := a.syncAccount(account, true); err != nil {
		t.Fatalf("sync: %v", err)
	}
	waitUntil(t, 10*time.Second, "sync tasks never ended", func() bool {
		st := a.accountSync(account.ID).sched.Stats()
		return st.Fast+st.Normal+st.Running == 0
	})

	list, err := a.ListMessages(ListMessagesRequest{Kind: "folder", FolderID: inbox.ID, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != stored || len(list.Messages) != stored {
		t.Fatalf("list shows %d of %d messages, want all %d", len(list.Messages), list.Total, stored)
	}
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	// uid 3 is far below the newest 100.
	if got := searchIDs(t, a, "s3"); len(got) != 1 {
		t.Fatalf("search for the old message found %v, want one hit", got)
	}
	if client.flagsCalls == 0 {
		t.Fatal("the sync never listed the folder: the check proves nothing")
	}
	if fetched := client.opsSnapshot(); len(fetched) != 0 {
		t.Errorf("sync fetched %v, want nothing: every message was stored already", fetched)
	}
}
