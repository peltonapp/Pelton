package desktop

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/outbox"
	psmtp "github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

var _ mailProtocol = imapProtocol{}

func (p imapProtocol) manualSync(ctx context.Context, account storage.Account, manual bool) error {
	return p.a.runIMAPManualSync(ctx, account, manual)
}

func (p imapProtocol) olderStep(ctx context.Context, account storage.Account, folder storage.Folder, older *olderList) ([]string, bool, error) {
	return p.a.imapOlderStep(ctx, account, folder, older)
}

func (p imapProtocol) onDemandBodies(ctx context.Context, account storage.Account, folder storage.Folder, remoteIDs []string) error {
	return p.a.execIMAPOnDemandBodies(ctx, account, folder, remoteIDs)
}

func (p imapProtocol) bodyBatch(ctx context.Context, account storage.Account, folder storage.Folder) error {
	return p.a.imapBodyBatch(ctx, account, folder)
}

func (p imapProtocol) planDownload(ctx context.Context, account storage.Account, folders []storage.Folder, since time.Time) ([]dlTask, []int64, error) {
	return p.a.planIMAPAccount(ctx, account, folders, since)
}

func (p imapProtocol) firstSync(ctx context.Context, account storage.Account) {
	a := p.a
	startIdle := sync.OnceFunc(func() {
		goSafe("watching for new mail", func() { a.idleLoop(ctx, account) })
	})
	// IDLE starts once Inbox is listed and has its first body batch, so mail
	// arriving during the rest of the pass (Inbox's older bodies included)
	// shows at once. At N=1 the scheduler keeps the only session on the
	// pass and checks new mail between tasks.
	err := a.syncIMAPInitial(ctx, account, startIdle)
	if err != nil {
		if errors.Is(err, errNoCredentials) {
			a.log.Warn("mailbox has no password, not syncing", "account", account.Email)
		} else if ctx.Err() == nil {
			a.log.Error("account sync", "account", account.Email, "err", err)
		}
	}
	// A pass that never reached Inbox (no password, no folder list) starts
	// IDLE here; it waits for a password on its own.
	startIdle()
	// Only a pass that got through: after a refused login or with the
	// server down every check would fail, each with a login of its own.
	if ctx.Err() == nil && err == nil {
		a.enqueueDueReconcile(ctx, account)
	}
}

// watch hands IMAP IDLE to the account's scheduler as its push watcher. It
// returns at once; the scheduler keeps IDLE until it stops.
func (p imapProtocol) watch(ctx context.Context, account storage.Account) {
	rt, err := p.a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		return
	}
	rt.sched.EnablePush(p.a.imapPush(account))
}

func (p imapProtocol) needsTimedSync(int64) bool { return true }

func (p imapProtocol) lockPerFolder() bool { return false }

func (p imapProtocol) limitNeedsBodies(ctx context.Context, f storage.Folder, newLimit int) bool {
	hasOlder, err := p.a.store.FolderHasOlderOnServer(ctx, f.ID)
	if err != nil {
		p.a.log.Error("sync floor for limit delta", "folder", f.ID, "err", err)
		return false
	}
	return hasOlder || newLimit == 0
}

