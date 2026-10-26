package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/peltonapp/Pelton/internal/storage"
)

// pushRejected marks the server refusing a push (ErrRefused from the adapter),
// as opposed to the local writes around it or a broken connection. The row
// keeps its pending marker and the next sync asks the server for it again, so
// a rejection does not hold the folder's cursor back.
type pushRejected struct{ err error }

// pushError wraps an adapter push error. Only a server refusal is soft; a
// transport failure or ErrGenerationChanged stays hard, so a dead session is
// not reported as a healthy one.
func pushError(err error) error {
	if errors.Is(err, ErrRefused) && !errors.Is(err, ErrGenerationChanged) {
		return pushRejected{err}
	}
	return err
}

func (p pushRejected) Error() string { return p.err.Error() }
func (p pushRejected) Unwrap() error { return p.err }

// IsSoftError reports an error after which the folder's sync otherwise went
// through: some messages could not be read (MessageError; they stay stubs)
// or the server refused a flag or delete push (the change stays pending for
// the next sync). The result returned with it is valid and should be used:
// log the error, announce what was stored, and do not fail the folder.
// SyncFolder and SyncFolderStubs return a soft error only when nothing hard
// went wrong.
func IsSoftError(err error) bool {
	return err != nil && (isMessageError(err) || isPushRejected(err))
}

// firstHard returns the first error that is not soft, else the first error.
func firstHard(errs ...error) error {
	var soft error
	for _, err := range errs {
		switch {
		case err == nil:
		case !IsSoftError(err):
			return err
		case soft == nil:
			soft = err
		}
	}
	return soft
}

// isPushRejected reports whether err is the server refusing a push.
func isPushRejected(err error) bool {
	var p pushRejected
	return errors.As(err, &p)
}

// pushFlags adds the merged flags on the server, then stores them and clears
// the pending marker. The push only adds: flags is local | server, so a
// managed flag missing from it was set by another client after the snapshot
// and must stay. A local change made after the sync read state is kept and
// stays pending; the next sync pushes it.
func (e *Engine) pushFlags(ctx context.Context, folder storage.Folder, state storage.MessageState, flags storage.Flag) error {
	if err := e.adapter.SetFlags(folderContext(ctx, folder), folder.RemoteID, state.RemoteID, flags); err != nil {
		return pushError(fmt.Errorf("sync: push flags for %q: %w", state.RemoteID, err))
	}
	return e.resolvePending(ctx, state, flags)
}

// resolvePending stores the merged flags and clears the pending marker,
// unless the row changed since the sync read state. pushFlags calls it after
// the push; the sync calls it alone when the server already had everything
// the local change wanted.
func (e *Engine) resolvePending(ctx context.Context, state storage.MessageState, flags storage.Flag) error {
	if _, err := e.store.ResolvePendingFlags(ctx, state.ID, state.Flags, flags); err != nil {
		return fmt.Errorf("sync: resolve pending flags for %q: %w", state.RemoteID, err)
	}
	return nil
}

// pushDeletes applies the user's deletions on the server, then removes them
// from the cache. The whole batch goes in one call.
//
// Deleting a message moves it to the account's trash when TrashRemoteID is set
// and the folder is not the trash itself; otherwise it is a permanent delete.
func (e *Engine) pushDeletes(ctx context.Context, folder storage.Folder, states []storage.MessageState) error {
	if len(states) == 0 {
		return nil
	}

	ids := make([]string, 0, len(states))
	for _, s := range states {
		ids = append(ids, s.RemoteID)
	}

	if e.trashable(folder) {
		if err := e.adapter.Move(ctx, folder.RemoteID, ids, e.TrashRemoteID); err != nil {
			return pushError(fmt.Errorf("sync: move to trash on server: %w", err))
		}
	} else if err := e.adapter.Delete(ctx, folder.RemoteID, ids); err != nil {
		return pushError(fmt.Errorf("sync: delete on server: %w", err))
	}

	for _, s := range states {
		if err := e.deleteLocal(ctx, folder, s); err != nil {
			return err
		}
	}
	return nil
}

// trashable reports whether deletions from this folder should move to the trash
// rather than be permanently deleted. Comparing the id as well as the remote id
// keeps a folder merely named like the trash from being mistaken for it.
func (e *Engine) trashable(folder storage.Folder) bool {
	if e.TrashRemoteID == "" {
		return false
	}
	return folder.ID != e.TrashFolderID && folder.RemoteID != e.TrashRemoteID
}
