package desktop

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/emersion/go-imap/v2"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/outbox"
	psmtp "github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

var _ mailProtocol = imapProtocol{}

func (p imapProtocol) transmit(ctx context.Context, account storage.Account, m outbox.Message) error {
	t := &accountTransmitter{app: p.a}
	cfg, err := t.app.resolveSMTP(account)
	if err != nil {
		return err
	}
	sender := psmtp.NewSender(cfg,
		psmtp.WithLogger(t.app.log),
		psmtp.WithSentAppender(func(raw []byte) (string, error) {
			return t.app.appendToSent(account, raw)
		}),
	)
	return sender.Transmit(ctx, m)
}

func (p imapProtocol) moveMessage(m *storage.Message, source, dest storage.Folder, account storage.Account) (ArchiveUndoDTO, error) {
	a := p.a
	cfg, err := a.resolveIMAP(account)
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
	if _, err := client.Select(source.IMAPPath); err != nil {
		return ArchiveUndoDTO{}, fmt.Errorf("move: select %q: %w", source.IMAPPath, err)
	}

	// the raw source has to be fetched before the move: afterwards the message
	// lives under a new uid in another mailbox. it rides the connection the move
	// already opened, and a failure here never blocks the archive.
	var (
		raw         []byte
		exportError string
	)
	if exportWanted(account, dest) {
		raw, err = client.FetchRawMessage(imap.UID(m.UID))
		if err != nil {
			raw = nil
			exportError = err.Error()
			a.log.Error("archive export: fetch source", "id", m.ID, "err", err)
		}
	}

	if err := a.moveRecordingUID(client, imap.UID(m.UID), dest, m.MessageID); err != nil {
		return ArchiveUndoDTO{}, err
	}
	return a.finishMove(m, source, dest, account, raw, exportError)
}

// moveBack moves the copy the move made back to dest. It is found by the
// UID the server named for it at move time (COPYUID), else by its rfc
// Message-ID: the highest UID carrying it in from. When the COPYUID copy is
// gone by now, nothing is moved and the undo reports it.
func (p imapProtocol) moveBack(rfcMessageID string, from, dest storage.Folder, account storage.Account) error {
	a := p.a
	if rfcMessageID == "" {
		return fmt.Errorf("pelton: this message cannot be moved back (no Message-ID)")
	}
	cfg, err := a.resolveIMAP(account)
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
	box, err := client.Select(from.IMAPPath)
	if err != nil {
		return fmt.Errorf("undo move: select %q: %w", from.IMAPPath, err)
	}
	moved, err := a.moveBackOnSession(client, rfcMessageID, from, dest, box.UIDValidity)
	if err != nil {
		return err
	}
	if moved == 0 {
		return fmt.Errorf("pelton: moved message not found to restore")
	}
	a.dropMovedCopy(account, from, moved, box.UIDValidity)
	a.emit(EventMailNew, MailNewEvent{AccountID: from.AccountID, FolderID: from.ID})
	a.emit(EventMailNew, MailNewEvent{AccountID: dest.AccountID, FolderID: dest.ID, Count: 1})
	return nil
}

// moveBackOnSession moves the recorded copy, else the search's highest UID,
// from from (selected on client under validity) to dest. It returns the UID
// it moved, 0 when nothing was.
func (a *App) moveBackOnSession(client mailClient, rfcMessageID string, from, dest storage.Folder, validity uint32) (imap.UID, error) {
	uid := a.movedUIDs.take(from.ID, rfcMessageID, validity)
	if m := asUIDMover(client); uid != 0 && m != nil {
		// A UID MOVE of a UID that is gone (another client expunged
		// it) is an OK that moves nothing. The UID was recorded from
		// this server's COPYUID, so a move that did happen names its
		// copy again; none means nothing moved. Another copy with the
		// same Message-ID is not the one the user moved, so there is
		// no search then: the undo fails plainly instead.
		back, _, err := m.MoveUID(uid, dest.IMAPPath)
		if err != nil || back == 0 {
			return 0, err
		}
		return uid, nil
	}
	if uid == 0 {
		uids, err := client.SearchByMessageID(rfcMessageID)
		if err != nil || len(uids) == 0 {
			return 0, err
		}
		uid = uids[len(uids)-1]
	}
	if err := client.Move(uid, dest.IMAPPath); err != nil {
		return 0, err
	}
	return uid, nil
}

