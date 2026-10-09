package desktop

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	keyring "github.com/zalando/go-keyring"

	"github.com/peltonapp/Pelton/internal/credentials"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// TestMain swaps go-keyring for its in-memory mock before any test in this
// package runs, so a test that resolves an account's credentials never reads or
// writes the machine's real credential store.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

// fakeIMAP records the commands a binding sends and answers them from canned
// values. Every method the mailClient interface declares is here, so a binding
// that starts calling a new one fails to compile rather than reaching the
// network from a test.
type fakeIMAP struct {
	// selected is every mailbox SELECT was called on, in order.
	selected []string
	// moved records each Move as the uid it was given and the destination.
	moved []movedMessage
	// searchResult is what SearchByMessageID returns.
	searchResult []imap.UID
	// created, renamed and deleted record the mailbox management commands.
	created []string
	renamed [][2]string
	deleted []string
	// loggedOut reports whether the binding closed its session cleanly, which
	// is otherwise invisible: a leaked session only shows up as a server
	// refusing the next login.
	loggedOut bool

	// appended is every message handed to AppendToSent, in order, and
	// failAppend makes the append fail so a test can check a send still
	// succeeds without one.
	appended   [][]byte
	failAppend error
	// failMove makes MOVE fail, for the paths that have to leave the local cache
	// alone when the server says no.
	failMove error

	// since is what SearchSince returns, and messages what FetchMessages serves
	// by uid; a uid missing from messages is skipped like one the server lost.
	since    []imap.UID
	messages map[imap.UID]*pimap.Message
}

type movedMessage struct {
	uid  imap.UID
	dest string
}

func (f *fakeIMAP) AppendToSent(raw []byte) (string, error) {
	if f.failAppend != nil {
		return "", f.failAppend
	}
	f.appended = append(f.appended, raw)
	return "Sent", nil
}

func (f *fakeIMAP) Login() error  { return nil }
func (f *fakeIMAP) Logout() error { f.loggedOut = true; return nil }
func (f *fakeIMAP) Close() error  { return nil }
func (f *fakeIMAP) Addr() string  { return "fake:993" }

func (f *fakeIMAP) Select(mailbox string) (*pimap.Mailbox, error) {
	f.selected = append(f.selected, mailbox)
	return &pimap.Mailbox{Name: mailbox}, nil
}

func (f *fakeIMAP) ListFolders() ([]pimap.Folder, error) { return nil, nil }
func (f *fakeIMAP) CreateFolder(path string) error {
	f.created = append(f.created, path)
	return nil
}

func (f *fakeIMAP) RenameFolder(path, newPath string) error {
	f.renamed = append(f.renamed, [2]string{path, newPath})
	return nil
}

func (f *fakeIMAP) DeleteFolder(path string) error {
	f.deleted = append(f.deleted, path)
	return nil
}

func (f *fakeIMAP) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	for _, uid := range uids {
		msg, ok := f.messages[uid]
		if !ok {
			continue
		}
		if err := fn(uid, msg, nil); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeIMAP) FetchRawMessage(imap.UID) ([]byte, error)      { return nil, nil }
func (f *fakeIMAP) FetchAllFlags() ([]pimap.MessageHeader, error) { return nil, nil }
func (f *fakeIMAP) FetchFlagsChangedSince(uint64) ([]pimap.MessageHeader, error) {
	return nil, nil
}
func (f *fakeIMAP) SearchAllUIDs() ([]imap.UID, error) { return nil, nil }
func (f *fakeIMAP) FetchHeaders([]imap.UID, *imap.FetchOptions) ([]pimap.MessageHeader, error) {
	return nil, nil
}

func (f *fakeIMAP) AddFlags(imap.UID, ...imap.Flag) error    { return nil }
func (f *fakeIMAP) RemoveFlags(imap.UID, ...imap.Flag) error { return nil }

func (f *fakeIMAP) Move(uid imap.UID, mailbox string) error {
	if f.failMove != nil {
		return f.failMove
	}
	f.moved = append(f.moved, movedMessage{uid: uid, dest: mailbox})
	return nil
}

func (f *fakeIMAP) MoveMessages([]imap.UID, string) error { return nil }
func (f *fakeIMAP) DeleteMessages(...imap.UID) error      { return nil }

func (f *fakeIMAP) SearchByMessageID(string) ([]imap.UID, error) { return f.searchResult, nil }

func (f *fakeIMAP) SearchSince(time.Time) ([]imap.UID, error) { return f.since, nil }

func (f *fakeIMAP) SupportsIdle() bool                      { return false }
func (f *fakeIMAP) IdleUntil(context.Context) (bool, error) { return false, nil }

// useFake points the app at client instead of a real connection and gives its
// account a password, since a binding resolves credentials before it connects.
func useFake(t *testing.T, a *App, accountID int64, client *fakeIMAP) {
	t.Helper()
	if err := credentials.Store(accountID, credentials.Secret{
		Method:   credentials.MethodPassword,
		Password: "hunter2",
	}); err != nil {
		t.Fatalf("store credentials: %v", err)
	}
	t.Cleanup(func() { _ = credentials.Delete(accountID) })
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
}

