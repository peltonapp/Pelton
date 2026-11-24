package desktop

import (
	"errors"
	"sort"
	"testing"

	goimap "github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

func storedPaths(t *testing.T, a *App, accountID int64) []string {
	t.Helper()
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range folders {
		out = append(out, f.IMAPPath)
	}
	sort.Strings(out)
	return out
}

func listed(paths ...string) []pimap.Folder {
	out := make([]pimap.Folder, len(paths))
	for i, p := range paths {
		out[i] = pimap.Folder{Name: p, Delimiter: '/'}
	}
	return out
}

// a folder deleted in webmail used to stay in the tree and fail every sync
// after, which marked the whole mailbox as failing (#484).
func TestRefreshFoldersDropsAFolderGoneFromTheServer(t *testing.T) {
	a := newTrashTestApp(t)
	accountID := seedAccount(t, a)
	seedFolder(t, a, accountID, "INBOX", "INBOX", nil, 2)
	old := seedFolder(t, a, accountID, "Old", "Old", nil, 3)

	changed, err := a.refreshFolders(&fakeIMAP{folders: listed("INBOX")}, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("refresh reported no change")
	}
	if got := storedPaths(t, a, accountID); len(got) != 1 || got[0] != "INBOX" {
		t.Fatalf("folders = %v, want only INBOX", got)
	}
	if _, err := a.store.GetFolder(a.ctx, old.ID); err == nil {
		t.Error("the deleted folder is still stored")
	}
}

func TestRefreshFoldersAddsAFolderNewOnTheServer(t *testing.T) {
	a := newTrashTestApp(t)
	accountID := seedAccount(t, a)
	seedFolder(t, a, accountID, "INBOX", "INBOX", nil, 0)
	projects := seedFolder(t, a, accountID, "Projects", "Projects", nil, 0)

	changed, err := a.refreshFolders(&fakeIMAP{folders: listed("INBOX", "Projects", "Projects/2026")}, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("refresh reported no change")
	}
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	var added *storage.Folder
	for i := range folders {
		if folders[i].IMAPPath == "Projects/2026" {
			added = &folders[i]
		}
	}
	if added == nil {
		t.Fatalf("new folder not added: %v", storedPaths(t, a, accountID))
	}
	if added.ParentID == nil || *added.ParentID != projects.ID || added.Name != "2026" {
		t.Errorf("new folder = %+v, want 2026 under Projects", added)
	}
}

func TestRefreshFoldersLeavesAMatchingTreeAlone(t *testing.T) {
	a := newTrashTestApp(t)
	accountID := seedAccount(t, a)
	seedFolder(t, a, accountID, "INBOX", "INBOX", nil, 1)
	seedFolder(t, a, accountID, "Sent", "Sent", nil, 1)

	changed, err := a.refreshFolders(&fakeIMAP{folders: listed("INBOX", "Sent")}, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("refresh reported a change on a matching tree")
	}
}

// a list that cannot be right, or a parent the server leaves out while still
// listing its children, must not cost the user any folders.
func TestRefreshFoldersKeepsFoldersOnADoubtfulList(t *testing.T) {
	cases := []struct {
		name   string
		client *fakeIMAP
		want   []string
	}{
		{"list failed", &fakeIMAP{failList: errors.New("connection reset")}, []string{"INBOX", "Work", "Work/Q3"}},
		{"no INBOX listed", &fakeIMAP{folders: listed("Work")}, []string{"INBOX", "Work", "Work/Q3"}},
		{"parent left out", &fakeIMAP{folders: listed("INBOX", "Work/Q3")}, []string{"INBOX", "Work", "Work/Q3"}},
		{"listed as nonexistent", &fakeIMAP{folders: []pimap.Folder{
			{Name: "INBOX", Delimiter: '/'},
			{Name: "Work", Delimiter: '/', Attrs: []goimap.MailboxAttr{goimap.MailboxAttrNonExistent}},
			{Name: "Work/Q3", Delimiter: '/'},
		}}, []string{"INBOX", "Work", "Work/Q3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTrashTestApp(t)
			accountID := seedAccount(t, a)
			var parent *int64
			for _, f := range []struct{ name, path string }{{"INBOX", "INBOX"}, {"Work", "Work"}, {"Q3", "Work/Q3"}} {
				id, err := a.store.CreateFolder(a.ctx, &storage.Folder{
					AccountID: accountID, Name: f.name, IMAPPath: f.path, Delimiter: "/", ParentID: parent,
				})
				if err != nil {
					t.Fatal(err)
				}
				if f.name == "Work" {
					parent = &id
				}
			}

			_, _ = a.refreshFolders(tc.client, accountID)
			got := storedPaths(t, a, accountID)
			if len(got) != len(tc.want) {
				t.Fatalf("folders = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("folders = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// the whole path the reporter hit: a folder deleted in webmail is dropped on the
// next sync instead of being opened, and the sync goes through.
func TestSyncSkipsAFolderDeletedOnTheServer(t *testing.T) {
	client := &fakeIMAP{
		folders:    listed("INBOX"),
		failSelect: map[string]error{"Old": errors.New("NO Mailbox doesn't exist: Old")},
	}
	a, account := sendingAccount(t, client)
	seedFolder(t, a, account.ID, "INBOX", "INBOX", nil, 0)
	seedFolder(t, a, account.ID, "Old", "Old", nil, 0)

	if err := a.TriggerSync(); err != nil {
		t.Fatalf("TriggerSync: %v", err)
	}
	for _, m := range client.selected {
		if m == "Old" {
			t.Error("the sync still opened the deleted folder")
		}
	}
	if got := storedPaths(t, a, account.ID); len(got) != 1 || got[0] != "INBOX" {
		t.Errorf("folders = %v, want only INBOX", got)
	}
}

// with every account failing for something other than a password, the sync
// used to say "no credentials", which pointed the reporter of #484 the wrong
// way.
func TestTriggerSyncReportsTheRealFailure(t *testing.T) {
	broken := errors.New("NO [SERVERBUG] try again later")
	client := &fakeIMAP{
		folders:    listed("INBOX"),
		failSelect: map[string]error{"INBOX": broken},
	}
	a, account := sendingAccount(t, client)
	seedFolder(t, a, account.ID, "INBOX", "INBOX", nil, 0)
	// The failing step is retried before the sync gives up; the real
	// backoff only made the test wait 1.5 s.
	useFastSyncRetryBackoff(t)

	err := a.TriggerSync()
	if errors.Is(err, errNoCredentials) {
		t.Fatalf("TriggerSync = %v, a missing password", err)
	}
	if !errors.Is(err, broken) {
		t.Errorf("TriggerSync = %v, want the server's error", err)
	}
}
