package desktop

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

func (p imapProtocol) manualSync(ctx context.Context, account storage.Account) error {
	return p.a.runIMAPManualSync(ctx, account)
}

// backfillStep ignores bodyLimit: an IMAP step fetches bodies by its batch.
func (p imapProtocol) backfillStep(ctx context.Context, account storage.Account, folder storage.Folder, kind syncsched.JobKind, batch, _ int, stats *imapStepStats) ([]string, error) {
	return p.a.execIMAPStep(ctx, account, folder, kind, batch, stats)
}

func (p imapProtocol) onDemandBodies(ctx context.Context, account storage.Account, folder storage.Folder) error {
	_, err := p.a.execIMAPOnDemandBodies(ctx, account, folder)
	return err
}

func (p imapProtocol) reconcileBodies(ctx context.Context, account storage.Account, folder storage.Folder) error {
	_, err := p.a.execIMAPBodies(ctx, account, folder, nil, true)
	return err
}

func (p imapProtocol) planDownload(ctx context.Context, account storage.Account, folders []storage.Folder, since time.Time) ([]dlTask, []int64, error) {
	return p.a.planIMAPAccount(ctx, account, folders, since)
}

func (p imapProtocol) backfillBodyLimit(context.Context, storage.Folder, syncsched.JobKind, int) (int, error) {
	return 0, nil
}

func (p imapProtocol) firstSync(ctx context.Context, account storage.Account) {
	a := p.a
	startIdle := sync.OnceFunc(func() {
		goSafe("watching for new mail", func() { a.idleLoop(ctx, account) })
	})
	err := a.syncIMAPInitial(ctx, account, a.idleAfterInbox(account.ID, startIdle))
	if err != nil {
		if errors.Is(err, errNoCredentials) {
			a.log.Warn("mailbox has no password, not syncing", "account", account.Email)
		} else if ctx.Err() == nil {
			a.log.Error("account sync", "account", account.Email, "err", err)
		}
	}
	// IDLE starts here at N=1, and also after a pass that never reached
	// Inbox (no password, no folder list); the idle loop waits for a
	// password on its own.
	startIdle()
	if ctx.Err() == nil && !errors.Is(err, errNoCredentials) {
		a.enqueueDueReconcile(ctx, account)
	}
}

func (p imapProtocol) watch(ctx context.Context, account storage.Account) {
	a := p.a
	for ctx.Err() == nil {
		if err := a.idleSession(ctx, account); err != nil && ctx.Err() == nil {
			if !errors.Is(err, errNoCredentials) {
				a.log.Error("idle session", "account", account.Email, "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(idleRetryWaitFor(err)):
			}
		}
	}
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
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return ArchiveUndoDTO{}, err
	}

	accountMu := a.accountLock(account.ID)
	accountMu.Lock()
	defer accountMu.Unlock()

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

	if err := client.Move(imap.UID(m.UID), dest.IMAPPath); err != nil {
		return ArchiveUndoDTO{}, err
	}
	return a.finishMove(m, source, dest, account, raw, exportError)
}

// moveBack finds the message by its rfc Message-ID: an IMAP move gives it a new uid.
func (p imapProtocol) moveBack(rfcMessageID string, from, dest storage.Folder, account storage.Account) error {
	a := p.a
	if rfcMessageID == "" {
		return fmt.Errorf("pelton: this message cannot be moved back (no Message-ID)")
	}
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return err
	}

	accountMu := a.accountLock(account.ID)
	accountMu.Lock()
	defer accountMu.Unlock()

	client, err := a.connectIMAP(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return err
	}
	defer client.Logout()
	if _, err := client.Select(from.IMAPPath); err != nil {
		return fmt.Errorf("undo move: select %q: %w", from.IMAPPath, err)
	}
	uids, err := client.SearchByMessageID(rfcMessageID)
	if err != nil {
		return err
	}
	if len(uids) == 0 {
		return fmt.Errorf("pelton: moved message not found to restore")
	}
	if err := client.Move(uids[len(uids)-1], dest.IMAPPath); err != nil {
		return err
	}
	a.emit(EventMailNew, MailNewEvent{AccountID: dest.AccountID, FolderID: dest.ID, Count: 1})
	return nil
}

func (p imapProtocol) source(m storage.Message, folder storage.Folder, account storage.Account) (string, error) {
	a := p.a
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return "", err
	}

	accountMu := a.accountLock(account.ID)
	accountMu.Lock()
	defer accountMu.Unlock()

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

	accountMu := a.accountLock(account.ID)
	accountMu.Lock()
	defer accountMu.Unlock()

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

func (p imapProtocol) withAdapter(account *storage.Account, fn func(psync.Adapter) error) error {
	a := p.a
	cfg, err := a.resolveIMAP(*account)
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
	return fn(pimap.NewAdapter(client))
}

// download keeps one session for the whole account: an IMAP session is not cheap to reopen.
func (p imapProtocol) download(ctx context.Context, account storage.Account, groups map[int64]*folderTasks, includeAttachments bool, done *int, total int, start time.Time) error {
	a := p.a
	return a.withAccountAdapter(account.ID, func(ad psync.Adapter) error {
		for _, ft := range groups {
			for chunk := range slices.Chunk(ft.remoteIDs, downloadBatch) {
				if err := ctx.Err(); err != nil {
					return err
				}
				a.fetchDownloadBatch(ctx, ad, account, ft, chunk, includeAttachments, done, total, start)
			}
		}
		return nil
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
	return a.createFolderTree(account.ID, folders)
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
