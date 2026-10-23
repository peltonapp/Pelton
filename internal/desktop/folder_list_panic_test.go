package desktop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/storage"
)

// A list that panics ends its unit (the scheduler recovers the panic) and
// lets go of its folder: the folder claims are the account's and outlive the
// runtime, so a claim left behind would keep every later list of the folder
// out, in a replacement runtime too. Each listing path is covered.
func TestPanickedListLetsGoOfItsFolder(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(ctx context.Context, a *App, account storage.Account, inbox storage.Folder) error
	}{
		{"listFolderOnce", func(ctx context.Context, a *App, account storage.Account, inbox storage.Folder) error {
			return a.listFolderOnce(ctx, account.ID, inbox.ID, func() error { panic("list blew up") })
		}},
		{"list page", func(ctx context.Context, a *App, account storage.Account, inbox storage.Folder) error {
			_, err := listWhole(ctx, a, account, inbox)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &inboxIMAP{uids: []imap.UID{1, 2}}
			a, account, inbox := reconcileInbox(t, client)
			client.onList = func() { panic("list blew up") }
			mustAccountSync(t, a, account.ID)
			err := a.runPooled(a.ctx, account, poolTask{FolderID: inbox.ID, Run: func(ctx context.Context, _ mailClient) error {
				return tc.run(ctx, a, account, inbox)
			}})
			if err == nil || !strings.Contains(err.Error(), "panicked") {
				t.Fatalf("task err = %v, want the recovered panic", err)
			}

			a.stopAccountScheduler(account.ID, false)
			mustAccountSync(t, a, account.ID)
			ctx, cancel := context.WithTimeout(a.ctx, 2*time.Second)
			defer cancel()
			if _, err := a.accountShared(account.ID).awaitFolderList(ctx, inbox.ID); err != nil {
				t.Fatalf("the replacement runtime could not claim the folder after the panicked list: %v", err)
			}
		})
	}
}
