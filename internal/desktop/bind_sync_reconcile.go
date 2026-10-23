package desktop

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// fullReconcileDays reads sync_full_reconcile_days, clamped.
func (a *App) fullReconcileDays() int {
	return clampFullReconcileDays(a.intSetting(settingSyncFullReconcileDays, defaultSyncFullReconcileDays))
}

func clampFullReconcileDays(n int) int {
	return min(max(n, 0), maxSyncFullReconcileDays)
}

// foldersDueForReconcile returns the selected folders, in sync order, whose
// last full reconcile is older than days or never happened. days <= 0 means
// only manual Sync reconciles, so nothing is due.
func foldersDueForReconcile(folders []storage.Folder, days int, now time.Time) []storage.Folder {
	if days <= 0 {
		return nil
	}
	return foldersCheckedBefore(folders, now.Add(-time.Duration(days)*24*time.Hour), now)
}

// foldersCheckedBefore returns the selected folders, in sync order, last
// checked before cutoff or never. A check stamped in the future (the clock
// was ahead) counts as due, or the folder would never be checked again.
func foldersCheckedBefore(folders []storage.Folder, cutoff, now time.Time) []storage.Folder {
	var due []storage.Folder
	for _, f := range foldersInSyncOrder(folders) {
		at := f.LastFullSyncAt
		if at.IsZero() || at.Before(cutoff) || at.After(now) {
			due = append(due, f)
		}
	}
	return due
}

// manualCheckAge is how old a folder's last full check must be for a manual
// Sync to queue another: a full list of a big folder is not worth repeating
// on every press.
const manualCheckAge = 24 * time.Hour

// enqueueDueReconcile queues a background full reconcile for every folder of
// the account that is due. The startup sync calls it once its own pass is
// done, and each successful timed auto-sync of the account after it.
func (a *App) enqueueDueReconcile(ctx context.Context, account storage.Account) {
	folders, err := a.store.ListFolders(ctx, account.ID)
	if err != nil {
		a.log.Error("list folders for full reconcile", "account", account.Email, "err", err)
		return
	}
	a.enqueueFullReconcile(ctx, account, foldersDueForReconcile(folders, a.fullReconcileDays(), time.Now()))
}

// enqueueFullReconcile queues a full folder check of each folder, a
// normal-lane task of the lowest class. A folder that already has one queued
// or running is skipped, and so is one whose last check failed recently
// (checkBackoff). Low-power mode queues none.
//
// The account's checks run one after another, never side by side: a check
// that fails on the connection or the login drops the rest, so a bad
// password or a server that is down costs one failed login, not one per
// folder.
func (a *App) enqueueFullReconcile(ctx context.Context, account storage.Account, folders []storage.Folder) {
	if len(folders) == 0 || a.lowPowerMode() {
		return
	}
	st := a.accountStateFor(account.ID)
	if st.checksPaused(time.Now()) {
		return
	}
	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		return
	}
	now := time.Now()
	for _, f := range folders {
		if st.folderCheckBackedOff(f.ID, now) || !rt.claimReconcile(f.ID) {
			continue
		}
		rt.mu.Lock()
		rt.checkQueue = append(rt.checkQueue, f)
		rt.mu.Unlock()
	}
	a.startNextCheck(account, rt)
}

