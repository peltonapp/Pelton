package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/outbox"
	psmtp "github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"

	pimap "github.com/peltonapp/Pelton/internal/imap"
)

// syncMu serializes imap sessions per process so manual and background syncs do
// not open competing logins for the same account at once.
var syncMu sync.Mutex

const (
	// idleRetry is how long an idle loop waits after a connection it did not
	// expect to lose.
	idleRetry = 15 * time.Second
	// idleNoCredentialsRetry is how long it waits when the account has no
	// password. Longer, because nothing changes until the user types one.
	idleNoCredentialsRetry = 60 * time.Second
)

// idleRetryWait says how long to wait before opening an account's idle
// connection again.
//
// A missing password used to end the loop for good. That made a mailbox
// unfixable without a restart: the user typed the password, the account still
// received nothing, and only the next launch brought it back. Waiting is the
// whole point, since a password can arrive at any moment.
func idleRetryWait(err error) time.Duration {
	if errors.Is(err, errNoCredentials) {
		return idleNoCredentialsRetry
	}
	return idleRetry
}

// startBackgroundServices launches the outbox worker and the initial sync plus
// per-account idle loops. Credentials come from the keyring (added by the
// wizard) with an environment fallback for the legacy cli account.
func (a *App) startBackgroundServices() {
	goSafe("sending queued mail", a.runOutboxWorker)
	goSafe("the first sync", a.runInitialSyncAndIdle)
	goSafe("waking snoozed mail", a.runSnoozePoller)
	goSafe("collecting addresses", a.harvestAddressBook)
	goSafe("the periodic sync", a.runAutoSyncLoop)
	a.startMCPIfEnabled()
	goSafe("counting unread mail", a.refreshViewCounts)
}

// runAutoSyncLoop periodically runs a full sync pass across every account, on
// top of the always-on imap idle push (which not every server supports, and
// which can silently drop on flaky networks). the interval is a user setting
// (0 disables it); a short base tick lets a changed interval or low-power
// toggle take effect promptly without needing its own change-notification
// channel. it does nothing while low-power mode is on.
func (a *App) runAutoSyncLoop() {
	const baseTick = 5 * time.Second
	ticker := time.NewTicker(baseTick)
	defer ticker.Stop()
	lastRun := time.Now()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			interval := a.intSetting(settingAutoSync, 900)
			if interval <= 0 || a.lowPowerMode() {
				continue
			}
			if time.Since(lastRun) < time.Duration(interval)*time.Second {
				continue
			}
			lastRun = time.Now()
			if err := a.TriggerSync(); err != nil && !errors.Is(err, errNoCredentials) {
				a.log.Error("auto sync", "err", err)
			}
		}
	}
}

// runOutboxWorker drains the outbox, resolving smtp credentials per message from
// the sending account. Messages whose account has no credentials stay queued and
// surface in the outbox view.
func (a *App) runOutboxWorker() {
	transmitter := &accountTransmitter{app: a}
	worker := outbox.NewWorker(a.queue, transmitter,
		outbox.WithLogger(a.log),
		// emit after every state change so the ui reflects sending -> sent/failed
		// promptly. without this the outbox banner stayed stuck on "sending".
		outbox.WithOnChange(func() { a.emit(EventOutboxChanged, nil) }),
	)
	if _, err := a.queue.RequeueStuck(a.ctx); err != nil {
		a.log.Error("requeue stuck outbox", "err", err)
	}
	if err := worker.Run(a.ctx); err != nil && a.ctx.Err() == nil {
		a.log.Error("outbox worker stopped", "err", err)
	}
}

// accountTransmitter sends a queued message using the credentials of its
// account, resolved fresh each attempt so refreshed oauth tokens are picked up.
type accountTransmitter struct {
	app *App
}

