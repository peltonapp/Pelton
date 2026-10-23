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

// downloadInbox is an app with one IMAP account holding an empty INBOX.
func downloadInbox(t *testing.T) (*App, *storage.DB, context.Context, int64, *storage.Folder) {
	t.Helper()
	a, db, ctx := moveTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	return a, db, ctx, accountID, inbox
}

// stateIDByUID is the id of the folder's message with uid, failing the test
// when it was not stored.
func stateIDByUID(t *testing.T, db *storage.DB, folderID int64, uid uint32) int64 {
	t.Helper()
	states, err := db.MessageBodyStates(context.Background(), folderID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.UID == uid {
			return s.ID
		}
	}
	t.Fatalf("uid %d was not stored", uid)
	return 0
}

// IMAP still asks the server what is in range, since the cache may not hold
// every uid. A uid already cached with its body is only pinned; one missing
// from the cache is fetched, stored and pinned.
func TestDownloadPlansAndFetchesIMAPUIDs(t *testing.T) {
	a, db, ctx, accountID, inbox := downloadInbox(t)
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
	fetchedID := stateIDByUID(t, db, inbox.ID, 2)
	fetched := mustGetMessage(t, db, fetchedID)
	if !fetched.BodyComplete || !fetched.Offline || fetched.BodyPlain != "fresh body" {
		t.Errorf("uid 2 body complete %v offline %v body %q", fetched.BodyComplete, fetched.Offline, fetched.BodyPlain)
	}
	if !mustGetMessage(t, db, cachedID).Offline {
		t.Error("uid 1 was cached with its body but not pinned offline")
	}
}

// A uid cached as a stub is in the range the user asked to keep offline: it
// is fetched, filled with its attachments as a sync's body fill would, and
// pinned. A uid not cached at all is stored without attachments, as the
// user asked, and pinned.
func TestDownloadFetchesAndPinsCachedStubs(t *testing.T) {
	a, db, ctx, accountID, inbox := downloadInbox(t)
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
	if len(tasks) != 2 || len(pin) != 0 {
		t.Fatalf("tasks %+v pin %v, want uids 2 and 3 to fetch", tasks, pin)
	}
	if err := a.runDownload(ctx, tasks, pin, false, len(tasks)); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	newID := stateIDByUID(t, db, inbox.ID, 2)
	if atts, err := db.ListAttachments(ctx, newID); err != nil || len(atts) != 0 {
		t.Errorf("uid 2 has %d attachments (%v), want none", len(atts), err)
	}
	if !mustGetMessage(t, db, newID).Offline {
		t.Error("uid 2 was downloaded but not pinned offline")
	}
	if m := mustGetMessage(t, db, stubID); !m.BodyComplete || !m.Offline || m.BodyPlain != "filled" {
		t.Errorf("stub body complete %v offline %v body %q, want a pinned body", m.BodyComplete, m.Offline, m.BodyPlain)
	}
	if atts, err := db.ListAttachments(ctx, stubID); err != nil || len(atts) != 1 {
		t.Errorf("stub has %d attachments (%v), want its one", len(atts), err)
	}
}

// A sync can store a planned message between the plan and the fetch. Either
// way it is in the range the user asked to keep offline: one stored with its
// body is pinned, one listed as a stub is filled with the fetched body and
// pinned.
func TestDownloadAfterASyncStoredThePlannedMessage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		complete bool
	}{
		{name: "with its body", complete: true},
		{name: "as a stub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, db, ctx, accountID, inbox := downloadInbox(t)
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
			row := &storage.Message{AccountID: accountID, FolderID: inbox.ID, UID: 1, Subject: "hi", Date: time.Now()}
			var id int64
			if tc.complete {
				row.BodyPlain = "synced"
				id, err = db.InsertMessageWithAttachments(ctx, row, nil)
			} else {
				id, err = db.UpsertMessageListMeta(ctx, row)
			}
			if err != nil {
				t.Fatalf("sync store: %v", err)
			}
			if err := a.runDownload(ctx, tasks, pin, true, len(tasks)); err != nil {
				t.Fatalf("runDownload: %v", err)
			}
			m := mustGetMessage(t, db, id)
			if !m.Offline || !m.BodyComplete {
				t.Errorf("offline %v body complete %v, want a pinned body", m.Offline, m.BodyComplete)
			}
		})
	}
}

func mustGetMessage(t *testing.T, db *storage.DB, id int64) *storage.Message {
	t.Helper()
	m, err := db.GetMessage(context.Background(), id)
	if err != nil {
		t.Fatalf("get message %d: %v", id, err)
	}
	return m
}
