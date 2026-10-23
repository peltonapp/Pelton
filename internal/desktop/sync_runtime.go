package desktop

// The per-account IMAP runtime on the task scheduler: pooled sessions, the
// fast and normal lanes (runNow, runPooled), sync plans built from small
// tasks, and IMAP IDLE as the scheduler's push watcher.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
	"github.com/peltonapp/Pelton/internal/sync/pool"
)

// imapTask is one task on an account's IMAP scheduler.
type imapTask = syncsched.Task[mailClient]

// idleNewMailEvery is how often, at N=1 while the normal lane has work and
// IDLE is away, a short new-mail check runs between tasks.
var idleNewMailEvery = 30 * time.Second

// idleRecheckAfter is how long IDLE trusts its last Inbox check when it
// parks again. Older than that, it queues a new-mail check first, for mail
// that arrived while IDLE was away.
var idleRecheckAfter = 5 * time.Second

// pooledIMAP is a logged-in session owned by an account's session pool.
type pooledIMAP struct {
	mailClient
	untrack func()
	// idleSince is when the session last went back to the pool; zero for a
	// fresh one.
	idleSince time.Time
	// broken is set when the session goes back to the pool unfit for reuse.
	broken atomic.Bool
	// logoutWait bounds Close's LOGOUT; zero is logoutTimeout.
	logoutWait time.Duration
}

// logoutTimeout bounds the LOGOUT of a healthy session being closed. The
// pool counts a closing session toward the cap until Close returns, so a
// server that does not answer must not hold the slot.
const logoutTimeout = 2 * time.Second

// Close ends the session. A broken one is dropped without LOGOUT, which
// would only wait on a dead connection; a healthy one logs out, for at most
// logoutTimeout. Either way the socket is closed before Close returns.
func (p *pooledIMAP) Close() error {
	if !p.broken.Load() {
		done := make(chan struct{})
		go func() {
			_ = p.mailClient.Logout()
			close(done)
		}()
		wait := p.logoutWait
		if wait <= 0 {
			wait = logoutTimeout
		}
		timer := time.NewTimer(wait)
		select {
		case <-done:
		case <-timer.C:
		}
		timer.Stop()
	}
	// Closing the socket also ends a LOGOUT still waiting for its answer.
	p.untrack()
	return nil
}

// markBroken flags a pooled session that is put back unfit for reuse, so
// its Close skips the LOGOUT.
func markBroken(c mailClient) {
	if p, ok := c.(*pooledIMAP); ok {
		p.broken.Store(true)
	}
}

// imapDialer opens and logs in one session of the account. Credentials are
// read at each dial, so a new password applies to the next session. The
// connection is tracked, so account removal can close it.
func (a *App) imapDialer(accountID int64) pool.Dialer[mailClient] {
	return func(ctx context.Context) (mailClient, error) {
		account, err := a.store.GetAccount(a.ctx, accountID)
		if err != nil {
			return nil, err
		}
		cfg, err := a.resolveIMAP(*account)
		if err != nil {
			return nil, err
		}
		started := time.Now()
		client, err := a.connectIMAP(cfg)
		if err != nil {
			// A server limiting connections may refuse the socket itself.
			a.noteSyncPoolDial(accountID, err)
			return nil, throttleCounted{err}
		}
		untrack := a.trackIMAP(accountID, client)
		if err := ctx.Err(); err != nil {
			untrack()
			return nil, err
		}
		if err := client.Login(); err != nil {
			a.noteLoginResult(accountID, err)
			a.noteSyncPoolDial(accountID, err)
			untrack()
			return nil, throttleCounted{err}
		}
		a.noteLoginResult(accountID, nil)
		a.noteSyncPoolLogin(accountID)
		a.log.Debug("imap session opened", "account", accountID, "connect_ms", time.Since(started).Milliseconds())
		return &pooledIMAP{mailClient: client, untrack: untrack}, nil
	}
}

// throttleCounted is a dial or login error the throttle already counted
// where it happened. It comes back to whoever waited for the session (a
// list, a sync), which reports its outcome too; noteSyncPoolOutcome skips
// it there, so one refused connection counts once.
type throttleCounted struct{ error }

func (e throttleCounted) Unwrap() error { return e.error }

// imapSessionBroken reports whether a task error leaves the session unfit
// for the next task. A tagged NO or BAD is the server answering, and a
// changed UIDVALIDITY is found before any command on the stored ids, so the
// connection is fine; anything else may have left it mid-command.
func imapSessionBroken(err error) bool {
	if errors.Is(err, psync.ErrGenerationChanged) {
		return false
	}
	var status *imap.Error
	return !errors.As(err, &status)
}

// taskSessionKey carries a task's session, so withIMAPSession inside the
// task uses it instead of asking the pool for a second one.
type taskSessionKey struct{}

type taskSession struct {
	accountID int64
	client    mailClient
}

func withTaskSession(ctx context.Context, accountID int64, c mailClient) context.Context {
	return context.WithValue(ctx, taskSessionKey{}, taskSession{accountID, c})
}

func sessionOf(ctx context.Context, accountID int64) (mailClient, bool) {
	ts, ok := ctx.Value(taskSessionKey{}).(taskSession)
	if !ok || ts.accountID != accountID {
		return nil, false
	}
	return ts.client, true
}

// withIMAPSession runs fn on an IMAP session of the account. Inside a task
// it is the task's own session. Outside one it runs fn as a fast-lane task,
// so it never opens a connection past the pool.
func (a *App) withIMAPSession(ctx context.Context, account storage.Account, fn func(mailClient) error) error {
	if c, ok := sessionOf(ctx, account.ID); ok {
		return fn(c)
	}
	// Its callers report the outcome themselves, in a task or not.
	return a.runFast(ctx, account, syncsched.KindSource, 0, "", func(_ context.Context, c mailClient) error {
		return fn(c)
	}, false)
}

// poolTask is one normal-lane task for runPooled and enqueuePooled.
type poolTask struct {
	Kind syncsched.Kind
	// Class orders the task; ClassAuto is the kind's default.
	Class    syncsched.Class
	FolderID int64
	// Key dedups: one queued task per key. Use folderKey for per-folder work.
	Key string
	// Barrier holds every other task of the account while it is queued or
	// running (see syncsched.Task.Barrier); it keeps its class.
	Barrier bool
	// Run does one bounded unit on the task's session. Returning
	// syncsched.ErrAgain sends the task back to the tail for the next unit.
	Run func(ctx context.Context, c mailClient) error
}

// folderKey is the dedup key of per-folder work: one queued task per
// (folder, kind) on an account.
func folderKey(kind syncsched.Kind, folderID int64) string {
	return fmt.Sprintf("%s:%d", kind, folderID)
}

// runNow runs fn as a fast-lane task of the account and waits for it. kind
// must be a fast kind (syncsched.Kind.Fast). fn gets a logged-in session and
// must stay tiny: one message, one action. The task starts on a free session,
// else on IDLE's, else when a running task ends. If ctx ends first runNow
// returns ctx's error; a queued task still runs.
func (a *App) runNow(ctx context.Context, account storage.Account, kind syncsched.Kind, folderID int64, fn func(ctx context.Context, c mailClient) error) error {
	return a.runNowKey(ctx, account, kind, folderID, "", fn)
}

// runNowKey is runNow with a dedup key: a second request while one is queued
// waits for that one.
//
// The operation's outcome counts toward the throttle (noteOpOutcome), except
// for the kinds whose unit reports its own: an opened message's body and a
// "load older" page (unitReportsOutcome).
func (a *App) runNowKey(ctx context.Context, account storage.Account, kind syncsched.Kind, folderID int64, key string, fn func(ctx context.Context, c mailClient) error) error {
	return a.runFast(ctx, account, kind, folderID, key, fn, !unitReportsOutcome(kind))
}