// startNextCheck queues the account's next waiting folder check, unless one
// is queued or running.
func (a *App) startNextCheck(account storage.Account, rt *accountSync) {
	rt.mu.Lock()
	if rt.checkRunning || len(rt.checkQueue) == 0 {
		rt.mu.Unlock()
		return
	}
	folder := rt.checkQueue[0]
	rt.checkQueue = rt.checkQueue[1:]
	rt.checkRunning = true
	rt.mu.Unlock()
	st := a.accountStateFor(account.ID)
	check := &fullCheck{pager: a.stubPager(account.ID)}
	// lowPower is set by a unit that found low-power mode on. That unit
	// returns nil, not errChecksLowPower: its session did nothing wrong, and
	// any other error would close it as broken. Done reads it; Run and Done
	// never run at once.
	lowPower := false
	rt.sched.Enqueue(imapTask{
		Kind: syncsched.KindFullCheck, FolderID: folder.ID,
		Key: folderKey(syncsched.KindFullCheck, folder.ID),
		Run: func(ctx context.Context, c mailClient) error {
			if a.lowPowerMode() {
				// Turned on after the check was queued: list nothing.
				lowPower = true
				return nil
			}
			a.emitVerifyProgress(account, rt, folder.Name)
			more, err := a.fullCheckUnit(withTaskSession(ctx, account.ID, c), account, folder, check)
			if more {
				return syncsched.ErrAgain
			}
			if err != nil && ctx.Err() == nil {
				// A failed check is not a failed sync: the account keeps its
				// state, and only the throttle hears of it.
				a.log.Error("full reconcile", "account", account.Email, "folder", folder.Name, "err", err)
				a.noteSyncPoolOutcome(account.ID, err)
			}
			return err
		},
		Done: func(err error) {
			if lowPower && err == nil {
				err = errChecksLowPower
			}
			var rest []storage.Folder
			now := time.Now()
			switch {
			case err == nil:
				st.checkSucceeded(folder.ID)
			case errors.Is(err, syncsched.ErrStopped), errors.Is(err, context.Canceled):
			case errors.Is(err, errChecksLowPower):
				rest = rt.takeCheckQueue()
			case checkStopsAccount(err):
				// Every other folder would fail the same way, each with a login.
				st.accountChecksFailed(now)
				rest = rt.takeCheckQueue()
			default:
				st.folderCheckFailed(folder.ID, now)
			}
			last := rt.releaseReconcile(folder.ID)
			for _, f := range rest {
				last = rt.releaseReconcile(f.ID)
			}
			rt.mu.Lock()
			rt.checkRunning = false
			rt.mu.Unlock()
			if last {
				a.emitVerifyProgress(account, rt, "")
			}
			a.startNextCheck(account, rt)
		},
	})
}

// errChecksLowPower ends a queued folder check that low-power mode, turned
// on meanwhile, keeps from running, and the rest with it.
var errChecksLowPower = errors.New("pelton: low power mode is on")

// checkStopsAccount reports a check failure that every other folder of the
// account would hit too: no session (the server is unreachable, refuses the
// login, or there is no password).
func checkStopsAccount(err error) bool {
	return isNetworkError(err) || loginRefused(err) || errors.Is(err, errNoCredentials) || isSyncThrottleError(err)
}

// takeCheckQueue empties the account's waiting checks and returns them.
func (rt *accountSync) takeCheckQueue() []storage.Folder {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rest := rt.checkQueue
	rt.checkQueue = nil
	return rest
}

// checkBackoffBase is the wait after a folder's or an account's first failed
// check; it doubles with each failure in a row, up to checkBackoffMax.
const (
	checkBackoffBase = time.Hour
	checkBackoffMax  = 24 * time.Hour
)

// checkBackoff is how long to leave a check alone after fails failures in a
// row.
func checkBackoff(fails int) time.Duration {
	d := checkBackoffBase
	for i := 1; i < fails && d < checkBackoffMax; i++ {
		d *= 2
	}
	return min(d, checkBackoffMax)
}

// checkFailure is a run of failed checks and when the next may run.
type checkFailure struct {
	fails int
	until time.Time
}

// folderCheckBackedOff reports whether the folder's last check failed and
// its backoff has not run out.
func (st *accountState) folderCheckBackedOff(folderID int64, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	f, ok := st.checkFails[folderID]
	return ok && now.Before(f.until)
}

// checksPaused reports whether the account's checks are backed off after a
// connection or login failure.
func (st *accountState) checksPaused(now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return now.Before(st.checksDown.until)
}

func (st *accountState) folderCheckFailed(folderID int64, now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.checkFails == nil {
		st.checkFails = make(map[int64]checkFailure)
	}
	f := st.checkFails[folderID]
	f.fails++
	f.until = now.Add(checkBackoff(f.fails))
	st.checkFails[folderID] = f
}

func (st *accountState) accountChecksFailed(now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.checksDown.fails++
	st.checksDown.until = now.Add(checkBackoff(st.checksDown.fails))
}

func (st *accountState) checkSucceeded(folderID int64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.checkFails, folderID)
	st.checksDown = checkFailure{}
}

// claimReconcile marks a folder as having a full reconcile in flight. It
// returns false when one already is.
func (rt *accountSync) claimReconcile(folderID int64) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.reconciling.claim(folderID) {
		return false
	}
	rt.verifyTotal++
	return true
}

// releaseReconcile ends a folder's full reconcile claim. It reports whether
// that was the last one in flight, so exactly one finishing job closes the
// "checking folders" line even when two finish at once.
func (rt *accountSync) releaseReconcile(folderID int64) (last bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.reconciling.held(folderID) {
		return false
	}
	rt.reconciling.drop(folderID)
	rt.verifyDone++
	if rt.reconciling.len() == 0 {
		rt.verifyDone, rt.verifyTotal = 0, 0
		return true
	}
	return false
}

