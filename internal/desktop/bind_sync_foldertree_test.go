package desktop

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// treeIMAP is a server whose mailbox list a rename changes, and which tells
// when a LIST comes in. onRename runs after the server renamed.
type treeIMAP struct {
	*fakeIMAP
	mu       sync.Mutex
	boxes    []pimap.Folder
	listed   chan struct{}
	onRename func()
}

func (c *treeIMAP) ListFolders() ([]pimap.Folder, error) {
	select {
	case c.listed <- struct{}{}:
	default:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]pimap.Folder(nil), c.boxes...), nil
}

func (c *treeIMAP) RenameFolder(path, newPath string) error {
	c.mu.Lock()
	for i, b := range c.boxes {
		if b.Name == path {
			c.boxes[i].Name = newPath
		}
	}
	fn := c.onRename
	c.onRename = nil
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
	return nil
}

func treeApp(t *testing.T, n int) (*App, storage.Account, *treeIMAP) {
	t.Helper()
	a := newHoldTestApp(t)
	id, _ := seedHoldAccount(t, a, "a@example.test")
	setParallel(t, a, n)
	c := &treeIMAP{fakeIMAP: &fakeIMAP{}, listed: make(chan struct{}, 1), boxes: []pimap.Folder{
		{Name: "INBOX", Delimiter: '.'}, {Name: "Work", Delimiter: '.'},
	}}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return c, nil }
	return a, mustAccount(t, a, id), c
}

// Manual and timed sync discover folders as a barrier: the LIST waits for
// the account's running work, and a fast task queued meanwhile runs right
// after the discovery instead of beside it.
func TestManualAndTimedFolderTreeIsABarrier(t *testing.T) {
	for _, manual := range []bool{true, false} {
		t.Run(map[bool]string{true: "manual", false: "timed"}[manual], func(t *testing.T) {
			testFolderTreeBarrier(t, manual)
		})
	}
}

func testFolderTreeBarrier(t *testing.T, manual bool) {
	a, account, c := treeApp(t, 3)
	rt := mustAccountSync(t, a, account.ID)
	hold := make(chan struct{})
	recv(t, signalJob(rt.sched, syncsched.KindBodies, 5, hold), 2*time.Second, "held task never started")

	synced := make(chan error, 1)
	go func() { synced <- a.runIMAPManualSync(a.ctx, account, manual) }()
	noRecv(t, c.listed, 300*time.Millisecond, "folder discovery ran beside a running task")

	fast := make(chan error, 1)
	go func() {
		fast <- a.runNow(a.ctx, account, syncsched.KindSource, 0, func(context.Context, mailClient) error { return nil })
	}()
	noRecv(t, fast, 100*time.Millisecond, "a fast task jumped a queued barrier")
	close(hold)
	recv(t, c.listed, 2*time.Second, "folder discovery never ran")
	if err := recv(t, fast, 2*time.Second, "the fast task waited past the discovery"); err != nil {
		t.Fatalf("manual=%v: fast task: %v", manual, err)
	}
	if err := recv(t, synced, 5*time.Second, "sync never ended"); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

// A rename changes the server and then the folder row inside one task.
// A folder discovery asked for in between runs after both, so it finds the
// row at its new path: the folder keeps its id (and with it its cached mail)
// instead of being dropped as gone and listed anew.
func TestFolderDiscoveryDoesNotSeeAHalfDoneRename(t *testing.T) {
	a, account, c := treeApp(t, 3)
	work := makeDotFolder(t, a.store, account.ID, "Work", "Work", nil, "")
	synced := make(chan error, 1)
	c.onRename = func() {
		go func() { synced <- a.runIMAPManualSync(a.ctx, account, true) }()
		// Give the discovery the chance to run in the window.
		select {
		case <-c.listed:
			c.listed <- struct{}{}
		case <-time.After(300 * time.Millisecond):
		}
	}
	if err := a.RenameFolder(work.ID, "Projects"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}
	if err := recv(t, synced, 5*time.Second, "sync never ended"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := a.store.GetFolder(a.ctx, work.ID)
	if err != nil {
		t.Fatalf("the renamed folder's row is gone: %v", err)
	}
	if got.IMAPPath != "Projects" {
		t.Fatalf("path = %q, want Projects", got.IMAPPath)
	}
	folders, err := a.store.ListFolders(a.ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range folders {
		if f.IMAPPath == "Projects" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d rows at the new path, want 1", n)
	}
}