func (t *accountTransmitter) Transmit(ctx context.Context, m outbox.Message) error {
	// note: the worker emits EventOutboxChanged via WithOnChange after the state
	// is persisted, so we must not emit here (that fired before markSent and left
	// the ui stuck on "sending").
	account, err := t.app.store.GetAccount(ctx, m.AccountID)
	if err != nil {
		return err
	}
	cfg, err := t.app.resolveSMTP(*account)
	if err != nil {
		return err
	}
	sender := psmtp.NewSender(cfg,
		psmtp.WithLogger(t.app.log),
		psmtp.WithSentAppender(func(raw []byte) (string, error) {
			return t.app.appendToSent(*account, raw)
		}),
	)
	return sender.Transmit(ctx, m)
}

// appendToSent puts a copy of a message that has just been sent in the
// account's Sent folder, so it is there in webmail and in every other client,
// not only in Pelton (#451).
//
// It opens its own imap session rather than borrowing the sync engine's. That
// one is parked in IDLE almost all the time, and interrupting it to append
// would cost more than a second connection does for something that happens
// once per sent message.
//
// The copy is appended to the server and not written locally: the folder's next
// sync pulls it down like any other message, which keeps one path for how mail
// arrives in the store.
func (a *App) appendToSent(account storage.Account, raw []byte) (string, error) {
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return "", err
	}
	// every other imap session in the package takes this, so a send during a
	// sync waits its turn rather than opening a second login for the account.
	syncMu.Lock()
	defer syncMu.Unlock()
	client, err := a.connectIMAP(cfg)
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()
	if err := client.Login(); err != nil {
		return "", err
	}
	defer func() { _ = client.Logout() }()
	return client.AppendToSent(raw)
}

// runInitialSyncAndIdle syncs every account once, then parks each on idle.
func (a *App) runInitialSyncAndIdle() {
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		a.log.Error("list accounts for sync", "err", err)
		return
	}
	for _, account := range accounts {
		if account.Local {
			continue
		}
		if err := a.syncAccount(account); err != nil {
			// a missing password is not an error to shout about, but dropping it
			// without a word left the one case that needs explaining with no
			// trace at all, even with file logging on.
			if errors.Is(err, errNoCredentials) {
				a.log.Warn("mailbox has no password, not syncing", "account", account.Email)
			} else {
				a.log.Error("initial sync", "account", account.Email, "err", err)
			}
		}
		goSafe("waiting for new mail", func() { a.idleLoop(account) })
	}
	// the marks from this pass are pushed like any other run's. without it a
	// mailbox that failed its first sync stayed unmarked until some later run
	// happened to end, which for a mailbox with no password is never.
	a.emitAccountSyncStates()
	// contacts ride along with the mail sync (#168). It is one cheap request
	// per address book when nothing changed, and it runs after the mail so a
	// slow contacts server never delays the inbox.
	a.syncContactsInBackground()
}

// TriggerSync syncs all accounts on demand (the ui refresh action). It returns a
// clear error only when no account could be synced for lack of credentials.
func (a *App) TriggerSync() error {
	if err := a.ready(); err != nil {
		return err
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return err
	}

	synced := 0
	netFailed := false
	syncable := 0
	var otherErr error
	for _, account := range accounts {
		if account.Local {
			continue
		}
		syncable++
		if err := a.syncAccount(account); err != nil {
			if errors.Is(err, errNoCredentials) {
				continue
			}
			if isNetworkError(err) {
				netFailed = true
			} else if otherErr == nil {
				otherErr = err
			}
			a.log.Error("sync account", "account", account.Email, "err", err)
			continue
		}
		synced++
	}
	// the marks are what carries a partial failure now, so they are pushed
	// whatever this returns: an account that failed while the others got
	// through used to leave the ui saying the sync was clean.
	a.emitAccountSyncStates()
	// the address books refresh with the mail rather than on a timer of their
	// own, off the calling goroutine so a contacts server that is down cannot
	// make the refresh button hang (#168).
	a.syncContactsInBackground()
	// the Local Folders account is not counted: an install holding only
	// imported mail has nothing to sync, which is not a credentials problem.
	if synced == 0 && syncable > 0 {
		// a dropped connection must not masquerade as a credentials problem: if
		// nothing synced and any account failed for the network, report offline.
		if netFailed {
			return errOffline
		}
		// nor must any other failure: a folder the server no longer has read as
		// a missing password (#484).
		if otherErr != nil {
			return otherErr
		}
		return errNoCredentials
	}
	return nil
}

