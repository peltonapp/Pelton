package desktop

import (
	"fmt"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/mailexport"
	"github.com/peltonapp/Pelton/internal/storage"
)

// ArchiveUndoDTO carries what undo needs to move a message back: its stable rfc
// Message-ID (the moved copy has a new UID), the folder it came from and the
// folder it went to. MessageID is empty when the message had no Message-ID
// header, in which case undo is not possible.
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
	return a.moveMessageTo(m, archive, nil)
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
	return a.moveMessageTo(m, *dest, nil)
}

// keywordChange is a keyword pair set on a message just before it moves, so the
// server's filter sees the user's verdict (RFC 5788 $Junk / $NotJunk).
type keywordChange struct {
	add, remove imap.Flag
}

// moveMessageTo performs the server move of a cached message to dest, then drops
// the local row and its files. It is the shared core of archive, move and the
// spam marks. A non-nil kw is stored on the message first, where the source
// mailbox keeps it; a keyword the server refuses never blocks the move.
//
// Asking for a move into the folder the message already sits in is an error
// rather than a silent success: the caller drops the row from the list on
// success, so reporting one for a move that never happened made the message
// look deleted until the next sync brought it back.
func (a *App) moveMessageTo(m *storage.Message, dest storage.Folder, kw *keywordChange) (ArchiveUndoDTO, error) {
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
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}

	syncMu.Lock()
	defer syncMu.Unlock()

	client, err := a.connectIMAP(cfg)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return ArchiveUndoDTO{}, err
	}
	defer client.Logout()
	mbox, err := client.Select(source.IMAPPath)
	if err != nil {
		return ArchiveUndoDTO{}, fmt.Errorf("move: select %q: %w", source.IMAPPath, err)
	}
	if kw != nil {
		a.storeKeywords(client, mbox, imap.UID(m.UID), *kw)
	}

	// the raw source has to be fetched before the move: afterwards the message
	// lives under a new uid in another mailbox. it rides the connection the move
	// already opened, and a failure here never blocks the archive.
	var (
		raw         []byte
		exportError string
	)
	if exportWanted(*account, dest) {
		raw, err = client.FetchRawMessage(imap.UID(m.UID))
		if err != nil {
			raw = nil
			exportError = err.Error()
			a.log.Error("archive export: fetch source", "id", m.ID, "err", err)
		}
	}

	if err := client.Move(imap.UID(m.UID), dest.IMAPPath); err != nil {
		return ArchiveUndoDTO{}, err
	}

	if err := a.store.DeleteMessage(a.ctx, m.ID); err != nil {
		return ArchiveUndoDTO{}, err
	}
	if err := a.store.DeleteAttachmentFilesForMessage(m.AccountID, m.ID); err != nil {
		a.log.Error("move: remove attachment files", "id", m.ID, "err", err)
	}
	a.emit(EventMailNew, MailNewEvent{AccountID: m.AccountID, FolderID: dest.ID, Count: 1})

	var path string
	if len(raw) > 0 {
		path, err = exportOptions(*account).Write(exportMeta(m), raw)
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

// RestoreMessage undoes a move: it finds the message in fromFolderID by its rfc
// Message-ID (its UID changed when it was moved) and moves it back to
// toFolderID. Undoing a spam mark also takes back the keyword the mark set, so
// the server's filter is not left learning from a verdict the user withdrew. It
// errors when the message has no Message-ID, the folders belong to different
// accounts, or the server can no longer find it.
func (a *App) RestoreMessage(rfcMessageID string, fromFolderID, toFolderID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if rfcMessageID == "" {
		return fmt.Errorf("pelton: this message cannot be moved back (no Message-ID)")
	}
	from, err := a.store.GetFolder(a.ctx, fromFolderID)
	if err != nil {
		return err
	}
	dest, err := a.store.GetFolder(a.ctx, toFolderID)
	if err != nil {
		return err
	}
	if from.AccountID != dest.AccountID {
		return fmt.Errorf("pelton: cannot move a message to another account's folder")
	}
	account, err := a.store.GetAccount(a.ctx, dest.AccountID)
	if err != nil {
		return err
	}
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return err
	}

	syncMu.Lock()
	defer syncMu.Unlock()

	client, err := a.connectIMAP(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return err
	}
	defer client.Logout()
	mbox, err := client.Select(from.IMAPPath)
	if err != nil {
		return fmt.Errorf("undo move: select %q: %w", from.IMAPPath, err)
	}
	uids, err := client.SearchByMessageID(rfcMessageID)
	if err != nil {
		return err
	}
	if len(uids) == 0 {
		return fmt.Errorf("pelton: moved message not found to restore")
	}
	uid := uids[len(uids)-1]
	switch {
	case folderRole(*from) == roleJunk:
		a.storeKeywords(client, mbox, uid, keywordChange{remove: keywordJunk})
		if err := a.store.ForgetSpamOrigin(a.ctx, from.AccountID, rfcMessageID); err != nil {
			a.log.Error("undo spam: forget origin", "err", err)
		}
	case folderRole(*dest) == roleJunk:
		a.storeKeywords(client, mbox, uid, keywordChange{remove: keywordNotJunk})
	}
	if err := client.Move(uid, dest.IMAPPath); err != nil {
		return err
	}
	a.emit(EventMailNew, MailNewEvent{AccountID: dest.AccountID, FolderID: dest.ID, Count: 1})
	return nil
}

// findArchiveFolder returns the account's archive-role folder, or an error when
// none exists.
func (a *App) findArchiveFolder(accountID int64) (storage.Folder, error) {
	f, ok, err := a.findRoleFolder(accountID, roleArchive)
	if err != nil {
		return storage.Folder{}, err
	}
	if !ok {
		return storage.Folder{}, fmt.Errorf("pelton: this account has no Archive folder")
	}
	return f, nil
}

// findRoleFolder returns the account's first folder with the given role, and
// false when it has none.
func (a *App) findRoleFolder(accountID int64, role string) (storage.Folder, bool, error) {
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return storage.Folder{}, false, err
	}
	for _, f := range folders {
		if folderRole(f) == role {
			return f, true, nil
		}
	}
	return storage.Folder{}, false, nil
}
