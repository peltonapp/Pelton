package sync

import (
	"context"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// pinKeyword is the imap keyword a pinned message carries on the server, so a
// pin survives a reinstall and shows on another machine.
const pinKeyword imap.Flag = "$Pinned"

// syncPins reconciles pins for the selected mailbox. A pin changed here is
// stored on the server; otherwise the server's keyword wins. A mailbox that
// cannot keep the keyword leaves pins local, and its pending changes are
// settled so they are not retried every sync. serverPins holds every uid on
// the server. Failures are logged and retried next sync.
func (e *Engine) syncPins(ctx context.Context, folder storage.Folder, mbox *pimap.Mailbox, serverPins map[uint32]bool) {
	if !mbox.KeepsKeyword(pinKeyword) {
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
	for uid, s := range local {
		onServer, present := serverPins[uid]
		switch {
		case !present:
			continue
		case s.Pending && s.Pinned != onServer:
			e.pushPin(ctx, folder, s)
		case s.Pending || s.Pinned != onServer:
			if err := e.store.AdoptPin(ctx, s.ID, onServer); err != nil {
				e.log.Error("pin sync: adopt", "uid", uid, "err", err)
			}
		}
	}

	var ids map[uint32]int64
	for uid, pinned := range serverPins {
		if !pinned {
			continue
		}
		if _, known := local[uid]; known {
			continue
		}
		if ids == nil {
			if ids = e.folderIDsByUID(ctx, folder); ids == nil {
				return
			}
		}
		if id, ok := ids[uid]; ok {
			if err := e.store.AdoptPin(ctx, id, true); err != nil {
				e.log.Error("pin sync: adopt", "uid", uid, "err", err)
			}
		}
	}
}

// pushPin stores one local pin change on the server and settles it.
func (e *Engine) pushPin(ctx context.Context, folder storage.Folder, s storage.PinState) {
	uid := imap.UID(s.UID)
	var err error
	if s.Pinned {
		err = e.client.AddFlags(uid, pinKeyword)
	} else {
		err = e.client.RemoveFlags(uid, pinKeyword)
	}
	if err != nil {
		e.log.Error("pin sync: store keyword", "folder", folder.IMAPPath, "uid", s.UID, "err", err)
		return
	}
	if err := e.store.AdoptPin(ctx, s.ID, s.Pinned); err != nil {
		e.log.Error("pin sync: settle", "uid", s.UID, "err", err)
	}
}

// folderIDsByUID maps the folder's cached uids to message ids, nil on error.
func (e *Engine) folderIDsByUID(ctx context.Context, folder storage.Folder) map[uint32]int64 {
	states, err := e.store.ListMessageStates(ctx, folder.ID)
	if err != nil {
		e.log.Error("pin sync: list states", "folder", folder.IMAPPath, "err", err)
		return nil
	}
	out := make(map[uint32]int64, len(states))
	for _, s := range states {
		out[s.UID] = s.ID
	}
	return out
}