// unitReportsOutcome reports a fast kind whose unit tells the throttle its
// outcome itself (a body fetch, a list page), so runNowKey does not count it
// twice.
func unitReportsOutcome(kind syncsched.Kind) bool {
	return kind == syncsched.KindOnDemandBody || kind == syncsched.KindLoadOlder
}

// runFast runs fn as a fast-lane task and waits for it; report counts its
// outcome toward the throttle.
func (a *App) runFast(ctx context.Context, account storage.Account, kind syncsched.Kind, folderID int64, key string, fn func(ctx context.Context, c mailClient) error, report bool) error {
	if !kind.Fast() {
		return fmt.Errorf("pelton: %s is not fast-lane work", kind)
	}
	err := a.awaitTask(ctx, account, imapTask{Kind: kind, FolderID: folderID, Key: key, Run: a.taskRun(account.ID, fn)})
	if report {
		a.noteOpOutcome(account.ID, err)
	}
	return err
}

// noteOpOutcome counts a pooled operation's result toward the throttle. A
// task that never ran (stopped, dropped from a full fast lane, the account
// going) or one its caller stopped waiting for says nothing of the server.
// A failed dial was counted where it happened (throttleCounted).
func (a *App) noteOpOutcome(accountID int64, err error) {
	switch {
	case errors.Is(err, syncsched.ErrStopped), errors.Is(err, syncsched.ErrDropped),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, errAccountSyncHeld):
		return
	}
	a.noteSyncPoolOutcome(accountID, err)
}

// noteUnitOutcome counts the result of the server operation one task unit
// ran. The unit calls it itself, inside its Run, right after the operation:
// a task that callers joined (an equal Key) runs each unit once, so its
// outcome counts once however many callers wait, and a task that never ran
// counts nothing. A unit that skipped its work (a stale task) did no server
// operation and must not call it. The exclusions are noteOpOutcome's.
func (a *App) noteUnitOutcome(accountID int64, err error) {
	a.noteOpOutcome(accountID, err)
}

// runPooled queues t in the normal lane and waits until it is done, with
// every ErrAgain unit. It reports no outcome: a unit that runs a server
// operation reports it with noteUnitOutcome.
func (a *App) runPooled(ctx context.Context, account storage.Account, t poolTask) error {
	if t.Kind.Fast() {
		return fmt.Errorf("pelton: %s is fast-lane work", t.Kind)
	}
	return a.awaitTask(ctx, account, a.poolTaskOf(account, t, nil))
}

// enqueuePooled queues t in the normal lane without waiting. done, when set,
// gets the final result, or why the task never ran.
func (a *App) enqueuePooled(ctx context.Context, account storage.Account, t poolTask, done func(error)) error {
	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		if done != nil {
			done(err)
		}
		return err
	}
	rt.sched.Enqueue(a.poolTaskOf(account, t, done))
	return nil
}

func (a *App) poolTaskOf(account storage.Account, t poolTask, done func(error)) imapTask {
	return imapTask{Kind: t.Kind, Class: t.Class, FolderID: t.FolderID, Key: t.Key, Barrier: t.Barrier, Run: a.taskRun(account.ID, t.Run), Done: done}
}

func (a *App) taskRun(accountID int64, fn func(ctx context.Context, c mailClient) error) func(context.Context, mailClient) error {
	return func(ctx context.Context, c mailClient) error {
		return fn(withTaskSession(ctx, accountID, c), c)
	}
}

