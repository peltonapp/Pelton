package desktop

import (
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestFoldersInSyncOrderInboxSentFirst(t *testing.T) {
	folders := []storage.Folder{
		{ID: 3, Name: "Junk", IMAPPath: "Junk"},
		{ID: 1, Name: "INBOX", IMAPPath: "INBOX"},
		{ID: 4, Name: "Archive", IMAPPath: "Archive"},
		{ID: 2, Name: "Sent Items", IMAPPath: "Sent Items", Attributes: []string{`\Sent`}},
		{ID: 5, Name: "Hidden", IMAPPath: "Nope", SyncExcluded: true},
	}
	got := foldersInSyncOrder(folders)
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4 selectable folders", len(got))
	}
	if got[0].IMAPPath != "INBOX" {
		t.Fatalf("first = %q, want INBOX", got[0].IMAPPath)
	}
	if got[1].IMAPPath != "Sent Items" {
		t.Fatalf("second = %q, want Sent Items", got[1].IMAPPath)
	}
	for _, f := range got[2:] {
		role := folderRole(f)
		if role == roleInbox || role == roleSent {
			t.Fatalf("folder %q with role %q must not follow Sent", f.IMAPPath, role)
		}
	}
}
