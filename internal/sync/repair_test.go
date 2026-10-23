package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// repairClient serves one message and can be told to refuse, which is what a
// message the server no longer has looks like from here.
type repairClient struct {
	fakeClient
	fetchErr error
}

func (c *repairClient) FetchMessage(uid imap.UID) (*pimap.Message, error) {
	c.fetched = append(c.fetched, uint32(uid))
	if c.fetchErr != nil {
		return nil, c.fetchErr
	}
	return &pimap.Message{
		UID: uid, Subject: "Grüße", Text: "café", CharsetGuess: "windows-1252",
	}, nil
}

func TestSyncRepairsMangledMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)

	broken := storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, UID: 1,
		MessageID: "a@example.com", Subject: "Gr\xfc\xdfe", BodyPlain: "caf\xe9",
	}
	if _, err := db.InsertMessage(ctx, &broken); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.MarkMangledMessages(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}

	client := &repairClient{uids: []uint32{1}}
	res, err := NewEngine(client, db, nil).SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Repaired != 1 || len(res.RepairedIDs) != 1 {
		t.Fatalf("repaired %d (%v), want 1", res.Repaired, res.RepairedIDs)
	}

	fixed, err := db.GetMessage(ctx, broken.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fixed.Subject != "Grüße" || fixed.BodyPlain != "café" {
		t.Errorf("text = %q / %q, want the refetched text", fixed.Subject, fixed.BodyPlain)
	}
	if fixed.CharsetGuess != "windows-1252" {
		t.Errorf("CharsetGuess = %q, want windows-1252", fixed.CharsetGuess)
	}
}

// the message is gone from the server: there is nothing to repair it from, so
// the mark comes off and the next sync does not try again.
func TestSyncStopsRetryingMessagesTheServerLost(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)

	broken := storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, UID: 1,
		MessageID: "a@example.com", BodyPlain: "caf\xe9",
	}
	if _, err := db.InsertMessage(ctx, &broken); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.MarkMangledMessages(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}

	client := &repairClient{uids: []uint32{1}, fetchErr: errors.New("no such message")}
	if _, err := NewEngine(client, db, nil).SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}

	left, err := db.MessagesNeedingRefetch(ctx, folder.ID, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d messages still marked after a failed refetch", len(left))
	}
}
