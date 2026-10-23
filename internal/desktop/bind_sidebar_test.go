package desktop

import (
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func newSidebarTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
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

func TestOrderUnifiedViews(t *testing.T) {
	keysOf := func(views []unifiedView) string {
		out := make([]string, 0, len(views))
		for _, v := range views {
			out = append(out, v.key)
		}
		return strings.Join(out, ",")
	}
	builtin := keysOf(unifiedViewOrder)

	tests := []struct {
		name   string
		stored string
		want   string
	}{
		{name: "no stored order", stored: "", want: builtin},
		{
			name:   "full order",
			stored: "trash,junk,archive,sent,drafts,flagged,inbox",
			want:   "trash,junk,archive,sent,drafts,flagged,inbox",
		},
		// a partial order is what an install written by an older version would
		// have; the views it never heard of must still show up.
		{
			name:   "partial order keeps the rest at the end",
			stored: "sent,inbox",
			want:   "sent,inbox,flagged,drafts,archive,junk,trash",
		},
		{
			name:   "unknown and repeated keys are dropped",
			stored: "sent,nonsense,sent,inbox",
			want:   "sent,inbox,flagged,drafts,archive,junk,trash",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keysOf(orderUnifiedViews(tt.stored)); got != tt.want {
				t.Errorf("orderUnifiedViews(%q) = %q, want %q", tt.stored, got, tt.want)
			}
		})
	}
}

// a reorder that mixes accounts or parents would mean an imap move rather than a
// display change, so the binding refuses it even though the ui never offers it.
func TestReorderFoldersRefusesCrossGroup(t *testing.T) {
	a := newSidebarTestApp(t)
	ctx := a.ctx

	first, err := a.store.CreateAccount(ctx, &storage.Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	second, err := a.store.CreateAccount(ctx, &storage.Account{Email: "b@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	mk := func(accountID int64, path string, parent *int64) int64 {
		f := storage.Folder{AccountID: accountID, Name: path, IMAPPath: path, Delimiter: "/", ParentID: parent}
		if _, err := a.store.CreateFolder(ctx, &f); err != nil {
			t.Fatalf("create folder %q: %v", path, err)
		}
		return f.ID
	}

	rootA := mk(first, "Work", nil)
	rootB := mk(first, "Personal", nil)
	child := mk(first, "Work/2026", &rootA)
	otherAccount := mk(second, "Team", nil)

	if err := a.ReorderFolders([]int64{rootB, rootA}); err != nil {
		t.Fatalf("reorder siblings: %v", err)
	}
	if err := a.ReorderFolders([]int64{rootA, child}); !errors.Is(err, errCrossGroupReorder) {
		t.Errorf("reorder across parents = %v, want errCrossGroupReorder", err)
	}
	if err := a.ReorderFolders([]int64{rootA, otherAccount}); !errors.Is(err, errCrossGroupReorder) {
		t.Errorf("reorder across accounts = %v, want errCrossGroupReorder", err)
	}

	folders, err := a.store.ListFolders(ctx, first)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	if folders[0].IMAPPath != "Personal" {
		t.Errorf("after the accepted reorder folders[0] = %q, want Personal", folders[0].IMAPPath)
	}
}

func TestReorderUnifiedViewsRejectsUnknownKeys(t *testing.T) {
	a := newSidebarTestApp(t)

	if err := a.ReorderUnifiedViews([]string{"sent", "unicorn"}); err == nil {
		t.Fatal("reorder with an unknown key succeeded, want an error")
	}
	if err := a.ReorderUnifiedViews([]string{"sent", "sent"}); err == nil {
		t.Fatal("reorder with a repeated key succeeded, want an error")
	}

	// a valid partial reorder is stored complete, so the setting never holds a
	// list that silently hides a view.
	if err := a.ReorderUnifiedViews([]string{"trash", "inbox"}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	stored, err := a.store.Get(a.ctx, settingUnifiedViewOrder)
	if err != nil {
		t.Fatalf("read stored order: %v", err)
	}
	if want := "trash,inbox,flagged,drafts,sent,archive,junk"; stored != want {
		t.Errorf("stored order = %q, want %q", stored, want)
	}
}

func TestSetFolderPinnedRoundTrip(t *testing.T) {
	a := newSidebarTestApp(t)
	ctx := a.ctx

	accountID, err := a.store.CreateAccount(ctx, &storage.Account{Email: "a@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	f := storage.Folder{AccountID: accountID, Name: "Work", IMAPPath: "Work", Delimiter: "/"}
	if _, err := a.store.CreateFolder(ctx, &f); err != nil {
		t.Fatalf("create folder: %v", err)
	}

	if err := a.SetFolderPinned(f.ID, true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	pinned, err := a.ListPinnedFolders()
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	if len(pinned) != 1 || pinned[0].ID != f.ID || !pinned[0].Pinned {
		t.Fatalf("pinned = %+v, want the one folder marked pinned", pinned)
	}

	// pinning mirrors, it does not move: the folder is still in its own tree,
	// and the dto there reports the pinned state too.
	tree, err := a.ListFolders(accountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	if len(tree) != 1 || !tree[0].Pinned {
		t.Fatalf("tree = %+v, want the folder still present and marked pinned", tree)
	}

	if err := a.SetFolderPinned(f.ID, false); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	pinned, err = a.ListPinnedFolders()
	if err != nil {
		t.Fatalf("list pinned: %v", err)
	}
	if len(pinned) != 0 {
		t.Fatalf("pinned after unpin = %+v, want empty", pinned)
	}
}
