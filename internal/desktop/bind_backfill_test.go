package desktop

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
)

// A sync plan lists a folder, then runs its body generator batch by batch
// until nothing is missing.
func TestSyncPlanListsThenBodyBatches(t *testing.T) {
	folder := storage.Folder{ID: 1, Name: "INBOX", IMAPPath: "INBOX"}
	var mu sync.Mutex
	var order []syncsched.Kind
	batches := 3
	s := newTestScheduler(t, 3)
	done := make(chan error, 1)
	enqueueSyncPlan(context.Background(), s, []storage.Folder{folder}, syncPlan{
		step: func(_ context.Context, _ mailClient, _ storage.Folder, kind syncsched.Kind) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, kind)
			if kind == syncsched.KindBodies {
				batches--
				if batches > 0 {
					return syncsched.ErrAgain
				}
			}
			return nil
		},
		done: done,
	})
	if err := recv(t, done, 2*time.Second, "plan did not finish"); err != nil {
		t.Fatalf("plan: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []syncsched.Kind{syncsched.KindListStubs, syncsched.KindBodies, syncsched.KindBodies, syncsched.KindBodies}
	if !slices.Equal(order, want) {
		t.Fatalf("task order %v, want %v", order, want)
	}
}

func TestFoldersWithOlderIncludesFloorID(t *testing.T) {
	a := newHoldTestApp(t)
	accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "a@b.test"})
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	folderID, err := a.store.CreateFolder(a.ctx, &storage.Folder{
		AccountID: accountID, Name: "INBOX", IMAPPath: "Mb1", RemoteID: "Mb1",
	})
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
	if err := a.store.SetFolderSyncWindow(a.ctx, folderID, "floor-id", true); err != nil {
		t.Fatalf("set window: %v", err)
	}

	got, err := a.foldersWithOlder([]int64{folderID})
	if err != nil {
		t.Fatalf("foldersWithOlder: %v", err)
	}
	if len(got) != 1 || got[0].ID != folderID {
		t.Fatalf("foldersWithOlder = %+v, want folder %d", got, folderID)
	}
}