// syncAccount syncs one account and records how it went, so a failure survives
// the run it happened in. Every caller goes through here rather than
// syncAccountOnce, since an unrecorded failure is the bug (#322).
func (a *App) syncAccount(account storage.Account) error {
	// Local Folders has no server behind it: imported mail is never uploaded,
	// reconciled or expunged. It has no sync state either, since it is not
	// failing to do something it never does.
	if account.Local {
		return nil
	}
	err := a.syncAccountOnce(account)
	a.noteSyncOutcome(account.ID, err)
	return err
}

// syncAccountOnce connects with the account's resolved credentials, syncs every
// folder emitting progress and new-mail events, then logs out.
func (a *App) syncAccountOnce(account storage.Account) error {
	cfg, err := a.resolveIMAP(account)
	if err != nil {
		return err
	}

	syncMu.Lock()
	defer syncMu.Unlock()

	a.emit(EventSyncState, SyncStateEvent{Running: true})
	defer a.emit(EventSyncState, SyncStateEvent{Running: false})

	client, err := a.connectIMAP(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		a.noteLoginResult(account.ID, err)
		return err
	}
	a.noteLoginResult(account.ID, nil)
	defer client.Logout()

	// an account can reach here with no folder rows at all: restored from a
	// backup import, or its setup-time discovery failed. syncFolders only walks
	// existing rows, so without this the sync would be a silent no-op forever.
	if err := a.ensureFolders(client, account.ID); err != nil {
		return err
	}
	// a failed refresh keeps the tree it had; the mail is still worth syncing.
	if changed, err := a.refreshFolders(client, account.ID); err != nil {
		a.log.Error("refresh folder list", "account", account.ID, "err", err)
	} else if changed {
		a.emit(EventFoldersChanged, nil)
	}

	return a.syncFolders(client, account.ID)
}

// syncFolders runs the sync engine over each stored folder of an account,
// emitting a progress event per folder and a new-mail event when one gained
// messages.
func (a *App) syncFolders(client mailClient, accountID int64) error {
	all, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return err
	}
	// folders the user unchecked are skipped here rather than inside the engine,
	// so they never reach a SELECT and never cost a round trip. They are also
	// left out of the progress total, since counting folders that are not being
	// synced makes the bar lie (#173).
	folders := make([]storage.Folder, 0, len(all))
	for _, f := range all {
		// a container the server marks \Noselect holds no mail and cannot be
		// selected, so syncing one fails every single time. It is kept as a row
		// because the tree needs it as a parent, but it is not synced.
		if !f.SyncExcluded && folderSelectable(f) {
			folders = append(folders, f)
		}
	}
	engine := a.newSyncEngine(client, accountID)
	email := a.accountEmail(accountID)
	a.syncTally.begin(len(folders))

	newTotal := 0
	// a folder that fails does not stop the others: mail the rest of the account
	// can still fetch is worth having. But the failure is carried out of here,
	// because reporting the account as synced when a folder did not sync is what
	// makes a mailbox go quiet with nothing to show for it.
	var failedFolders []string
	var firstFolderErr error
	for i, f := range folders {
		a.syncTally.enterFolder(i, f.Name)
		a.emitSyncProgress(accountID, email, client.Addr(), a.syncTally.counts())
		res, err := engine.SyncFolder(a.ctx, f)
		if err != nil {
			a.log.Error("sync folder", "folder", f.Name, "err", err)
			failedFolders = append(failedFolders, f.Name)
			if firstFolderErr == nil {
				firstFolderErr = err
			}
			continue
		}
		if res.New > 0 {
			newTotal += res.New
			a.emit(EventMailNew, MailNewEvent{AccountID: accountID, FolderID: f.ID, Count: res.New})
			goSafe("announcing new mail", func() { a.notifyNewMail(f, res.NewIDs) })
		}
		a.afterRepairs(f, res.RepairedIDs)
	}
	// an empty folder name is how the ui knows the run is over and clears its
	// line; the counts ride along so a finished bar reads full rather than
	// snapping back to nothing.
	final := a.syncTally.counts()
	final.Folder = ""
	final.FoldersDone = len(folders)
	a.emitSyncProgress(accountID, email, client.Addr(), final)

	// index the freshly synced mail so it becomes searchable. run it off the sync
	// path so the search backfill never holds up the next sync.
	if newTotal > 0 {
		goSafe("indexing new mail", func() { _ = a.indexNewMessages() })
		goSafe("counting unread mail", a.refreshViewCounts)
		if !a.lowPowerMode() {
			goSafe("collecting addresses", a.harvestAddressBook)
		}
	}
	return folderSyncError(failedFolders, len(folders), firstFolderErr)
}

