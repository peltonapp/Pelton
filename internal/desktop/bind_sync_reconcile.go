package desktop

import (
	"context"
	"errors"
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
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	var due []storage.Folder
	for _, f := range foldersInSyncOrder(folders) {
		if f.LastFullSyncAt.IsZero() || f.LastFullSyncAt.Before(cutoff) {
			due = append(due, f)
		}
	}
	return due
}

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

// enqueueFullReconcile queues one lowest-priority full reconcile per folder.
// A folder that already has one queued or running is skipped. A reconcile
// that yields to live work is requeued by the scheduler and keeps its claim.
func (a *App) enqueueFullReconcile(ctx context.Context, account storage.Account, folders []storage.Folder) {
	if len(folders) == 0 {
		return
	}
	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		return
	}
	for _, f := range folders {
		folder := f
		if !rt.claimReconcile(folder.ID) {
			continue
		}
		rt.sched.Enqueue(syncsched.Job{
			Priority: syncsched.PriorityBackgroundReconcile,
			Kind:     syncsched.JobFullReconcile,
			FolderID: folder.ID,
			Run: func(jobCtx context.Context) error {
				a.emitVerifyProgress(account, rt, folder.Name)
				err := a.execFullReconcile(jobCtx, account, folder)
				if errors.Is(err, psync.ErrSoftPaused) && jobCtx.Err() == nil {
					return err
				}
				if rt.releaseReconcile(folder.ID) {
					a.emitVerifyProgress(account, rt, "")
				}
				if err != nil && jobCtx.Err() == nil {
					a.log.Error("full reconcile", "account", account.Email, "folder", folder.Name, "err", err)
					a.noteSyncOutcome(account.ID, err)
				}
				return nil
			},
		})
	}
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

// claimReconcileBodies marks a folder as having a reconcile body job queued
// or running. It returns false when one already is.
func (rt *accountSync) claimReconcileBodies(folderID int64) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.reconcileBodies.claim(folderID)
}

// releaseReconcileBodies ends a folder's reconcile body job claim.
func (rt *accountSync) releaseReconcileBodies(folderID int64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.reconcileBodies.drop(folderID)
}

// dropReconciles forgets every reconcile and reconcile body claim once the
// scheduler that held their jobs has stopped. It reports whether a reconcile
// was among them.
func (rt *accountSync) dropReconciles() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.reconcileBodies.clear()
	had := rt.reconciling.clear()
	rt.verifyDone, rt.verifyTotal = 0, 0
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

// reconcilePause makes a full reconcile give way to live work (opening a
// message, a push follow-up, manual Sync) when the account has one sync slot.
// It does not follow the soft-pause flag: IMAP IDLE sets that at N=1 and is
// not a scheduler job, so yielding to it would restart the reconcile every
// time IDLE took the session back.
func (a *App) reconcilePause(accountID int64) func() bool {
	rt := a.accountSync(accountID)
	if rt == nil || rt.sched == nil {
		return nil
	}
	return func() bool {
		if rt.pool != nil && rt.pool.Effective() >= 2 {
			return false
		}
		return rt.sched.LiveBusy()
	}
}