func (a *App) awaitTask(ctx context.Context, account storage.Account, t imapTask) error {
	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	t.Done = func(err error) { done <- err }
	rt.sched.Enqueue(t)
	select {
	case err := <-done:
		if errors.Is(err, syncsched.ErrStopped) {
			if held, _ := a.syncBlock(account.ID); held {
				return errAccountSyncHeld
			}
			return context.Canceled
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// bodyWant is what an account's body generators should fetch beyond the
// sync window: ids a "load older" page or a raised limit admitted.
type bodyWant struct {
	extra  []string
	notify bool
	// skip holds ids a batch asked for and could not get (gone from the
	// server, say), so this run of the generator moves past them. The next
	// sync tries them again.
	skip map[string]struct{}
	// partial is a message being fetched in chunks, in memory only.
	partial *psync.PartialBody
}

func (rt *accountSync) addBodyWant(folderID int64, ids []string, notify bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.bodies == nil {
		rt.bodies = make(map[int64]*bodyWant)
	}
	w := rt.bodies[folderID]
	if w == nil {
		w = &bodyWant{}
		rt.bodies[folderID] = w
	}
	w.extra = mergeNewestIDs(w.extra, ids)
	w.notify = w.notify || notify
}

func (rt *accountSync) bodyWantOf(folderID int64) bodyWant {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	var want bodyWant
	if w := rt.bodies[folderID]; w != nil {
		want = bodyWant{extra: append([]string(nil), w.extra...), notify: w.notify, skip: maps.Clone(w.skip)}
	}
	// Messages that failed to read too often wait for a manual Sync.
	for _, id := range rt.unreadableLocked(folderID) {
		if want.skip == nil {
			want.skip = make(map[string]struct{})
		}
		want.skip[id] = struct{}{}
	}
	return want
}

func (rt *accountSync) skipBodies(folderID int64, ids []string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.bodies == nil {
		rt.bodies = make(map[int64]*bodyWant)
	}
	w := rt.bodies[folderID]
	if w == nil {
		w = &bodyWant{}
		rt.bodies[folderID] = w
	}
	if w.skip == nil {
		w.skip = make(map[string]struct{})
	}
	for _, id := range ids {
		w.skip[id] = struct{}{}
	}
}

// pruneBodyExtra drops filled from the folder's extra ids. It removes only
// those, so ids addBodyWant added meanwhile (another copy's unit ran in
// between) stay.
func (rt *accountSync) pruneBodyExtra(folderID int64, filled []string) {
	if len(filled) == 0 {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	w := rt.bodies[folderID]
	if w == nil {
		return
	}
	gone := make(map[string]struct{}, len(filled))
	for _, id := range filled {
		gone[id] = struct{}{}
	}
	w.extra = slices.DeleteFunc(w.extra, func(id string) bool {
		_, ok := gone[id]
		return ok
	})
}

func (rt *accountSync) partialBody(folderID int64) *psync.PartialBody {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if w := rt.bodies[folderID]; w != nil {
		return w.partial
	}
	return nil
}

// startPartialBody makes p the folder's message fetched in chunks, unless
// it has one already: a folder fetches one large message at a time.
func (rt *accountSync) startPartialBody(folderID int64, p *psync.PartialBody) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.bodies == nil {
		rt.bodies = make(map[int64]*bodyWant)
	}
	w := rt.bodies[folderID]
	if w == nil {
		w = &bodyWant{}
		rt.bodies[folderID] = w
	}
	if w.partial != nil {
		return false
	}
	w.partial = p
	return true
}

// endPartialBody forgets p as the folder's chunked message, if it still is.
func (rt *accountSync) endPartialBody(folderID int64, p *psync.PartialBody) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if w := rt.bodies[folderID]; w != nil && w.partial == p {
		w.partial = nil
	}
}

// bodyRunState is a folder's body generator run, as its repair pass sees
// it. work counts what may need the pass again: a run queued or joined, a
// unit that found bodies missing.
type bodyRunState struct {
	runs      int // runs queued or joined (bodyRun) not ended yet
	work      uint64
	finishing bool // a unit runs the repair pass now
	repaired  bool // the pass ended well with no work since
}

func (rt *accountSync) bodyRunLocked(folderID int64) *bodyRunState {
	if rt.bodyRuns == nil {
		rt.bodyRuns = make(map[int64]*bodyRunState)
	}
	st := rt.bodyRuns[folderID]
	if st == nil {
		st = &bodyRunState{}
		rt.bodyRuns[folderID] = st
	}
	return st
}

// bodyRun records a run of the folder's body generator queued or joined:
// its repair pass is due again. The returned func, called once as that
// generator ends, forgets the folder's width estimate, so the next run's
// first unit runs alone and tells it again; a later run started meanwhile,
// in this runtime or another of the account's, keeps it (endBodyRun). The
// folder's run state is dropped with its last run here, so it does not
// stay for every folder ever synced.
func (rt *accountSync) bodyRun(folderID int64) (ended func()) {
	rt.mu.Lock()
	st := rt.bodyRunLocked(folderID)
	st.runs++
	st.work++
	st.repaired = false
	var run uint64
	if rt.shared != nil {
		run = rt.shared.startBodyRun(folderID)
	}
	rt.mu.Unlock()
	return func() {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if rt.shared != nil {
			rt.shared.endBodyRun(folderID, run)
		}
		if st := rt.bodyRuns[folderID]; st != nil {
			if st.runs--; st.runs <= 0 && !st.finishing {
				delete(rt.bodyRuns, folderID)
			}
		}
	}
}

// moreBodyWork records a unit that found bodies missing: the repair pass
// is due again once they are in.
func (rt *accountSync) moreBodyWork(folderID int64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	st := rt.bodyRunLocked(folderID)
	st.work++
	st.repaired = false
}

// startFinish claims the folder's repair pass for one body copy. It
// refuses while another copy runs it, and once it ended well with no work
// since: a copy that comes back after the pass (its ErrAgain reopened the
// generator) and finds nothing missing ends without a second one. endFinish
// lets it go with the work count startFinish returned.
func (rt *accountSync) startFinish(folderID int64) (work uint64, ok bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	st := rt.bodyRunLocked(folderID)
	if st.finishing || st.repaired {
		return 0, false
	}
	st.finishing = true
	return st.work, true
}

func (rt *accountSync) endFinish(folderID int64, work uint64, ok bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	st := rt.bodyRunLocked(folderID)
	st.finishing = false
	st.repaired = ok && st.work == work
}

// bodyClaims are the account's claimed body ids, shared by its runtimes.
func (rt *accountSync) bodyClaims() *syncsched.Claims { return &rt.shared.bodyClaims }

// bodyWidth is the width of a folder's body generator: N, or fewer when
// the folder's last unit saw fewer batches missing.
func (rt *accountSync) bodyWidth(folderID int64) func(n int) int {
	if rt.shared == nil {
		return nil
	}
	return func(n int) int { return min(n, rt.shared.bodyBacklog(folderID)) }
}

func (rt *accountSync) dropBodyWant(folderID int64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.bodies, folderID)
}

// imapBodyBatch is one unit of a folder's body generator. It asks the store
// for the newest missing bodies inside the sync window, plus the ids a "load
// older" page admitted, claims one batch of them that no other running unit
// holds (the account's adaptive count and byte budget, see
// syncsched.BodyLimits), fetches it and returns syncsched.ErrAgain while more
// are missing. Several units of one folder run at once (the generator's
// width), so the claims keep them disjoint; when
// everything missing is claimed by others it returns syncsched.ErrSaturated.
// A message larger than the account's chunk size comes alone, one chunk per
// unit (imapBodyChunk), one such message per folder at a time. The folder's
// repair and colour pass runs once, in the unit that finds nothing missing.
func (a *App) imapBodyBatch(ctx context.Context, account storage.Account, folder storage.Folder) error {
	rt := a.accountSync(account.ID)
	if rt == nil {
		return errAccountSyncHeld
	}
	// The folder as stored now: a list may have reset its UIDVALIDITY since
	// the task was queued, and the fetch checks the ids against it.
	stored, err := a.store.GetFolder(ctx, folder.ID)
	if errors.Is(err, storage.ErrFolderNotFound) {
		rt.dropBodyWant(folder.ID)
		return nil
	}
	if err != nil {
		return err
	}
	folder = *stored
	want := rt.bodyWantOf(folder.ID)
	extra, err := a.store.RemoteIDsNeedingBody(ctx, folder.ID, want.extra)
	if err != nil {
		return err
	}
	rt.pruneBodyExtra(folder.ID, idsNotIn(want.extra, extra))
	floorUID, err := a.store.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		return err
	}
	window, err := a.store.RemoteIDsNeedingBodyNewest(ctx, folder.ID, floorUID, a.syncMessageLimit())
	if err != nil {
		return err
	}
	all := slices.DeleteFunc(mergeNewestIDs(window, extra), func(id string) bool {
		_, skip := want.skip[id]
		return skip
	})
	limits := rt.sched.BodyLimits()
	count, budget := limits.Get()
	rt.shared.setBodyBacklog(folder.ID, (len(all)+count-1)/count)
	if len(all) == 0 {
		// Nothing missing: one unit of the run runs the repair pass; another
		// one that found the same, beside it or after it, ends.
		work, ok := rt.startFinish(folder.ID)
		if !ok {
			return nil
		}
		// Deferred, so a pass that panics lets go too (and is due again).
		repaired := false
		defer func() { rt.endFinish(folder.ID, work, repaired) }()
		rt.dropBodyWant(folder.ID)
		if onBodiesFinished != nil {
			onBodiesFinished(folder.ID)
		}
		err := a.withIMAPSession(ctx, account, func(client mailClient) error {
			return a.finishIMAPFolder(ctx, a.newSyncEngine(pimap.NewAdapter(client), account.ID), folder)
		})
		repaired = err == nil
		return err
	}
	rt.moreBodyWork(folder.ID)
	claims := rt.bodyClaims()
	if p := rt.partialBody(folder.ID); p != nil {
		if !slices.Contains(all, p.RemoteID) {
			rt.endPartialBody(folder.ID, p)
		} else if tok, got := claims.Take(folder.ID, []string{p.RemoteID}, 1); len(got) == 1 {
			defer claims.Release(folder.ID, tok)
			return a.imapBodyChunk(ctx, account, folder, p, !want.notify)
		}
		// Else another unit is fetching its next chunk now.
	}
	chunk := rt.sched.ChunkBudget().Get()
	var tok uint64
	var batch []string
	var full bool
	// Units dispatched together plan the same newest ids; one whose claim
	// lost them all plans again from what is left, which is smaller each
	// time.
	for len(batch) == 0 {
		free := slices.DeleteFunc(slices.Clone(all), func(id string) bool { return claims.Claimed(folder.ID, id) })
		if len(free) == 0 {
			return syncsched.ErrSaturated
		}
		sizes, err := a.store.MessageSizesByRemoteID(ctx, folder.ID, free[:min(count, len(free))])
		if err != nil {
			return err
		}
		size := func(id string) int64 { return sizes[id] }
		big := func(id string) bool { return size(id) > chunk }
		if big(free[0]) {
			p := psync.NewPartialBody(free[0])
			if rt.startPartialBody(folder.ID, p) {
				tok, got := claims.Take(folder.ID, []string{p.RemoteID}, 1)
				defer claims.Release(folder.ID, tok)
				if len(got) == 0 { // another unit took it since
					rt.endPartialBody(folder.ID, p)
					return syncsched.ErrSaturated
				}
				return a.imapBodyChunk(ctx, account, folder, p, !want.notify)
			}
			// The folder has a large message coming in chunks already:
			// this unit takes the small ones.
			free = slices.DeleteFunc(free, big)
			if len(free) == 0 {
				return syncsched.ErrSaturated
			}
			if sizes, err = a.store.MessageSizesByRemoteID(ctx, folder.ID, free[:min(count, len(free))]); err != nil {
				return err
			}
		}
		var plan []string
		plan, full = syncsched.PlanBatchFull(free, size, count, budget)
		// A message over the chunk size waits to lead a later unit alone.
		if i := slices.IndexFunc(plan, big); i >= 0 {
			plan = plan[:i]
		}
		if len(plan) == 0 {
			return syncsched.ErrSaturated
		}
		tok, batch = claims.Take(folder.ID, plan, len(plan))
	}
	defer claims.Release(folder.ID, tok)
	started := time.Now()
	if err := a.execIMAPBodies(ctx, account, folder, batch, !want.notify); err != nil {
		if errors.Is(err, psync.ErrGenerationChanged) {
			// The stored ids may name other mail now: stop, and let the
			// folder's next list reset it.
			a.log.Warn("bodies: folder changed on the server", "folder", folder.Name)
			rt.dropBodyWant(folder.ID)
			return nil
		}
		return err
	}
	limits.Observe(time.Since(started), full)
	still, err := a.store.RemoteIDsNeedingBody(ctx, folder.ID, batch)
	if err != nil {
		return err
	}
	if len(still) > 0 {
		// Asked for and not stored, without an error: the server no longer
		// has them. Move past them for this run so they cannot stall the rest.
		a.log.Warn("bodies not returned by the server", "folder", folder.Name, "count", len(still))
		rt.skipBodies(folder.ID, still)
	}
	return syncsched.ErrAgain
}