// folderSyncError turns the folders that failed in one run into the error the
// account's sync outcome records, or nil when they all got through.
//
// The first failure is wrapped rather than described, so the reason survives:
// the ui classifies a sync failure by what the error is, and the detail dialog
// shows the server's own words.
func folderSyncError(failed []string, total int, first error) error {
	if first == nil {
		return nil
	}
	if len(failed) == 1 {
		return fmt.Errorf("sync folder %q: %w", failed[0], first)
	}
	return fmt.Errorf("%d of %d folders failed to sync, starting with %q: %w",
		len(failed), total, failed[0], first)
}

// findInboxFolder returns the account's INBOX folder row. IMAP's INBOX is a
// case-insensitive special name, so the match ignores case.
func (a *App) findInboxFolder(accountID int64) (*storage.Folder, error) {
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		return nil, err
	}
	for i := range folders {
		if strings.EqualFold(folders[i].IMAPPath, "INBOX") {
			return &folders[i], nil
		}
	}
	return nil, fmt.Errorf("no inbox folder for account %d", accountID)
}

// syncOneFolder runs the sync engine over a single folder, emitting the same
// progress/new-mail events syncFolders would, without touching any other
// folder on the account. Used by the idle push handler so a single INBOX
// update does not pay for a full-account resync.
func (a *App) syncOneFolder(client mailClient, folder storage.Folder) error {
	// the idle push path reaches this directly, so it has to honour the
	// exclusion too. An excluded INBOX is unusual but it is the user's call.
	if folder.SyncExcluded {
		return nil
	}
	engine := a.newSyncEngine(client, folder.AccountID)

	res, err := engine.SyncFolder(a.ctx, folder)
	if err != nil {
		return err
	}
	a.afterRepairs(folder, res.RepairedIDs)
	if res.New > 0 {
		a.emit(EventMailNew, MailNewEvent{AccountID: folder.AccountID, FolderID: folder.ID, Count: res.New})
		goSafe("announcing new mail", func() { a.notifyNewMail(folder, res.NewIDs) })
		goSafe("indexing new mail", func() { _ = a.indexNewMessages() })
		goSafe("counting unread mail", a.refreshViewCounts)
		if !a.lowPowerMode() {
			goSafe("collecting addresses", a.harvestAddressBook)
		}
	}
	return nil
}

