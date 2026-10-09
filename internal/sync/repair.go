package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/peltonapp/Pelton/internal/storage"
)

// repairBatch caps how many mangled messages one folder sync refetches. The
// mark is left on the rest, so a mailbox full of them is repaired over several
// syncs rather than turning one into a full redownload.
const repairBatch = 50

// repairMangled refetches messages that were cached before charset detection
// existed and whose stored text is not valid utf-8. Only the text is replaced:
// the message is the same message, so its flags, colour and attachments stay
// as they are.
//
// A message the server no longer has loses its mark instead, otherwise every
// sync from here on would try it again.
func (e *Engine) repairMangled(ctx context.Context, folder storage.Folder, res *FolderSyncResult) {
	broken, err := e.store.MessagesNeedingRefetch(ctx, folder.ID, repairBatch)
	if err != nil {
		e.log.Error("list messages needing refetch", "folder", folder.IMAPPath, "err", err)
		return
	}
	for _, m := range broken {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := e.repairOne(ctx, folder, m); err != nil {
			e.log.Error("refetch mangled message", "folder", folder.IMAPPath, "uid", m.UID, "err", err)
			continue
		}
		res.Repaired++
		res.RepairedIDs = append(res.RepairedIDs, m.ID)
	}
}

func (e *Engine) repairOne(ctx context.Context, folder storage.Folder, m storage.MangledMessage) error {
	stored, err := e.store.GetMessage(ctx, m.ID)
	if err != nil {
		return err
	}
	remoteID := stored.RemoteID
	if remoteID == "" {
		return fmt.Errorf("sync: mangled message %d has empty remote id", m.ID)
	}
	fetched, err := e.adapter.Fetch(ctx, folder.RemoteID, []string{remoteID})
	if err != nil || len(fetched) == 0 {
		if clearErr := e.store.ClearRefetchMark(ctx, m.ID); clearErr != nil {
			return errors.Join(err, clearErr)
		}
		if err != nil {
			return fmt.Errorf("sync: refetch message %q: %w", remoteID, err)
		}
		return fmt.Errorf("sync: refetch message %q: empty fetch", remoteID)
	}
	msg := fetched[0]
	if err := e.store.RepairMessageText(ctx, m.ID, msg.Subject, msg.Text, msg.HTML, msg.CharsetGuess); err != nil {
		return err
	}
	return nil
}
