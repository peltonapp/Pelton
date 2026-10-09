package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/peltonapp/Pelton/internal/storage"
)

// pushRejected marks a failure of the server half of a push (the adapter
// call), as opposed to the local writes around it. The row keeps its pending
// marker and the next sync asks the server for it again, so a rejection does
// not hold the folder's cursor back.
type pushRejected struct{ err error }

func (p pushRejected) Error() string { return p.err.Error() }
func (p pushRejected) Unwrap() error { return p.err }

// isPushRejected reports whether err is the server refusing a push.
func isPushRejected(err error) bool {
	var p pushRejected
	return errors.As(err, &p)
}

// pushFlags pushes the merged flags through the adapter, then stores them
// and clears the pending marker. A local change made after the sync read
// state is kept and stays pending; the next sync pushes it.
func (e *Engine) pushFlags(ctx context.Context, folder storage.Folder, state storage.MessageState, flags storage.Flag) error {
	if err := e.adapter.SetFlags(ctx, folder.RemoteID, state.RemoteID, flags); err != nil {
		return pushRejected{fmt.Errorf("sync: push flags for %q: %w", state.RemoteID, err)}
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
			return pushRejected{fmt.Errorf("sync: move to trash on server: %w", err)}
		}
	} else if err := e.adapter.Delete(ctx, folder.RemoteID, ids); err != nil {
		return pushRejected{fmt.Errorf("sync: delete on server: %w", err)}
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