// idleLoop parks one account on imap idle and re-syncs when the server reports
// activity, reconnecting with a short backoff and exiting on app shutdown.
func (a *App) idleLoop(account storage.Account) {
	if account.Local {
		return
	}
	ctx := a.sessionCtx()
	for ctx.Err() == nil {
		if err := a.idleSession(ctx, account); err != nil && ctx.Err() == nil {
			if !errors.Is(err, errNoCredentials) {
				a.log.Error("idle session", "account", account.Email, "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(idleRetryWait(err)):
			}
		}
	}
}

// idleSession opens one connection for an account, idles, and re-syncs on each
// server update until the connection drops or the context is cancelled.
func (a *App) idleSession(ctx context.Context, account storage.Account) error {
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
		a.noteLoginResult(account.ID, err)
		return err
	}
	a.noteLoginResult(account.ID, nil)
	defer client.Logout()

	if !client.SupportsIdle() {
		<-ctx.Done()
		return nil
	}

	// IDLE requires a selected mailbox; the server reports unsolicited activity
	// for whichever mailbox is selected, so we monitor INBOX (where new mail
	// lands). without this SELECT the server rejects IDLE outright.
	inbox, err := a.findInboxFolder(account.ID)
	if err != nil {
		return fmt.Errorf("look up inbox folder: %w", err)
	}
	if _, err := client.Select(inbox.IMAPPath); err != nil {
		return fmt.Errorf("select inbox for idle: %w", err)
	}

	// Park on IDLE; when the server pushes activity, IdleUntil stops IDLE and
	// returns, freeing the connection so the resync FETCH can run on it. Doing
	// the FETCH while IDLE was still held is what made new mail take minutes to
	// arrive. Only INBOX is resynced here (idle watches only INBOX); other
	// folders are covered by the periodic full sync (runAutoSyncLoop). syncMu is
	// held only for the brief resync so manual and background syncs are not
	// blocked while idling.
	for ctx.Err() == nil {
		gotUpdate, err := client.IdleUntil(ctx)
		if err != nil {
			return err
		}
		if !gotUpdate {
			return nil
		}
		syncMu.Lock()
		err = a.syncOneFolder(client, *inbox)
		syncMu.Unlock()
		if err != nil {
			a.log.Error("idle resync", "err", err)
		}
	}
	return nil
}

// newSyncEngine builds a sync engine for one account with the settings every
// caller needs, including where deleted mail goes. Roles are resolved here
// because the sync package does not know about them.
func (a *App) newSyncEngine(client mailClient, accountID int64) *psync.Engine {
	engine := psync.NewEngine(client, a.store, a.log)
	engine.ColorSync = a.boolSetting(settingFlagColorSync, false)
	engine.InitialLimit = a.syncMessageLimit()
	// a message the user pressed send on goes out during a sync, not after it.
	engine.YieldTo = a.outboxPending
	// and the list fills as mail arrives rather than staying empty until the
	// whole folder is down.
	engine.OnStored = a.announceStored
	// the counts behind the progress bar. The account and server are captured
	// here because the engine does not know them and the line names them.
	email := a.accountEmail(accountID)
	// the trash-folder tests build an engine with no client, and a progress line
	// is not worth a panic over.
	server := ""
	if client != nil {
		server = client.Addr()
	}
	engine.OnProgress = func(p psync.FolderProgress) {
		a.emitSyncProgress(accountID, email, server, a.syncTally.record(p))
	}
	if trash, ok := a.findTrashFolder(accountID); ok {
		engine.TrashPath = trash.IMAPPath
		engine.TrashFolderID = trash.ID
	}
	return engine
}

// streamInterval is the shortest gap between two "mail arrived" events during a
// sync. Each one costs the ui a list reload, and a fast server can store a
// batch every few milliseconds; twice a second looks live without the list
// spending the sync redrawing itself.
const streamInterval = 500 * time.Millisecond

// streamGate rate-limits those events. It is a value on App rather than a
// package variable so two accounts syncing at once do not silence each other
// unfairly, and it is safe for the concurrent syncs that produce them.
type streamGate struct {
	mu   sync.Mutex
	last time.Time
}

func (g *streamGate) ready() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if now.Sub(g.last) < streamInterval {
		return false
	}
	g.last = now
	return true
}

// emitSyncProgress sends one progress event. It is the only place the event is
// built, so the folder line and the message counts can never disagree.
func (a *App) emitSyncProgress(accountID int64, email, server string, c syncCounts) {
	a.emit(EventSyncProgress, SyncProgressEvent{
		AccountID: accountID, AccountEmail: email, Server: server,
		Folder: c.Folder, Done: c.Done, Total: c.Total,
		FolderDone: c.FolderDone, FolderTotal: c.FolderTotal,
		FoldersDone: c.FoldersDone, FoldersTotal: c.FoldersTotal,
	})
}

