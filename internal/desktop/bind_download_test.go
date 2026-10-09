package desktop

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// IMAP still asks the server what is in range, since the cache may not hold
// every uid. A uid already cached with its body is only pinned; one missing
// from the cache is fetched, stored and pinned.
func TestDownloadPlansAndFetchesIMAPUIDs(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	cachedID, err := db.InsertMessage(ctx, &storage.Message{
		AccountID: accountID, FolderID: inbox.ID, UID: 1, Subject: "cached",
		BodyPlain: "body", Date: time.Now(), BodyComplete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeIMAP{
		since: []imap.UID{1, 2},
		messages: map[imap.UID]*pimap.Message{
			2: {UID: 2, Subject: "new", Text: "fresh body", Date: time.Now()},
		},
	}
	useFake(t, a, accountID, client)

	tasks, pin, err := a.planDownload(ctx, time.Now().AddDate(0, -1, 0))
	if err != nil {
		t.Fatalf("planDownload: %v", err)
	}
	if len(tasks) != 1 || tasks[0].remoteID != "2" {
		t.Fatalf("tasks %+v, want only uid 2", tasks)
	}
	if !slices.Equal(pin, []int64{cachedID}) {
		t.Fatalf("pin %v, want [%d]", pin, cachedID)
	}

	if err := a.runDownload(context.Background(), tasks, pin, true, len(tasks)); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	states, err := db.MessageBodyStates(ctx, inbox.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var fetchedID int64
	for _, s := range states {
		if s.UID == 2 {
			fetchedID = s.ID
		}
	}
	if fetchedID == 0 {
		t.Fatal("uid 2 was not stored")
	}
	fetched, err := db.GetMessage(ctx, fetchedID)
	if err != nil {
		t.Fatal(err)
	}
	if !fetched.BodyComplete || !fetched.Offline || fetched.BodyPlain != "fresh body" {
		t.Errorf("uid 2 body complete %v offline %v body %q", fetched.BodyComplete, fetched.Offline, fetched.BodyPlain)
	}
	cached, err := db.GetMessage(ctx, cachedID)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Offline {
		t.Error("uid 1 was cached with its body but not pinned offline")
	}
}

// For IMAP the same holds for a uid cached without its body, while a uid not
// cached at all is stored without attachments, as the user asked.
func TestDownloadWithoutAttachmentsSkipsThemOnlyForUncachedMessages(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	stubID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID, FolderID: inbox.ID, UID: 3, Subject: "stub", Date: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	file := []pimap.Attachment{{Filename: "notes.txt", ContentType: "text/plain", Content: []byte("attached text")}}
	client := &fakeIMAP{
		since: []imap.UID{2, 3},
		messages: map[imap.UID]*pimap.Message{
			2: {UID: 2, Subject: "new", Text: "fresh", Date: time.Now(), Attachments: file},
			3: {UID: 3, Subject: "stub", Text: "filled", Date: time.Now(), Attachments: file},
		},
	}
	useFake(t, a, accountID, client)

	tasks, pin, err := a.planDownload(ctx, time.Now().AddDate(0, -1, 0))
	if err != nil {
		t.Fatalf("planDownload: %v", err)
	}
	if err := a.runDownload(ctx, tasks, pin, false, len(tasks)); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	states, err := db.MessageBodyStates(ctx, inbox.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var newID int64
	for _, s := range states {
		if s.UID == 2 {
			newID = s.ID
		}
	}
	if newID == 0 {
		t.Fatal("uid 2 was not stored")
	}
	for id, want := range map[int64]int{stubID: 1, newID: 0} {
		atts, err := db.ListAttachments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if len(atts) != want {
			t.Errorf("message %d has %d attachments, want %d", id, len(atts), want)
		}
	}
}

// Body sync can store a planned message between the plan and the fetch. The
// download then finds the row complete and stores nothing, but the message is
// still in the range the user asked to keep offline, so it has to be pinned.
func TestDownloadPinsAMessageWhoseBodyLandedAfterThePlan(t *testing.T) {
	a, db, ctx := moveTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	stubID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID, FolderID: inbox.ID, UID: 1, Subject: "hi", Date: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeIMAP{
		since: []imap.UID{1},
		messages: map[imap.UID]*pimap.Message{
			1: {UID: 1, Subject: "hi", Text: "fetched", Date: time.Now()},
		},
	}
	useFake(t, a, accountID, client)

	tasks, pin, err := a.planDownload(ctx, time.Now().AddDate(0, -1, 0))
	if err != nil {
		t.Fatalf("planDownload: %v", err)
	}
	if _, err := db.InsertMessageWithAttachments(ctx, &storage.Message{
		AccountID: accountID, FolderID: inbox.ID, UID: 1, Subject: "hi", BodyPlain: "synced", Date: time.Now(),
	}, nil); err != nil {
		t.Fatalf("body sync fill: %v", err)
	}
	if err := a.runDownload(ctx, tasks, pin, true, len(tasks)); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	got, err := db.GetMessage(ctx, stubID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Offline {
		t.Error("a message whose body landed after the plan was not pinned offline")
	}
}