// Archiving is the path two bug reports came from, and until the seam existed a
// test could only reach the guard in front of it. This is the whole action: the
// source mailbox is selected, the message is moved to Archive, and only then is
// the local row dropped.
func TestArchiveMessageMovesOnTheServerAndDropsTheRow(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, archive, messageID := moveTestAccount(t, db, ctx, false)
	client := &fakeIMAP{}
	useFake(t, a, inbox.AccountID, client)

	undo, err := a.ArchiveMessage(messageID)
	if err != nil {
		t.Fatalf("ArchiveMessage: %v", err)
	}

	if len(client.selected) != 1 || client.selected[0] != inbox.IMAPPath {
		t.Errorf("selected %v, want just %q", client.selected, inbox.IMAPPath)
	}
	if len(client.moved) != 1 {
		t.Fatalf("moved %d messages, want 1", len(client.moved))
	}
	if client.moved[0].dest != archive.IMAPPath {
		t.Errorf("moved to %q, want %q", client.moved[0].dest, archive.IMAPPath)
	}
	if !client.loggedOut {
		t.Error("the imap session was left open")
	}
	if undo.OriginalFolderID != inbox.ID {
		t.Errorf("undo points at folder %d, want the inbox %d", undo.OriginalFolderID, inbox.ID)
	}
	if undo.DestFolderID != archive.ID {
		t.Errorf("undo DestFolderID = %d, want the archive %d", undo.DestFolderID, archive.ID)
	}
	if _, err := db.GetMessage(ctx, messageID); err == nil {
		t.Error("the local row survived a successful archive")
	}
}

// A server that refuses the move must leave the cache alone. Dropping the row
// on a failed move is exactly the shape of "my mail disappeared": the list stops
// showing a message that is still on the server.
func TestArchiveMessageKeepsTheRowWhenTheServerRefuses(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _, messageID := moveTestAccount(t, db, ctx, false)
	useFake(t, a, inbox.AccountID, &fakeIMAP{failMove: errors.New("over quota")})

	if _, err := a.ArchiveMessage(messageID); err == nil {
		t.Fatal("ArchiveMessage returned no error though the move failed")
	}
	if _, err := db.GetMessage(ctx, messageID); err != nil {
		t.Errorf("the message was dropped from the cache after a failed move: %v", err)
	}
}

// Undo searches the folder the message went to by Message-ID because the move
// gave it a new uid, and moves whatever it finds back to where it came from.
func TestUnarchiveMessageMovesTheFoundUIDBack(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, archive, _ := moveTestAccount(t, db, ctx, true)
	client := &fakeIMAP{searchResult: []imap.UID{7}}
	useFake(t, a, inbox.AccountID, client)

	if err := a.UnarchiveMessage("<one@example.com>", archive.ID, inbox.ID); err != nil {
		t.Fatalf("UnarchiveMessage: %v", err)
	}

	if len(client.selected) != 1 || client.selected[0] != archive.IMAPPath {
		t.Errorf("selected %v, want just %q", client.selected, archive.IMAPPath)
	}
	if len(client.moved) != 1 || client.moved[0].uid != 7 || client.moved[0].dest != inbox.IMAPPath {
		t.Errorf("moved %+v, want uid 7 to %q", client.moved, inbox.IMAPPath)
	}
}

// A message the server cannot find must say so rather than reporting a restore
// that did not happen.
func TestUnarchiveMessageFailsWhenTheServerHasNoMatch(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, archive, _ := moveTestAccount(t, db, ctx, true)
	client := &fakeIMAP{}
	useFake(t, a, inbox.AccountID, client)

	if err := a.UnarchiveMessage("<one@example.com>", archive.ID, inbox.ID); err == nil {
		t.Fatal("UnarchiveMessage returned no error though the search found nothing")
	}
	if len(client.moved) != 0 {
		t.Errorf("moved %+v though there was nothing to restore", client.moved)
	}
}

// Undoing a move to an ordinary folder must search that folder, not Archive:
// the message is not in Archive, so the old lookup could never find it.
func TestUndoMoveSearchesTheDestinationFolder(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _, _ := moveTestAccount(t, db, ctx, false)
	projects := &storage.Folder{AccountID: inbox.AccountID, Name: "Projects", IMAPPath: "Projects"}
	if _, err := db.CreateFolder(ctx, projects); err != nil {
		t.Fatal(err)
	}
	client := &fakeIMAP{searchResult: []imap.UID{7}}
	useFake(t, a, inbox.AccountID, client)

	if err := a.UnarchiveMessage("<one@example.com>", projects.ID, inbox.ID); err != nil {
		t.Fatalf("UnarchiveMessage: %v", err)
	}
	if len(client.selected) != 1 || client.selected[0] != "Projects" {
		t.Errorf("selected %v, want just Projects", client.selected)
	}
	if len(client.moved) != 1 || client.moved[0].uid != 7 || client.moved[0].dest != "INBOX" {
		t.Errorf("moved %+v, want uid 7 to INBOX", client.moved)
	}
}

