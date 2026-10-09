package desktop

import (
	"fmt"

	"github.com/peltonapp/Pelton/internal/mailexport"
	"github.com/peltonapp/Pelton/internal/storage"
)

// ArchiveUndoDTO carries what undo needs to move a message back: the folder it
// came from, the folder the action put it in, and its stable rfc Message-ID (the
// moved copy has a new UID). MessageID is empty when the message had no
// Message-ID header, in which case undo is not possible.
// ExportPath is the .eml copy written by the account's export-on-archive
// option, empty when the option is off. ExportError explains why no copy was
// written when one was expected; the archive itself still succeeded, so the ui
// reports it rather than treating the whole action as failed.
type ArchiveUndoDTO struct {
	MessageID        string `json:"messageId"`
	OriginalFolderID int64  `json:"originalFolderId"`
	DestFolderID     int64  `json:"destFolderId"`
	ExportPath       string `json:"exportPath"`
	ExportError      string `json:"exportError"`
}

// ArchiveMessage moves a message to its account's Archive folder on the server.
// It errors clearly when the account has no archive folder. It returns the info
// needed to undo the archive.
func (a *App) ArchiveMessage(id int64) (ArchiveUndoDTO, error) {
	if err := a.ready(); err != nil {
		return ArchiveUndoDTO{}, err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	archive, err := a.findArchiveFolder(m.AccountID)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	return a.moveMessageTo(m, archive)
}

// MoveMessage moves a message to any folder (of its own account) on the server,
// dropping the local row so it leaves the current view immediately; the next sync
// re-fetches it under the destination. It returns the info needed to undo.
func (a *App) MoveMessage(id, destFolderID int64) (ArchiveUndoDTO, error) {
	if err := a.ready(); err != nil {
		return ArchiveUndoDTO{}, err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	dest, err := a.store.GetFolder(a.ctx, destFolderID)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	if dest.AccountID != m.AccountID {
		return ArchiveUndoDTO{}, fmt.Errorf("pelton: cannot move a message to another account's folder")
	}
	return a.moveMessageTo(m, *dest)
}

// moveMessageTo performs the server move of a cached message to dest, then drops
// the local row and its files. It is the shared core of archive and move.
//
// Asking for a move into the folder the message already sits in is an error
// rather than a silent success: the caller drops the row from the list on
// success, so reporting one for a move that never happened made the message
// look deleted until the next sync brought it back.
func (a *App) moveMessageTo(m *storage.Message, dest storage.Folder) (ArchiveUndoDTO, error) {
	source, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	if dest.ID == source.ID {
		return ArchiveUndoDTO{}, fmt.Errorf("pelton: this message is already in %s", dest.Name)
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	return a.protocolFor(*account).moveMessage(m, *source, dest, *account)
}

// finishMove is the local half of a move the server has already accepted: it
// drops the cached row and its files, announces the destination, and writes the
// export copy when raw was fetched.
func (a *App) finishMove(m *storage.Message, source, dest storage.Folder, account storage.Account, raw []byte, exportError string) (ArchiveUndoDTO, error) {
	if err := a.store.DeleteMessage(a.ctx, m.ID); err != nil {
		return ArchiveUndoDTO{}, err
	}
	if err := a.store.DeleteAttachmentFilesForMessage(m.AccountID, m.ID); err != nil {
		a.log.Error("move: remove attachment files", "id", m.ID, "err", err)
	}
	a.emit(EventMailNew, MailNewEvent{AccountID: m.AccountID, FolderID: dest.ID, Count: 1})

	var path string
	if len(raw) > 0 {
		var err error
		path, err = exportOptions(account).Write(exportMeta(m), raw)
		if err != nil {
			exportError = err.Error()
			a.log.Error("archive export: write file", "id", m.ID, "err", err)
		}
	}
	return ArchiveUndoDTO{
		MessageID:        m.MessageID,
		OriginalFolderID: source.ID,
		DestFolderID:     dest.ID,
		ExportPath:       path,
		ExportError:      exportError,
	}, nil
}

// exportWanted reports whether this move should also leave a local .eml copy:
// the account asked for it, a directory is set, and the destination really is
// the archive rather than any other folder the user moved mail to.
func exportWanted(account storage.Account, dest storage.Folder) bool {
	return account.ExportOnArchive && account.ExportDir != "" && folderRole(dest) == roleArchive
}

// exportOptions maps an account's stored export settings onto the exporter.
func exportOptions(account storage.Account) mailexport.Options {
	return mailexport.Options{
		Dir:        account.ExportDir,
		Subfolders: account.ExportSubfolders,
		Template:   account.ExportNameTemplate,
	}
}

// exportMeta is what the file name is built from. The display name is preferred
// over the bare address for {from}, matching what the message list shows.
func exportMeta(m *storage.Message) mailexport.Meta {
	from := m.FromName
	if from == "" {
		from = m.FromAddress
	}
	return mailexport.Meta{
		Date:      m.Date,
		Subject:   m.Subject,
		From:      from,
		MessageID: m.MessageID,
	}
}

// UnarchiveMessage undoes an archive or move: it moves the message from
// fromFolderID, where the action put it, back to originalFolderID. The move gave
// the message a new uid, so it is found by its rfc Message-ID.
func (a *App) UnarchiveMessage(rfcMessageID string, fromFolderID, originalFolderID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	dest, err := a.store.GetFolder(a.ctx, originalFolderID)
	if err != nil {
		return err
	}
	from, err := a.store.GetFolder(a.ctx, fromFolderID)
	if err != nil {
		return err
	}
	if from.AccountID != dest.AccountID {
		return fmt.Errorf("pelton: cannot undo a move across accounts")
	}
	account, err := a.store.GetAccount(a.ctx, dest.AccountID)
	if err != nil {
		return err
	}
	return a.protocolFor(*account).moveBack(rfcMessageID, *from, *dest, *account)
}

// findArchiveFolder returns the account's archive-role folder, or an error when
// none exists.
func (a *App) findArchiveFolder(accountID int64) (storage.Folder, error) {
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return storage.Folder{}, err
	}
	for _, f := range folders {
		if folderRole(f) == roleArchive {
			return f, nil
		}
	}
	return storage.Folder{}, fmt.Errorf("pelton: this account has no Archive folder")
}
