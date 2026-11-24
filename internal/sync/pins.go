package sync

import (
	"context"
	"strconv"

	"github.com/peltonapp/Pelton/internal/storage"
)

// syncPins reconciles pins for a folder its adapter just listed. A pin changed
// here is stored on the server; otherwise the server's keyword wins. A
// mailbox that cannot keep the keyword leaves pins local, and its pending
// changes are settled so they are not retried every sync. The adapter names
// the pins of the messages its last list covered (all of them after a full
// list, the changed ones after a delta): a pin it did not name is left as it
// is, except a pending local change, which is pushed. Failures are logged and
// retried next sync.
func (e *Engine) syncPins(ctx context.Context, folder storage.Folder) {
	src, ok := e.adapter.(PinSource)
	if !ok {
		return
	}
	serverPins, keeps := src.Pins(folder.RemoteID)
	if serverPins == nil {
		return
	}
	if !keeps {
		if err := e.store.ClearPinPending(ctx, folder.ID); err != nil {
			e.log.Error("pin sync: settle local pins", "folder", folder.IMAPPath, "err", err)
		}
		return
	}
	local, err := e.store.FolderPinStates(ctx, folder.ID)
	if err != nil {
		e.log.Error("pin sync: local pins", "folder", folder.IMAPPath, "err", err)
		return
	}
	seen := make(map[string]bool, len(local))
	for uid, s := range local {
		remoteID := strconv.FormatUint(uint64(uid), 10)
		seen[remoteID] = true
		onServer, present := serverPins[remoteID]
		switch {
		case s.Pending && (!present || s.Pinned != onServer):
			e.pushPin(ctx, src, folder, remoteID, s)
		case !present:
		case s.Pending || s.Pinned != onServer:
			if err := e.store.AdoptPin(ctx, s.ID, onServer); err != nil {
				e.log.Error("pin sync: adopt", "remote_id", remoteID, "err", err)
			}
		}
	}

	var ids map[string]int64
	for remoteID, pinned := range serverPins {
		if !pinned || seen[remoteID] {
			continue
		}
		if ids == nil {
			if ids = e.folderIDsByRemoteID(ctx, folder); ids == nil {
				return
			}
		}
		if id, ok := ids[remoteID]; ok {
			if err := e.store.AdoptPin(ctx, id, true); err != nil {
				e.log.Error("pin sync: adopt", "remote_id", remoteID, "err", err)
			}
		}
	}
}

// pushPin stores one local pin change on the server and settles it.
func (e *Engine) pushPin(ctx context.Context, src PinSource, folder storage.Folder, remoteID string, s storage.PinState) {
	if err := src.SetPin(folderContext(ctx, folder), folder.RemoteID, remoteID, s.Pinned); err != nil {
		e.log.Error("pin sync: store keyword", "folder", folder.IMAPPath, "remote_id", remoteID, "err", err)
		return
	}
	if err := e.store.AdoptPin(ctx, s.ID, s.Pinned); err != nil {
		e.log.Error("pin sync: settle", "remote_id", remoteID, "err", err)
	}
}

// folderIDsByRemoteID maps the folder's cached remote ids to message ids, nil
// on error.
func (e *Engine) folderIDsByRemoteID(ctx context.Context, folder storage.Folder) map[string]int64 {
	states, err := e.states.ListMessageStates(ctx, folder.ID)
	if err != nil {
		e.log.Error("pin sync: list states", "folder", folder.IMAPPath, "err", err)
		return nil
	}
	out := make(map[string]int64, len(states))
	for _, s := range states {
		out[s.RemoteID] = s.ID
	}
	return out
}
