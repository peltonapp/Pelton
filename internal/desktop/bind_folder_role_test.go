package desktop

import (
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func newRoleTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler)}
}

// makeFolder creates an account and one folder, returning the folder.
func makeFolder(t *testing.T, a *App, f storage.Folder) storage.Folder {
	t.Helper()
	accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	f.AccountID = accountID
	if _, err := a.store.CreateFolder(a.ctx, &f); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	return f
}

// The bug: a server that reports no special-use attribute and calls its archive
// something other than "Archive" leaves the folder classified as normal, so the
// unified Archive view never gathers it.
func TestLocalizedFolderIsNotDetected(t *testing.T) {
	f := storage.Folder{Name: "Archiv", IMAPPath: "Archiv", Delimiter: "/"}
	if got := folderRole(f); got != roleNormal {
		t.Fatalf("folderRole(Archiv) = %q, want %q; the test's premise is gone", got, roleNormal)
	}
}

// Assigning the role by hand is the escape hatch, and it has to survive a round
// trip through storage rather than living in the ui.
func TestAssignedRoleOverridesDetection(t *testing.T) {
	a := newRoleTestApp(t)
	f := makeFolder(t, a, storage.Folder{Name: "Archiv", IMAPPath: "Archiv", Delimiter: "/"})

	if err := a.SetFolderRole(f.ID, roleArchive); err != nil {
		t.Fatalf("set role: %v", err)
	}

	folders, err := a.ListFolders(f.AccountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	if len(folders) != 1 {
		t.Fatalf("got %d folders, want 1", len(folders))
	}
	if folders[0].Role != roleArchive {
		t.Errorf("Role = %q, want %q", folders[0].Role, roleArchive)
	}
	// the ui needs the assignment itself, not just the resolved role, to show
	// which entry is ticked.
	if folders[0].RoleOverride != roleArchive {
		t.Errorf("RoleOverride = %q, want %q", folders[0].RoleOverride, roleArchive)
	}
}

// An assignment wins over the server's own attribute too: a server that flags
// the wrong mailbox is exactly the case the user needs to correct.
func TestAssignedRoleBeatsSpecialUse(t *testing.T) {
	f := storage.Folder{
		Name:         "Whatever",
		IMAPPath:     "Whatever",
		Attributes:   []string{"\\Sent"},
		RoleOverride: roleArchive,
	}
	if got := folderRole(f); got != roleArchive {
		t.Errorf("folderRole() = %q, want the assigned %q", got, roleArchive)
	}
}

// Assigning "normal" is meaningful rather than redundant: it forces a folder
// out of a role detection gave it wrongly.
func TestAssignedNormalClearsADetectedRole(t *testing.T) {
	f := storage.Folder{
		Name:         "Archive",
		IMAPPath:     "Archive",
		RoleOverride: roleNormal,
	}
	if got := folderRole(f); got != roleNormal {
		t.Errorf("folderRole() = %q, want %q", got, roleNormal)
	}
}

// Clearing the assignment hands the folder back to detection rather than
// leaving it stuck on whatever was chosen.
func TestClearingTheRoleRestoresDetection(t *testing.T) {
	a := newRoleTestApp(t)
	f := makeFolder(t, a, storage.Folder{Name: "Archive", IMAPPath: "Archive", Delimiter: "/"})

	if err := a.SetFolderRole(f.ID, roleJunk); err != nil {
		t.Fatalf("set role: %v", err)
	}
	if err := a.SetFolderRole(f.ID, ""); err != nil {
		t.Fatalf("clear role: %v", err)
	}

	folders, err := a.ListFolders(f.AccountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	if folders[0].RoleOverride != "" {
		t.Errorf("RoleOverride = %q, want it cleared", folders[0].RoleOverride)
	}
	// back to the name fallback, which does recognise this one.
	if folders[0].Role != roleArchive {
		t.Errorf("Role = %q, want detection to resolve %q again", folders[0].Role, roleArchive)
	}
}

func TestUnknownRoleIsRefused(t *testing.T) {
	a := newRoleTestApp(t)
	f := makeFolder(t, a, storage.Folder{Name: "Work", IMAPPath: "Work", Delimiter: "/"})

	if err := a.SetFolderRole(f.ID, "banana"); !errors.Is(err, errUnknownRole) {
		t.Errorf("SetFolderRole(banana) = %v, want errUnknownRole", err)
	}
	// inbox is not assignable: every account has exactly one, named by the
	// protocol, and a second would break the unified inbox.
	if err := a.SetFolderRole(f.ID, roleInbox); !errors.Is(err, errUnknownRole) {
		t.Errorf("SetFolderRole(inbox) = %v, want errUnknownRole", err)
	}
}

// A stored value that is no longer assignable must not resurrect itself as a
// role, or a downgrade would leave folders classified by a rule that is gone.
func TestUnknownStoredOverrideIsIgnored(t *testing.T) {
	f := storage.Folder{Name: "Work", IMAPPath: "Work", RoleOverride: "banana"}
	if got := folderRole(f); got != roleNormal {
		t.Errorf("folderRole() = %q, want %q", got, roleNormal)
	}
}

// A server that reports \Inbox must win over a path that is not literally INBOX,
// so special-use detection covers providers that expose a renamed inbox.
func TestInboxAttributeBeatsPathFallback(t *testing.T) {
	f := storage.Folder{
		Name:       "Mb_inbox",
		IMAPPath:   "Mb_inbox",
		Attributes: []string{`\Inbox`},
	}
	if got := folderRole(f); got != roleInbox {
		t.Errorf("folderRole() = %q, want %q", got, roleInbox)
	}
}