func (p imapProtocol) withListEngine(ctx context.Context, account storage.Account, fn func(*psync.Engine) error) error {
	return p.a.withIMAPSession(ctx, account, func(client mailClient) error {
		return fn(p.a.newSyncEngine(pimap.NewAdapter(client), account.ID))
	})
}

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
	var (
		undo     ArchiveUndoDTO
		localErr error
	)
	err := a.runNow(a.ctx, account, syncsched.KindMove, source.ID, func(_ context.Context, client mailClient) error {
		unlock := a.lockMutations(account.ID)
		defer unlock()
		if _, err := client.Select(source.IMAPPath); err != nil {
			return fmt.Errorf("move: select %q: %w", source.IMAPPath, err)
		}
		// The raw source has to be fetched before the move: afterwards the
		// message has a new uid in another mailbox. A failure here never
		// blocks the archive.
		var (
			raw         []byte
			exportError string
		)
		if exportWanted(account, dest) {
			var err error
			raw, err = client.FetchRawMessage(imap.UID(m.UID))
			if err != nil {
				raw = nil
				exportError = err.Error()
				a.log.Error("archive export: fetch source", "id", m.ID, "err", err)
			}
		}
		if err := a.moveRecordingUID(client, imap.UID(m.UID), dest, m.MessageID); err != nil {
			return err
		}
		// A local error is returned past the task, so it does not close a
		// healthy session.
		undo, localErr = a.finishMove(m, source, dest, account, raw, exportError)
		return nil
	})
	if err != nil {
		return ArchiveUndoDTO{}, err
	}
	return undo, localErr
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
	var (
		moved    imap.UID
		validity uint32
	)
	err := a.runNow(a.ctx, account, syncsched.KindMove, from.ID, func(_ context.Context, client mailClient) error {
		unlock := a.lockMutations(account.ID)
		defer unlock()
		box, err := client.Select(from.IMAPPath)
		if err != nil {
			return fmt.Errorf("undo move: select %q: %w", from.IMAPPath, err)
		}
		uid := a.movedUIDs.take(from.ID, rfcMessageID, box.UIDValidity)
		if m := asUIDMover(client); uid != 0 && m != nil {
			// A UID MOVE of a UID that is gone (another client expunged
			// it) is an OK that moves nothing. The UID was recorded from
			// this server's COPYUID, so a move that did happen names its
			// copy again; none means nothing moved. Another copy with the
			// same Message-ID is not the one the user moved, so there is
			// no search then: the undo fails plainly instead.
			back, _, err := m.MoveUID(uid, dest.IMAPPath)
			if err != nil {
				return err
			}
			if back != 0 {
				moved, validity = uid, box.UIDValidity
			}
			return nil
		}
		if uid == 0 {
			uids, err := client.SearchByMessageID(rfcMessageID)
			if err != nil || len(uids) == 0 {
				return err
			}
			uid = uids[len(uids)-1]
		}
		if err := client.Move(uid, dest.IMAPPath); err != nil {
			return err
		}
		moved, validity = uid, box.UIDValidity
		return nil
	})
	if err != nil {
		return err
	}
	if moved == 0 {
		return fmt.Errorf("pelton: moved message not found to restore")
	}
	a.dropMovedCopy(account, from, moved, validity)
	a.emit(EventMailNew, MailNewEvent{AccountID: from.AccountID, FolderID: from.ID})
	a.emit(EventMailNew, MailNewEvent{AccountID: dest.AccountID, FolderID: dest.ID, Count: 1})
	return nil
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

// asUIDMover returns client as a uidMover, looking through the pool's
// wrapper, or nil when the session cannot report COPYUID.
func asUIDMover(client mailClient) uidMover {
	c := client
	if p, ok := c.(*pooledIMAP); ok {
		c = p.mailClient
	}
	m, _ := c.(uidMover)
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
	var raw []byte
	err := a.runNow(a.ctx, account, syncsched.KindSource, folder.ID, func(_ context.Context, client mailClient) error {
		if _, err := client.Select(folder.IMAPPath); err != nil {
			return err
		}
		var err error
		raw, err = client.FetchRawMessage(imap.UID(m.UID))
		return err
	})
	if err != nil {
		return "", offlineOrErr(err)
	}
	return string(raw), nil
}

func (p imapProtocol) setColor(m storage.Message, folder storage.Folder, account storage.Account, color int) {
	a := p.a
	err := a.runNow(a.ctx, account, syncsched.KindFlags, folder.ID, func(_ context.Context, client mailClient) error {
		unlock := a.lockMutations(account.ID)
		defer unlock()
		if _, err := client.Select(folder.IMAPPath); err != nil {
			return fmt.Errorf("select %q: %w", folder.IMAPPath, err)
		}
		uid := imap.UID(m.UID)
		// A failed step does not stop the next; the first failure is the
		// task's outcome, for the throttle and the session's health.
		var failed error
		if err := client.RemoveFlags(uid, colorKeywords...); err != nil {
			failed = fmt.Errorf("clear labels: %w", err)
		}
		if color >= 1 && color <= len(colorKeywords) {
			if err := client.AddFlags(uid, colorKeywords[color-1]); err != nil {
				if failed != nil {
					a.log.Error("color sync: add label", "err", err)
				} else {
					failed = fmt.Errorf("add label: %w", err)
				}
			}
		}
		return failed
	})
	switch {
	case errors.Is(err, errNoCredentials), errors.Is(err, errAccountSyncHeld):
		// No password, or the account is going: the colour stays local.
	case err != nil:
		a.log.Error("color sync", "err", err)
	}
}