// onBodiesFinished runs as a body unit starts the folder's repair pass.
// Tests set it.
var onBodiesFinished func(folderID int64)

// imapBodyChunk fetches the next chunk of p's message (BODY.PEEK[]<o.n>,
// the account's adaptive chunk size) and returns syncsched.ErrAgain, so the
// fast lane can run between chunks. The stub is filled when the last chunk
// is in. A message that does not parse, or that the server no longer has,
// is skipped for the rest of this run.
func (a *App) imapBodyChunk(ctx context.Context, account storage.Account, folder storage.Folder, p *psync.PartialBody, quiet bool) error {
	rt := a.accountSync(account.ID)
	if rt == nil {
		return errAccountSyncHeld
	}
	budget := rt.sched.ChunkBudget()
	started := time.Now()
	var done bool
	err := a.withIMAPSession(ctx, account, func(client mailClient) error {
		engine := a.newSyncEngine(pimap.NewAdapter(client), account.ID)
		if quiet {
			engine.OnProgress = nil
		}
		var res psync.FolderSyncResult
		var err error
		done, res, err = engine.FetchBodyChunk(ctx, folder, p, budget.Get())
		if done && err == nil {
			a.announceStoredBodies(folder, res)
			a.notifyFresh(ctx, folder, []string{p.RemoteID}, res.NewIDs)
		}
		return err
	})
	a.noteSyncPoolOutcome(account.ID, err)
	// A failed chunk keeps the parts: the retry goes on from the offset.
	if done {
		rt.endPartialBody(folder.ID, p)
	}
	var unreadable *psync.MessageError
	switch {
	case errors.Is(err, psync.ErrGenerationChanged):
		// FetchBodyChunk ended the fetch (done): the folder's stored ids may
		// name other mail now, so the generator stops and its next list
		// resets the folder.
		a.log.Warn("large message: folder changed on the server", "folder", folder.Name)
		rt.dropBodyWant(folder.ID)
		return nil
	case errors.As(err, &unreadable):
		a.log.Warn("large message could not be read", "folder", folder.Name, "err", err)
		rt.skipBodies(folder.ID, []string{p.RemoteID})
		rt.noteUnreadable(folder.ID, []string{p.RemoteID})
		return syncsched.ErrAgain
	case err != nil:
		return err
	}
	budget.Observe(time.Since(started), !done) // the last chunk may be short
	if done {
		still, err := a.store.RemoteIDsNeedingBody(ctx, folder.ID, []string{p.RemoteID})
		if err != nil {
			return err
		}
		if len(still) > 0 {
			rt.skipBodies(folder.ID, still)
		}
	}
	return syncsched.ErrAgain
}

// enqueueBodies starts (or joins) the folder's body generator in the normal
// lane. notify announces the bodies as new mail. done gets the result.
func (a *App) enqueueBodies(ctx context.Context, account storage.Account, folder storage.Folder, class syncsched.Class, notify bool, done func(error)) {
	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		if done != nil {
			done(err)
		}
		return
	}
	a.enqueueBodiesOn(rt, account, folder, class, notify, done)
}

// enqueueBodiesOn is enqueueBodies on rt. A task's Done uses it with the
// runtime the task ran on: it must not look the account's runtime up, which
// could build a new one for an account whose scheduler was just stopped. A
// stopped rt sends done ErrStopped.
func (a *App) enqueueBodiesOn(rt *accountSync, account storage.Account, folder storage.Folder, class syncsched.Class, notify bool, done func(error)) {
	if notify {
		rt.addBodyWant(folder.ID, nil, true)
	}
	ended := rt.bodyRun(folder.ID)
	rt.sched.Enqueue(bodyTask(folder, class, rt.bodyWidth(folder.ID), func(ctx context.Context, c mailClient) error {
		return a.imapBodyBatch(withTaskSession(ctx, account.ID, c), account, folder)
	}, func(err error) {
		ended()
		if done != nil {
			done(err)
		}
	}))
}

// taskEnded reports a Done result that means the task never finished its
// work: its scheduler stopped or dropped it. Its follow-ups are not queued.
func taskEnded(err error) bool {
	return errors.Is(err, syncsched.ErrStopped) || errors.Is(err, syncsched.ErrDropped)
}

// bodyTask is a folder's body generator, running as up to width units at
// once (nil: one; see syncsched.Task.Width). A failed batch is tried again
// at the tail, up to syncStepAttempts failure rounds in a row (a batch done
// resets the count); one that failed on the connection gives its session up
// (retryUnit), so the next task dials fresh. Copies that fail together (one
// dropped link fails every session at once) are one round: a failure
// counts only when its unit started after the last counted one.
func bodyTask(folder storage.Folder, class syncsched.Class, width func(n int) int, batch func(ctx context.Context, c mailClient) error, done func(error)) imapTask {
	var retries failureRounds
	return imapTask{
		Kind: syncsched.KindBodies, Class: class, FolderID: folder.ID,
		Key: folderKey(syncsched.KindBodies, folder.ID), Width: width,
		Run: func(ctx context.Context, c mailClient) error {
			round := retries.start()
			err := batch(ctx, c)
			switch {
			case errors.Is(err, syncsched.ErrSaturated):
				return err
			case err == nil, errors.Is(err, syncsched.ErrAgain):
				retries.succeeded()
				return err
			case ctx.Err() != nil:
				return err
			}
			if retries.failed(round) < syncStepAttempts {
				return retryUnit(err)
			}
			return err
		},
		Done: done,
	}
}

