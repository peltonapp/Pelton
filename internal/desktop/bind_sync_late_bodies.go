package desktop

import (
	"context"
	"errors"
	"sync"

	"github.com/peltonapp/Pelton/internal/storage"
)

// lateBodies is one account's watch over the bodies its timed and manual
// passes left running (runSyncPlan returns after the lists). It lives in
// accountState, so it outlives a scheduler.
//
// One goroutine per account waits, and only for the newest pass. Each
// folder has one body generator, and a later pass's bodies join it, so the
// newest pass's end covers what the older ones still wait for: their end
// adds nothing, and handling each of them repeated one generator failure
// once per pass (log, throttle step, recorded failure, closing event), with
// one goroutine per 30 s tick parked until the backlog drained.
type lateBodies struct {
	mu sync.Mutex
	// outcomes counts the sync outcomes recorded for the account. A late
	// outcome is recorded only while no outcome newer than its pass's own
	// was: a body failure from an old pass must not hide a newer pass's OK.
	// The store writes are made under mu, so they land in count order.
	outcomes uint64
	// pass is the newest pass whose bodies are awaited; waiting says the
	// account's waiter runs.
	pass    *latePass
	waiting bool
	// newer wakes the waiter when pass was replaced.
	newer chan struct{}
}

// latePass is a pass whose bodies go on after it returned.
type latePass struct {
	ctx     context.Context
	account storage.Account
	// listErr set means the pass recorded a failure already.
	listErr error
	done    <-chan error
	// since is the account's outcome count as the pass handed its bodies
	// over, before its own outcome was recorded.
	since uint64
	// own says whether the pass's own outcome was counted; nil for a pass
	// run outside syncAccountCtx, whose outcome nothing counts.
	own *passOutcome
}

// passOutcome is whether a pass's own sync outcome was counted in
// lateBodies.outcomes: a cancel or a removal is not. It is guarded by the
// account's lateBodies.mu, so a late body outcome reads it with the count.
type passOutcome struct{ counted bool }

type passOutcomeKey struct{}

// withPassOutcome gives the pass run under the returned context a record of
// whether its own outcome was counted (notePassOutcome).
func withPassOutcome(ctx context.Context) context.Context {
	return context.WithValue(ctx, passOutcomeKey{}, &passOutcome{})
}

func passOutcomeOf(ctx context.Context) *passOutcome {
	own, _ := ctx.Value(passOutcomeKey{}).(*passOutcome)
	return own
}

// onLateBodiesHandled runs once the account's waiter handled its newest
// pass's bodies (or gave up on a cancelled context). Tests set it.
var onLateBodiesHandled func(accountID int64)

// watchLateBodies hands the bodies of a pass that returned after its lists
// to the account's waiter. A pass already waited for is replaced: it is
// older, and its bodies' end is covered by this one's.
func (a *App) watchLateBodies(p *latePass) {
	l := &a.accountStateFor(p.account.ID).late
	p.own = passOutcomeOf(p.ctx)
	l.mu.Lock()
	p.since = l.outcomes
	l.pass = p
	start := !l.waiting
	l.waiting = true
	if l.newer == nil {
		l.newer = make(chan struct{}, 1)
	}
	l.mu.Unlock()
	if start {
		goSafe("finishing a sync's downloads", func() { a.awaitLateBodies(l) })
		return
	}
	select {
	case l.newer <- struct{}{}:
	default: // a wake is pending already
	}
}

// awaitLateBodies waits for the newest pass's bodies and handles their end
// once.
func (a *App) awaitLateBodies(l *lateBodies) {
	for {
		l.mu.Lock()
		p := l.pass
		l.mu.Unlock()
		var err error
		ended := true
		select {
		case err = <-p.done:
		case <-l.newer:
			continue
		case <-p.ctx.Done():
			ended = false
		}
		l.mu.Lock()
		if l.pass != p {
			// Replaced as it ended: the newer pass's end covers it.
			l.mu.Unlock()
			continue
		}
		l.pass, l.waiting = nil, false
		l.mu.Unlock()
		if ended {
			a.finishLateBodies(p, err)
		}
		if onLateBodiesHandled != nil {
			onLateBodiesHandled(p.account.ID)
		}
		return
	}
}

// finishLateBodies handles the end (err) of the bodies pass p left running:
// it closes the account's progress line if they reopened it and no run is
// going, and records a failure as the account's sync failure, as when the
// pass waited for its bodies, unless a newer outcome was recorded since. A
// removed account (its sync held) gets neither.
func (a *App) finishLateBodies(p *latePass, err error) {
	id := p.account.ID
	if held, _ := a.syncBlock(id); held {
		return
	}
	a.closeIdleProgress(id, p.account.Email)
	if err == nil || p.listErr != nil {
		return
	}
	err = a.stoppedErr(id, err)
	if errors.Is(err, context.Canceled) || errors.Is(err, errAccountSyncHeld) || p.ctx.Err() != nil {
		return
	}
	if !a.noteLateSyncOutcome(id, p.since, p.own, err) {
		a.log.Info("sync bodies failed after a newer sync outcome", "account", p.account.Email, "err", err)
		return
	}
	a.log.Error("sync bodies", "account", p.account.Email, "err", err)
	a.emitAccountSyncStates()
}

// noteLateSyncOutcome is noteSyncOutcome for a pass's late body result:
// it records err only if no outcome but the pass's own (when own says it
// was counted) was recorded since the pass handed its bodies over (since),
// and reports whether it did. A pass whose own outcome was not counted gets
// no slack: an outcome after since is then a newer pass's.
func (a *App) noteLateSyncOutcome(accountID int64, since uint64, own *passOutcome, err error) bool {
	l := &a.accountStateFor(accountID).late
	l.mu.Lock()
	allowed := since
	if own != nil && own.counted {
		allowed++
	}
	if l.outcomes > allowed {
		l.mu.Unlock()
		return false
	}
	l.outcomes++
	a.recordSyncOutcome(accountID, err)
	l.mu.Unlock()
	a.noteSyncPoolOutcome(accountID, err)
	return true
}
