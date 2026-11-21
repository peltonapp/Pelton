package desktop

import (
	"testing"
)

func TestLocalFolderCreateRenameDeleteNeedNoServer(t *testing.T) {
	a := newTrashTestApp(t)
	local, err := a.store.EnsureLocalAccount(a.ctx)
	if err != nil {
		t.Fatalf("ensure local account: %v", err)
	}

	created, err := a.CreateFolder(CreateFolderRequest{AccountID: local.ID, Name: "Archive 2019"})
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	folders, err := a.store.ListFolders(a.ctx, local.ID)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(folders) != 1 || folders[0].Name != "Archive 2019" {
		t.Fatalf("folders = %+v, want the new folder only", folders)
	}

	if err := a.RenameFolder(folders[0].ID, "Archive 2020"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}
	renamed, err := a.store.GetFolder(a.ctx, folders[0].ID)
	if err != nil {
		t.Fatalf("GetFolder: %v", err)
	}
	if renamed.Name != "Archive 2020" || renamed.IMAPPath != "Archive 2020" {
		t.Errorf("renamed folder = %q at %q, want Archive 2020", renamed.Name, renamed.IMAPPath)
	}

	if err := a.DeleteFolder(folders[0].ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	left, err := a.store.ListFolders(a.ctx, local.ID)
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d folders left after delete, want 0", len(left))
	}
	if created.Name != "Archive 2019" {
		t.Errorf("created name = %q", created.Name)
	}
}
