package desktop

import (
	"github.com/peltonapp/Pelton/internal/storage"
)

// AccountSyncStateDTO is how one account's last sync went. LastOK survives a
// later failure, so the ui can say how long a mailbox has been broken rather
// than only that it is. Both times are rfc3339, empty for never.
type AccountSyncStateDTO struct {
	AccountID int64  `json:"accountId"`
	Email     string `json:"email"`
	LastOK    string `json:"lastOk"`
	FailedAt  string `json:"failedAt"`
	// Reason is one of "auth", "network", "credentials" or "other"; the ui turns
	// it into a sentence. Detail is the underlying error, shown for the reader
	// who wants what the server actually said.
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// AccountSyncStates returns the recorded sync outcome of every account the
// current profile shows. Accounts that have never been synced are absent, which
// the ui reads as "nothing to say yet" rather than as a failure.
func (a *App) AccountSyncStates() ([]AccountSyncStateDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	states, err := a.store.AccountSyncStates(a.ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]storage.AccountSyncState, len(states))
	for _, s := range states {
		byID[s.AccountID] = s
	}

	out := make([]AccountSyncStateDTO, 0, len(accounts))
	for _, account := range accounts {
		state, ok := byID[account.ID]
		if !ok {
			continue
		}
		out = append(out, AccountSyncStateDTO{
			AccountID: account.ID,
			Email:     account.Email,
			LastOK:    formatDate(state.LastOK),
			FailedAt:  formatDate(state.FailedAt),
			Reason:    state.Reason,
			Detail:    state.Detail,
		})
	}
	return out, nil
}

// SyncAccountNow syncs one account on demand, for the retry button on a failed
// mailbox. It returns the failure so the button can say what happened this
// time, and the recorded state is updated either way.
func (a *App) SyncAccountNow(accountID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}
	syncErr := a.syncAccount(*account, true)
	a.emitAccountSyncStates()
	if syncErr != nil {
		return offlineOrErr(syncErr)
	}
	return nil
}

// emitAccountSyncStates pushes the current per-account state to the ui, so the
// sidebar mark and the status line update when a sync run ends instead of
// waiting for something else to reload them.
func (a *App) emitAccountSyncStates() {
	states, err := a.AccountSyncStates()
	if err != nil {
		a.log.Error("read account sync state", "err", err)
		return
	}
	a.emit(EventAccountSyncState, AccountSyncStateEvent{States: states})
}
