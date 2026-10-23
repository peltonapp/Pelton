package mailimport

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func newTestStore(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const emlMessage = "Message-ID: <one@example.com>\r\n" +
	"Subject: =?utf-8?q?caf=C3=A9?=\r\n" +
	"From: Alice Example <alice@example.com>\r\n" +
	"To: bob@example.com\r\n" +
	"Date: Mon, 06 Jan 2020 10:00:00 +0000\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"hello there\r\n"

func TestImportEmlIntoLocalFolders(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := writeFile(t, dir, "one.eml", emlMessage)

	result, err := New(db, nil).Import(ctx, []Source{{Path: path, Folder: "Imported"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("result = %+v, want 1 imported", result)
	}

	account, err := db.LocalAccount(ctx)
	if err != nil {
		t.Fatalf("local account: %v", err)
	}
	if !account.Local {
		t.Fatal("the imported-mail account is not flagged local, so sync would try to connect to it")
	}

	folders, err := db.ListFolders(ctx, account.ID)
	if err != nil {
		t.Fatalf("list folders: %v", err)
	}
	if len(folders) != 1 || folders[0].Name != "Imported" {
		t.Fatalf("folders = %+v, want one named Imported", folders)
	}

	messages, err := db.ListMessages(ctx, folders[0].ID, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	m := messages[0]
	// the subject arrives rfc2047-encoded, so a raw header read would show the
	// encoded word instead of the text.
	if m.Subject != "café" {
		t.Fatalf("subject = %q, want café", m.Subject)
	}
	if m.FromAddress != "Alice Example <alice@example.com>" {
		t.Fatalf("from = %q", m.FromAddress)
	}
	if m.MessageID != "one@example.com" {
		t.Fatalf("message id = %q", m.MessageID)
	}
	if m.Date.IsZero() {
		t.Fatal("date was not parsed")
	}
	if m.UID == 0 {
		t.Fatal("uid 0 leaves no room for the next import in this folder")
	}
}

// re-importing the same file must not double the folder's contents.
func TestImportSkipsMessagesAlreadyPresent(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()
	path := writeFile(t, t.TempDir(), "one.eml", emlMessage)

	importer := New(db, nil)
	if _, err := importer.Import(ctx, []Source{{Path: path, Folder: "Imported"}}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	result, err := importer.Import(ctx, []Source{{Path: path, Folder: "Imported"}})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if result.Imported != 0 || result.Skipped != 1 {
		t.Fatalf("result = %+v, want 0 imported and 1 skipped", result)
	}
}

// every message in an mbox lands in one folder, and each needs its own uid:
// the messages table is keyed on (folder_id, uid), so a repeated uid would
// silently lose mail.
func TestImportMboxAssignsDistinctUIDs(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()

	const mbox = "From alice@example.com Mon Jan  6 10:00:00 2020\r\n" +
		"Message-ID: <a@example.com>\r\nSubject: a\r\nX-Mozilla-Status: 0001\r\n\r\nfirst\r\n" +
		"\r\n" +
		"From bob@example.com Tue Jan  7 10:00:00 2020\r\n" +
		"Message-ID: <b@example.com>\r\nSubject: b\r\nX-Mozilla-Status: 0000\r\n\r\nsecond\r\n" +
		"\r\n" +
		"From carol@example.com Wed Jan  8 10:00:00 2020\r\n" +
		"Message-ID: <c@example.com>\r\nSubject: c\r\nX-Mozilla-Status: 0004\r\n\r\nthird\r\n"

	path := writeFile(t, t.TempDir(), "Archive", mbox)
	result, err := New(db, nil).Import(ctx, []Source{{Path: path, Folder: "Archive"}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported != 3 {
		t.Fatalf("result = %+v, want 3 imported", result)
	}

	account, _ := db.LocalAccount(ctx)
	folders, _ := db.ListFolders(ctx, account.ID)
	messages, err := db.ListMessages(ctx, folders[0].ID, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}
	seen := map[uint32]bool{}
	for _, m := range messages {
		if seen[m.UID] {
			t.Fatalf("uid %d was reused", m.UID)
		}
		seen[m.UID] = true
	}

	bySubject := map[string]storage.Message{}
	for _, m := range messages {
		bySubject[m.Subject] = m
	}
	if !bySubject["a"].Flags.Has(storage.FlagSeen) {
		t.Fatal("a message Thunderbird marked read imported as unread")
	}
	if bySubject["b"].Flags.Has(storage.FlagSeen) {
		t.Fatal("a message Thunderbird marked unread imported as read")
	}
	if !bySubject["c"].Flags.Has(storage.FlagFlagged) {
		t.Fatal("a message Thunderbird marked imported without its flag")
	}
}

// a second import into the same folder continues the uid sequence rather than
// restarting it and colliding with what is already there.
func TestImportContinuesUIDsAcrossRuns(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()

	first := writeFile(t, dir, "one.eml", emlMessage)
	second := writeFile(t, dir, "two.eml",
		"Message-ID: <two@example.com>\r\nSubject: two\r\nFrom: b@example.com\r\n\r\nbody\r\n")

	importer := New(db, nil)
	if _, err := importer.Import(ctx, []Source{{Path: first, Folder: "Imported"}}); err != nil {
		t.Fatalf("first import: %v", err)
	}
	result, err := importer.Import(ctx, []Source{{Path: second, Folder: "Imported"}})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if result.Imported != 1 {
		t.Fatalf("result = %+v, want 1 imported", result)
	}

	account, _ := db.LocalAccount(ctx)
	folders, _ := db.ListFolders(ctx, account.ID)
	if len(folders) != 1 {
		t.Fatalf("got %d folders, want the second import to reuse the first one", len(folders))
	}
	messages, _ := db.ListMessages(ctx, folders[0].ID, 0)
	if len(messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(messages))
	}
}

func TestImportReportsProgress(t *testing.T) {
	db := newTestStore(t)
	path := writeFile(t, t.TempDir(), "one.eml", emlMessage)

	var last Progress
	importer := New(db, nil)
	importer.OnProgress = func(p Progress) { last = p }
	if _, err := importer.Import(context.Background(), []Source{{Path: path, Folder: "Imported"}}); err != nil {
		t.Fatalf("import: %v", err)
	}
	if last.Imported != 1 || last.Folder != "Imported" {
		t.Fatalf("progress = %+v", last)
	}
	if last.BytesTotal == 0 || last.BytesDone == 0 {
		t.Fatalf("progress has no byte counters: %+v", last)
	}
}

// bytes alone say how far along an import is, not where it is. "Archive,
// mailbox 3 of 12" is what makes a long import feel like it is moving, so the
// position has to be reported per source (#308).
func TestImportReportsWhichFileItIsOn(t *testing.T) {
	db := newTestStore(t)
	dir := t.TempDir()
	sources := []Source{
		{Path: writeFile(t, dir, "one.eml", emlMessage), Folder: "One"},
		{Path: writeFile(t, dir, "two.eml", emlMessage), Folder: "Two"},
		{Path: writeFile(t, dir, "three.eml", emlMessage), Folder: "Three"},
	}

	seen := map[string]Progress{}
	importer := New(db, nil)
	importer.OnProgress = func(p Progress) { seen[p.Folder] = p }
	if _, err := importer.Import(context.Background(), sources); err != nil {
		t.Fatalf("import: %v", err)
	}

	for i, folder := range []string{"One", "Two", "Three"} {
		p, ok := seen[folder]
		if !ok {
			t.Fatalf("no progress reported for %s", folder)
		}
		if p.FileIndex != i+1 {
			t.Errorf("%s reported as file %d, want %d", folder, p.FileIndex, i+1)
		}
		if p.FileTotal != len(sources) {
			t.Errorf("%s reported %d files in total, want %d", folder, p.FileTotal, len(sources))
		}
	}
}