// dropReconciles forgets every reconcile claim and waiting check once the
// scheduler that held their tasks has stopped. It reports whether a
// reconcile was among them.
func (rt *accountSync) dropReconciles() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	had := rt.reconciling.clear()
	rt.verifyDone, rt.verifyTotal = 0, 0
	rt.checkQueue = nil
	return had
}

// emitVerifyProgress shows the "checking folders" line. An empty folder name
// after the last reconcile closes the line.
func (a *App) emitVerifyProgress(account storage.Account, rt *accountSync, folderName string) {
	rt.mu.Lock()
	done, total := rt.verifyDone, rt.verifyTotal
	rt.mu.Unlock()
	if folderName == "" && total != 0 {
		return
	}
	a.emitSyncProgress(account.ID, account.Email, "", syncCounts{
		Folder: folderName, FoldersDone: done, FoldersTotal: total, Phase: SyncPhaseVerify,
	})
}

// fullCheck is one folder's full check across its units: the paged list,
// whether a list was asked for while a page held the folder, and what the
// folder looked like when the check started, which tells new mail the check
// itself lists from what it heals (see freshSince).
type fullCheck struct {
	pager    *psync.StubPager
	followUp bool
	started  bool
	lastSeen uint32
	before   map[string]struct{}
}

// freshSince is the ids of toFetch above lastSeen, the highest UID the
// folder had seen, that were not incomplete stubs already (before): mail
// that arrived since the last list. A folder never listed (lastSeen 0) has
// no new mail, only a first list; ids that are not UIDs are not judged.
func freshSince(toFetch []string, lastSeen uint32, before map[string]struct{}) []string {
	if lastSeen == 0 {
		return nil
	}
	var out []string
	for _, id := range toFetch {
		uid, err := strconv.ParseUint(id, 10, 32)
		if err != nil || uint32(uid) <= lastSeen {
			continue
		}
		if _, old := before[id]; !old {
			out = append(out, id)
		}
	}
	return out
}

// execFullReconcile re-lists one folder in full, in one call. The task runs
// it a page per unit (fullCheckUnit).
func (a *App) execFullReconcile(ctx context.Context, account storage.Account, folder storage.Folder) error {
	_, err := a.fullCheckUnit(ctx, account, folder, &fullCheck{pager: &psync.StubPager{}})
	return err
}

