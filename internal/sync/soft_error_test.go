package sync

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

// pushRefusingFake refuses every flag push.
type pushRefusingFake struct{ *fakeAdapter }

func (pushRefusingFake) SetFlags(context.Context, string, string, storage.Flag) error {
	return fmt.Errorf("%w: NO [READONLY] mailbox is read-only", ErrRefused)
}

// A refused flag push and an unreadable message are soft: SyncFolder still
// stores and reports the new mail, and says so with an error IsSoftError
// accepts. A hard body error in the same run outranks the refused push.
func TestSyncFolderSoftErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		fetchErr error
		wantSoft bool
	}{
		{"unreadable message", &MessageError{RemoteIDs: []string{"3"}, Err: errors.New("malformed MIME")}, true},
		{"connection lost", errors.New("connection reset by peer"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, folder := newSyncTestFolder(t)
			fake := &fakeAdapter{ids: fakeIDs(1, 2)}
			if _, err := NewEngine(fake, db, nil).SyncFolder(ctx, folder); err != nil {
				t.Fatal(err)
			}
			markPendingSeen(t, db, folder, "1")
			fake.ids = fakeIDs(1, 2, 3, 4)
			fake.fetchErr = tc.fetchErr

			res, err := NewEngine(pushRefusingFake{fake}, db, nil).SyncFolder(ctx, folder)
			if err == nil {
				t.Fatal("no error for a refused push")
			}
			if IsSoftError(err) != tc.wantSoft {
				t.Fatalf("IsSoftError(%v) = %v, want %v", err, IsSoftError(err), tc.wantSoft)
			}
			if res.New != 2 || len(res.NewIDs) != 2 {
				t.Errorf("new %d %v, want the two stored bodies reported", res.New, res.NewIDs)
			}
			states, err := db.ListMessageStates(ctx, folder.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range states {
				if s.RemoteID == "1" && !s.PendingFlags {
					t.Error("refused push lost its pending mark")
				}
			}
		})
	}
}

// scriptedFetchAdapter returns errs[i] from the i-th Fetch call, after the
// inner fake has served the batch.
type scriptedFetchAdapter struct {
	*fakeAdapter
	errs  []error
	calls int
}

func (a *scriptedFetchAdapter) Fetch(ctx context.Context, box string, ids []string) ([]Fetched, error) {
	out, err := a.fakeAdapter.Fetch(ctx, box, ids)
	if i := a.calls; i < len(a.errs) && a.errs[i] != nil {
		err = a.errs[i]
	}
	a.calls++
	return out, err
}

// An unreadable message in one batch and a dropped connection in the next:
// the dropped connection is hard and must win, or the dead session would be
// reported healthy and the rest of the bodies skipped silently.
func TestFetchNewHardErrorOutranksEarlierSoft(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	fake := &scriptedFetchAdapter{
		fakeAdapter: &fakeAdapter{ids: fakeIDs(1, 2, 3)},
		errs: []error{
			&MessageError{RemoteIDs: []string{"3"}, Err: errors.New("malformed MIME")},
			errors.New("connection reset by peer"),
		},
	}
	engine := NewEngine(fake, db, nil)
	engine.FetchBatchSize = 1
	_, err := engine.SyncFolder(ctx, folder)
	if err == nil {
		t.Fatal("no error for a dropped connection")
	}
	if IsSoftError(err) {
		t.Fatalf("IsSoftError(%v) = true, want the connection reset to be hard", err)
	}
	if fake.calls != 2 {
		t.Errorf("fetch calls = %d, want the campaign to stop at the dropped connection", fake.calls)
	}
}

// pushFailingFake fails every flag push and delete with err.
type pushFailingFake struct {
	*fakeAdapter
	err error
}

func (a pushFailingFake) SetFlags(context.Context, string, string, storage.Flag) error {
	return a.err
}

func (a pushFailingFake) Delete(context.Context, string, []string) error { return a.err }

// Only the server refusing a push is soft. A dropped connection or a changed
// UIDVALIDITY during the push is hard: the session or the ids are no good.
func TestPushErrorOnlyRefusalIsSoft(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		err      error
		wantSoft bool
	}{
		{"refused", fmt.Errorf("%w: NO [READONLY] mailbox is read-only", ErrRefused), true},
		{"connection lost", errors.New("connection reset by peer"), false},
		{"generation changed", fmt.Errorf("imap adapter: %w", ErrGenerationChanged), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fake := &fakeAdapter{ids: fakeIDs(1, 2)}
			for _, push := range []struct {
				name    string
				prepare func(*testing.T, *storage.DB, storage.Folder)
			}{
				{"flags", func(t *testing.T, db *storage.DB, f storage.Folder) { markPendingSeen(t, db, f, "1") }},
				{"delete", func(t *testing.T, db *storage.DB, f storage.Folder) { deleteOne(t, db, f.ID, "1") }},
			} {
				t.Run(push.name, func(t *testing.T) {
					db, folder := newSyncTestFolder(t)
					if _, err := NewEngine(fake, db, nil).SyncFolder(ctx, folder); err != nil {
						t.Fatal(err)
					}
					push.prepare(t, db, folder)
					_, err := NewEngine(pushFailingFake{fake, tc.err}, db, nil).SyncFolder(ctx, folder)
					if err == nil {
						t.Fatal("no error for a failed push")
					}
					if IsSoftError(err) != tc.wantSoft {
						t.Fatalf("IsSoftError(%v) = %v, want %v", err, IsSoftError(err), tc.wantSoft)
					}
				})
			}
		})
	}
}