// dropMovedCopy removes from's row for uid, which a sync of from may have
// stored already: the message is gone from there now. A row stored under
// another UIDVALIDITY than the server's (validity) may be another message,
// so it is left to from's next sync, which resets the folder.
func (a *App) dropMovedCopy(account storage.Account, from storage.Folder, uid imap.UID, validity uint32) {
	if from.UIDValidity != 0 && from.UIDValidity != validity {
		return
	}
	id, err := a.store.MessageIDByRemoteID(a.ctx, from.ID, strconv.FormatUint(uint64(uid), 10))
	switch {
	case errors.Is(err, storage.ErrMessageNotFound):
	case err != nil:
		a.log.Error("undo move: look up moved copy", "folder", from.ID, "uid", uid, "err", err)
	default:
		if err := a.store.DeleteMessage(a.ctx, id); err != nil {
			a.log.Error("undo move: drop moved copy", "id", id, "err", err)
		} else if err := a.store.DeleteAttachmentFilesForMessage(account.ID, id); err != nil {
			a.log.Error("undo move: remove attachment files", "id", id, "err", err)
		}
	}
}

// uidMover is a session that reports where a MOVE put the message (UIDPLUS
// COPYUID): its UID in the destination and the destination's UIDVALIDITY.
// destUID is 0 when the server did not say.
type uidMover interface {
	MoveUID(uid imap.UID, mailbox string) (destUID imap.UID, uidValidity uint32, err error)
}

// The real client is one, so undo after a move on a UIDPLUS server needs no
// Message-ID search.
var _ uidMover = (*pimap.Client)(nil)

// asUIDMover returns client as a uidMover, or nil when the session cannot
// report COPYUID.
func asUIDMover(client mailClient) uidMover {
	m, _ := client.(uidMover)
	return m
}

// moveRecordingUID moves uid to dest and, when the server names the copy's
// UID, records it so an undo moves back exactly that copy.
func (a *App) moveRecordingUID(client mailClient, uid imap.UID, dest storage.Folder, rfcMessageID string) error {
	m := asUIDMover(client)
	if m == nil {
		return client.Move(uid, dest.IMAPPath)
	}
	destUID, validity, err := m.MoveUID(uid, dest.IMAPPath)
	if err == nil && destUID != 0 && rfcMessageID != "" {
		a.movedUIDs.record(dest.ID, rfcMessageID, destUID, validity)
	}
	return err
}

// movedCopies remembers, for the last moves, the UID each moved message got
// in its destination, so undo needs no search. Undo is offered for seconds,
// so a few hundred entries are plenty; the oldest go first.
type movedCopies struct {
	mu    sync.Mutex
	byKey map[movedKey]movedCopy
	order []movedKey
}

type movedKey struct {
	folderID  int64
	messageID string
}

type movedCopy struct {
	uid      imap.UID
	validity uint32
}

const movedCopiesKept = 256

func (m *movedCopies) record(folderID int64, messageID string, uid imap.UID, validity uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byKey == nil {
		m.byKey = make(map[movedKey]movedCopy)
	}
	k := movedKey{folderID, messageID}
	if _, ok := m.byKey[k]; !ok {
		m.order = append(m.order, k)
	}
	m.byKey[k] = movedCopy{uid, validity}
	if len(m.order) > movedCopiesKept {
		delete(m.byKey, m.order[0])
		m.order = m.order[1:]
	}
}

// take returns and forgets the UID recorded for messageID in folderID, or 0
// when none was, or it belongs to another UIDVALIDITY than validity.
func (m *movedCopies) take(folderID int64, messageID string, validity uint32) imap.UID {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := movedKey{folderID, messageID}
	c, ok := m.byKey[k]
	if !ok {
		return 0
	}
	delete(m.byKey, k)
	m.order = slices.DeleteFunc(m.order, func(o movedKey) bool { return o == k })
	if c.validity != validity {
		return 0
	}
	return c.uid
}

func (p imapProtocol) source(m storage.Message, folder storage.Folder, account storage.Account) (string, error) {
	a := p.a
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return "", err
	}

	syncMu.Lock()
	defer syncMu.Unlock()

	client, err := a.connectIMAP(cfg)
	if err != nil {
		return "", offlineOrErr(err)
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return "", offlineOrErr(err)
	}
	defer client.Logout()
	if _, err := client.Select(folder.IMAPPath); err != nil {
		return "", offlineOrErr(err)
	}

	raw, err := client.FetchRawMessage(imap.UID(m.UID))
	if err != nil {
		return "", offlineOrErr(err)
	}
	return string(raw), nil
}