// execFullReconcile re-lists one folder in full. The bodies that found, up to
// the body limit, are left to a background body job, so a live job does not
// wait for them. A folder that was deleted or excluded meanwhile is skipped.
func (a *App) execFullReconcile(ctx context.Context, account storage.Account, folder storage.Folder) error {
	if a.fullReconcileForTest != nil {
		return a.fullReconcileForTest(ctx, account, folder)
	}
	fresh, err := a.store.GetFolder(ctx, folder.ID)
	if errors.Is(err, storage.ErrFolderNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if fresh.SyncExcluded {
		return nil
	}
	folder = *fresh
	var res psync.StubSyncResult
	err = a.protocolFor(account).withListEngine(ctx, account, func(e *psync.Engine) error {
		var err error
		res, err = a.reconcileList(ctx, e, account.ID, folder)
		return err
	})
	if err != nil {
		return err
	}
	a.enqueueReconcileBodies(ctx, account, folder, newestIDs(res.ToFetch, a.syncMessageLimit()))
	return nil
}

// reconcileList runs the forced full list of a reconcile on engine. It holds
// the folder like listFolderOnce, so no delta stores its flags and cursor
// while the full list runs: it waits for a list already running, and a list
// asked for meanwhile runs after it as a delta, so the newer state is stored
// last. The ids a follow-up finds join the result.
func (a *App) reconcileList(ctx context.Context, engine *psync.Engine, accountID int64, folder storage.Folder) (psync.StubSyncResult, error) {
	// The check has its own calm "checking folders" line. The engine's folder
	// progress would open the normal running-sync line beside it, which only a
	// sync's own close ends.
	engine.OnProgress = nil
	engine.PauseCheck = a.reconcilePause(accountID)
	engine.ForceFull = true
	rt := a.accountSync(accountID)
	if rt == nil {
		return engine.SyncFolderStubs(ctx, folder, 0)
	}
	if err := rt.awaitFolderList(ctx, folder.ID); err != nil {
		return psync.StubSyncResult{}, err
	}
	res, err := engine.SyncFolderStubs(ctx, folder, 0)
	engine.ForceFull = false
	followErr := err
	for {
		if ctx.Err() != nil || errors.Is(followErr, psync.ErrSoftPaused) {
			rt.dropFolderList(folder.ID)
			if err == nil {
				err = followErr
			}
			return res, err
		}
		if !rt.releaseFolderList(folder.ID) {
			return res, err
		}
		var more psync.StubSyncResult
		more, followErr = engine.SyncFolderStubs(ctx, folder, 0)
		if followErr != nil && !errors.Is(followErr, psync.ErrSoftPaused) && ctx.Err() == nil {
			a.log.Warn("list after full reconcile", "folder", folder.Name, "err", followErr)
		}
		res.ToFetch = mergeNewestIDs(more.ToFetch, res.ToFetch)
	}
}

// enqueueReconcileBodies queues the bodies a full reconcile found as an
// ordinary background body job. Like every body job it soft-pauses for live
// work and resumes with the ids it had not started. A folder that already has
// one queued or running is skipped: that job fills the folder's missing bodies
// up to the same limit, and the next check finds whatever it left.
func (a *App) enqueueReconcileBodies(ctx context.Context, account storage.Account, folder storage.Folder, ids []string) {
	if len(ids) == 0 {
		return
	}
	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		return
	}
	if !rt.claimReconcileBodies(folder.ID) {
		return
	}
	rt.sched.Enqueue(syncsched.Job{
		Priority:  syncsched.PriorityBackgroundBody,
		Kind:      syncsched.JobFetchBodies,
		FolderID:  folder.ID,
		RemoteIDs: ids,
		Run: func(jobCtx context.Context) error {
			err := a.protocolFor(account).reconcileBodies(jobCtx, account, folder)
			if errors.Is(err, psync.ErrSoftPaused) && jobCtx.Err() == nil {
				return err
			}
			rt.releaseReconcileBodies(folder.ID)
			if err != nil && jobCtx.Err() == nil {
				a.log.Error("full reconcile bodies", "account", account.Email, "folder", folder.Name, "err", err)
			}
			return nil
		},
	})
}

// newestIDs caps a newest-first id list at limit; limit <= 0 keeps all.
func newestIDs(ids []string, limit int) []string {
	if limit > 0 && len(ids) > limit {
		return ids[:limit]
	}
	return ids
}

// enqueueReconcileAfterManual queues a full reconcile of every selected folder
// after a user-initiated Sync's delta pass succeeded. The Sync button does not
// wait for it.
func (a *App) enqueueReconcileAfterManual(ctx context.Context, account storage.Account) {
	if folders, ferr := a.store.ListFolders(ctx, account.ID); ferr == nil {
		a.enqueueFullReconcile(ctx, account, foldersInSyncOrder(folders))
	}
}

// dueReconcileInterval is how often runDueReconcileLoop looks for due folder
// checks. Tests shorten it.
var dueReconcileInterval = time.Hour

// runDueReconcileLoop queues the due full folder checks of every running
// account each dueReconcileInterval until the app shuts down. Timed auto-sync
// queues them after each pass, but it does not run at all when switched off,
// so without this an account would only be checked at startup or on manual
// Sync. Like auto-sync
// it does nothing in low-power mode.
func (a *App) runDueReconcileLoop() {
	ticker := time.NewTicker(dueReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			// Low-power mode stops timed auto-sync, and with it its checks.
			if a.lowPowerMode() {
				continue
			}
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
	ctx := a.sessionCtx()
	for _, account := range accounts {
		if account.Local || !a.hasAccountWorker(account.ID) {
			continue
		}
		if held, _ := a.syncBlock(account.ID); held {
			continue
		}
		a.enqueueDueReconcile(ctx, account)
	}
}