// createFolder creates the mailbox at path, the parent's stored path and
// delimiter plus name (see folderPath), and stores it in the same task.
func (p imapProtocol) createFolder(ctx context.Context, account storage.Account, _ *storage.Folder, _, path string, apply func(string) error) error {
	return p.a.runFolderOp(ctx, account, 0, func(client mailClient) error {
		return client.CreateFolder(path)
	}, func() error { return apply(path) })
}

// renameFolder renames the mailbox to newPath, computed from its stored path
// and delimiter, and updates the store in the same task. The server moves
// its children along.
func (p imapProtocol) renameFolder(ctx context.Context, account storage.Account, folder storage.Folder, _, newPath string, apply func() error) error {
	return p.a.runFolderOp(ctx, account, folder.ID, func(client mailClient) error {
		return client.RenameFolder(folder.IMAPPath, newPath)
	}, apply)
}

// deleteFolders deletes the mailboxes in order, in one fast-lane task, and
// drops their rows in it.
func (p imapProtocol) deleteFolders(ctx context.Context, account storage.Account, folders []storage.Folder, apply func() error) error {
	return p.a.runFolderOp(ctx, account, 0, func(client mailClient) error {
		for _, f := range folders {
			if err := client.DeleteFolder(f.IMAPPath); err != nil {
				return err
			}
		}
		return nil
	}, apply)
}

// runFolderOp runs a mailbox command and then apply, its store update, in
// one fast-lane task. A folder discovery is a barrier, so it runs before or
// after both, never between. apply's error is the store's: the task ends
// cleanly and the session stays.
func (a *App) runFolderOp(ctx context.Context, account storage.Account, folderID int64, server func(mailClient) error, apply func() error) error {
	var applyErr error
	err := a.runNow(ctx, account, syncsched.KindFolderOp, folderID, func(_ context.Context, client mailClient) error {
		if err := server(client); err != nil {
			return err
		}
		applyErr = apply()
		return nil
	})
	if err != nil {
		return err
	}
	return applyErr
}

// download runs each folder as a normal-lane generator task: one batch per
// unit, back to the tail while more is left, so the Sent copy and other
// fast-lane work get a session between batches.
func (p imapProtocol) download(ctx context.Context, account storage.Account, groups map[int64]*folderTasks, includeAttachments bool, prog *dlProgress) error {
	a := p.a
	folders := make([]storage.Folder, 0, len(groups))
	for _, ft := range groups {
		folders = append(folders, ft.folder)
	}
	// One task per folder key, so a folder's units never run at once; they
	// share the body claims with the folder's body units (offlineUnit).
	return a.runFolderTasks(ctx, account, syncsched.KindOffline, syncsched.KindOffline.String(), folders,
		func(taskCtx context.Context, client mailClient, folder storage.Folder) error {
			if ctx.Err() != nil {
				return nil
			}
			return a.offlineUnit(withTaskSession(taskCtx, account.ID, client), client, account, groups[folder.ID], includeAttachments, prog)
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
		if loginRefused(err) {
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

// discoverFolders lists the mailboxes and creates the folder rows, as the
// account's first task: nothing else of the account starts until it ends.
func (p imapProtocol) discoverFolders(ctx context.Context, account storage.Account) error {
	a := p.a
	return a.runFolderTree(ctx, account, func(c mailClient) error {
		folders, err := c.ListFolders()
		if err != nil {
			return err
		}
		return a.createFolderTree(account.ID, folders, nil)
	})
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