// An account with no Archive folder can still move mail elsewhere, so it must
// be able to undo that move.
func TestUndoMoveWithoutArchiveFolder(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	projects := &storage.Folder{AccountID: accountID, Name: "Projects", IMAPPath: "Projects"}
	for _, f := range []*storage.Folder{inbox, projects} {
		if _, err := db.CreateFolder(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	client := &fakeIMAP{searchResult: []imap.UID{3}}
	useFake(t, a, accountID, client)

	if err := a.UnarchiveMessage("<one@example.com>", projects.ID, inbox.ID); err != nil {
		t.Fatalf("UnarchiveMessage: %v", err)
	}
	if len(client.moved) != 1 || client.moved[0].dest != "INBOX" {
		t.Errorf("moved %+v, want one move to INBOX", client.moved)
	}
}

// Folder creation changes the server first and the local tree second, so a
// refused command leaves no folder row behind. The path is built from the
// parent, which is the part a flat name cannot express.
func TestCreateFolderSendsThePathAndCachesIt(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _, _ := moveTestAccount(t, db, ctx, false)
	client := &fakeIMAP{}
	useFake(t, a, inbox.AccountID, client)

	parentID, err := db.CreateFolder(ctx, &storage.Folder{
		AccountID: inbox.AccountID,
		Name:      "Projects",
		IMAPPath:  "Projects",
		Delimiter: "/",
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	dto, err := a.CreateFolder(CreateFolderRequest{
		AccountID: inbox.AccountID,
		ParentID:  parentID,
		Name:      "Receipts",
	})
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	if len(client.created) != 1 || client.created[0] != "Projects/Receipts" {
		t.Errorf("created %v, want [Projects/Receipts]", client.created)
	}
	if dto.Name != "Receipts" {
		t.Errorf("returned folder named %q, want Receipts", dto.Name)
	}
	folders, err := db.ListFolders(ctx, inbox.AccountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	var found bool
	for _, f := range folders {
		if f.IMAPPath == "Projects/Receipts" {
			found = true
		}
	}
	if !found {
		t.Error("the new folder is not in the local tree")
	}
}

// Renaming has to move the stored paths of the children too: the server renames
// the whole subtree in one command, and children left on the old prefix are not
// findable afterwards.
func TestRenameFolderMovesTheSubtreePaths(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _, _ := moveTestAccount(t, db, ctx, false)
	client := &fakeIMAP{}
	useFake(t, a, inbox.AccountID, client)

	parent := &storage.Folder{
		AccountID: inbox.AccountID,
		Name:      "Projects",
		IMAPPath:  "Projects",
		Delimiter: "/",
	}
	parentID, err := db.CreateFolder(ctx, parent)
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child := &storage.Folder{
		AccountID: inbox.AccountID,
		Name:      "Pelton",
		IMAPPath:  "Projects/Pelton",
		Delimiter: "/",
		ParentID:  &parentID,
	}
	if _, err := db.CreateFolder(ctx, child); err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := a.RenameFolder(parentID, "Work"); err != nil {
		t.Fatalf("RenameFolder: %v", err)
	}

	if len(client.renamed) != 1 || client.renamed[0] != [2]string{"Projects", "Work"} {
		t.Errorf("renamed %v, want [[Projects Work]]", client.renamed)
	}
	folders, err := db.ListFolders(ctx, inbox.AccountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	for _, f := range folders {
		if f.Name == "Pelton" && f.IMAPPath != "Work/Pelton" {
			t.Errorf("child path is %q, want Work/Pelton", f.IMAPPath)
		}
	}
}

// Deleting a folder destroys mail on the server, and the order matters: a
// server that refuses to delete a mailbox with children only works if the
// deepest one goes first. That ordering was invisible to a test until the
// commands could be recorded.
func TestDeleteFolderRemovesTheDeepestMailboxFirst(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _, _ := moveTestAccount(t, db, ctx, false)
	client := &fakeIMAP{}
	useFake(t, a, inbox.AccountID, client)

	parentID, err := db.CreateFolder(ctx, &storage.Folder{
		AccountID: inbox.AccountID,
		Name:      "Projects",
		IMAPPath:  "Projects",
		Delimiter: "/",
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if _, err := db.CreateFolder(ctx, &storage.Folder{
		AccountID: inbox.AccountID,
		Name:      "Pelton",
		IMAPPath:  "Projects/Pelton",
		Delimiter: "/",
		ParentID:  &parentID,
	}); err != nil {
		t.Fatalf("create child: %v", err)
	}

	if err := a.DeleteFolder(parentID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}

	want := []string{"Projects/Pelton", "Projects"}
	if len(client.deleted) != len(want) {
		t.Fatalf("deleted %v, want %v", client.deleted, want)
	}
	for i := range want {
		if client.deleted[i] != want[i] {
			t.Errorf("deleted %v, want %v", client.deleted, want)
			break
		}
	}

	folders, err := db.ListFolders(ctx, inbox.AccountID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	for _, f := range folders {
		if f.Name == "Projects" || f.Name == "Pelton" {
			t.Errorf("folder %q survived the delete locally", f.Name)
		}
	}
}
