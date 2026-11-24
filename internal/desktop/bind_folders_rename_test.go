package desktop

import (
	"errors"
	"testing"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Rename builds the new path from the stored path and delimiter. No LIST is
// consulted, so a failed one cannot send RENAME "Projects.Pelton" "Old" and
// move a nested folder to the top level.
func TestRenameFolderUsesTheStoredPath(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failList error
	}{
		{"list works", nil},
		{"list fails", errors.New("LIST refused")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, db, ctx := moveTestApp(t)
			inbox, _, _ := moveTestAccount(t, db, ctx, false)
			client := &fakeIMAP{failList: tc.failList}
			useFake(t, a, inbox.AccountID, client)

			projects := makeDotFolder(t, db, inbox.AccountID, "Projects", "Projects", nil, "")
			pelton := makeDotFolder(t, db, inbox.AccountID, "Pelton", "Projects.Pelton", &projects.ID, "")
			child := makeDotFolder(t, db, inbox.AccountID, "Done", "Projects.Pelton.Done", &pelton.ID, roleArchive)

			if err := a.RenameFolder(pelton.ID, "Old"); err != nil {
				t.Fatalf("RenameFolder: %v", err)
			}
			if want := [2]string{"Projects.Pelton", "Projects.Old"}; len(client.renamed) != 1 || client.renamed[0] != want {
				t.Fatalf("renamed %v, want [%v]", client.renamed, want)
			}
			got := folderByID(t, db, pelton.ID)
			if got.IMAPPath != "Projects.Old" || got.Name != "Old" {
				t.Errorf("renamed row = %q (%q), want Projects.Old (Old)", got.IMAPPath, got.Name)
			}
			// The child keeps its row, so its role and its cached mail stay.
			moved := folderByID(t, db, child.ID)
			if moved.IMAPPath != "Projects.Old.Done" {
				t.Errorf("child path = %q, want Projects.Old.Done", moved.IMAPPath)
			}
			if folderRole(moved) != roleArchive {
				t.Errorf("child role = %q, want %q", folderRole(moved), roleArchive)
			}
		})
	}
}

// After the rename the server lists the new names. The folder tree refresh
// must find the renamed rows by path, so they keep their id and assigned
// role instead of being dropped and recreated.
func TestRenamedFolderKeepsItsRoleThroughARefresh(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _, _ := moveTestAccount(t, db, ctx, false)
	client := &fakeIMAP{}
	useFake(t, a, inbox.AccountID, client)
	// A folder named like a special one, assigned "normal" by hand so it
	// may be renamed.
	sent := makeDotFolder(t, db, inbox.AccountID, "Sent", "Sent", nil, roleNormal)
	box := makeDotFolder(t, db, inbox.AccountID, "Box", "Box", nil, "")
	child := makeDotFolder(t, db, inbox.AccountID, "Spam", "Box.Spam", &box.ID, roleJunk)

	if err := a.RenameFolder(sent.ID, "Old sent"); err != nil {
		t.Fatalf("RenameFolder(sent): %v", err)
	}
	if err := a.RenameFolder(box.ID, "Crate"); err != nil {
		t.Fatalf("RenameFolder(box): %v", err)
	}
	client.folders = []pimap.Folder{
		{Name: "INBOX", Delimiter: '.'}, {Name: "Archive", Delimiter: '.'},
		{Name: "Old sent", Delimiter: '.'}, {Name: "Crate", Delimiter: '.'}, {Name: "Crate.Spam", Delimiter: '.'},
	}
	if _, err := a.refreshFolders(client, inbox.AccountID); err != nil {
		t.Fatalf("refreshFolders: %v", err)
	}
	for _, tc := range []struct {
		id   int64
		path string
		role string
	}{
		{sent.ID, "Old sent", roleNormal},
		{child.ID, "Crate.Spam", roleJunk},
	} {
		f := folderByID(t, db, tc.id)
		if f.IMAPPath != tc.path || f.RoleOverride != tc.role {
			t.Errorf("folder %d = %q role %q, want %q role %q", tc.id, f.IMAPPath, f.RoleOverride, tc.path, tc.role)
		}
	}
}

// Create joins the parent's stored path and delimiter. A failing LIST does not
// matter, and a flat parent refuses a child rather than guessing "/".
func TestCreateFolderUsesTheParentsDelimiter(t *testing.T) {
	for _, tc := range []struct {
		name, delim, wantPath string
		wantErr               bool
	}{
		{"dot server", ".", "Projects.New", false},
		{"flat server", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, db, ctx := moveTestApp(t)
			inbox, _, _ := moveTestAccount(t, db, ctx, false)
			client := &fakeIMAP{failList: errors.New("LIST refused")}
			useFake(t, a, inbox.AccountID, client)
			parent := makeFolderWithDelim(t, db, inbox.AccountID, "Projects", "Projects", nil, "", tc.delim)

			dto, err := a.CreateFolder(CreateFolderRequest{AccountID: inbox.AccountID, ParentID: parent.ID, Name: "New"})
			if tc.wantErr {
				if err == nil || len(client.created) != 0 {
					t.Fatalf("CreateFolder = %+v, %v (sent %v), want a refusal", dto, err, client.created)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateFolder: %v", err)
			}
			if len(client.created) != 1 || client.created[0] != tc.wantPath {
				t.Fatalf("created %v, want [%s]", client.created, tc.wantPath)
			}
			if got := folderByID(t, db, dto.ID); got.IMAPPath != tc.wantPath || got.Delimiter != tc.delim {
				t.Errorf("stored %q delim %q, want %q delim %q", got.IMAPPath, got.Delimiter, tc.wantPath, tc.delim)
			}
		})
	}
}

func makeDotFolder(t *testing.T, db *storage.DB, accountID int64, name, path string, parent *int64, role string) storage.Folder {
	t.Helper()
	return makeFolderWithDelim(t, db, accountID, name, path, parent, role, ".")
}

func makeFolderWithDelim(t *testing.T, db *storage.DB, accountID int64, name, path string, parent *int64, role, delim string) storage.Folder {
	t.Helper()
	f := storage.Folder{AccountID: accountID, Name: name, IMAPPath: path, RemoteID: path, Delimiter: delim, ParentID: parent}
	id, err := db.CreateFolder(t.Context(), &f)
	if err != nil {
		t.Fatalf("create folder %q: %v", path, err)
	}
	f.ID = id
	if role != "" {
		if err := db.SetFolderRoleOverride(t.Context(), id, role); err != nil {
			t.Fatalf("set role: %v", err)
		}
		f.RoleOverride = role
	}
	return f
}

func folderByID(t *testing.T, db *storage.DB, id int64) storage.Folder {
	t.Helper()
	f, err := db.GetFolder(t.Context(), id)
	if err != nil {
		t.Fatalf("get folder %d: %v", id, err)
	}
	return *f
}
