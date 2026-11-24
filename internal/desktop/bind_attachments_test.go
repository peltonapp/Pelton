package desktop

import (
	"bytes"
	"context"
	"testing"
	"time"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// browserAccount builds an account with an inbox and a trash folder.
func browserAccount(t *testing.T, db *storage.DB, ctx context.Context) (inbox, trash storage.Folder) {
	t.Helper()
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	inbox = storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	trash = storage.Folder{AccountID: accountID, Name: "Trash", IMAPPath: "Trash", Attributes: []string{`\Trash`}}
	for _, f := range []*storage.Folder{&inbox, &trash} {
		id, err := db.CreateFolder(ctx, f)
		if err != nil {
			t.Fatalf("create folder: %v", err)
		}
		f.ID = id
	}
	return inbox, trash
}

func insertWithParts(t *testing.T, db *storage.DB, ctx context.Context, f storage.Folder, uid uint32, from string, date time.Time, parts ...storage.IncomingAttachment) int64 {
	t.Helper()
	id, err := db.InsertMessageWithAttachments(ctx, &storage.Message{
		AccountID: f.AccountID, FolderID: f.ID, UID: uid, Subject: "files",
		FromAddress: from, Date: date,
	}, parts)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	return id
}

func TestQueryAttachmentsSkipsTrashAndReportsDownloads(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, trash := browserAccount(t, db, ctx)
	now := time.Now().UTC().Truncate(time.Second)
	insertWithParts(t, db, ctx, inbox, 1, "Ann <ann@example.com>", now,
		storage.IncomingAttachment{Filename: "kept.pdf", Content: bytes.NewReader(make([]byte, 5000))},
		storage.IncomingAttachment{Filename: "later.zip", Size: 9000})
	insertWithParts(t, db, ctx, trash, 2, "ann@example.com", now,
		storage.IncomingAttachment{Filename: "binned.pdf", Content: bytes.NewReader(make([]byte, 5000))})

	page, err := a.QueryAttachments(AttachmentQueryRequest{WithTotal: true})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Entries) != 2 {
		t.Fatalf("page = %+v, want the two inbox files", page)
	}
	byName := map[string]AttachmentEntryDTO{}
	for _, e := range page.Entries {
		byName[e.Filename] = e
	}
	if !byName["kept.pdf"].Downloaded || byName["kept.pdf"].Kind != "pdf" {
		t.Errorf("kept.pdf = %+v", byName["kept.pdf"])
	}
	if byName["later.zip"].Downloaded || byName["later.zip"].SizeBytes != 9000 {
		t.Errorf("later.zip = %+v", byName["later.zip"])
	}

	day := now.Local().Format(time.DateOnly)
	filtered, err := a.QueryAttachments(AttachmentQueryRequest{Kinds: []string{"archive"}, Since: day, Until: day})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Entries) != 1 || filtered.Entries[0].Filename != "later.zip" || filtered.Total != -1 {
		t.Fatalf("filtered = %+v", filtered)
	}
	if _, err := a.QueryAttachments(AttachmentQueryRequest{Since: "yesterday"}); err == nil {
		t.Error("a malformed date was accepted")
	}
}

// a part recorded without its bytes is fetched from the server on request,
// matched by position, and written where later reads find it.
func TestFetchAttachmentRestoresFromServer(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _ := browserAccount(t, db, ctx)
	id := insertWithParts(t, db, ctx, inbox, 42, "ann@example.com", time.Now(),
		storage.IncomingAttachment{Filename: "a.txt", Content: bytes.NewReader([]byte("first"))},
		storage.IncomingAttachment{Filename: "b.txt", Size: 6})
	atts, err := db.ListAttachments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeIMAP{message: &pimap.Message{Attachments: []pimap.Attachment{
		{Filename: "a.txt", Content: []byte("first")},
		{Filename: "b.txt", Content: []byte("second")},
	}}}
	useFake(t, a, inbox.AccountID, client)

	entry, err := a.FetchAttachment(atts[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Downloaded {
		t.Fatalf("entry = %+v, want downloaded", entry)
	}
	if len(client.fetchedUIDs) != 1 || client.fetchedUIDs[0] != 42 || len(client.selected) == 0 || client.selected[len(client.selected)-1] != "INBOX" {
		t.Fatalf("server calls: uids %v, selected %v", client.fetchedUIDs, client.selected)
	}
	content, err := a.ReadAttachment(id, atts[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if content.Data != "c2Vjb25k" {
		t.Fatalf("data = %q, want base64 of second", content.Data)
	}

	// downloaded files are read locally without asking the server again.
	if _, err := a.FetchAttachment(atts[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(client.fetchedUIDs) != 1 {
		t.Fatalf("fetched again for a local file: %v", client.fetchedUIDs)
	}
}

func TestFetchAttachmentReportsAPartTheServerLost(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _ := browserAccount(t, db, ctx)
	id := insertWithParts(t, db, ctx, inbox, 7, "ann@example.com", time.Now(),
		storage.IncomingAttachment{Filename: "gone.pdf", Size: 10})
	atts, _ := db.ListAttachments(ctx, id)
	useFake(t, a, inbox.AccountID, &fakeIMAP{message: &pimap.Message{}})
	if _, err := a.FetchAttachment(atts[0].ID); err != errAttachmentGone {
		t.Fatalf("err = %v, want errAttachmentGone", err)
	}
}

func TestSenderOverview(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, trash := browserAccount(t, db, ctx)
	base := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)
	for i := uint32(0); i < 3; i++ {
		insertWithParts(t, db, ctx, inbox, 10+i, "Ann <Ann@Example.com>", base.Add(time.Duration(i)*time.Hour),
			storage.IncomingAttachment{Filename: "f.pdf", Content: bytes.NewReader(make([]byte, 4000))})
	}
	insertWithParts(t, db, ctx, trash, 20, "ann@example.com", base.Add(10*time.Hour))

	o, err := a.SenderOverview("Ann <ann@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if o.Address != "ann@example.com" || o.Count != 3 || o.FilesTotal != 3 || len(o.Files) != 3 || o.Automated {
		t.Fatalf("overview = %+v", o)
	}
	if len(o.Recent) == 0 || o.Recent[0].Date != formatDate(base.Add(2*time.Hour)) {
		t.Fatalf("recent = %+v", o.Recent)
	}
}

func TestOnePerThreadAndAutomated(t *testing.T) {
	msgs := []storage.Message{{ID: 1, ThreadID: 5}, {ID: 2, ThreadID: 5}, {ID: 3}, {ID: 4}, {ID: 5, ThreadID: 6}}
	got := onePerThread(msgs, 3)
	if len(got) != 3 || got[0].ID != 1 || got[1].ID != 3 || got[2].ID != 4 {
		t.Fatalf("got %+v", got)
	}
	for addr, want := range map[string]bool{
		"noreply@shop.example":        true,
		"no-reply+abc@x.example":      true,
		"notifications@forge.example": true,
		"ann@example.com":             false,
		"noreplyish@x.example":        true,
		"not-an-address":              false,
	} {
		if looksAutomated(addr) != want {
			t.Errorf("looksAutomated(%q) = %v", addr, !want)
		}
	}
}

func TestOpenAttachmentRejectsUnknownIDs(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	inbox, _ := browserAccount(t, db, ctx)
	id := insertWithParts(t, db, ctx, inbox, 3, "a@b", time.Now(),
		storage.IncomingAttachment{Filename: "x.txt", Content: bytes.NewReader([]byte("x"))})
	if err := a.OpenAttachmentFile(id, 999); err == nil {
		t.Fatal("an attachment of another message was opened")
	}
}