func (p imapProtocol) setColor(m storage.Message, folder storage.Folder, account storage.Account, color int) {
	a := p.a
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return // no credentials: color stays local until sync is possible
	}

	syncMu.Lock()
	defer syncMu.Unlock()

	client, err := a.connectIMAP(cfg)
	if err != nil {
		a.log.Error("color sync: connect", "err", err)
		return
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		a.log.Error("color sync: login", "err", err)
		return
	}
	defer client.Logout()
	if _, err := client.Select(folder.IMAPPath); err != nil {
		a.log.Error("color sync: select", "folder", folder.IMAPPath, "err", err)
		return
	}

	uid := imap.UID(m.UID)
	if err := client.RemoveFlags(uid, colorKeywords...); err != nil {
		a.log.Error("color sync: clear labels", "err", err)
	}
	if color >= 1 && color <= len(colorKeywords) {
		if err := client.AddFlags(uid, colorKeywords[color-1]); err != nil {
			a.log.Error("color sync: add label", "err", err)
		}
	}
}

// withSession resolves the account's IMAP config (an OAuth refresh may go
// to the network) before taking syncMu, then runs fn on a fresh logged-in
// session under it, so a folder operation never races a sync.
func (p imapProtocol) withSession(account storage.Account, fn func(mailClient) error) error {
	a := p.a
	cfg, err := a.resolveIMAP(account)
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
	return fn(client)
}

// createFolder creates the mailbox at path, the parent's stored path and
// delimiter plus name (see folderPath).
func (p imapProtocol) createFolder(_ context.Context, account storage.Account, _ *storage.Folder, _, path string) (string, error) {
	err := p.withSession(account, func(client mailClient) error {
		return client.CreateFolder(path)
	})
	return path, err
}

// renameFolder renames the mailbox to newPath, computed from its stored path
// and delimiter. The server moves its children along.
func (p imapProtocol) renameFolder(_ context.Context, account storage.Account, folder storage.Folder, _, newPath string) error {
	return p.withSession(account, func(client mailClient) error {
		return client.RenameFolder(folder.IMAPPath, newPath)
	})
}

// deleteFolders deletes the mailboxes in order, on one session.
func (p imapProtocol) deleteFolders(_ context.Context, account storage.Account, folders []storage.Folder) error {
	return p.withSession(account, func(client mailClient) error {
		for _, f := range folders {
			if err := client.DeleteFolder(f.IMAPPath); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p imapProtocol) withAdapter(account *storage.Account, fn func(psync.Adapter) error) error {
	return p.withSession(*account, func(client mailClient) error {
		return fn(pimap.NewAdapter(client))
	})
}

// checkPassword dials the server and logs in with the typed password.
func (p imapProtocol) checkPassword(account storage.Account, password string) PasswordCheckDTO {
	a := p.a
	dial, err := a.accountDial(account)
	if err != nil {
		return PasswordCheckDTO{Error: err.Error()}
	}
	client, err := pimap.Connect(pimap.Config{
		Host:     account.IMAPHost,
		Port:     account.IMAPPort,
		Username: loginName(account),
		Password: password,
		TLS:      imapTLSMode(account.IMAPTLS),
		Trust:    accountTrust(account),
		Dial:     dial,
	})
	if err != nil {
		return PasswordCheckDTO{Error: err.Error()}
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		if errors.Is(err, pimap.ErrAuthFailed) {
			return PasswordCheckDTO{Rejected: true, Error: err.Error()}
		}
		return PasswordCheckDTO{Error: err.Error()}
	}
	if err := client.Logout(); err != nil {
		a.log.Debug("logout after password check", "account", account.ID, "err", err)
	}
	return PasswordCheckDTO{OK: true}
}

// routeServers is the servers the request names: imap and smtp.
func (p imapProtocol) routeServers(_ storage.Account, requested []routeServer) ([]routeServer, error) {
	return requested, nil
}

// discoverFolders lists the mailboxes over imap and creates the folder rows.
func (p imapProtocol) discoverFolders(ctx context.Context, account storage.Account) error {
	a := p.a
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return err
	}
	client, err := a.connectIMAP(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return err
	}
	defer client.Logout()

	folders, err := client.ListFolders()
	if err != nil {
		return err
	}
	return a.createFolderTree(account.ID, folders, nil)
}

// folderPath is the path of a new folder under parent. A flat mailbox list
// cannot nest.
func (p imapProtocol) folderPath(parent storage.Folder, name string) (string, error) {
	if parent.Delimiter == "" {
		return "", errors.New("this server has a flat mailbox list and cannot nest folders")
	}
	return parent.IMAPPath + parent.Delimiter + name, nil
}

// createdPath is the path stored for a folder the server created.
func (p imapProtocol) createdPath(path, _ string) string { return path }

// renameKeepsPath is false: imap rewrites the path of a renamed folder.
func (p imapProtocol) renameKeepsPath(storage.Folder, string) bool { return false }