// failureRounds counts the failure rounds in a row of a generator whose
// units run concurrently. A round is the units started before a failure is
// counted: the first of them to fail counts, the others belong to it.
type failureRounds struct {
	mu    sync.Mutex
	round uint64 // bumped by every counted failure
	in    int    // failure rounds in a row
}

// start is the round a unit starting now belongs to.
func (f *failureRounds) start() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.round
}

// succeeded resets the count: a batch got through.
func (f *failureRounds) succeeded() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = 0
}

// failed records a failure of a unit started in round and returns the
// failure rounds in a row.
func (f *failureRounds) failed(round uint64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if round == f.round {
		f.in++
		f.round++
	}
	return f.in
}

// idsNotIn is the ids of all that are not in some, in all's order.
func idsNotIn(all, some []string) []string {
	if len(all) == 0 {
		return nil
	}
	in := make(map[string]struct{}, len(some))
	for _, id := range some {
		in[id] = struct{}{}
	}
	var out []string
	for _, id := range all {
		if _, ok := in[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}

// retryUnit sends a failed generator unit back to the tail. A tagged NO or
// BAD leaves the session fit for the next task; any other error may have
// left it mid-command, so it is closed (syncsched.ErrAgainBroken).
func retryUnit(err error) error {
	if imapSessionBroken(err) {
		return errors.Join(syncsched.ErrAgainBroken, err)
	}
	return syncsched.ErrAgain
}

// syncStep is one step of a sync plan: list a folder (KindListStubs) or
// fetch one body batch (KindBodies, may return syncsched.ErrAgain).
type syncStep func(ctx context.Context, c mailClient, folder storage.Folder, kind syncsched.Kind) error

// syncPlan is one sync campaign over some folders, as small tasks: each
// folder is listed, then its body generator runs. Inbox goes first, alone,
// until its list and first body batch (the newest window) are in; then
// every other folder starts at once (Sent queued first) while the rest of
// Inbox's bodies go on, so they share the workers and take turns batch by
// batch.
type syncPlan struct {
	// class orders the plan's tasks; ClassManualSync for a manual Sync.
	class syncsched.Class
	// step runs each task.
	step syncStep
	// onList runs as a folder's list task starts (progress), onFolderDone
	// once a folder is done, listed and filled or failed.
	onList       func(folder storage.Folder)
	onFolderDone func()
	// onListed runs once a folder's list has ended, listed or failed.
	onListed func()
	// onInboxDone runs once Inbox is listed and has its first body batch,
	// not after all of its bodies (with sync_message_limit=0 that is hours).
	// It also runs if Inbox fails, is missing or is not synced.
	onInboxDone func()
	// listed gets nil when every folder's list has ended and its bodies are
	// queued, or folderSyncError when any list failed after its retries.
	// The bodies may still run for hours. A stopped scheduler sends its
	// error.
	listed chan<- error
	// done gets nil when every folder is done, or folderSyncError when any
	// folder failed after its retries. A stopped scheduler sends its error.
	done chan<- error
	// bodyWidth, when set, is the width of a folder's body generator (see
	// accountSync.bodyWidth); nil runs one unit of a folder at a time.
	bodyWidth func(folderID int64) func(n int) int
	// bodyRun, when set, runs as the plan queues or joins a folder's body
	// generator, and what it returns as that generator ends (see
	// accountSync.bodyRun).
	bodyRun func(folderID int64) (ended func())
}

// enqueueSyncPlan queues plan p over folders on s. Inbox's first body batch
// releases onInboxDone and the other folders; listed waits for every
// folder's list, done for every folder's bodies too, Inbox's included. A
// folder's bodies join its body generator if one is queued or running (an
// earlier plan's): one generator per folder, however many of its units run
// at once (p.bodyWidth), and both plans' done wait for its end.
// A failed list waits out syncRetryBackoff and is queued again, up to
// syncStepAttempts; then that folder counts as failed. ctx ending stops the
// retries.
func enqueueSyncPlan(ctx context.Context, s *syncsched.Scheduler[mailClient], folders []storage.Folder, p syncPlan) {
	ordered := foldersInSyncOrder(folders)
	var reportOnce, listedOnce sync.Once
	reportListed := func(err error) {
		if p.listed != nil {
			listedOnce.Do(func() { p.listed <- err })
		}
	}
	report := func(err error) {
		if p.done != nil {
			reportOnce.Do(func() { p.done <- err })
		}
	}
	inboxDone := sync.OnceFunc(func() {
		if p.onInboxDone != nil {
			p.onInboxDone()
		}
	})
	var mu sync.Mutex
	var failed, listFailed []string
	var firstErr, firstListErr error
	remaining, listsLeft := len(ordered), len(ordered)
	listEnded := func(name string, err error) {
		mu.Lock()
		if err != nil {
			if firstListErr == nil {
				firstListErr = err
			}
			listFailed = append(listFailed, name)
		}
		listsLeft--
		last := listsLeft == 0
		if p.onListed != nil {
			p.onListed()
		}
		result := folderSyncError(listFailed, len(ordered), firstListErr)
		mu.Unlock()
		if last {
			reportListed(result)
		}
	}
	chainDone := func(name string, err error) {
		mu.Lock()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed = append(failed, name)
		}
		remaining--
		last := remaining == 0
		if p.onFolderDone != nil {
			p.onFolderDone()
		}
		result := folderSyncError(failed, len(ordered), firstErr)
		mu.Unlock()
		if last {
			report(result)
		}
	}
	stopped := func(err error) bool {
		if ctx.Err() != nil || errors.Is(err, syncsched.ErrStopped) || errors.Is(err, context.Canceled) {
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			reportListed(err)
			report(err)
			return true
		}
		return false
	}

	// startChain lists f, then runs its body generator. then runs once,
	// after the first body batch that succeeds (more may follow), when the
	// bodies join a generator already queued or running (its batches run
	// another plan's Run, so waiting for ours would hold then until it
	// ends), or when the chain ends without one (failed list, failed
	// bodies).
	startChain := func(f storage.Folder, then func()) {
		if then == nil {
			then = func() {}
		}
		then = sync.OnceFunc(then)
		finish := func(err error) {
			chainDone(f.Name, err)
			then()
		}
		bodies := func() {
			var width func(int) int
			if p.bodyWidth != nil {
				width = p.bodyWidth(f.ID)
			}
			ended := func() {}
			if p.bodyRun != nil {
				ended = p.bodyRun(f.ID)
			}
			joined := s.EnqueueJoined(bodyTask(f, p.class, width, func(ctx context.Context, c mailClient) error {
				err := p.step(ctx, c, f, syncsched.KindBodies)
				// A saturated unit did no batch: others of the folder run.
				batched := err == nil || errors.Is(err, syncsched.ErrAgain) && !errors.Is(err, syncsched.ErrSaturated)
				if ctx.Err() == nil && batched {
					then()
				}
				return err
			}, func(err error) {
				ended()
				if stopped(err) {
					return
				}
				finish(err)
			}))
			if joined {
				then()
			}
		}
		var list func(attempt int)
		list = func(attempt int) {
			s.Enqueue(imapTask{
				Kind: syncsched.KindListStubs, Class: p.class, FolderID: f.ID,
				Key: folderKey(syncsched.KindListStubs, f.ID),
				Run: func(ctx context.Context, c mailClient) error {
					if p.onList != nil {
						p.onList(f)
					}
					return p.step(ctx, c, f, syncsched.KindListStubs)
				},
				Done: func(err error) {
					switch {
					case stopped(err):
					case err == nil:
						bodies()
						listEnded(f.Name, nil)
					case attempt+1 < syncStepAttempts:
						scheduleSyncRetry(ctx, attempt, func() { list(attempt + 1) })
					default:
						listEnded(f.Name, err)
						finish(err)
					}
				},
			})
		}
		list(0)
	}

	if len(ordered) == 0 {
		inboxDone()
		reportListed(nil)
		report(nil)
		return
	}
	startRest := func(from int) {
		inboxDone()
		for _, f := range ordered[from:] {
			startChain(f, nil)
		}
	}
	if folderRole(ordered[0]) == roleInbox {
		startChain(ordered[0], func() { startRest(1) })
		return
	}
	startRest(0)
}

// imapPlanStep is the IMAP step of a sync plan: a paged list and the body
// generator. notify announces the bodies as new mail.
func (a *App) imapPlanStep(account storage.Account, notify bool) syncStep {
	pagers := &listPagers{}
	return func(ctx context.Context, c mailClient, folder storage.Folder, kind syncsched.Kind) error {
		ctx = withTaskSession(ctx, account.ID, c)
		if kind == syncsched.KindBodies {
			return a.imapBodyBatch(ctx, account, folder)
		}
		_, more, err := a.imapListPage(ctx, account, folder, pagers.of(a, account.ID, folder.ID))
		if more {
			return syncsched.ErrAgain
		}
		pagers.drop(folder.ID)
		if rt := a.accountSync(account.ID); rt != nil && notify {
			rt.addBodyWant(folder.ID, nil, true)
		}
		return err
	}
}

// listPagers are the in-memory list pagers of one sync plan, one per folder.
type listPagers struct {
	mu       sync.Mutex
	byFolder map[int64]*psync.StubPager
}

// of returns the folder's pager, sized by the account's tunables.
func (l *listPagers) of(a *App, accountID, folderID int64) *psync.StubPager {
	l.mu.Lock()
	defer l.mu.Unlock()
	if p := l.byFolder[folderID]; p != nil {
		return p
	}
	p := a.stubPager(accountID)
	if l.byFolder == nil {
		l.byFolder = make(map[int64]*psync.StubPager)
	}
	l.byFolder[folderID] = p
	return p
}

func (l *listPagers) drop(folderID int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byFolder, folderID)
}

