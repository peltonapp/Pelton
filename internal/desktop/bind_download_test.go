package desktop

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
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

// A stub inside the sync window is filled with its attachments whatever the
// choice: nothing fetches them later. A uid with no row inside the window
// (uid 4) is stored with its attachments whatever the choice too, since a sync
// that lists it later will not fetch them. "Without attachments" holds only
// below the window: a stub there (uid 1) and a uid with no row there (uid 2)
// are stored body-complete with no attachment files. "With attachments" stores
// them all with files.
func TestDownloadAttachmentChoiceHoldsOutsideTheWindow(t *testing.T) {
	for _, include := range []bool{false, true} {
		t.Run(map[bool]string{false: "without", true: "with"}[include], func(t *testing.T) { downloadStubWithChoice(t, include) })
	}
}

func downloadStubWithChoice(t *testing.T, include bool) {
	a, db, ctx, accountID, inbox := downloadInbox(t)
	if err := db.SetFolderSyncFloorUID(ctx, inbox.ID, 3); err != nil {
		t.Fatal(err)
	}
	stub := func(uid imap.UID, subject string) int64 {
		id, err := db.UpsertMessageListMeta(ctx, &storage.Message{
			AccountID: accountID, FolderID: inbox.ID, UID: uint32(uid), Subject: subject, Date: time.Now(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	inWindowID := stub(3, "in window")
	belowID := stub(1, "below the window")
	file := []pimap.Attachment{{Filename: "notes.txt", ContentType: "text/plain", Content: []byte("attached text")}}
	client := &fakeIMAP{
		since: []imap.UID{1, 2, 3, 4},
		messages: map[imap.UID]*pimap.Message{
			1: {UID: 1, Subject: "below the window", Text: "old", Date: time.Now(), Attachments: file},
			2: {UID: 2, Subject: "new", Text: "fresh", Date: time.Now(), Attachments: file},
			3: {UID: 3, Subject: "in window", Text: "filled", Date: time.Now(), Attachments: file},
			4: {UID: 4, Subject: "new in window", Text: "fresh", Date: time.Now(), Attachments: file},
		},
	}
	useFake(t, a, accountID, client)

	tasks, pin, err := a.planDownload(ctx, time.Now().AddDate(0, -1, 0))
	if err != nil {
		t.Fatalf("planDownload: %v", err)
	}
	if err := a.runDownload(ctx, tasks, pin, include, len(tasks)); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	newID := stateIDByUID(t, db, inbox.ID, 2)
	newInWindowID := stateIDByUID(t, db, inbox.ID, 4)
	for id, withFiles := range map[int64]bool{inWindowID: true, belowID: include, newID: include, newInWindowID: true} {
		m := mustGetMessage(t, db, id)
		if !m.BodyComplete || !m.Offline || m.HasAttachments != withFiles {
			t.Errorf("include=%v: message %q body complete %v offline %v has attachments %v, want attachments %v",
				include, m.Subject, m.BodyComplete, m.Offline, m.HasAttachments, withFiles)
		}
		atts, err := db.ListAttachments(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{false: 0, true: 1}[withFiles]; len(atts) != want {
			t.Errorf("include=%v: message %q has %d attachments, want %d", include, m.Subject, len(atts), want)
		}
	}
}

// Body sync can store a planned message between the plan and the fetch. The
// download then finds the row complete and stores nothing, but the message is
// still in the range the user asked to keep offline, so it has to be pinned.
func TestDownloadPinsAMessageWhoseBodyLandedAfterThePlan(t *testing.T) {
	a, db, ctx, accountID, inbox := downloadInbox(t)
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
	if !mustGetMessage(t, db, stubID).Offline {
		t.Error("a message whose body landed after the plan was not pinned offline")
	}
}

// A sync can list a planned message as a stub between the plan and the
// fetch. The download fills that stub in place and pins it: with its
// attachments inside the sync window even "without attachments", with the
// choice (here: without) below it.
func TestDownloadFillsAStubListedAfterThePlanByTheWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		floor uint32
		want  int
	}{{"in window", 0, 1}, {"below the window", 5, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			a, db, ctx, accountID, inbox := downloadInbox(t)
			if err := db.SetFolderSyncFloorUID(ctx, inbox.ID, tc.floor); err != nil {
				t.Fatal(err)
			}
			file := []pimap.Attachment{{Filename: "notes.txt", ContentType: "text/plain", Content: []byte("attached text")}}
			client := &fakeIMAP{
				since: []imap.UID{2},
				messages: map[imap.UID]*pimap.Message{
					2: {UID: 2, Subject: "new", Text: "fresh", Date: time.Now(), Attachments: file},
				},
			}
			useFake(t, a, accountID, client)

			tasks, pin, err := a.planDownload(ctx, time.Now().AddDate(0, -1, 0))
			if err != nil {
				t.Fatalf("planDownload: %v", err)
			}
			stubID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
				AccountID: accountID, FolderID: inbox.ID, UID: 2, Subject: "new", Date: time.Now(),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := a.runDownload(ctx, tasks, pin, false, len(tasks)); err != nil {
				t.Fatalf("runDownload: %v", err)
			}
			m := mustGetMessage(t, db, stubID)
			if !m.BodyComplete || !m.Offline {
				t.Errorf("stub body complete %v offline %v, want both", m.BodyComplete, m.Offline)
			}
			if atts, err := db.ListAttachments(ctx, stubID); err != nil || len(atts) != tc.want {
				t.Errorf("stub has %d attachments (%v), want %d", len(atts), err, tc.want)
			}
		})
	}
}

// A stub the user moves or archives while the download runs is gone from the
// folder. The download fills stubs in place only, so it does not store the
// message there again (a ghost row, pinned offline).
func TestDownloadDoesNotBringBackAMovedStub(t *testing.T) {
	a, db, ctx, accountID, inbox := downloadInbox(t)
	stubID, err := db.UpsertMessageListMeta(ctx, &storage.Message{
		AccountID: accountID, FolderID: inbox.ID, UID: 3, Subject: "stub", Date: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeIMAP{
		since:    []imap.UID{3},
		messages: map[imap.UID]*pimap.Message{3: {UID: 3, Subject: "stub", Text: "body", Date: time.Now()}},
	}
	useFake(t, a, accountID, client)
	tasks, pin, err := a.planDownload(ctx, time.Now().AddDate(0, -1, 0))
	if err != nil {
		t.Fatalf("planDownload: %v", err)
	}
	if err := db.DeleteMessage(ctx, stubID); err != nil { // moved away meanwhile
		t.Fatal(err)
	}
	if err := a.runDownload(ctx, tasks, pin, true, len(tasks)); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	if _, err := db.MessageIDByRemoteID(ctx, inbox.ID, "3"); !errors.Is(err, storage.ErrMessageNotFound) {
		t.Fatalf("the moved stub is back in the folder (%v)", err)
	}
}

// fetchLog records the uids of every FETCH.
type fetchLog struct {
	fakeIMAP
	mu      sync.Mutex
	batches [][]imap.UID
}

func (c *fetchLog) FetchMessages(uids []imap.UID, fn func(imap.UID, *pimap.Message, error) error) error {
	c.mu.Lock()
	c.batches = append(c.batches, slices.Clone(uids))
	c.mu.Unlock()
	return c.fakeIMAP.FetchMessages(uids, fn)
}

// An offline unit is one adaptive body batch, like the body generator's, so
// it never holds a session for a long fetch: the first of a folder's 12
// stubs come 5 at a time (the starting count), newest first, and the count
// adapts from there.
func TestDownloadUnitsAreAdaptiveBodyBatches(t *testing.T) {
	a, db, ctx, accountID, inbox := downloadInbox(t)
	client := &fetchLog{}
	client.messages = make(map[imap.UID]*pimap.Message)
	for uid := imap.UID(1); uid <= 12; uid++ {
		client.since = append(client.since, uid)
		client.messages[uid] = &pimap.Message{UID: uid, Subject: "s", Text: "body", Date: time.Now()}
		if _, err := db.UpsertMessageListMeta(ctx, &storage.Message{
			AccountID: accountID, FolderID: inbox.ID, UID: uint32(uid), Subject: "s", Date: time.Now(), SizeBytes: 1 << 10,
		}); err != nil {
			t.Fatal(err)
		}
	}
	useFake(t, a, accountID, &client.fakeIMAP)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) { return client, nil }
	tasks, pin, err := a.planDownload(ctx, time.Now().AddDate(0, -1, 0))
	if err != nil {
		t.Fatalf("planDownload: %v", err)
	}
	if err := a.runDownload(ctx, tasks, pin, true, len(tasks)); err != nil {
		t.Fatalf("runDownload: %v", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.batches) < 2 || len(client.batches[0]) != syncsched.DefaultBodyCount || client.batches[0][0] != 12 {
		t.Fatalf("fetches %v, want a first batch of %d, newest first", client.batches, syncsched.DefaultBodyCount)
	}
	for uid := uint32(1); uid <= 12; uid++ {
		if m := mustGetMessage(t, db, stateIDByUID(t, db, inbox.ID, uid)); !m.BodyComplete || !m.Offline {
			t.Fatalf("uid %d complete %v offline %v", uid, m.BodyComplete, m.Offline)
		}
	}
}
