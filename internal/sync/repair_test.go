package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// repairAdapter serves one message and can be told to refuse, which is what a
// message the server no longer has looks like from here.
type repairAdapter struct {
	fakeAdapter
	fetchErr error
}

func (a *repairAdapter) Fetch(_ context.Context, _ string, remoteIDs []string) ([]Fetched, error) {
	a.commands++
	if a.fetchErr != nil {
		for _, id := range remoteIDs {
			a.fetched = append(a.fetched, id)
		}
		return nil, a.fetchErr
	}
	out := make([]Fetched, 0, len(remoteIDs))
	for _, id := range remoteIDs {
		a.fetched = append(a.fetched, id)
		out = append(out, Fetched{
			RemoteID:     id,
			LegacyUID:    1,
			Subject:      "Grüße",
			Text:         "café",
			CharsetGuess: "windows-1252",
		})
	}
	return out, nil
}

func TestSyncRepairsMangledMessages(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)

	broken := storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, UID: 1, RemoteID: "1",
		MessageID: "a@example.com", Subject: "Gr\xfc\xdfe", BodyPlain: "caf\xe9",
	}
	if _, err := db.InsertMessage(ctx, &broken); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.MarkMangledMessages(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}

	adapter := &repairAdapter{ids: fakeIDs(1)}
	res, err := NewEngine(adapter, db, nil).SyncFolder(ctx, folder)
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
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)

	broken := storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, UID: 1, RemoteID: "1",
		MessageID: "a@example.com", BodyPlain: "caf\xe9",
	}
	if _, err := db.InsertMessage(ctx, &broken); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.MarkMangledMessages(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}

	adapter := &repairAdapter{ids: fakeIDs(1), fetchErr: errors.New("no such message")}
	if _, err := NewEngine(adapter, db, nil).SyncFolder(ctx, folder); err != nil {
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

// The initial sync calls CompleteFolder after stubs and bodies instead of SyncFolder.
// Repair still has to run there.
func TestCompleteFolderRepairsMangledMessages(t *testing.T) {
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)

	broken := storage.Message{
		AccountID: folder.AccountID, FolderID: folder.ID, UID: 1, RemoteID: "1",
		MessageID: "a@example.com", Subject: "Gr\xfc\xdfe", BodyPlain: "caf\xe9",
	}
	if _, err := db.InsertMessage(ctx, &broken); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.MarkMangledMessages(ctx); err != nil {
		t.Fatalf("mark: %v", err)
	}

	adapter := &repairAdapter{ids: fakeIDs(1)}
	res, err := NewEngine(adapter, db, nil).CompleteFolder(ctx, folder)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Repaired != 1 || len(res.RepairedIDs) != 1 {
		t.Fatalf("repaired %d (%v), want 1", res.Repaired, res.RepairedIDs)
	}
}
