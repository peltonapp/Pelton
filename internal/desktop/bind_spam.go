package desktop

import (
	"fmt"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The RFC 5788 keywords spam filters (rspamd, Dovecot's antispam plugins,
// Stalwart) train on when a client marks or rescues a message.
const (
	keywordJunk    imap.Flag = "$Junk"
	keywordNotJunk imap.Flag = "$NotJunk"
)

// MarkSpam moves a message to its account's junk folder and tags it $Junk
// (dropping $NotJunk) where the server keeps keywords. The folder it came from
// is remembered so MarkNotSpam can send it back there. It errors when the
// account has no junk folder or the message is already in it.
func (a *App) MarkSpam(id int64) (ArchiveUndoDTO, error) {
	if err := a.ready(); err != nil {
		return ArchiveUndoDTO{}, err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	junk, ok, err := a.findRoleFolder(m.AccountID, roleJunk)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	if !ok {
		return ArchiveUndoDTO{}, fmt.Errorf("pelton: this account has no spam folder")
	}
	undo, err := a.moveMessageTo(m, junk, &keywordChange{add: keywordJunk, remove: keywordNotJunk})
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	if m.MessageID != "" {
		// the move already happened, so losing the record only means a later
		// "not spam" falls back to the inbox.
		if err := a.store.RecordSpamOrigin(a.ctx, m.AccountID, m.MessageID, m.FolderID); err != nil {
			a.log.Error("mark spam: record origin", "id", m.ID, "err", err)
		}
	}
	return undo, nil
}

// MarkNotSpam moves a message out of the junk folder and tags it $NotJunk
// (dropping $Junk) where the server keeps keywords. It goes back to the folder
// it was marked as spam from when that was recorded here and the folder still
// exists; otherwise, as for anything the server filtered, to the inbox.
func (a *App) MarkNotSpam(id int64) (ArchiveUndoDTO, error) {
	if err := a.ready(); err != nil {
		return ArchiveUndoDTO{}, err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	dest, err := a.notSpamDestination(m)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	undo, err := a.moveMessageTo(m, dest, &keywordChange{add: keywordNotJunk, remove: keywordJunk})
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	if m.MessageID != "" {
		if err := a.store.ForgetSpamOrigin(a.ctx, m.AccountID, m.MessageID); err != nil {
			a.log.Error("not spam: forget origin", "id", m.ID, "err", err)
		}
	}
	return undo, nil
}

// notSpamDestination picks where a rescued message goes: its recorded origin if
// that folder is still a usable home, else the account's inbox.
func (a *App) notSpamDestination(m *storage.Message) (storage.Folder, error) {
	if m.MessageID != "" {
		folderID, ok, err := a.store.SpamOrigin(a.ctx, m.AccountID, m.MessageID)
		if err != nil {
			return storage.Folder{}, err
		}
		if ok {
			f, err := a.store.GetFolder(a.ctx, folderID)
			if err == nil && f.AccountID == m.AccountID && folderSelectable(*f) && folderRole(*f) != roleJunk {
				return *f, nil
			}
		}
	}
	inbox, ok, err := a.findRoleFolder(m.AccountID, roleInbox)
	if err != nil {
		return storage.Folder{}, err
	}
	if !ok {
		return storage.Folder{}, fmt.Errorf("pelton: this account has no inbox")
	}
	return inbox, nil
}

// storeKeywords applies kw to uid in the selected mailbox, skipping any keyword
// the mailbox would not keep. Failures are logged only: the keyword is a hint
// for the server's filter, and the move it rides along with matters more.
func (a *App) storeKeywords(client mailClient, mbox *pimap.Mailbox, uid imap.UID, kw keywordChange) {
	if kw.remove != "" && mbox.KeepsKeyword(kw.remove) {
		if err := client.RemoveFlags(uid, kw.remove); err != nil {
			a.log.Error("spam keyword: remove", "keyword", kw.remove, "err", err)
		}
	}
	if kw.add != "" && mbox.KeepsKeyword(kw.add) {
		if err := client.AddFlags(uid, kw.add); err != nil {
			a.log.Error("spam keyword: add", "keyword", kw.add, "err", err)
		}
	}
}