// stubPager is a new paged list, sized by the account's tunables.
func (a *App) stubPager(accountID int64) *psync.StubPager {
	p := &psync.StubPager{First: syncsched.FirstListPage, Page: syncsched.ListPage}
	if rt := a.accountSync(accountID); rt != nil {
		t := rt.sched.Tunables()
		p.First, p.Page = t.FirstListPage, t.ListPage
	}
	return p
}

// imapListPage lists one page of a folder's stubs on the task's session
// (psync.SyncFolderStubsPage) and reports whether more follow. Deletes,
// flags and the cursor wait for the last page. Like listFolderOnce, it
// leaves the folder to a list already running, and one asked for while it
// runs makes it list once more.
func (a *App) imapListPage(ctx context.Context, account storage.Account, folder storage.Folder, pager *psync.StubPager) ([]string, bool, error) {
	return a.listStubPage(ctx, account, folder, pager, listOpts{})
}

// listOpts tunes listStubPage.
type listOpts struct {
	// paged lists in full from the first page on even when a delta is
	// possible: a delta runs in one call however much it holds.
	paged bool
	// older lists the window a "load older" or a raised limit just widened
	// (paged too, since a delta would not see it). It draws no progress
	// line, and its stubs are old mail, except those above the highest UID
	// the folder had seen when the listing began (see olderList.lastSeen):
	// mail that arrived meanwhile, or a list folded into it, is new.
	older *olderList
}

// listStubPage is imapListPage with opts.
func (a *App) listStubPage(ctx context.Context, account storage.Account, folder storage.Folder, pager *psync.StubPager, opts listOpts) ([]string, bool, error) {
	older := opts.older != nil
	rt := a.accountSync(account.ID)
	fl := a.accountShared(account.ID)
	var tok uint64
	if fl != nil {
		if older {
			// An older listing moves the window, which it may only do while
			// it holds the folder, so it waits for a list already running,
			// as a full check does.
			var err error
			if tok, err = fl.awaitFolderList(ctx, folder.ID); err != nil {
				return nil, false, err
			}
		} else if tok = fl.claimFolderList(folder.ID); tok == 0 {
			return nil, false, nil
		}
		defer fl.dropFolderList(folder.ID, tok) // a panic; a no-op once let go
	}
	var ids []string
	var more bool
	// What the page lists that was not a stub before it and lies above the
	// highest UID the folder had seen is fresh mail.
	since, err := a.freshMark(ctx, folder, opts.older)
	if err != nil {
		if fl != nil {
			fl.dropFolderList(folder.ID, tok)
		}
		return nil, false, err
	}
	err = a.withIMAPSession(ctx, account, func(client mailClient) error {
		if older && opts.older.fast {
			// "Load older"'s fast-lane unit: the page just below the floor,
			// bounded whatever the folder holds. The rest of the batch and
			// the reconcile are left to the normal lane.
			var err error
			ids, err = a.imapOlderPage(ctx, client, account, folder, opts.older)
			more = err == nil
			if rt != nil {
				rt.addFresh(folder.ID, since.fresh(ids))
			}
			return err
		}
		if older && !opts.older.lowered {
			if err := a.lowerIMAPFloor(ctx, client, folder, opts.older.batch); err != nil {
				return err
			}
			opts.older.lowered = true
		}
		engine := a.newSyncEngine(pimap.NewAdapter(client), account.ID)
		if older {
			engine.OnProgress = nil
		}
		engine.ForceFull = (older || opts.paged) && pager.Pages() == 0
		res, m, err := engine.SyncFolderStubsPage(ctx, folder, pager)
		err = softListError(a, folder, err)
		ids, more = res.ToFetch, m
		if rt != nil {
			rt.addFresh(folder.ID, since.fresh(res.ToFetch))
		}
		// Bodies will not run for this attempt. Repair and color still
		// happen, matching SyncFolder after a list failure.
		if err != nil && ctx.Err() == nil {
			if ferr := a.finishIMAPFolder(ctx, engine, folder); ferr != nil {
				a.log.Error("finish folder after a failed list", "folder", folder.Name, "err", ferr)
			}
		}
		return err
	})
	a.noteSyncPoolOutcome(account.ID, err)
	if fl != nil {
		switch {
		case ctx.Err() != nil:
			fl.dropFolderList(folder.ID, tok)
		case fl.releaseFolderList(folder.ID, tok):
			fl.dropFolderList(folder.ID, tok)
			more = more || err == nil
		}
	}
	return ids, more, err
}

// freshMark is what tells a list's fresh mail: the highest UID the folder
// had seen and its incomplete stubs. A list reads them as it starts. An
// older listing reads them once, at its first unit, before it moves the
// window, and keeps them for all its units.
type freshMark struct {
	lastSeen uint32
	before   map[string]struct{}
	// older marks an older listing: what it lists is old mail but for what
	// lies above lastSeen.
	older bool
}

func (a *App) freshMark(ctx context.Context, folder storage.Folder, older *olderList) (freshMark, error) {
	if older != nil && older.started {
		return freshMark{lastSeen: older.lastSeen, before: older.before, older: true}, nil
	}
	seen, err := a.store.FolderLastSeenUID(ctx, folder.ID)
	if err != nil {
		return freshMark{}, err
	}
	before, err := a.incompleteStubs(ctx, folder)
	if err != nil {
		return freshMark{}, err
	}
	if older != nil {
		older.started, older.lastSeen, older.before = true, seen, before
	}
	return freshMark{lastSeen: seen, before: before, older: older != nil}, nil
}

