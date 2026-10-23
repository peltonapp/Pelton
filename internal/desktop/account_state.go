package desktop

import (
	"errors"
	"sync"
)

// accountState is the per-account sync state that must outlive one scheduler.
// The accountSync runtime is deleted and recreated whenever the account's
// scheduler stops and starts; account removal has to abort this account's
// sessions, keep its sync from starting again and hold its lock across that
// teardown, so these live in their own map. Only account removal drops an
// entry.
type accountState struct {
	// lock serializes one account's mailbox mutations.
	// It replaces the old process-wide syncMu: another account never waits on
	// it.
	lock sync.Mutex

	// mu guards closers and holds.
	mu sync.Mutex
	// holds counts holdAccountSync callers. While it is above zero no
	// scheduler can be started for the account, so no sync job can run.
	holds int
	// closers are the account's open IMAP sessions. Abort closes them so a
	// blocked FETCH returns.
	closers []*closerToken

	// tally is the account's progress counts. One tally per account keeps two
	// mailboxes syncing at once from mixing their numbers.
	tally syncTally

	// progressMu orders the account's progress events and guards
	// lastProgress, the latest running event the heartbeat re-sends. A closing
	// event clears it.
	progressMu   sync.Mutex
	lastProgress *SyncProgressEvent

	// late watches the bodies the account's passes left running and counts
	// its recorded sync outcomes.
	late lateBodies
}

// accountStateFor returns the state for an account, creating it on first use.
// A removed account gets a throwaway state that is never stored, so a task
// that outlived the removal cannot bring the account's entry back.
func (a *App) accountStateFor(accountID int64) *accountState {
	a.accountStatesMu.Lock()
	defer a.accountStatesMu.Unlock()
	if _, gone := a.removedAccounts[accountID]; gone {
		return &accountState{}
	}
	if a.accountStates == nil {
		a.accountStates = make(map[int64]*accountState)
	}
	st := a.accountStates[accountID]
	if st == nil {
		st = &accountState{}
		a.accountStates[accountID] = st
	}
	return st
}

// errAccountSyncHeld is what a sync entry point gets for an account that is
// being removed or was removed. UI paths treat it as a quiet no-op.
var errAccountSyncHeld = errors.New("pelton: mailbox is being removed or was removed")

// holdAccountSync keeps every caller of ensureAccountSync from starting a
// scheduler for the account until the returned release runs, so no sync job
// of it can start meanwhile. It does not stop what is already running: the
// caller stops the account's worker and scheduler after taking the hold.
// Release is safe to call more than once.
func (a *App) holdAccountSync(accountID int64) (release func()) {
	st := a.accountStateFor(accountID)
	st.mu.Lock()
	st.holds++
	st.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			st.mu.Lock()
			st.holds--
			st.mu.Unlock()
		})
	}
}

// syncBlock reports whether the account is held by holdAccountSync, and
// whether it was removed. It never creates a state entry.
func (a *App) syncBlock(accountID int64) (held, removed bool) {
	a.accountStatesMu.Lock()
	defer a.accountStatesMu.Unlock()
	if _, ok := a.removedAccounts[accountID]; ok {
		return true, true
	}
	st := a.accountStates[accountID]
	if st == nil {
		return false, false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.holds > 0, false
}

// accountRemoved reports whether the account was removed in this run.
func (a *App) accountRemoved(accountID int64) bool {
	a.accountStatesMu.Lock()
	defer a.accountStatesMu.Unlock()
	_, gone := a.removedAccounts[accountID]
	return gone
}

// forgetAccountState drops a removed account's state and remembers the id, so
// a caller that looked the account up before it was removed cannot start sync
// work for it afterwards. Account ids are never reused.
func (a *App) forgetAccountState(accountID int64) {
	a.accountStatesMu.Lock()
	defer a.accountStatesMu.Unlock()
	delete(a.accountStates, accountID)
	if a.removedAccounts == nil {
		a.removedAccounts = make(map[int64]struct{})
	}
	a.removedAccounts[accountID] = struct{}{}
}

// accountLock is the mutex one account's mailbox mutations hold. Different
// accounts get different mutexes.
func (a *App) accountLock(accountID int64) *sync.Mutex {
	return &a.accountStateFor(accountID).lock
}

// lockMutations takes the account's lock and returns its unlock. A pooled
// task takes it inside its fn, never around runNow, so a caller waiting for
// a session never holds it.
func (a *App) lockMutations(accountID int64) func() {
	mu := a.accountLock(accountID)
	mu.Lock()
	return mu.Unlock
}

// accountTally is the progress tally for one account's sync runs.
func (a *App) accountTally(accountID int64) *syncTally {
	return &a.accountStateFor(accountID).tally
}

// abortAccountSync closes the account's open IMAP sessions, so a blocked
// command returns. Other accounts are untouched. It is the hard stop for
// account removal only.
func (a *App) abortAccountSync(accountID int64) {
	st := a.accountStateFor(accountID)
	st.mu.Lock()
	closers := st.closers
	st.closers = nil
	st.mu.Unlock()
	for _, c := range closers {
		c.Close()
	}
}

// trackIMAP registers an open IMAP session under its account so that
// account's abort can close it. The returned func unregisters and closes it.
func (a *App) trackIMAP(accountID int64, c mailClient) func() {
	st := a.accountStateFor(accountID)
	tok := &closerToken{fn: func() { _ = c.Close() }}
	st.mu.Lock()
	st.closers = append(st.closers, tok)
	st.mu.Unlock()
	return func() {
		st.mu.Lock()
		next := make([]*closerToken, 0, len(st.closers))
		for _, t := range st.closers {
			if t != tok {
				next = append(next, t)
			}
		}
		st.closers = next
		st.mu.Unlock()
		tok.Close()
	}
}