// fullCheckUnit lists one page of a folder's full check and reports whether
// more is left. The bodies the check found are left to the folder's body
// generator in the normal lane. A folder that was deleted or excluded
// meanwhile is skipped.
func (a *App) fullCheckUnit(ctx context.Context, account storage.Account, folder storage.Folder, c *fullCheck) (bool, error) {
	if a.fullReconcileForTest != nil {
		return false, a.fullReconcileForTest(ctx, account, folder)
	}
	fresh, err := a.store.GetFolder(ctx, folder.ID)
	if errors.Is(err, storage.ErrFolderNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fresh.SyncExcluded {
		return false, nil
	}
	folder = *fresh
	if !c.started {
		c.started = true
		if c.lastSeen, err = a.store.FolderLastSeenUID(ctx, folder.ID); err != nil {
			return false, err
		}
		if c.before, err = a.incompleteStubs(ctx, folder); err != nil {
			return false, err
		}
	}
	var (
		res  psync.StubSyncResult
		more bool
	)
	err = a.protocolFor(account).withListEngine(ctx, account, func(e *psync.Engine) error {
		var err error
		res, more, err = a.reconcileList(ctx, e, account.ID, folder, c)
		return err
	})
	if rt := a.accountSync(account.ID); rt != nil {
		rt.addFresh(folder.ID, freshSince(res.ToFetch, c.lastSeen, c.before))
	}
	if err != nil {
		return false, err
	}
	if !more && len(res.ToFetch) > 0 {
		a.enqueueBodies(ctx, account, folder, syncsched.ClassAuto, false, nil)
	}
	return more, nil
}

// reconcileList runs one page of a reconcile's forced full list on engine.
// It holds the folder like listFolderOnce, so no delta stores its flags and
// cursor while the full list runs: it waits for a list already running, and
// a list asked for meanwhile runs after the last page as a delta, so the
// newer state is stored last. The ids a follow-up finds join the result.
func (a *App) reconcileList(ctx context.Context, engine *psync.Engine, accountID int64, folder storage.Folder, c *fullCheck) (psync.StubSyncResult, bool, error) {
	// The check has its own calm "checking folders" line. The engine's folder
	// progress would open the normal running-sync line beside it, which only a
	// sync's own close ends.
	engine.OnProgress = nil
	engine.ForceFull = true
	fl := a.accountShared(accountID)
	if fl == nil {
		res, more, err := engine.SyncFolderStubsPage(ctx, folder, c.pager)
		return res, more, softListError(a, folder, err)
	}
	tok, err := fl.awaitFolderList(ctx, folder.ID)
	if err != nil {
		return psync.StubSyncResult{}, false, err
	}
	defer fl.dropFolderList(folder.ID, tok) // a panic; a no-op once let go
	res, more, err := engine.SyncFolderStubsPage(ctx, folder, c.pager)
	err = softListError(a, folder, err)
	engine.ForceFull = false
	if more && err == nil {
		if fl.releaseFolderList(folder.ID, tok) {
			fl.dropFolderList(folder.ID, tok)
			c.followUp = true
		}
		return res, true, nil
	}
	followErr := err
	for again := c.followUp; ; again = false {
		if ctx.Err() != nil {
			fl.dropFolderList(folder.ID, tok)
			if err == nil {
				err = followErr
			}
			return res, false, err
		}
		if !again && !fl.releaseFolderList(folder.ID, tok) {
			return res, false, err
		}
		// A follow-up is a delta someone asked for meanwhile: its new mail
		// notifies like any delta's, unlike what the full list healed.
		before, berr := a.incompleteStubs(ctx, folder)
		var more psync.StubSyncResult
		more, followErr = engine.SyncFolderStubs(ctx, folder, 0)
		followErr = softListError(a, folder, followErr)
		if followErr != nil && ctx.Err() == nil {
			a.log.Warn("list after full reconcile", "folder", folder.Name, "err", followErr)
		}
		if rt := a.accountSync(accountID); berr == nil && rt != nil {
			rt.addFresh(folder.ID, freshIDs(more.ToFetch, before))
		}
		res.ToFetch = mergeNewestIDs(more.ToFetch, res.ToFetch)
	}
}

// enqueueReconcileAfterManual queues a full reconcile of every selected folder
// not checked in manualCheckAge, after a user-initiated Sync's delta pass
// succeeded. The Sync button does not wait for it.
func (a *App) enqueueReconcileAfterManual(ctx context.Context, account storage.Account) {
	if folders, ferr := a.store.ListFolders(ctx, account.ID); ferr == nil {
		now := time.Now()
		a.enqueueFullReconcile(ctx, account, foldersCheckedBefore(folders, now.Add(-manualCheckAge), now))
	}
}

// dueReconcileInterval is how often runDueReconcileLoop looks for due folder
// checks. Tests shorten it.
var dueReconcileInterval = time.Hour

// runDueReconcileLoop queues the due full folder checks of every running
// account that last synced fine each dueReconcileInterval until the app
// shuts down. Timed auto-sync
// queues them after each pass, but it does not run at all when switched off,
// so without this an account would only be checked at startup or on manual
// Sync. Like every check, it queues nothing in low-power mode.
func (a *App) runDueReconcileLoop() {
	ticker := time.NewTicker(dueReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.enqueueDueReconcileRunning()
		}
	}
}

// enqueueDueReconcileRunning queues the due folder checks of every account of
// the active profile that has a running worker and is not held for removal.
// A folder already queued is skipped by enqueueFullReconcile.
func (a *App) enqueueDueReconcileRunning() {
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		a.log.Error("list accounts for full reconcile", "err", err)
		return
	}
	failing := make(map[int64]bool)
	if states, err := a.store.AccountSyncStates(a.ctx); err == nil {
		for _, s := range states {
			failing[s.AccountID] = s.Failing()
		}
	}
	ctx := a.sessionCtx()
	for _, account := range accounts {
		if account.Local || !a.hasAccountWorker(account.ID) {
			continue
		}
		if held, _ := a.syncBlock(account.ID); held {
			continue
		}
		// An account that cannot sync now would only fail its checks, each
		// with a login the server may count against it.
		if failing[account.ID] || a.loginRejected(account.ID) || a.noPassword(account) {
			continue
		}
		a.enqueueDueReconcile(ctx, account)
	}
}
