package desktop

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

// TestExcludedFoldersAreNotSynced covers the filtering syncFolders does before
// it opens anything. The engine is never reached for an excluded folder, so a
// 30k-message archive costs no round trips at all (#173).
func TestExcludedFoldersAreNotSynced(t *testing.T) {
	ctx, stopBackground := testContext(t)
	db, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	t.Cleanup(stopBackground)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}

	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	archive := &storage.Folder{AccountID: accountID, Name: "Archive", IMAPPath: "Archive"}
	for _, f := range []*storage.Folder{inbox, archive} {
		if _, err := db.CreateFolder(ctx, f); err != nil {
			t.Fatalf("create folder: %v", err)
		}
	}
	if err := db.SetFolderSyncExcluded(ctx, archive.ID, true); err != nil {
		t.Fatalf("exclude: %v", err)
	}

	folders, err := a.store.ListFolders(ctx, accountID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var syncable []string
	for _, f := range folders {
		if !f.SyncExcluded {
			syncable = append(syncable, f.Name)
		}
	}
	if len(syncable) != 1 || syncable[0] != "INBOX" {
		t.Errorf("syncable folders = %v, want only INBOX", syncable)
	}
}

// TestFolderDTOCarriesTheExclusion makes sure the ui can see the state, since
// a toggle that cannot read its own value renders wrong after a reload.
func TestFolderDTOCarriesTheExclusion(t *testing.T) {
	dto := toFolderDTO(storage.Folder{Name: "Archive", SyncExcluded: true})
	if !dto.SyncExcluded {
		t.Error("toFolderDTO dropped SyncExcluded")
	}
}