// fresh is the ids of toFetch that are new mail. Mail that arrives gets a
// UID above every one the folder had, so an id at or below lastSeen is old
// mail a list (re)admitted: a lowered window, say. A folder never listed
// (lastSeen 0) only has the stubs to go by, unless the list is an older
// listing, which then has no new mail.
func (m freshMark) fresh(toFetch []string) []string {
	if m.lastSeen == 0 && !m.older {
		return freshIDs(toFetch, m.before)
	}
	return freshSince(toFetch, m.lastSeen, m.before)
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

// softListError logs a soft error of a list (psync.IsSoftError: a refused
// flag or delete push stays pending, an unreadable message stays a stub) and
// returns nil for it: the list's result stands. Any other error is returned.
func softListError(a *App, folder storage.Folder, err error) error {
	if psync.IsSoftError(err) {
		a.log.Warn("list folder", "folder", folder.Name, "err", err)
		return nil
	}
	return err
}

// imapOlderStep is one unit of an olderList on the task's session. The
// first lowers the folder's window floor by the batch, under the folder's
// list claim; each lists one page of the stubs that admitted.
func (a *App) imapOlderStep(ctx context.Context, account storage.Account, folder storage.Folder, older *olderList) ([]string, bool, error) {
	return a.listStubPage(ctx, account, folder, older.pager, listOpts{older: older})
}

// imapOlderPage is "load older"'s fast-lane unit (olderList.fast): it
// stores the first page (older.pager.First) of the messages just below the
// folder's window floor and lowers the floor over them
// (psync.Engine.SyncOlderPage), with a bounded number of UID FETCHes and no
// full list. It takes them off older's batch and leaves the rest, in full
// pages, to the normal lane, whose last unit reconciles. When the page
// cannot run (a changed UIDVALIDITY, say), it stores nothing and the normal
// lane does it all.
func (a *App) imapOlderPage(ctx context.Context, client mailClient, account storage.Account, folder storage.Folder, older *olderList) ([]string, error) {
	older.fast = false
	engine := a.newSyncEngine(pimap.NewAdapter(client), account.ID)
	engine.OnProgress = nil
	res, err := engine.SyncOlderPage(ctx, folder, older.pager.First)
	if errors.Is(err, psync.ErrNeedFullList) {
		return nil, nil
	}
	if err != nil {
		return res.ToFetch, err
	}
	older.pager.First = 0
	if older.batch > 0 {
		older.batch -= res.Admitted
		// The page held the whole batch: the normal lane only reconciles.
		older.lowered = older.batch <= 0
	}
	return res.ToFetch, nil
}

// lowerIMAPFloor moves the folder's window floor batch messages further
// down (0: to the bottom, so the whole folder is in the window). It finds
// the new floor in the folder's UIDs (UID SEARCH ALL); the list that
// follows stores what that admitted. A folder whose UIDVALIDITY changed is
// left alone: its next list resets it anyway.
//
// It drops the folder's cursor with the floor. Until the listing has
// stored all it admitted, the window covers mail that has no row, and a
// delta from the old cursor would never list it: a listing that fails, or
// the app quitting, would leave it missing until a full check. Without a
// cursor the folder's next list is a full one, which stores it; the
// listing's own last unit lists in full and sets the cursor again.
func (a *App) lowerIMAPFloor(ctx context.Context, c mailClient, folder storage.Folder, batch int) error {
	fresh, err := a.store.GetFolder(ctx, folder.ID)
	if err != nil {
		return err
	}
	floorUID, err := a.store.FolderSyncFloorUID(ctx, folder.ID)
	if err != nil {
		return err
	}
	if fresh.SyncFloorID == "" && floorUID == 0 {
		return nil // nothing below the window
	}
	var target uint32
	if batch > 0 {
		if floorUID == 0 {
			id, err := strconv.ParseUint(fresh.SyncFloorID, 10, 32)
			if err != nil {
				return fmt.Errorf("pelton: folder %q has no numeric window floor %q", folder.Name, fresh.SyncFloorID)
			}
			floorUID = uint32(id)
		}
		box, err := c.Select(folder.IMAPPath)
		if err != nil {
			return err
		}
		if fresh.UIDValidity != 0 && box.UIDValidity != fresh.UIDValidity {
			return nil
		}
		uids, err := c.SearchAllUIDs()
		if err != nil {
			return err
		}
		if len(uids) == 0 && box.NumMessages > 0 {
			// A server quirk, not an empty folder: taken at its word it would
			// open the whole folder.
			return fmt.Errorf("pelton: UID SEARCH listed nothing in %q, which holds %d messages", folder.Name, box.NumMessages)
		}
		below := make([]uint32, 0, len(uids))
		for _, u := range uids {
			if uint32(u) < floorUID {
				below = append(below, uint32(u))
			}
		}
		slices.Sort(below)
		slices.Reverse(below)
		if len(below) > batch {
			target = below[batch-1]
		}
	}
	if err := a.store.SetFolderStateToken(ctx, folder.ID, ""); err != nil {
		return err
	}
	if target == 0 {
		if err := a.store.SetFolderSyncWindow(ctx, folder.ID, "", true); err != nil {
			return err
		}
		return a.store.SetFolderSyncFloorUID(ctx, folder.ID, 0)
	}
	if err := a.store.SetFolderSyncWindow(ctx, folder.ID, strconv.FormatUint(uint64(target), 10), true); err != nil {
		return err
	}
	return a.store.SetFolderSyncFloorUID(ctx, folder.ID, target)
}

// imapPush is IMAP IDLE as the account's push watcher. It parks on a pooled
// session (one of N), queues a new-mail task on every change, and re-checks
// Inbox before parking when its last check is stale.
func (a *App) imapPush(account storage.Account) syncsched.Push[mailClient] {
	w := &imapWatch{}
	return syncsched.Push[mailClient]{
		Park: func(ctx context.Context, c mailClient, wake <-chan struct{}) (bool, error) {
			return a.parkIMAPIdle(ctx, account, c, wake, w)
		},
		NewMail: func() imapTask {
			return a.newMailTask(account, w)
		},
		CheckEvery: idleNewMailEvery,
		RetryWait:  idleRetryWaitFor,
	}
}

// imapWatch is what IDLE remembers between parks: when Inbox was last
// checked, whether a stale check it reported is still waiting to end, and
// Inbox's folder id as last looked up, so a new-mail task is built without
// a store query.
type imapWatch struct {
	mu        sync.Mutex
	lastCheck time.Time
	pending   bool
	inboxID   int64
}

func (w *imapWatch) checked() {
	w.mu.Lock()
	w.lastCheck = time.Now()
	w.mu.Unlock()
}

// claimStaleCheck reports whether IDLE should report a change at once for
// a stale check, and marks that check pending. While it is pending (its
// task queued or running) IDLE waits instead: the scheduler wakes it when
// the task can run, so a task held back by a folder rule is not reported
// again on every park.
func (w *imapWatch) claimStaleCheck() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending || time.Since(w.lastCheck) < idleRecheckAfter {
		return false
	}
	w.pending = true
	return true
}

// checkEnded clears the pending stale check once a new-mail task ended,
// ran or not, so a dropped task does not turn the check off for good.
func (w *imapWatch) checkEnded(error) {
	w.mu.Lock()
	w.pending = false
	w.mu.Unlock()
}

func (w *imapWatch) setInbox(id int64) {
	w.mu.Lock()
	w.inboxID = id
	w.mu.Unlock()
}

func (w *imapWatch) inbox() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.inboxID
}

