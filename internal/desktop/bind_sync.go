package desktop

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/pool"
	"github.com/peltonapp/Pelton/internal/sync/throttle"
)

const (
	// idleRetry is how long an idle loop waits after a connection it did not
	// expect to lose.
	idleRetry = 15 * time.Second
	// idleNoCredentialsRetry is how long it waits when the account has no
	// password. Longer, because nothing changes until the user types one.
	idleNoCredentialsRetry = 60 * time.Second
)

// syncStepAttempts is how many times ListStubs or FetchBodies runs for one
// folder before the initial sync order (Inbox stubs, Inbox bodies, Sent, then
// the rest) marks that folder failed and moves on. The first try counts.
const syncStepAttempts = 3

// syncRetryBackoff is the wait before another try of a failed ListStubs or
// FetchBodies step. attempt is 0 after the first failure. Tests shorten it.
var syncRetryBackoff = func(attempt int) time.Duration {
	d := 500 * time.Millisecond << attempt
	if d > 8*time.Second {
		return 8 * time.Second
	}
	return d
}

// accountWorker is one account's sync+idle/watch goroutine.
type accountWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// closerToken closes an IMAP session once, whether its task returns or
// abort wins the race.
type closerToken struct {
	once sync.Once
	fn   func()
}

func (t *closerToken) Close() {
	if t == nil || t.fn == nil {
		return
	}
	t.once.Do(t.fn)
}

// accountSync is one account's task scheduler, connection cap and pooled
// sessions.
type accountSync struct {
	sched    *syncsched.Scheduler[mailClient]
	pool     *pool.Pool
	sessions *pool.Sessions[mailClient]
	// shared is the account's state that outlives this runtime: the
	// sessions above, the running work of every runtime of the account, the
	// folder-list claims and the throttle.
	shared   *accountSessions
	stopOnce sync.Once
	// stopped closes once stop has returned.
	stopped chan struct{}
	// ctx is the profile session the runtime belongs to. Once it is done
	// the runtime is stale and ensureAccountSync replaces it. Tasks run
	// under the app context, so a profile switch does not cancel them.
	ctx context.Context
	mu  sync.Mutex
	// bodies is what the account's body generators fetch beyond the window.
	bodies map[int64]*bodyWant
	// bodyRuns is, per folder, its body generator's run: whether its
	// repair pass is running or done (see startFinish).
	bodyRuns map[int64]*bodyRunState
	// fresh holds, per folder, the ids a list found as new mail and when;
	// their bodies notify (see notify_fresh.go).
	fresh map[int64]map[string]time.Time
	// listedNext holds, per folder, the UIDNEXT the last clean new-mail
	// list saw (see newMailBurst).
	listedNext map[int64]uidMark
	// unreadable counts, per folder and remote id, the body fetches that
	// failed on the message itself (psync.MessageError). From
	// unreadableTries on, background fetches skip it until a manual Sync.
	unreadable map[int64]map[string]int
}

// unreadableTries is how many times a message that cannot be read is tried
// before background fetches leave it alone until the next manual Sync.
const unreadableTries = 3

// noteUnreadable counts one more failed read of each id.
func (rt *accountSync) noteUnreadable(folderID int64, ids []string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.unreadable == nil {
		rt.unreadable = make(map[int64]map[string]int)
	}
	if rt.unreadable[folderID] == nil {
		rt.unreadable[folderID] = make(map[string]int)
	}
	for _, id := range ids {
		rt.unreadable[folderID][id]++
	}
}

// unreadableLocked is the folder's ids tried unreadableTries times.
func (rt *accountSync) unreadableLocked(folderID int64) []string {
	var out []string
	for id, n := range rt.unreadable[folderID] {
		if n >= unreadableTries {
			out = append(out, id)
		}
	}
	return out
}

// forgetUnreadable lets a manual Sync try every message again.
func (rt *accountSync) forgetUnreadable() {
	rt.mu.Lock()
	rt.unreadable = nil
	rt.mu.Unlock()
}

const (
	syncThrottleFailThreshold    = throttle.FailThreshold
	syncThrottleRecoverThreshold = throttle.RecoverThreshold
)

// stop ends the runtime at a safe point: queued tasks are dropped and the
// running ones finish their own task, waited for up to
// stopAccountWorkerTimeout. hard cancels them instead (account removal).
func (rt *accountSync) stop(hard bool) {
	if rt == nil || rt.sched == nil {
		return
	}
	rt.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), stopAccountWorkerTimeout)
		defer cancel()
		if rt.stopped != nil {
			defer close(rt.stopped)
		}
		if hard {
			_ = rt.sched.Abort(ctx)
			return
		}
		_ = rt.sched.Stop(ctx)
	})
}

// accountWorkerKey marks a worker's context with its account id. Every sync
// call made under it (initial sync, IDLE and its follow-ups) counts as the
// worker's own; see ensureAccountSync.
type accountWorkerKey struct{}

// accountTaskKey marks the context of an account's scheduler, so every task
// of it (and everything a task calls) is known to run inside one.
type accountTaskKey struct{}

// workerOwns reports whether ctx belongs to the account's worker.
func workerOwns(ctx context.Context, accountID int64) bool {
	id, ok := ctx.Value(accountWorkerKey{}).(int64)
	return ok && id == accountID
}

// startAccountWorker cancels any existing worker for the account, then starts
// a fresh sync and idle/watch loop under the profile session. While the
// account is held (removal) it does nothing: the holder
// starts the worker itself when it is done, with startHeldAccountWorker.
func (a *App) startAccountWorker(accountID int64) {
	if held, _ := a.syncBlock(accountID); held {
		return
	}
	a.launchAccountWorker(accountID, false)
}

// startHeldAccountWorker is startAccountWorker for the caller holding the
// account. The worker it starts runs while the hold is still in place.
func (a *App) startHeldAccountWorker(accountID int64) {
	a.launchAccountWorker(accountID, true)
}

