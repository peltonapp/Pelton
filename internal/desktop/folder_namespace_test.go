package desktop

import (
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Folders a server keeps under an INBOX. namespace with no special-use
// attributes get their role from the name after the namespace.
func TestFolderRoleInsideInboxNamespace(t *testing.T) {
	for _, tc := range []struct {
		path, delim, want string
	}{
		{"INBOX.Sent", ".", roleSent},
		{"INBOX.Drafts", ".", roleDrafts},
		{"INBOX.Trash", ".", roleTrash},
		{"INBOX/Sent", "/", roleSent},
		{"INBOX.Clients.Sent", ".", roleNormal},
		{"INBOX", ".", roleInbox},
	} {
		f := storage.Folder{IMAPPath: tc.path, Delimiter: tc.delim}
		if got := folderRole(f); got != tc.want {
			t.Errorf("folderRole(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