// parkIMAPIdle selects Inbox and waits in IDLE until the server reports a
// change or the scheduler wakes it. A stale last check reports a change at
// once, so Inbox is checked for mail that came while IDLE was away; only
// once until that check's task ends.
func (a *App) parkIMAPIdle(ctx context.Context, account storage.Account, c mailClient, wake <-chan struct{}, w *imapWatch) (bool, error) {
	if !c.SupportsIdle() {
		return false, syncsched.ErrIdleUnsupported
	}
	inbox, err := a.findInboxFolder(account.ID)
	if err != nil {
		return false, fmt.Errorf("look up inbox folder: %w", err)
	}
	w.setInbox(inbox.ID)
	// Only an Inbox with a cursor gets the check: there it is a cheap delta.
	// Without one it would fetch every flag; IDLE events and timed sync cover it.
	if !inbox.SyncExcluded && inbox.StateToken != "" && w.claimStaleCheck() {
		return true, nil
	}
	if _, err := c.Select(inbox.IMAPPath); err != nil {
		return false, fmt.Errorf("select inbox for idle: %w", err)
	}
	idleCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	woken := make(chan struct{})
	go func() {
		select {
		case <-wake:
			close(woken)
			cancel()
		case <-idleCtx.Done():
		}
	}()
	got, err := c.IdleUntil(idleCtx)
	cancel()
	select {
	case <-woken:
		// The wake cut IDLE short, so its error is ours; mail the server
		// announced before it still counts.
		return got, nil
	default:
	}
	return got, err
}

// newMailTask is the fast-lane check after an IDLE push. With a cursor it
// lists what is new (a delta, or the first page when the server needs a
// full list); the rest of a full list and the bodies go to the normal lane.
// Without a cursor nothing is listed here: a paged list is queued instead.
// It is built on the scheduler's behalf, so it reads nothing: Inbox is
// looked up when the task runs.
func (a *App) newMailTask(account storage.Account, w *imapWatch) imapTask {
	inboxID := w.inbox()
	key := "new-mail"
	if inboxID != 0 {
		key = folderKey(syncsched.KindNewMail, inboxID)
	}
	return imapTask{
		Kind: syncsched.KindNewMail, FolderID: inboxID, Key: key,
		Done: w.checkEnded,
		Run: func(ctx context.Context, c mailClient) error {
			defer w.checked()
			inbox, err := a.findInboxFolder(account.ID)
			if err != nil || inbox.SyncExcluded {
				return nil
			}
			w.setInbox(inbox.ID)
			folder := *inbox
			pager := a.stubPager(account.ID)
			if folder.StateToken == "" {
				a.enqueueListPages(ctx, account, folder, pager, false, uidMark{})
				return nil
			}
			ctx = withTaskSession(ctx, account.ID, c)
			burst, mark, err := a.newMailBurst(ctx, c, folder, pager.First)
			if err != nil {
				return err
			}
			if burst {
				// More than a page arrived: a delta would store it all in this
				// fast task. The normal lane lists it a page per unit.
				a.enqueueListPages(ctx, account, folder, pager, true, mark)
				return nil
			}
			_, more, err := a.imapListPage(ctx, account, folder, pager)
			switch {
			case err != nil:
				if ctx.Err() == nil {
					a.log.Error("new mail check", "account", account.Email, "err", err)
				}
				return err
			case more:
				a.enqueueListPages(ctx, account, folder, pager, false, mark)
			default:
				if rt := a.accountSync(account.ID); rt != nil {
					rt.noteListedNext(folder.ID, mark)
				}
				a.enqueueBodies(ctx, account, folder, syncsched.ClassAuto, true, nil)
			}
			return nil
		},
	}
}

// uidMark is a folder's UIDNEXT as a SELECT reported it, under its
// UIDVALIDITY.
type uidMark struct {
	validity, next uint32
}

// noteListedNext records m as the UIDNEXT a clean list of the folder saw:
// everything below it was listed. A mark under another UIDVALIDITY replaces
// the old one; under the same one it only rises.
func (rt *accountSync) noteListedNext(folderID int64, m uidMark) {
	if m.next == 0 {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.listedNext == nil {
		rt.listedNext = make(map[int64]uidMark)
	}
	if old, ok := rt.listedNext[folderID]; ok && old.validity == m.validity && old.next >= m.next {
		return
	}
	rt.listedNext[folderID] = m
}

// hasListedNext reports whether a clean list recorded the folder's UIDNEXT.
func (rt *accountSync) hasListedNext(folderID int64) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	_, ok := rt.listedNext[folderID]
	return ok
}

// listedNextOf is the UIDNEXT the folder's last clean list saw under
// validity, 0 when none is known.
func (rt *accountSync) listedNextOf(folderID int64, validity uint32) uint32 {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if m, ok := rt.listedNext[folderID]; ok && m.validity == validity {
		return m.next
	}
	return 0
}

// newMailBurst reports whether more than page messages may have arrived in
// folder since its last list: UIDNEXT has moved more than page past the
// UIDNEXT of the last clean new-mail list, or, before one, past the highest
// UID seen. It returns the mark it read, for the list to record once clean.
// The highest UID seen drops when the newest mail is deleted, so alone it
// would read every check after such a delete as a burst; UIDNEXT never
// drops. Expunged UIDs make it overcount, never undercount.
func (a *App) newMailBurst(ctx context.Context, c mailClient, folder storage.Folder, page int) (bool, uidMark, error) {
	if page <= 0 {
		return false, uidMark{}, nil
	}
	seen, err := a.store.FolderLastSeenUID(ctx, folder.ID)
	if err != nil {
		return false, uidMark{}, err
	}
	rt := a.accountSync(folder.AccountID)
	if seen == 0 && (rt == nil || !rt.hasListedNext(folder.ID)) {
		// Nothing to compare with, so no SELECT for it.
		return false, uidMark{}, nil
	}
	box, err := c.Select(folder.IMAPPath)
	if err != nil {
		return false, uidMark{}, err
	}
	mark := uidMark{validity: box.UIDValidity, next: uint32(box.UIDNext)}
	base := uint32(0)
	if seen > 0 {
		base = seen + 1
	}
	if rt != nil {
		base = max(base, rt.listedNextOf(folder.ID, mark.validity))
	}
	if base == 0 {
		return false, mark, nil
	}
	return mark.next > base+uint32(page), mark, nil
}

// enqueueListPages lists the rest of a folder in the normal lane, a page per
// unit on pager, then starts its body generator. paged lists in full from
// the first page on (listOpts.paged). A clean end records mark, the UIDNEXT
// read before the list (see newMailBurst); a zero mark records nothing.
func (a *App) enqueueListPages(ctx context.Context, account storage.Account, folder storage.Folder, pager *psync.StubPager, paged bool, mark uidMark) {
	rt, err := a.ensureAccountSync(ctx, account.ID)
	if err != nil {
		return
	}
	rt.sched.Enqueue(imapTask{
		Kind: syncsched.KindListStubs, FolderID: folder.ID,
		Key: folderKey(syncsched.KindListStubs, folder.ID),
		Run: func(ctx context.Context, c mailClient) error {
			_, more, err := a.listStubPage(withTaskSession(ctx, account.ID, c), account, folder, pager, listOpts{paged: paged})
			if err == nil && more {
				return syncsched.ErrAgain
			}
			return err
		},
		Done: func(err error) {
			if err == nil {
				rt.noteListedNext(folder.ID, mark)
				a.enqueueBodiesOn(rt, account, folder, syncsched.ClassAuto, true, nil)
			}
		},
	})
}