func (a *App) launchAccountWorker(accountID int64, holder bool) {
	parent := a.sessionCtx()
	ctx, cancel := context.WithCancel(context.WithValue(parent, accountWorkerKey{}, accountID))
	done := make(chan struct{})

	for {
		a.stopAccountWorker(accountID)
		a.workersMu.Lock()
		// Checked again under workersMu: a hold taken after the check above is
		// followed by the holder's stopAccountWorker, which also takes workersMu,
		// so this worker is either refused here or stopped there.
		if held, _ := a.syncBlock(accountID); held && !holder {
			a.workersMu.Unlock()
			cancel()
			return
		}
		// Another launch registered its worker since the stop: stop that one
		// too, so the account never has two.
		if a.workers[accountID] != nil {
			a.workersMu.Unlock()
			continue
		}
		if a.workers == nil {
			a.workers = make(map[int64]*accountWorker)
		}
		a.workers[accountID] = &accountWorker{cancel: cancel, done: done}
		a.workersMu.Unlock()
		break
	}

	goSafe("syncing a mailbox", func() {
		defer close(done)
		// The worker owns the scheduler it started or found. If it exits late,
		// after a replacement worker started its own, it must leave that one
		// running, so teardown only removes this exact runtime.
		var rt *accountSync
		defer func() { a.stopAccountSchedulerIf(accountID, rt) }()
		account, err := a.store.GetAccount(a.ctx, accountID)
		if err != nil {
			a.log.Error("load account for worker", "account", accountID, "err", err)
			return
		}
		// The scheduler owns this account's sync tasks until the worker
		// exits, which stops it at a safe point.
		rt, err = a.ensureAccountSync(ctx, account.ID)
		if err != nil {
			return
		}
		a.protocolFor(*account).firstSync(ctx, *account)
		<-ctx.Done()
	})
}

// stopAccountWorkerTimeout is how long account removal (and similar) wait for
// an account worker to exit after cancel. A stuck IMAP FETCH must not freeze
// the settings forever. Tests shorten it.
var stopAccountWorkerTimeout = 5 * time.Second

// stopAccountWorker cancels and joins the worker for one account, if any,
// and stops its scheduler at a safe point: queued tasks are dropped, running
// ones end their own task. Other accounts are untouched. After
// stopAccountWorkerTimeout it returns anyway.
func (a *App) stopAccountWorker(accountID int64) {
	a.endAccountWorker(accountID, false)
}

// abortAccountWorker is stopAccountWorker for account removal: it cancels
// running tasks and closes the account's IMAP sessions so a blocked command
// returns and the account's lock can be released.
func (a *App) abortAccountWorker(accountID int64) {
	a.endAccountWorker(accountID, true)
}

func (a *App) endAccountWorker(accountID int64, hard bool) {
	a.workersMu.Lock()
	w := a.workers[accountID]
	if w != nil {
		delete(a.workers, accountID)
	}
	a.workersMu.Unlock()
	if hard {
		a.abortAccountSync(accountID)
	}
	if w == nil {
		a.stopAccountScheduler(accountID, hard)
		return
	}
	w.cancel()
	started := time.Now()
	a.stopAccountScheduler(accountID, hard)
	jobsJoined := time.Now()
	timer := time.NewTimer(stopAccountWorkerTimeout)
	defer timer.Stop()
	select {
	case <-w.done:
	case <-timer.C:
		if a.log != nil {
			a.log.Warn("account worker stop timed out", "accountID", accountID)
		}
	}
	if a.log != nil {
		a.log.Debug("account worker stopped", "account", accountID,
			"jobs_ms", jobsJoined.Sub(started).Milliseconds(), "worker_ms", time.Since(jobsJoined).Milliseconds())
	}
}

// hasAccountWorker reports whether the account has a registered worker.
func (a *App) hasAccountWorker(accountID int64) bool {
	a.workersMu.Lock()
	defer a.workersMu.Unlock()
	return a.workers[accountID] != nil
}

// joinAllAccountWorkers waits for every registered worker to exit. The session
// cancel should already have been fired so they leave their loops.
func (a *App) joinAllAccountWorkers() {
	a.workersMu.Lock()
	workers := make([]*accountWorker, 0, len(a.workers))
	for id, w := range a.workers {
		workers = append(workers, w)
		delete(a.workers, id)
	}
	a.workersMu.Unlock()
	for _, w := range workers {
		w.cancel()
		<-w.done
	}
}

// idleRetryWait says how long to wait before opening an account's idle
// connection again.
//
// A missing password used to end the loop for good. That made a mailbox
// unfixable without a restart: the user typed the password, the account still
// received nothing, and only the next launch brought it back. Waiting is the
// whole point, since a password can arrive at any moment.
//
// Tests replace idleRetryWaitFor to avoid multi-second sleeps.
var idleRetryWaitFor = idleRetryWait

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
			if err := a.runAutoSyncPass(); err != nil && !errors.Is(err, errNoCredentials) {
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
		outbox.WithOnChange(func() {
			a.emit(EventOutboxChanged, nil)
		}),
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
	// SMTP stays outside the sync pool and pause
	// nothing: background sync keeps running while mail is sent. Never
	// Acquire a slot here.
	account, err := t.app.store.GetAccount(ctx, m.AccountID)
	if err != nil {
		return err
	}
	return t.app.protocolFor(*account).transmit(ctx, *account, m)
}

// appendToSent puts a copy of a message that has just been sent in the
// account's Sent folder, so it is there in webmail and in every other client,
// not only in Pelton (#451). It is a fast-lane task on the account's pool;
// SMTP itself stays outside the pool.
//
// The copy is appended to the server and not written locally: the folder's next
// sync pulls it down like any other message, which keeps one path for how mail
// arrives in the store.
func (a *App) appendToSent(account storage.Account, raw []byte) (string, error) {
	var folder string
	err := a.runNow(a.ctx, account, syncsched.KindSentCopy, 0, func(_ context.Context, client mailClient) error {
		unlock := a.lockMutations(account.ID)
		defer unlock()
		var err error
		folder, err = client.AppendToSent(raw)
		return err
	})
	return folder, err
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
		a.startAccountWorker(account.ID)
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
	return a.syncListedAccounts(nil, true)
}

// runAutoSyncPass is one tick of timed auto-sync.
func (a *App) runAutoSyncPass() error {
	return a.syncListedAccounts(a.autoSyncAccount, false)
}

// autoSyncAccount reports whether timed auto-sync should touch this account.
func (a *App) autoSyncAccount(account storage.Account) bool {
	if account.Local {
		return false
	}
	return a.protocolFor(account).needsTimedSync(account.ID)
}