// the coarse kinds of sync failure. The ui has a sentence for each; the raw
// error travels alongside as detail for whoever wants the server's own words.
const (
	syncFailAuth        = "auth"
	syncFailNetwork     = "network"
	syncFailCredentials = "credentials"
	syncFailCertificate = "certificate"
	syncFailOther       = "other"
)

// noteSyncOutcome records how an account's sync went. This is what makes a
// failure outlive the run it happened in: before it, one broken account among
// several left nothing behind but a log line, and logging is off by default
// (#322).
func (a *App) noteSyncOutcome(accountID int64, err error) {
	if err == nil {
		if e := a.store.RecordSyncOK(a.ctx, accountID); e != nil {
			a.log.Error("record sync ok", "account", accountID, "err", e)
		}
		return
	}
	if e := a.store.RecordSyncFailure(a.ctx, accountID, syncFailureReason(err), err.Error()); e != nil {
		a.log.Error("record sync failure", "account", accountID, "err", e)
	}
}

// syncFailureReason classifies a sync error into the kinds the ui can explain.
func syncFailureReason(err error) string {
	switch {
	case errors.Is(err, errNoCredentials):
		return syncFailCredentials
	case errors.Is(err, pimap.ErrAuthFailed):
		return syncFailAuth
	case isUntrustedCert(err):
		return syncFailCertificate
	case isNetworkError(err):
		return syncFailNetwork
	default:
		return syncFailOther
	}
}

// accountEmail is the address to name in a progress line, empty when the
// account cannot be read. A progress line is not worth failing a sync over.
func (a *App) accountEmail(accountID int64) string {
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return ""
	}
	return account.Email
}

// outboxPending reports whether anything is waiting to be sent. Sync asks it
// between messages and stands aside while it is true.
func (a *App) outboxPending() bool {
	if a.queue == nil {
		return false
	}
	return a.queue.Pending(a.ctx)
}

// announceStored tells the ui about mail stored so far in a folder that is
// still syncing. The list reloads on it, which is cheap now that the read is
// indexed, so a first sync looks like mail arriving instead of a frozen window.
// Notifications are not sent from here: those still go out once per folder, so
// a first sync of fourteen thousand messages does not become fourteen thousand
// notifications.
func (a *App) announceStored(folder storage.Folder, ids []int64) {
	if !a.streamTick.ready() {
		return
	}
	a.emit(EventMailNew, MailNewEvent{
		AccountID: folder.AccountID, FolderID: folder.ID, Count: len(ids),
	})
}

// findTrashFolder returns the account's trash-role folder. Without one, a
// delete has nowhere to go and falls back to a permanent expunge, so the caller
// has to know whether there is one.
func (a *App) findTrashFolder(accountID int64) (storage.Folder, bool) {
	folders, err := a.store.ListFolders(a.ctx, accountID)
	if err != nil {
		a.log.Error("find trash folder", "account", accountID, "err", err)
		return storage.Folder{}, false
	}
	for _, f := range folders {
		if folderRole(f) == roleTrash {
			return f, true
		}
	}
	return storage.Folder{}, false
}

// afterRepairs deals with messages whose text was fetched again because what
// was cached could not be decoded. The list reloads so the reader sees the
// fixed subject without reopening the folder, and the search index is
// rewritten for those messages, which the incremental pass would never revisit.
func (a *App) afterRepairs(folder storage.Folder, ids []int64) {
	if len(ids) == 0 {
		return
	}
	a.log.Info("repaired cached mail with broken text", "folder", folder.Name, "count", len(ids))
	a.emit(EventMailRepaired, MailRepairedEvent{
		AccountID: folder.AccountID, FolderID: folder.ID, Count: len(ids),
	})
	goSafe("reindexing repaired mail", func() { a.reindexMessages(ids) })
}
