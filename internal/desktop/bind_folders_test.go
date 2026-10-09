package desktop

import (
	"errors"
	"testing"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

func TestRenamedPath(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		to    string
		delim string
		want  string
	}{
		{name: "root level", path: "Projects", to: "Work", delim: "/", want: "Work"},
		{name: "nested keeps its parent", path: "Work/2026/Q1", to: "Q2", delim: "/", want: "Work/2026/Q2"},
		{name: "dot delimiter", path: "INBOX.Archive.Old", to: "Older", delim: ".", want: "INBOX.Archive.Older"},
		// a flat server has no hierarchy, so the whole name is the path.
		{name: "flat server", path: "Projects", to: "Work", delim: "", want: "Work"},
		// the delimiter appearing inside a name segment must not be treated as a
		// level, which is why only the last one splits.
		{name: "name containing the delimiter elsewhere", path: "a/b/c", to: "d", delim: "/", want: "a/b/d"},
		{name: "delimiter first", path: "/a", to: "b", delim: "/", want: "/b"},
		{name: "delimiter last", path: "a/", to: "b", delim: "/", want: "a/b"},
		{name: "multi-byte delimiter", path: "a::b::c", to: "d", delim: "::", want: "a::b::d"},
		{name: "multi-byte delimiter absent", path: "a:b", to: "d", delim: "::", want: "d"},
		{name: "multi-byte rune delimiter", path: "a\u203ab", to: "c", delim: "\u203a", want: "a\u203ac"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renamedPath(tt.path, tt.to, tt.delim); got != tt.want {
				t.Errorf("renamedPath(%q, %q, %q) = %q, want %q",
					tt.path, tt.to, tt.delim, got, tt.want)
			}
		})
	}
}

func TestProtectSpecialFolder(t *testing.T) {
	tests := []struct {
		name      string
		folder    storage.Folder
		protected bool
	}{
		{
			name:      "inbox by name",
			folder:    storage.Folder{Name: "INBOX", IMAPPath: "INBOX"},
			protected: true,
		},
		{
			name:      "sent by special-use attribute",
			folder:    storage.Folder{Name: "Verzonden", IMAPPath: "Verzonden", Attributes: []string{"\\Sent"}},
			protected: true,
		},
		{
			name:      "trash by name",
			folder:    storage.Folder{Name: "Trash", IMAPPath: "Trash"},
			protected: true,
		},
		{
			name:   "an ordinary folder is fair game",
			folder: storage.Folder{Name: "Projects", IMAPPath: "Projects"},
		},
		{
			// the role fallback matches names exactly, so this is not detected as
			// an archive and stays renameable. Tracked in #186; asserted here so
			// the behavior is deliberate rather than a surprise.
			name:   "localized archive is not detected as special",
			folder: storage.Folder{Name: "Archiv", IMAPPath: "Archiv"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := protectSpecialFolder(tt.folder)
			if tt.protected && !errors.Is(err, errFolderProtected) {
				t.Errorf("protectSpecialFolder(%q) = %v, want errFolderProtected", tt.folder.Name, err)
			}
			if !tt.protected && err != nil {
				t.Errorf("protectSpecialFolder(%q) = %v, want nil", tt.folder.Name, err)
			}
		})
	}
}

func TestCreateFolderTreeSplitsOnTheLastDelimiter(t *testing.T) {
	tests := []struct {
		name  string
		delim rune
		paths []string
		// want maps each stored name to its parent's IMAP path ("" for none).
		want map[string]string
	}{
		{
			name: "nested", delim: '/',
			paths: []string{"Work/2026/Q1", "Work", "Work/2026"},
			want:  map[string]string{"Work": "", "2026": "Work", "Q1": "Work/2026"},
		},
		{
			name: "flat server", delim: 0,
			paths: []string{"A/B"},
			want:  map[string]string{"A/B": ""},
		},
		{
			name: "no delimiter in the name", delim: '/',
			paths: []string{"Inbox"},
			want:  map[string]string{"Inbox": ""},
		},
		{
			name: "delimiter first leaves no parent", delim: '/',
			paths: []string{"/a"},
			want:  map[string]string{"a": ""},
		},
		{
			name: "delimiter last gives an empty name", delim: '/',
			paths: []string{"a", "a/"},
			want:  map[string]string{"a": "", "": "a"},
		},
		{
			name: "missing parent", delim: '.',
			paths: []string{"x.y.z"},
			want:  map[string]string{"z": ""},
		},
		{
			name: "multi-byte delimiter", delim: '›',
			paths: []string{"a›b›c", "a", "a›b"},
			want:  map[string]string{"a": "", "b": "a", "c": "a›b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newRoleTestApp(t)
			accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "a@example.com"})
			if err != nil {
				t.Fatalf("create account: %v", err)
			}
			folders := make([]pimap.Folder, 0, len(tt.paths))
			for _, p := range tt.paths {
				folders = append(folders, pimap.Folder{Name: p, Delimiter: tt.delim})
			}
			if err := a.createFolderTree(accountID, folders); err != nil {
				t.Fatalf("createFolderTree: %v", err)
			}
			rows, err := a.store.ListFolders(a.ctx, accountID)
			if err != nil {
				t.Fatalf("list folders: %v", err)
			}
			pathByID := map[int64]string{}
			for _, r := range rows {
				pathByID[r.ID] = r.IMAPPath
			}
			got := map[string]string{}
			for _, r := range rows {
				parent := ""
				if r.ParentID != nil {
					parent = pathByID[*r.ParentID]
				}
				got[r.Name] = parent
			}
			if len(got) != len(tt.want) {
				t.Fatalf("stored %v, want %v", got, tt.want)
			}
			for name, parent := range tt.want {
				if g, ok := got[name]; !ok || g != parent {
					t.Errorf("folder %q: parent %q (present %v), want %q; all: %v", name, g, ok, parent, got)
				}
			}
		})
	}
}
