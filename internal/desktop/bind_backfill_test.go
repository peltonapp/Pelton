package desktop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

func TestFetchOlderEnqueuesStubThenBody(t *testing.T) {
	folder := storage.Folder{ID: 1, Name: "INBOX", IMAPPath: "INBOX"}

	var mu sync.Mutex
	var order []syncsched.JobKind
	bodiesStarted := make(chan struct{})

	s := syncsched.New(1)
	s.Start(context.Background(), pool.New(3), 0)
	t.Cleanup(s.Stop)

	done := make(chan error, 1)
	enqueueDeltaSync(s, []storage.Folder{folder}, func(ctx context.Context, f storage.Folder, kind syncsched.JobKind) ([]string, error) {
		mu.Lock()
		order = append(order, kind)
		mu.Unlock()
		if kind == syncsched.JobListStubs {
			return []string{"older1", "older2"}, nil
		}
		close(bodiesStarted)
		ids := syncsched.RemoteIDs(ctx)
		if len(ids) != 2 || ids[0] != "older1" || ids[1] != "older2" {
			t.Errorf("body remote ids %v, want [older1 older2]", ids)
		}
		return nil, nil
	}, done)

	select {
	case <-bodiesStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("fetch bodies never started")
	}

	mu.Lock()
	got := append([]syncsched.JobKind(nil), order...)
	mu.Unlock()
	if len(got) != 2 || got[0] != syncsched.JobListStubs || got[1] != syncsched.JobFetchBodies {
		t.Fatalf("job order %v, want stubs then bodies", got)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("delta sync: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delta sync did not finish")
	}
}

func TestFoldersWithOlderIncludesFloorID(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "a@b.test"})
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	folderID, err := db.CreateFolder(ctx, &storage.Folder{
		AccountID: accountID, Name: "INBOX", IMAPPath: "Mb1", RemoteID: "Mb1",
	})
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
	if err := db.SetFolderSyncWindow(ctx, folderID, "floor-id", true); err != nil {
		t.Fatalf("set window: %v", err)
	}

	a := &App{ctx: ctx, store: db}
	got, err := a.foldersWithOlder([]int64{folderID})
	if err != nil {
		t.Fatalf("foldersWithOlder: %v", err)
	}
	if len(got) != 1 || got[0].ID != folderID {
		t.Fatalf("foldersWithOlder = %+v, want folder %d", got, folderID)
	}
}
