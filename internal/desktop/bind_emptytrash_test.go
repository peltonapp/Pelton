package desktop

import (
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func newTrashTestApp(t *testing.T) *App {
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

// seedFolder creates an account (once per app) plus a folder with n messages.
func seedFolder(t *testing.T, a *App, accountID int64, name, path string, attrs []string, n int) storage.Folder {
	t.Helper()
	folder := &storage.Folder{
		AccountID:  accountID,
		Name:       name,
		IMAPPath:   path,
		Attributes: attrs,
	}
	id, err := a.store.CreateFolder(a.ctx, folder)
	if err != nil {
		t.Fatalf("create folder %q: %v", name, err)
	}
	folder.ID = id
	for i := range n {
		_, err := a.store.InsertMessage(a.ctx, &storage.Message{
			AccountID: accountID,
			FolderID:  id,
			UID:       uint32(i + 1),
			Subject:   "junk",
			Date:      time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}
	return *folder
}

func seedAccount(t *testing.T, a *App) int64 {
	t.Helper()
	id, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "a@example.test"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return id
}

func TestEmptyTrashMarksEveryMessage(t *testing.T) {
	a := newTrashTestApp(t)
	acct := seedAccount(t, a)
	trash := seedFolder(t, a, acct, "Trash", "Trash", nil, 3)

	got, err := a.EmptyTrash(trash.ID)
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if got != 3 {
		t.Errorf("marked %d messages, want 3", got)
	}

	states, err := a.store.ListMessageStates(a.ctx, trash.ID)
	if err != nil {
		t.Fatalf("ListMessageStates: %v", err)
	}
	for _, s := range states {
		if !s.PendingDelete {
			t.Errorf("uid %d was left un-marked", s.UID)
		}
	}
}

// Emptying is unrecoverable, so pointing it at anything but a trash folder has
// to fail outright rather than deleting whatever it was given.
func TestEmptyTrashRefusesOtherFolders(t *testing.T) {
	a := newTrashTestApp(t)
	acct := seedAccount(t, a)

	for _, tt := range []struct {
		name  string
		fname string
		path  string
		attrs []string
	}{
		{"inbox", "INBOX", "INBOX", nil},
		{"archive by attribute", "Old", "Old", []string{"\\Archive"}},
		{"an ordinary folder", "Projects", "Projects", nil},
		// a folder merely named like the trash of another language is not one.
		{"lookalike name", "Trashcan", "Trashcan", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := seedFolder(t, a, acct, tt.fname, tt.path, tt.attrs, 2)
			if _, err := a.EmptyTrash(f.ID); !errors.Is(err, errNotTrash) {
				t.Fatalf("EmptyTrash(%s) = %v, want errNotTrash", tt.name, err)
			}
			states, err := a.store.ListMessageStates(a.ctx, f.ID)
			if err != nil {
				t.Fatalf("ListMessageStates: %v", err)
			}
			for _, s := range states {
				if s.PendingDelete {
					t.Errorf("a refused empty still marked uid %d", s.UID)
				}
			}
		})
	}
}

// The special-use attribute is the reliable signal, so a trash folder named in
// another language still has to be recognised.
func TestEmptyTrashAcceptsTrashByAttribute(t *testing.T) {
	a := newTrashTestApp(t)
	acct := seedAccount(t, a)
	folder := seedFolder(t, a, acct, "Papierkorb", "Papierkorb", []string{"\\Trash"}, 2)

	got, err := a.EmptyTrash(folder.ID)
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if got != 2 {
		t.Errorf("marked %d, want 2", got)
	}
}

func TestEmptyTrashOnAnEmptyFolderDoesNothing(t *testing.T) {
	a := newTrashTestApp(t)
	acct := seedAccount(t, a)
	trash := seedFolder(t, a, acct, "Trash", "Trash", nil, 0)

	got, err := a.EmptyTrash(trash.ID)
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if got != 0 {
		t.Errorf("marked %d, want 0", got)
	}
}

// Emptying twice must not report the same messages again, since the second call
// pushes nothing new.
func TestEmptyTrashTwiceCountsOnlyWhatItMarked(t *testing.T) {
	a := newTrashTestApp(t)
	acct := seedAccount(t, a)
	trash := seedFolder(t, a, acct, "Trash", "Trash", nil, 4)

	if got, err := a.EmptyTrash(trash.ID); err != nil || got != 4 {
		t.Fatalf("first EmptyTrash = %d, %v; want 4, nil", got, err)
	}
	if got, err := a.EmptyTrash(trash.ID); err != nil || got != 0 {
		t.Fatalf("second EmptyTrash = %d, %v; want 0, nil", got, err)
	}
}

// expungeIMAP records each expunge.
type expungeIMAP struct {
	fakeIMAP
	mu       sync.Mutex
	expunged [][]imap.UID
}

func (c *expungeIMAP) DeleteMessages(uids ...imap.UID) error {
	c.mu.Lock()
	c.expunged = append(c.expunged, slices.Clone(uids))
	c.mu.Unlock()
	return nil
}

// Emptying a big trash expunges a page per normal-lane unit, planned from
// the store, so the task holds a session for one page at a time and the
// rows leave the cache as their page is done.
func TestEmptyTrashExpungesAPagePerUnit(t *testing.T) {
	a := newHoldTestApp(t)
	acct, _ := seedHoldAccount(t, a, "a@example.test")
	client := &expungeIMAP{}
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	trash := seedFolder(t, a, acct, "Trash", "Trash", []string{`\Trash`}, 1200)
	if got, err := a.EmptyTrash(trash.ID); err != nil || got != 1200 {
		t.Fatalf("EmptyTrash = %d, %v; want 1200", got, err)
	}
	waitUntil(t, 10*time.Second, "trash never emptied", func() bool { return rowCount(t, a, trash.ID) == 0 })
	client.mu.Lock()
	defer client.mu.Unlock()
	var sizes []int
	total := 0
	for _, uids := range client.expunged {
		sizes = append(sizes, len(uids))
		total += len(uids)
	}
	if total != 1200 || len(sizes) != 3 || slices.Max(sizes) > syncsched.ListPage {
		t.Fatalf("expunges of %v messages, want 1200 in pages of at most %d", sizes, syncsched.ListPage)
	}
}
