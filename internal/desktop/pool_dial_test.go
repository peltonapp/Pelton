package desktop

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Every IMAP operation of an active account runs on the account's pool, so
// the connection cap and the throttle see all of them. A dial is pooled when
// the pool's dialer made it; anything else is a connection past the cap.
func TestEveryAccountDialGoesThroughThePool(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, archive, messageID := moveTestAccount(t, db, ctx, false)
	accountID := inbox.AccountID
	useFake(t, a, accountID, &fakeIMAP{})
	freshID, err := db.CreateAccount(ctx, &storage.Account{Email: "new@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	useFake(t, a, freshID, &fakeIMAP{})
	fresh, err := db.GetAccount(ctx, freshID)
	if err != nil {
		t.Fatal(err)
	}
	var pooled, outside atomic.Int32
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		if strings.Contains(string(debug.Stack()), ".imapDialer.") {
			pooled.Add(1)
		} else {
			outside.Add(1)
		}
		return &fakeIMAP{
			searchResult: []imap.UID{7},
			since:        []imap.UID{5},
			folders:      []pimap.Folder{{Name: "INBOX", Delimiter: '/'}, {Name: "Archive", Delimiter: '/'}, {Name: "Trash", Delimiter: '/'}},
			messages:     map[imap.UID]*pimap.Message{5: {UID: 5, Subject: "hi", Text: "body", Date: time.Now()}},
		}, nil
	}
	trash := makeFolderWithDelim(t, db, accountID, "Trash", "Trash", nil, "", "/")
	if _, err := db.InsertMessage(ctx, &storage.Message{AccountID: accountID, FolderID: trash.ID, UID: 9, Subject: "old", Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	stubID, err := db.UpsertMessageListMeta(ctx, &storage.Message{AccountID: accountID, FolderID: inbox.ID, UID: 5, Subject: "hi", Date: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	account, err := db.GetAccount(ctx, accountID)
	if err != nil {
		t.Fatal(err)
	}
	idle := func() bool {
		rt := a.accountSync(accountID)
		if rt == nil {
			return true
		}
		st := rt.sched.Stats()
		return st.Fast == 0 && st.Normal == 0 && st.Running == 0
	}

	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"discover folders", func() error { return a.discoverFolders(*fresh) }},
		{"source", func() error { _, err := a.GetMessageSource(messageID); return err }},
		{"colour", func() error { a.pushColorKeyword(messageID, 2); return nil }},
		{"archive", func() error { _, err := a.ArchiveMessage(messageID); return err }},
		{"undo", func() error { return a.UnarchiveMessage("<one@example.com>", archive.ID, inbox.ID) }},
		{"create folder", func() error {
			_, err := a.CreateFolder(CreateFolderRequest{AccountID: accountID, Name: "Projects"})
			return err
		}},
		{"rename folder", func() error {
			f := folderByPath(t, db, accountID, "Projects")
			return a.RenameFolder(f.ID, "Work")
		}},
		{"delete folder", func() error { return a.DeleteFolder(folderByPath(t, db, accountID, "Work").ID) }},
		{"sent copy", func() error { _, err := a.appendToSent(*account, []byte("Subject: hi\r\n\r\nbody\r\n")); return err }},
		{"empty trash", func() error { _, err := a.EmptyTrash(trash.ID); return err }},
		{"open a stub", func() error { return a.fetchMessageBodyOnDemand(mustGetMessage(t, db, stubID)) }},
		{"offline download", func() error {
			tasks, pin, err := a.planDownload(ctx, time.Now().AddDate(0, -1, 0))
			if err != nil {
				return err
			}
			return a.runDownload(ctx, tasks, pin, true, len(tasks))
		}},
		{"sync", func() error { return a.syncAccount(*account, true) }},
	} {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		waitUntil(t, 5*time.Second, step.name+": queued work never ended", idle)
		if n := outside.Load(); n != 0 {
			t.Fatalf("%s: %d dials outside the pool", step.name, n)
		}
	}
	if pooled.Load() == 0 {
		t.Fatal("no dial went through the pool: the check proves nothing")
	}

	// The wizard's connection test uses credentials not saved yet, so it is
	// the one dial outside the pool, and the check above sees it.
	if _, err := a.TestConnection(TestConnectionRequest{IMAPHost: "127.0.0.1", IMAPPort: 1}); err != nil {
		t.Logf("TestConnection: %v", err)
	}
	if outside.Load() == 0 {
		t.Fatal("the wizard's dial was not seen outside the pool")
	}
}

// Only the pool's dialer and the two checks of new credentials (the
// wizard's connection test and the password check) may open an IMAP
// connection. A new dial site anywhere else fails here.
func TestOnlyThePoolDialsIMAP(t *testing.T) {
	allowed := []string{"bind_account_setup.go", "imapseam.go", "protocol_imap.go", "sync_runtime.go"}
	dial := regexp.MustCompile(`\.connectIMAP\(|pimap\.Connect\(`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(dial.FindAll(src, -1)); n > 0 {
			found = append(found, f)
			if f != "imapseam.go" && n > 1 {
				t.Errorf("%s dials IMAP %d times, want at most one site", f, n)
			}
		}
	}
	if !slices.Equal(found, allowed) {
		t.Errorf("IMAP dial sites in %v, want only %v", found, allowed)
	}
}

func folderByPath(t *testing.T, db *storage.DB, accountID int64, path string) storage.Folder {
	t.Helper()
	folders, err := db.ListFolders(t.Context(), accountID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range folders {
		if f.IMAPPath == path {
			return f
		}
	}
	t.Fatalf("no folder %q", path)
	return storage.Folder{}
}
