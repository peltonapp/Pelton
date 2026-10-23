package sync

import (
	"context"
	"errors"
	"fmt"
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

// seedMangled stores message 1 with mis-decoded Latin-1 text and marks it for
// refetch.
func seedMangled(t *testing.T, db *storage.DB, folder storage.Folder) int64 {
	t.Helper()
	ctx := context.Background()
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
	return broken.ID
}

func TestSyncRepairsMangledMessages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	brokenID := seedMangled(t, db, folder)

	adapter := &repairAdapter{ids: fakeIDs(1)}
	res, err := NewEngine(adapter, db, nil).SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Repaired != 1 || len(res.RepairedIDs) != 1 {
		t.Fatalf("repaired %d (%v), want 1", res.Repaired, res.RepairedIDs)
	}

	fixed, err := db.GetMessage(ctx, brokenID)
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
	seedMangled(t, db, folder)

	adapter := &repairAdapter{ids: fakeIDs(1), fetchErr: fmt.Errorf("no such message: %w", ErrNotOnServer)}
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
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedMangled(t, db, folder)

	adapter := &repairAdapter{ids: fakeIDs(1)}
	res, err := NewEngine(adapter, db, nil).CompleteFolder(ctx, folder)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Repaired != 1 || len(res.RepairedIDs) != 1 {
		t.Fatalf("repaired %d (%v), want 1", res.Repaired, res.RepairedIDs)
	}
}

// A refetch that fails for any other reason (offline, a timeout, the server
// busy) keeps the mark: the message is still broken and the next try can work.
func TestRepairKeepsTheMarkOnATransientFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedMangled(t, db, folder)

	adapter := &repairAdapter{ids: fakeIDs(1), fetchErr: errors.New("i/o timeout")}
	if _, err := NewEngine(adapter, db, nil).SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if left, err := db.MessagesNeedingRefetch(ctx, folder.ID, 0); err != nil || len(left) != 1 {
		t.Fatalf("still marked %d, %v, want the mark kept", len(left), err)
	}
}

// An empty answer is how IMAP reports a uid the server no longer has.
func TestRepairClearsTheMarkWhenTheServerReturnsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedMangled(t, db, folder)

	engine := NewEngine(&emptyFetchAdapter{repairAdapter{ids: fakeIDs(1)}}, db, nil)
	res, err := engine.SyncFolder(ctx, folder)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Repaired != 0 {
		t.Errorf("repaired %d, want 0 for an empty refetch", res.Repaired)
	}
	if left, _ := db.MessagesNeedingRefetch(ctx, folder.ID, 0); len(left) != 0 {
		t.Errorf("%d still marked, want the mark cleared", len(left))
	}
}

type emptyFetchAdapter struct{ repairAdapter }

func (a *emptyFetchAdapter) Fetch(context.Context, string, []string) ([]Fetched, error) {
	return nil, nil
}

// A message the server serves but that does not parse cannot be repaired by
// downloading it again; the mark comes off.
func TestRepairClearsTheMarkOnAnUnreadableMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	seedMangled(t, db, folder)

	adapter := &repairAdapter{ids: fakeIDs(1), fetchErr: &MessageError{RemoteIDs: []string{"1"}, Err: errors.New("malformed MIME")}}
	if _, err := NewEngine(adapter, db, nil).SyncFolder(ctx, folder); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if left, err := db.MessagesNeedingRefetch(ctx, folder.ID, 0); err != nil || len(left) != 0 {
		t.Fatalf("still marked %d, %v, want the mark cleared", len(left), err)
	}
}