// syncListedAccounts syncs every remote account include accepts (all when nil).
// manual is true when the user asked for the sync.
func (a *App) syncListedAccounts(include func(storage.Account) bool, manual bool) error {
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
		if include != nil && !include(account) {
			continue
		}
		syncable++
		if err := a.syncAccount(account, manual); err != nil {
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
// syncAccountOnce, since an unrecorded failure is the bug (#322). manual is
// true only for a sync the user asked for.
func (a *App) syncAccount(account storage.Account, manual bool) error {
	return a.syncAccountCtx(a.ctx, account, manual)
}

func (a *App) syncAccountCtx(ctx context.Context, account storage.Account, manual bool) error {
	// Local Folders has no server behind it: imported mail is never uploaded,
	// reconciled or expunged. It has no sync state either, since it is not
	// failing to do something it never does.
	if account.Local {
		return nil
	}
	// The pass's bodies may end after it returned; their late outcome
	// reads whether this pass's own outcome was counted (passOutcome).
	ctx = withPassOutcome(ctx)
	err := a.syncAccountOnceCtx(ctx, account, manual)
	if ctx.Err() != nil {
		// Aborted for removal or shutdown, not a sync failure.
		return ctx.Err()
	}
	if errors.Is(err, errAccountSyncHeld) {
		// The account is being removed or was just removed.
		return nil
	}
	a.notePassOutcome(ctx, account.ID, err)
	return err
}

// syncAccountOnceCtx syncs one account as normal-lane tasks; manual (the
// user's Sync) puts them in the top class, a timed sync in the ordinary ones.
func (a *App) syncAccountOnceCtx(ctx context.Context, account storage.Account, manual bool) error {
	return a.protocolFor(account).manualSync(ctx, account, manual)
}

// syncFolders runs the sync engine over each stored folder of an account,
// emitting a progress event per folder and a new-mail event when one gained
// messages.
func (a *App) syncFolders(ctx context.Context, adapter psync.Adapter, accountID int64) error {
	all, err := a.store.ListFolders(ctx, accountID)
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
	engine := a.newSyncEngine(adapter, accountID)
	email := a.accountEmail(accountID)
	server := ""
	if adapter != nil {
		server = adapter.Addr()
	}
	run := a.startSyncRun(ctx, accountID, email, server, len(folders))
	// an early return (cancelled run) would otherwise leave the ui's line for
	// this account running forever.
	defer run.abort()

	// a driver that locks per folder lets ReleaseLock yield between body
	// batches. The lock is this account's only; other accounts never wait on
	// it. IMAP sync runs as scheduler tasks and does not come through here.
	lockPerFolder := false
	lock := a.accountLock(accountID)
	if acc, err := a.store.GetAccount(a.ctx, accountID); err == nil && a.protocolFor(*acc).lockPerFolder() {
		lockPerFolder = true
	}

	newTotal := 0
	// a folder that fails does not stop the others: mail the rest of the account
	// can still fetch is worth having. But the failure is carried out of here,
	// because reporting the account as synced when a folder did not sync is what
	// makes a mailbox go quiet with nothing to show for it.
	var failedFolders []string
	var firstFolderErr error
	for _, f := range folders {
		if err := ctx.Err(); err != nil {
			return err
		}
		run.enter(f.Name)
		if lockPerFolder {
			lock.Lock()
		}
		var res psync.FolderSyncResult
		err := a.listFolderOnce(ctx, accountID, f.ID, func() error {
			r, err := engine.SyncFolder(ctx, f)
			res.New += r.New
			res.NewIDs = append(res.NewIDs, r.NewIDs...)
			res.RepairedIDs = append(res.RepairedIDs, r.RepairedIDs...)
			if psync.IsSoftError(err) {
				// unreadable messages stay stubs and a refused push stays
				// pending; the folder synced, so its new mail is announced.
				a.log.Warn("sync folder", "folder", f.Name, "err", err)
				return nil
			}
			return err
		})
		if lockPerFolder {
			lock.Unlock()
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.log.Error("sync folder", "folder", f.Name, "err", err)
			failedFolders = append(failedFolders, f.Name)
			if firstFolderErr == nil {
				firstFolderErr = err
			}
			run.folderDone()
			continue
		}
		if res.New > 0 {
			newTotal += res.New
			a.emit(EventMailNew, MailNewEvent{AccountID: accountID, FolderID: f.ID, Count: res.New})
			goSafe("announcing new mail", func() { a.notifyNewMail(f, res.NewIDs) })
		}
		a.afterRepairs(f, res.RepairedIDs)
		run.folderDone()
	}
	// an empty folder name is how the ui knows the run is over and clears its
	// line; the counts ride along so a finished bar reads full rather than
	// snapping back to nothing.
	run.finish(len(folders))

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

// idleLoop parks one account on imap idle and re-syncs when the
// server reports activity, reconnecting with a short backoff and exiting on
// worker or app shutdown.
func (a *App) idleLoop(ctx context.Context, account storage.Account) {
	if account.Local {
		return
	}
	a.protocolFor(account).watch(ctx, account)
}

// newSyncEngine builds a sync engine for one account with the settings every
// caller needs, including where deleted mail goes. Roles are resolved here
// because the sync package does not know about them.
func (a *App) newSyncEngine(adapter psync.Adapter, accountID int64) *psync.Engine {
	engine := psync.NewEngine(adapter, a.store, a.log)
	engine.ColorSync = a.boolSetting(settingFlagColorSync, false)
	engine.InitialLimit = a.syncMessageLimit()
	// Stored ids are announced as they land, so the list fills as mail arrives
	// rather than staying empty until the whole folder is down.
	engine.OnStored = a.announceStored
	// the counts behind the progress bar. The account and server are captured
	// here because the engine does not know them and the line names them.
	email := a.accountEmail(accountID)
	// the trash-folder tests build an engine with no adapter, and a progress line
	// is not worth a panic over.
	server := ""
	if adapter != nil {
		server = adapter.Addr()
	}
	engine.OnProgress = func(p psync.FolderProgress) {
		a.emitSyncProgress(accountID, email, server, a.accountTally(accountID).record(p))
	}
	if trash, ok := a.findTrashFolder(accountID); ok {
		engine.TrashRemoteID = trash.RemoteID
		if engine.TrashRemoteID == "" {
			engine.TrashRemoteID = trash.IMAPPath
		}
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

// emitSyncProgress sends one progress event. progressEvent is the only place
// the event is built, so the folder line and the message counts can never
// disagree.
func (a *App) emitSyncProgress(accountID int64, email, server string, c syncCounts) {
	ev := progressEvent(accountID, email, server, c)
	if a.accountRemoved(accountID) {
		// A late task of a removed account: the ui no longer lists it.
		return
	}
	// recording and sending under one lock keeps a heartbeat from slipping a
	// stale running event in after a closing one.
	st := a.accountStateFor(accountID)
	st.progressMu.Lock()
	defer st.progressMu.Unlock()
	a.emitSyncProgressLocked(st, ev)
}

// progressEvent is the progress event of counts c.
func progressEvent(accountID int64, email, server string, c syncCounts) SyncProgressEvent {
	return SyncProgressEvent{
		AccountID: accountID, AccountEmail: email, Server: server,
		Folder: c.Folder, Done: c.Done, Total: c.Total,
		FolderDone: c.FolderDone, FolderTotal: c.FolderTotal,
		FoldersDone: c.FoldersDone, FoldersTotal: c.FoldersTotal,
		Phase: c.Phase,
	}
}

// emitSyncProgressLocked records and sends ev; the caller holds
// st.progressMu.
func (a *App) emitSyncProgressLocked(st *accountState, ev SyncProgressEvent) {
	if ev.Folder == "" {
		st.lastProgress = nil
	} else {
		st.lastProgress = &ev
	}
	a.emit(EventSyncProgress, ev)
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
	a.noteOutcome(accountID, err, nil)
}

// notePassOutcome is noteSyncOutcome for the pass that ran under ctx
// (withPassOutcome): it marks the pass's own outcome counted, which its late
// body outcome reads (noteLateSyncOutcome).
func (a *App) notePassOutcome(ctx context.Context, accountID int64, err error) {
	a.noteOutcome(accountID, err, passOutcomeOf(ctx))
}

func (a *App) noteOutcome(accountID int64, err error, own *passOutcome) {
	// A cancel is how stop, removal and shutdown end a sync on
	// purpose. It says nothing about the account's health, so it is neither a
	// failure to show nor a throttle signal. Real network errors are not
	// Canceled and are still recorded.
	if errors.Is(err, context.Canceled) || errors.Is(err, errAccountSyncHeld) {
		return
	}
	a.noteSyncPoolOutcome(accountID, err)
	l := &a.accountStateFor(accountID).late
	l.mu.Lock()
	defer l.mu.Unlock()
	l.outcomes++
	if own != nil {
		own.counted = true
	}
	a.recordSyncOutcome(accountID, err)
}

// recordSyncOutcome stores err (nil: OK) as the account's sync state. The
// caller holds the account's lateBodies.mu and counted the outcome.
func (a *App) recordSyncOutcome(accountID int64, err error) {
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

// isSyncThrottleError reports connection and rate-limit failures that should
// shrink the sync pool's effective parallelism.
func isSyncThrottleError(err error) bool {
	if err == nil {
		return false
	}
	if isNetworkError(err) {
		return true
	}
	return serverThrottled(err.Error()) || connectionLimit(err)
}

// connectionLimit reports a server refusing work because of how much the
// account is doing, not who it is:
//   - Gmail's "Too many simultaneous connections" and Dovecot's "Maximum
//     number of connections from user+IP exceeded";
//   - the RFC 5530 response codes [LIMIT], [UNAVAILABLE] and [INUSE];
//   - Exchange Online's "Request is throttled. Suggested Backoff Time: N
//     milliseconds".
//
// Each may come as a LOGIN NO, which is then not a wrong password
// (loginRefused). go-imap writes a response code into the error's text as
// "[CODE]", so the text is enough.
//
// A server that says it with an untagged BYE instead (in the greeting or
// before the LOGIN answer) is not seen here: go-imap keeps no BYE text, so
// LOGIN fails with a bare "unexpected EOF". Telling that apart needs
// internal/imap to keep the BYE (or ask WaitGreeting) and return it.
func connectionLimit(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"too many simultaneous connections",
		"maximum number of connections",
		"[limit]",
		"[unavailable]",
		"[inuse]",
		"request is throttled",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return throttle.SuggestedBackoff(msg) > 0
}

// serverThrottled reports the server explicitly asking to slow down:
// Exchange Online's "Request is throttled" or a suggested backoff, a "rate
// limit", "too many requests" or an HTTP 429 status. A network error is not.
func serverThrottled(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "request is throttled") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") ||
		throttleStatus.MatchString(msg) ||
		throttle.SuggestedBackoff(msg) > 0
}

// throttleStatus is an HTTP 429 status named as one, so a uid or a size that
// merely contains the digits does not count.
var throttleStatus = regexp.MustCompile(`\b(?:status|code|http/[0-9.]+)\D{0,3}429\b`)

// noteSyncPoolOutcome adjusts adaptive throttle on the account pool. It does
// not rewrite the saved sync_max_parallel preference. A dial error the
// dialer counted already (throttleCounted) is skipped. The count and the
// resize are the account's, not a runtime's: every runtime of the account
// shares the pool.
func (a *App) noteSyncPoolOutcome(accountID int64, err error) {
	if _, counted := errors.AsType[throttleCounted](err); counted {
		return
	}
	a.noteSyncPool(accountID, err, false)
}

// noteSyncPoolDial counts a failed dial or LOGIN toward the throttle, where
// it happened (imapDialer). A refusal of a session above the current N (one
// dialed before N shrank, while others are still open) is no news about the
// server's limit: it counts as nothing, but what it names (a backoff, a
// limit) still applies. Without that, the dials one shrink left in flight
// shrank N again below what the server takes.
func (a *App) noteSyncPoolDial(accountID int64, err error) {
	a.noteSyncPool(accountID, err, true)
}

func (a *App) noteSyncPool(accountID int64, err error, dial bool) {
	s := a.accountShared(accountID)
	if s == nil {
		return
	}
	// The new N is set under the same lock its decision read the old one
	// under, so a resize in between (a settings save, another outcome) is
	// never overwritten by a value worked out from before it.
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	sig := throttleSignal(err)
	sig.Dial = dial
	if dial && sig.Kind == throttle.Refused && s.sessions.Open() > s.pool.Effective() {
		sig.Kind = throttle.Neutral
	}
	next := s.throttleStepLocked(sig)
	if afterThrottleStep != nil {
		afterThrottleStep(sig)
	}
	if next > 0 {
		s.pool.SetEffective(next)
	}
}

// afterThrottleStep runs between the throttle's decision and its resize,
// with the outcome the throttle counted. Tests set it.
var afterThrottleStep func(throttle.Signal)

// throttleStepLocked counts one outcome toward the throttle and returns the
// new effective N when it changes, else 0. s.resizeMu is held.
func (s *accountSessions) throttleStepLocked(sig throttle.Signal) int {
	return s.throttle.Step(sig, s.pool.Effective(), s.pool.Configured(), s.clock())
}

// throttleSignal is what err tells the throttle: a refusal (with the wait
// or the connection limit the server named, if it named one, and whether it
// is a connection-limit refusal rather than a network error), a success
// (nil), or nothing about the server's capacity. Whether it came from a dial
// is the caller's to set.
func throttleSignal(err error) throttle.Signal {
	switch {
	case err == nil:
		return throttle.Signal{Kind: throttle.OK}
	case !isSyncThrottleError(err):
		return throttle.Signal{Kind: throttle.Neutral}
	}
	msg := err.Error()
	return throttle.Signal{
		Kind:    throttle.Refused,
		Backoff: throttle.SuggestedBackoff(msg),
		Limit:   throttle.NamedLimit(msg),
		// This kind of refusal of a new session ends a probe; Throttled
		// ends one on any session.
		ConnLimit: connectionLimit(err),
		Throttled: serverThrottled(msg),
	}
}

// noteSyncPoolLogin tells the throttle a new session of the account logged
// in: the server took one more connection. imapDialer calls it after each
// good LOGIN, the session already counted as open.
func (a *App) noteSyncPoolLogin(accountID int64) {
	s := a.accountShared(accountID)
	if s == nil {
		return
	}
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	s.throttle.Connected(s.sessions.Open())
}

// applyUserSetting reads the parallelism the user set for the account with
// read and applies it, the read under the same resizeMu as the write. When
// it changed, the throttle starts over at the new size, forgetting the
// ceiling and the probe backoff it learnt: SetConfigured alone
// only ever lowers the effective size, so a raise would not take effect
// until a success streak grew it back. A save that leaves the size as it
// was keeps any throttle in force. Every apply
// of the setting goes through here, so one that read before a save cannot
// land after the save's own apply: it reads the saved value itself. read is
// a local store query; resizeMu's holders never wait for the store, and the
// pool's watchers never take resizeMu.
func (s *accountSessions) applyUserSetting(read func() int) {
	s.resizeMu.Lock()
	defer s.resizeMu.Unlock()
	n := read()
	changed := n != s.userN
	if changed {
		s.userN = n
		s.throttle.Reset()
	}
	// The pool's watchers take scheduler and session locks, never resizeMu.
	s.pool.SetConfigured(n)
	if changed {
		s.pool.SetEffective(n)
	}
}

// syncFailureReason classifies a sync error into the kinds the ui can explain.
func syncFailureReason(err error) string {
	switch {
	case errors.Is(err, errNoCredentials):
		return syncFailCredentials
	case loginRefused(err):
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

// announceStored tells the ui about mail stored so far in a folder that is
// still syncing. The list reloads on it, which is cheap now that the read is
// indexed, so a first sync looks like mail arriving instead of a frozen window.
// Notifications are not sent from here: those still go out once per folder, so
// a first sync of fourteen thousand messages does not become fourteen thousand
// notifications.
func (a *App) announceStored(folder storage.Folder, ids []int64) {
	if a.accountRemoved(folder.AccountID) || !a.streamTick.ready() {
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

// foldersInSyncOrder keeps selectable, included folders and orders them Inbox,
// then Sent, then everything else (stable list order within each group).
func foldersInSyncOrder(folders []storage.Folder) []storage.Folder {
	var inbox, sent, rest []storage.Folder
	for _, f := range folders {
		if f.SyncExcluded || !folderSelectable(f) {
			continue
		}
		switch folderRole(f) {
		case roleInbox:
			inbox = append(inbox, f)
		case roleSent:
			sent = append(sent, f)
		default:
			rest = append(rest, f)
		}
	}
	out := append(inbox, sent...)
	return append(out, rest...)
}

// scheduleSyncRetry waits, then enqueues the next try. The wait is outside
// the job so the pool slot is not held during backoff. A cancelled context
// drops the retry; the caller is already selecting on that context.
func scheduleSyncRetry(ctx context.Context, attempt int, enqueue func()) {
	wait := syncRetryBackoff(attempt)
	if wait <= 0 {
		enqueue()
		return
	}
	go func() {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
			enqueue()
		}
	}()
}

// announceStoredBodies tells the ui about bodies a fetch stored and starts
// the follow-up work (search index, unread counts). Notifications are
// notifyFresh's.
func (a *App) announceStoredBodies(folder storage.Folder, res psync.FolderSyncResult) {
	if res.New == 0 || a.accountRemoved(folder.AccountID) {
		return
	}
	a.emit(EventMailNew, MailNewEvent{AccountID: folder.AccountID, FolderID: folder.ID, Count: res.New})
	a.afterStoredBodies()
}

// afterStoredBodies is the follow-up work of stored bodies: the search
// index and the unread counts.
func (a *App) afterStoredBodies() {
	goSafe("indexing new mail", func() { _ = a.indexNewMessages() })
	goSafe("counting unread mail", a.refreshViewCounts)
}

// ensureAccountSync returns the account's scheduler runtime, starting one if
// it has none or the one it has belongs to an ended profile session. Tasks
// run under the app context, not ctx: callers pass the context of one sync
// run, and a scheduler bound to it would die with that run. Only app quit
// and account removal (Abort) cancel a running task; everything else stops
// the scheduler at a safe point. An app with no ctx (bare test fixtures)
// falls back to ctx.
//
// While the account is held (removal) or after it was removed it returns
// errAccountSyncHeld and starts nothing, so no sync job of the account can
// begin. The one exception is a call under the account's live worker context:
// a holder that starts a worker again (startHeldAccountWorker) does so before
// it lets go of the hold, and every sync call of that worker has to get
// through, not only its first.
// A cancelled worker's calls (the old worker exiting late) are refused, and a
// removed account is refused for everyone.
func (a *App) ensureAccountSync(ctx context.Context, accountID int64) (*accountSync, error) {
	owner := workerOwns(ctx, accountID)
	for {
		// Read before syncsMu: no store query runs under it. n is only the
		// cap of sessions created new here; the setting is applied after
		// syncsMu by applyAccountParallel, which reads it again, so a value
		// read here before a save never undoes it.
		n := a.accountSyncMaxParallel(accountID)
		if beforeAccountSyncLock != nil {
			beforeAccountSyncLock()
		}
		a.syncsMu.Lock()
		// Checked under syncsMu: holdAccountSync runs before the holder stops
		// the scheduler, which takes syncsMu, so a runtime started just before
		// the hold is the one that stop removes.
		held, removed := a.syncBlock(accountID)
		if removed || (held && !owner) {
			a.syncsMu.Unlock()
			return nil, errAccountSyncHeld
		}
		if owner && ctx.Err() != nil {
			a.syncsMu.Unlock()
			return nil, ctx.Err()
		}
		if a.syncs == nil {
			a.syncs = make(map[int64]*accountSync)
		}
		rt := a.syncs[accountID]
		if rt == nil {
			base, appCtx := ctx, ctx
			if a.ctx != nil {
				base, appCtx = a.sessionCtx(), a.ctx
			}
			rt = a.newAccountSync(appCtx, base, accountID, n)
			a.syncs[accountID] = rt
			a.syncsMu.Unlock()
			a.applyAccountParallel(accountID, rt)
			return rt, nil
		}
		if rt.ctx == nil || rt.ctx.Err() == nil {
			a.syncsMu.Unlock()
			a.applyAccountParallel(accountID, rt)
			return rt, nil
		}
		// Its profile session is gone. Drop it and start a fresh one. The old
		// one stops in the background: the stop waits for its running tasks,
		// and the caller may be one of them, or a Done callback on one of its
		// workers, which would wait for itself until the stop timeout. The
		// new runtime is on the same shared sessions, so the cap holds while
		// the old tasks end.
		delete(a.syncs, accountID)
		a.syncsMu.Unlock()
		goSafe("stopping a mailbox's old sync tasks", func() { a.stopSyncRuntime(accountID, rt, false) })
	}
}

// applyAccountParallel applies the account's parallelism setting to rt's
// shared pool, read under the account's resize lock like a settings save's
// (accountSessions.applyUserSetting). Call it on no lock.
func (a *App) applyAccountParallel(accountID int64, rt *accountSync) {
	if rt.shared == nil {
		return
	}
	rt.shared.applyUserSetting(func() int { return a.accountSyncMaxParallel(accountID) })
}

// beforeAccountSyncLock runs in ensureAccountSync after its read of the
// parallelism and before it takes syncsMu. Tests set it.
var beforeAccountSyncLock func()

// newAccountSync builds an account's runtime over its connection cap and
// session pool (cap n when they are new): the task scheduler, whose tasks
// run under appCtx. The caller applies n to a shared pool once it holds no
// lock.
func (a *App) newAccountSync(appCtx, session context.Context, accountID int64, n int) *accountSync {
	shared := a.accountSessionsFor(accountID, n)
	p, sessions := shared.pool, shared.sessions
	sched := syncsched.New(context.WithValue(appCtx, accountTaskKey{}, accountID), shared.view(), syncsched.Options{
		AccountID: accountID,
		Broken:    imapSessionBroken,
		Log:       a.log,
		Group:     &shared.group,
	})
	rt := &accountSync{sched: sched, pool: p, sessions: sessions, shared: shared, ctx: session, stopped: make(chan struct{})}
	// App quit cancels the tasks; wait for them like any background work,
	// so the store is not closed under a running task.
	goSafe("sync tasks", func() {
		select {
		case <-appCtx.Done():
			ctx, cancel := context.WithTimeout(context.Background(), stopAccountWorkerTimeout)
			defer cancel()
			_ = sched.Abort(ctx)
		case <-rt.stopped:
		}
	})
	return rt
}

func (a *App) accountSync(accountID int64) *accountSync {
	a.syncsMu.Lock()
	defer a.syncsMu.Unlock()
	if a.syncs == nil {
		return nil
	}
	return a.syncs[accountID]
}

// stopAccountScheduler stops and forgets whatever scheduler the account has
// now; hard cancels running tasks. Only paths that own the account's whole
// runtime call it: stopAccountWorker, tests.
func (a *App) stopAccountScheduler(accountID int64, hard bool) {
	a.syncsMu.Lock()
	rt := a.syncs[accountID]
	if rt != nil {
		delete(a.syncs, accountID)
	}
	a.syncsMu.Unlock()
	if rt != nil {
		a.stopSyncRuntime(accountID, rt, hard)
	}
}

// stopAccountSchedulerIf stops rt and removes it from the map only if the map
// still holds that same runtime. A worker that exits after its replacement
// started a new scheduler leaves the new one alone. rt is still stopped even
// when it is no longer mapped, since nothing else will stop it. A nil rt is a
// no-op.
func (a *App) stopAccountSchedulerIf(accountID int64, rt *accountSync) {
	if rt == nil {
		return
	}
	a.syncsMu.Lock()
	if a.syncs[accountID] == rt {
		delete(a.syncs, accountID)
	}
	a.syncsMu.Unlock()
	a.stopSyncRuntime(accountID, rt, false)
}

// stopSyncRuntime stops rt's scheduler (see accountSync.stop); hard also
// closes the account's sessions.
func (a *App) stopSyncRuntime(accountID int64, rt *accountSync, hard bool) {
	rt.stop(hard)
	if hard {
		a.closeAccountSessions(accountID)
	}
}

// fetchMessageBodyOnDemand fetches the body of one stub the user opened, as
// a fast-lane task: it starts on a free session, else on IDLE's, else when
// a running task ends. The list row stays on failure; GetMessage surfaces
// the error. While the account is being removed it fetches nothing and
// returns nil, so the stub shows.
func (a *App) fetchMessageBodyOnDemand(m *storage.Message) error {
	if m == nil || m.BodyComplete {
		return nil
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return err
	}
	if account.Local {
		return nil
	}
	folder, err := a.store.GetFolder(a.ctx, m.FolderID)
	if err != nil {
		return err
	}
	msgID := m.ID
	remoteIDs := []string{m.RemoteID}
	if rt, err := a.ensureAccountSync(a.ctx, account.ID); err == nil && m.SizeBytes > rt.sched.ChunkBudget().Get() {
		err = a.fetchLargeBodyOnDemand(*account, *folder, msgID, m.RemoteID)
		if errors.Is(err, errAccountSyncHeld) {
			return nil
		}
		return err
	}
	// A message that cannot be read leaves the task as a value, not as its
	// error: the session is fine and must not be closed for it.
	var unreadable error
	err = a.runNowKey(a.ctx, *account, syncsched.KindOnDemandBody, folder.ID, fmt.Sprintf("open:%d", msgID),
		func(ctx context.Context, _ mailClient) error {
			// A queued open is stale once the body arrived another way, or
			// the message left the folder.
			need, err := a.store.MessageNeedsBody(ctx, msgID)
			if errors.Is(err, storage.ErrMessageNotFound) || (err == nil && !need) {
				return nil
			}
			if a.onDemandFetchForTest != nil {
				err = a.onDemandFetchForTest(ctx, *account, *folder, remoteIDs)
			} else {
				err = a.protocolFor(*account).onDemandBodies(ctx, *account, *folder, remoteIDs)
			}
			if _, ok := errors.AsType[*psync.MessageError](err); ok {
				unreadable = err
				return nil
			}
			return err
		})
	if errors.Is(err, errAccountSyncHeld) {
		// Being removed: the stub stays on screen until the account goes.
		return nil
	}
	if err == nil && unreadable != nil {
		return fmt.Errorf("%w: %w", errMessageUnreadable, unreadable)
	}
	return err
}

// errMessageUnreadable is an opened message the server sent but that could
// not be read (psync.MessageError). Fetching it again gets the same bytes.
var errMessageUnreadable = errors.New("pelton: this message could not be read")

// fetchLargeBodyOnDemand fetches an opened message larger than the chunk
// size in the normal lane, one chunk per unit, so it never holds a
// fast-lane session for a long download. Its kind (KindOpenedBody) runs at
// the top class and waits for no listing or body work of the folder.
func (a *App) fetchLargeBodyOnDemand(account storage.Account, folder storage.Folder, msgID int64, remoteID string) error {
	part := psync.NewPartialBody(remoteID)
	var unreadable error
	err := a.runPooled(a.ctx, account, poolTask{
		Kind: syncsched.KindOpenedBody, FolderID: folder.ID,
		Key: fmt.Sprintf("open:%d", msgID),
		Run: func(ctx context.Context, c mailClient) error {
			if need, err := a.store.MessageNeedsBody(ctx, msgID); err == nil && !need {
				return nil
			}
			rt := a.accountSync(account.ID)
			if rt == nil {
				return errAccountSyncHeld
			}
			engine := a.newSyncEngine(pimap.NewAdapter(c), account.ID)
			engine.OnProgress = nil
			engine.OnStored = nil
			done, res, err := engine.FetchBodyChunk(ctx, folder, part, rt.sched.ChunkBudget().Get())
			// One chunk is one FETCH on this task's session. Joined
			// callers share this unit, so it counts once.
			a.noteUnitOutcome(account.ID, err)
			if _, ok := errors.AsType[*psync.MessageError](err); ok {
				unreadable = err
				return nil
			}
			if err != nil {
				return err
			}
			if !done {
				return syncsched.ErrAgain
			}
			// The pane hears mail:updated from its caller; the list needs no
			// mail:new for a row filled in place.
			if res.New > 0 {
				a.afterStoredBodies()
			}
			a.notifyFresh(ctx, folder, []string{remoteID}, res.NewIDs, false)
			return nil
		},
	})
	if err == nil && unreadable != nil {
		return fmt.Errorf("%w: %w", errMessageUnreadable, unreadable)
	}
	return err
}

// execIMAPOnDemandBodies fetches the bodies of the messages the user opened
// on the task's session. One that cannot be read comes back as a
// *psync.MessageError, the others stored. It draws no progress line, raises
// no notification (the user is reading the message) and sends no mail:new:
// the stubs are filled in place, and the pane hears mail:updated from its
// caller.
func (a *App) execIMAPOnDemandBodies(ctx context.Context, account storage.Account, folder storage.Folder, remoteIDs []string) error {
	err := a.withIMAPSession(ctx, account, func(client mailClient) error {
		engine := a.newSyncEngine(pimap.NewAdapter(client), account.ID)
		engine.OnProgress = nil
		engine.OnStored = nil
		res, err := engine.FetchBodies(ctx, folder, remoteIDs)
		if bad, ok := errors.AsType[*psync.MessageError](err); ok {
			a.log.Warn("opened message could not be read", "folder", folder.Name, "ids", bad.RemoteIDs, "err", bad.Err)
		} else if psync.IsSoftError(err) {
			// A push refused between batches stays pending; the body stands.
			err = nil
		}
		if res.New > 0 {
			a.afterStoredBodies()
		}
		a.notifyFresh(ctx, folder, remoteIDs, res.NewIDs, false)
		return err
	})
	// Called from the open's task unit (or outside a task, where
	// withIMAPSession's own task does not count): once per FETCH.
	a.noteUnitOutcome(account.ID, err)
	return err
}

// syncIMAPInitial records the initial sync pass the way syncAccountCtx records
// every other account sync. onInboxDone runs once Inbox is listed and has its
// first body batch, so IDLE can start before the other folders and the rest
// of Inbox's bodies.
func (a *App) syncIMAPInitial(ctx context.Context, account storage.Account, onInboxDone func()) error {
	if account.Local {
		return nil
	}
	err := a.syncIMAPInitialPass(ctx, account, onInboxDone)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	a.noteSyncOutcome(account.ID, err)
	return err
}

// syncIMAPInitialPass discovers folders as the account's first task, which
// holds every other task until it ends, then queues the sync plan: Inbox
// list and first body batch first, then every other selected folder at once
// while the rest of Inbox's bodies go on. onInboxDone runs at that point, so
// IDLE can start before the other folders and does not wait for every Inbox
// body. The window is the engine's InitialLimit (sync_message_limit).
func (a *App) syncIMAPInitialPass(ctx context.Context, account storage.Account, onInboxDone func()) error {
	if err := a.runFolderTree(ctx, account, func(c mailClient) error {
		return a.syncFolderTree(c, account.ID)
	}); err != nil {
		return err
	}
	return a.runSyncPlan(ctx, account, syncsched.ClassAuto, a.imapPlanStep(account, true), onInboxDone, true)
}

// runFolderTree runs fn as the account's barrier task (KindFolderTree), so
// nothing else of the account starts until it ends, and waits for it.
func (a *App) runFolderTree(ctx context.Context, account storage.Account, fn func(mailClient) error) error {
	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	rt.sched.Enqueue(imapTask{
		Kind: syncsched.KindFolderTree, Key: "folder-tree", Barrier: true,
		Run:  func(_ context.Context, c mailClient) error { return fn(c) },
		Done: func(err error) { done <- err },
	})
	select {
	case err := <-done:
		return a.stoppedErr(account.ID, err)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runSyncPlan runs a sync plan over every selected folder of the account,
// with the account's progress line. toEnd (the initial pass) waits for every
// folder's bodies. Otherwise (a timed or manual pass) it waits for every
// folder's list and returns while the bodies it queued go on: a big body
// backlog ("all" mail, offline) takes hours, and the next pass must still
// list the folders. The progress line then ends with the lists; the bodies
// report their own progress, and watchLateBodies their end.
func (a *App) runSyncPlan(ctx context.Context, account storage.Account, class syncsched.Class, step syncStep, onInboxDone func(), toEnd bool) error {
	all, err := a.store.ListFolders(ctx, account.ID)
	if err != nil {
		return err
	}
	ordered := foldersInSyncOrder(all)
	run := a.startSyncRun(ctx, account.ID, account.Email, "", len(ordered))
	defer run.abort()

	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	plan := syncPlan{
		class:       class,
		step:        step,
		onList:      func(folder storage.Folder) { run.enter(folder.Name) },
		onInboxDone: onInboxDone,
		done:        done,
		bodyWidth:   rt.bodyWidth,
		bodyRun:     rt.bodyRun,
	}
	wait := done
	if toEnd {
		plan.onFolderDone = run.folderDone
	} else {
		listed := make(chan error, 1)
		plan.onListed, plan.listed, wait = run.folderDone, listed, listed
	}
	enqueueSyncPlan(ctx, rt.sched, ordered, plan)

	var runErr error
	select {
	case runErr = <-wait:
	case <-ctx.Done():
		return ctx.Err()
	}
	if !toEnd {
		a.watchLateBodies(&latePass{ctx: ctx, account: account, listErr: runErr, done: done})
	}
	if runErr != nil && (errors.Is(runErr, syncsched.ErrStopped) || errors.Is(runErr, context.Canceled)) {
		return a.stoppedErr(account.ID, runErr)
	}
	run.finish(len(ordered))
	return runErr
}

// stoppedErr turns a task dropped by a stopping scheduler into what sync
// callers expect: errAccountSyncHeld during removal, else context.Canceled.
func (a *App) stoppedErr(accountID int64, err error) error {
	if !errors.Is(err, syncsched.ErrStopped) {
		return err
	}
	if held, _ := a.syncBlock(accountID); held {
		return errAccountSyncHeld
	}
	return context.Canceled
}

// runIMAPManualSync is the sync behind TriggerSync, SyncAccountNow and timed
// auto-sync: a group of normal-lane tasks (folder tree, then each folder's
// list and body batches). The user's Sync (manual) runs them in the top
// class. An opened message's body still goes first, in the fast lane. It
// does not hold the account lock. It returns once every folder is listed;
// the bodies go on in the background (runSyncPlan).
//
// The folder tree is a barrier: it may drop folder rows, so it waits for the
// account's running units to end and nothing starts beside it. A fast task
// asked for meanwhile waits for those units and the one LIST.
func (a *App) runIMAPManualSync(ctx context.Context, account storage.Account, manual bool) error {
	class := syncsched.ClassAuto
	if manual {
		class = syncsched.ClassManualSync
		if rt := a.accountSync(account.ID); rt != nil {
			rt.forgetUnreadable()
		}
	}
	err := a.runPooled(ctx, account, poolTask{
		Kind: syncsched.KindFolderTree, Class: class, Key: "folder-tree", Barrier: true,
		Run: func(_ context.Context, c mailClient) error {
			return a.syncFolderTree(c, account.ID)
		},
	})
	if err != nil {
		return err
	}
	return a.runSyncPlan(ctx, account, class, a.imapPlanStep(account, true), nil, false)
}

// mergeNewestIDs puts the ids of a follow-up list, which are newer, ahead of
// the ones an earlier list of the same folder returned, without repeats.
func mergeNewestIDs(newer, older []string) []string {
	out := append([]string(nil), newer...)
	seen := make(map[string]struct{}, len(newer))
	for _, id := range newer {
		seen[id] = struct{}{}
	}
	for _, id := range older {
		if _, dup := seen[id]; !dup {
			out = append(out, id)
		}
	}
	return out
}

// execIMAPBodies fetches the bodies of remoteIDs on the task's session and
// announces them; the fresh ones among them notify. quiet draws no progress
// line (bodies outside a sync run: backfill, a full folder check).
func (a *App) execIMAPBodies(ctx context.Context, account storage.Account, folder storage.Folder, remoteIDs []string, quiet bool) error {
	return a.fetchIMAPBodies(ctx, account, folder, remoteIDs, quiet, true)
}

// fetchIMAPBodies is execIMAPBodies; notify false never notifies.
func (a *App) fetchIMAPBodies(ctx context.Context, account storage.Account, folder storage.Folder, remoteIDs []string, quiet, notify bool) error {
	return a.fillIMAPBodies(ctx, account, folder, remoteIDs, quiet, notify, true)
}

// fillIMAPBodies is fetchIMAPBodies; withAttachments false stores the
// bodies without attachment files (an offline download without them).
func (a *App) fillIMAPBodies(ctx context.Context, account storage.Account, folder storage.Folder, remoteIDs []string, quiet, notify, withAttachments bool) error {
	err := a.withIMAPSession(ctx, account, func(client mailClient) error {
		engine := a.newSyncEngine(pimap.NewAdapter(client), account.ID)
		engine.WithoutAttachments = !withAttachments
		if quiet {
			engine.OnProgress = nil
		}
		res, err := engine.FetchBodies(ctx, folder, remoteIDs)
		if bad, ok := errors.AsType[*psync.MessageError](err); ok {
			// Only some messages could not be read: the session and the
			// folder are fine, and the rest of the batch is stored.
			a.log.Warn("messages could not be read", "folder", folder.Name, "ids", bad.RemoteIDs, "err", bad.Err)
			if rt := a.accountSync(account.ID); rt != nil {
				if still, serr := a.store.RemoteIDsNeedingBody(ctx, folder.ID, remoteIDs); serr == nil {
					rt.noteUnreadable(folder.ID, still)
				}
			}
		}
		if psync.IsSoftError(err) {
			// Also a push the server refused between batches: it stays
			// pending, and what the batch stored stands.
			err = nil
		}
		if err == nil {
			a.announceStoredBodies(folder, res)
			a.notifyFresh(ctx, folder, remoteIDs, res.NewIDs, notify)
		}
		return err
	})
	a.noteSyncPoolOutcome(account.ID, err)
	return err
}

// finishIMAPFolder repairs mangled cached text and adopts server flag colors,
// then tells the UI about repairs. It is the SyncFolder tail for the initial sync
// and scroll backfill.
func (a *App) finishIMAPFolder(ctx context.Context, engine *psync.Engine, folder storage.Folder) error {
	if engine == nil {
		return nil
	}
	res, err := engine.CompleteFolder(ctx, folder)
	a.afterRepairs(folder, res.RepairedIDs)
	return err
}
