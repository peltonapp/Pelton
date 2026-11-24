package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// gatedReader blocks its first Read until released, holding a fill in the
// middle of writing its attachment files.
type gatedReader struct {
	r       io.Reader
	reached chan struct{}
	release chan struct{}
	started bool
}

func (g *gatedReader) Read(p []byte) (int, error) {
	if !g.started {
		g.started = true
		close(g.reached)
		<-g.release
	}
	return g.r.Read(p)
}

// Two fills of one stub: A is writing its third attachment when B starts. B
// must wait for A's commit before touching the message directory and then
// give up, leaving every file A committed. Staging before the write lock let
// B move A's first files aside, A wrote its fourth into a new live dir, and
// B's abandon then deleted it.
func TestConcurrentBodyFillsKeepCommittedFiles(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	id, err := db.UpsertMessageListMeta(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "7", UID: 7, Subject: "stub"})
	if err != nil {
		t.Fatal(err)
	}
	gate := &gatedReader{r: bytes.NewReader([]byte("three")), reached: make(chan struct{}), release: make(chan struct{})}
	errA := make(chan error, 1)
	go func() {
		_, err := db.FillMessageBody(ctx, BodyTarget{}, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "7", UID: 7, BodyPlain: "A"},
			[]IncomingAttachment{
				{Filename: "one.txt", Content: bytes.NewReader([]byte("one"))},
				{Filename: "two.txt", Content: bytes.NewReader([]byte("two"))},
				{Filename: "three.txt", Content: gate},
				{Filename: "four.txt", Content: bytes.NewReader([]byte("four"))},
			})
		errA <- err
	}()
	<-gate.reached

	live := filepath.Join(db.AttachmentsDir(), accountSegment(accountID), messageSegment(id))
	errB := make(chan error, 1)
	go func() {
		_, err := db.FillMessageBody(ctx, BodyTarget{}, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "7", UID: 7, BodyPlain: "B"},
			[]IncomingAttachment{{Filename: "b.txt", Content: bytes.NewReader([]byte("b"))}})
		errB <- err
	}()
	// Give B time to reach its write; with staging outside the lock it moves
	// A's directory away first.
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(live, "one.txt")); err != nil {
			break
		}
	}
	close(gate.release)

	if err := <-errA; err != nil {
		t.Fatalf("fill A: %v", err)
	}
	if err := <-errB; !errors.Is(err, ErrMessageBodyComplete) {
		t.Fatalf("fill B: err = %v, want ErrMessageBodyComplete", err)
	}
	atts, err := db.ListAttachments(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 4 {
		t.Fatalf("attachments %+v, want A's four", atts)
	}
	for _, a := range atts {
		if _, err := os.Stat(filepath.Join(db.AttachmentsDir(), filepath.FromSlash(a.DiskPath))); err != nil {
			t.Errorf("committed attachment %s lost: %v", a.DiskPath, err)
		}
	}
	if got := mustMessage(t, db, id); got.BodyPlain != "A" {
		t.Errorf("body %q, want A's", got.BodyPlain)
	}
	if roots, _ := filepath.Glob(filepath.Join(db.AttachmentsDir(), cacheStagingDirName+"-*")); len(roots) != 0 {
		t.Errorf("staging roots left behind: %v", roots)
	}
}

// A fill that loses to a completed row leaves no staging tree behind.
func TestAbandonedFillRemovesStagingRoot(t *testing.T) {
	ctx := context.Background()
	db, accountID, folder := newBodyTestFolder(t)
	id, err := db.InsertMessageWithAttachments(ctx, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "9", Subject: "s"},
		[]IncomingAttachment{{Filename: "old.txt", Content: bytes.NewReader([]byte("old"))}})
	if err != nil {
		t.Fatal(err)
	}
	upExec(t, db.sql, `UPDATE messages SET body_complete = 0 WHERE id = ?`, id)
	db.deleteTxHook = func(context.Context) error { return errors.New("inject fail") }
	if _, err := db.FillMessageBody(ctx, BodyTarget{}, &Message{AccountID: accountID, FolderID: folder.ID, RemoteID: "9"},
		[]IncomingAttachment{{Filename: "new.txt", Content: bytes.NewReader([]byte("new"))}}); err == nil {
		t.Fatal("fill succeeded despite the injected failure")
	}
	if roots, _ := filepath.Glob(filepath.Join(db.AttachmentsDir(), cacheStagingDirName+"-*")); len(roots) != 0 {
		t.Errorf("staging roots left behind: %v", roots)
	}
	if _, err := os.Stat(filepath.Join(db.AttachmentsDir(), accountSegment(accountID), messageSegment(id), "old.txt")); err != nil {
		t.Errorf("old attachment not restored: %v", err)
	}
}
