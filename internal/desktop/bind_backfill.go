package desktop

import (
	"context"
	"errors"
	"math"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
)

// enqueueDeltaSync schedules ListStubs then FetchBodies per folder (Inbox first).
// Scroll backfill and a raised sync_message_limit both use this path.
func enqueueDeltaSync(s *syncsched.Scheduler, folders []storage.Folder, run func(ctx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error), done chan<- error) {
	enqueueIMAPInitialSync(s, folders, run, nil, done)
}

// Fetching older mail on demand (#175). A first sync only caches a folder's
// newest messages, so reaching the end of the list is not necessarily the end
// of the mailbox: the rest is still on the server, behind the folder's sync
// window. FetchOlderMessages lowers that window by one page and syncs the
// newly-admitted range.

// syncMessageLimit is how many of a folder's newest messages a first sync
// fetches. A negative stored value is treated as unlimited, matching 0, so a
// hand-edited setting cannot produce a nonsensical window.
func (a *App) syncMessageLimit() int {
	limit := a.intSetting(settingSyncMessageLimit, defaultSyncMessageLimit)
	if limit < 0 {
		return 0
	}
	return limit
}

// backfillBatch is how many older messages one FetchOlderMessages call pulls.
// It follows the same setting as the initial limit, so "sync 100 messages" means
// the same thing in both directions. An unlimited setting fetches the rest of
// the folder in one go, which is what asking for no limit means.
func (a *App) backfillBatch() int {
	return a.syncMessageLimit()
}

// FetchOlderResult reports what a backfill did. Fetched is how many messages
// were newly cached across every folder in the selection, and HasOlder whether
// any of them still has more waiting on the server.
type FetchOlderResult struct {
	Fetched  int  `json:"fetched"`
	HasOlder bool `json:"hasOlder"`
}

// FetchOlderMessages pulls the next page of older messages from the server for
// the given selection, which takes the same shape as ListMessages: a single
// folder, or a unified view spanning one folder per account. Folders that are
// already cached in full are skipped, so calling this when nothing is left is
// cheap and reports Fetched 0.
func (a *App) FetchOlderMessages(req ListMessagesRequest) (FetchOlderResult, error) {
	if err := a.ready(); err != nil {
		return FetchOlderResult{}, err
	}

	folderIDs, err := a.selectionFolderIDs(req)
	if err != nil {
		return FetchOlderResult{}, err
	}
	pending, err := a.foldersWithOlder(folderIDs)
	if err != nil {
		return FetchOlderResult{}, err
	}
	if len(pending) == 0 {
		hasOlder, err := a.store.AnyFolderHasOlder(a.ctx, folderIDs)
		if err != nil {
			return FetchOlderResult{}, err
		}
		return FetchOlderResult{HasOlder: hasOlder}, nil
	}

	// one scheduler per account, not a process-wide lock: each folder is
	// stubs then bodies, and the pool caps how many sessions are open.
	byAccount := make(map[int64][]storage.Folder, len(pending))
	for _, f := range pending {
		byAccount[f.AccountID] = append(byAccount[f.AccountID], f)
	}

	a.emit(EventSyncState, SyncStateEvent{Running: true})
	defer a.emit(EventSyncState, SyncStateEvent{Running: false})

	var res FetchOlderResult
	for accountID, folders := range byAccount {
		fetched, hasOlder, err := a.backfillAccount(accountID, folders)
		// a failure on one account must not lose the mail another already
		// fetched, so the error is logged and the rest still counts.
		if err != nil {
			a.log.Error("fetch older messages", "account", accountID, "err", err)
			continue
		}
		res.Fetched += fetched
		res.HasOlder = res.HasOlder || hasOlder
	}

	if res.Fetched > 0 {
		goSafe("indexing new mail", func() { _ = a.indexNewMessages() })
		goSafe("counting unread mail", a.refreshViewCounts)
	}
	return res, nil
}

// backfillAccount enqueues stubs then bodies for each folder in the selection.
// Sessions are checked out per job. A folder's bodies finish before the next
// folder's stubs are queued.
func (a *App) backfillAccount(accountID int64, folders []storage.Folder) (int, bool, error) {
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return 0, false, err
	}
	email := a.accountEmail(accountID)
	tally := a.accountTally(accountID)
	tally.begin(len(folders))
	stopBeat := a.startProgressHeartbeat(a.ctx, accountID)
	defer stopBeat()
	batch := a.backfillBatch()
	stats := &imapStepStats{}
	done := make(chan error, 1)
	sched, err := a.ensureAccountScheduler(a.ctx, accountID)
	if errors.Is(err, errAccountSyncHeld) {
		// The account is being removed.
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	enqueueDeltaSync(sched, folders, func(ctx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		if kind == syncsched.JobListStubs {
			for i := range folders {
				if folders[i].ID == folder.ID {
					tally.enterFolder(i, folder.Name)
					a.emitSyncProgress(accountID, email, "", tally.counts())
					break
				}
			}
		}
		p := a.protocolFor(*account)
		bodyLimit, err := p.backfillBodyLimit(ctx, folder, kind, batch)
		if err != nil {
			return nil, err
		}
		return p.backfillStep(ctx, *account, folder, kind, batch, bodyLimit, stats)
	}, done)

	var runErr error
	finished := false
	select {
	case runErr = <-done:
		finished = true
	case <-a.schedulerEnded(accountID):
		runErr = context.Canceled
	case <-a.ctx.Done():
		runErr = a.ctx.Err()
	}
	// The job goroutine owns stats until it reports on done.
	if !finished {
		stats = &imapStepStats{}
	}
	if runErr != nil {
		a.log.Error("backfill folder", "account", accountID, "err", runErr)
	}
	final := tally.counts()
	final.Folder = ""
	final.FoldersDone = len(folders)
	stopBeat()
	a.emitSyncProgress(accountID, email, "", final)
	// A folder error is logged here. Counts already stored stay visible, matching
	// the previous per-folder continue.
	return stats.newCount, stats.hasOlder, nil
}

// selectionFolderIDs resolves a list request to the folder ids it reads from.
// Saved views are searches over whatever is already cached rather than a fixed
// mailbox set, so they have no folders to backfill and resolve to none.
func (a *App) selectionFolderIDs(req ListMessagesRequest) ([]int64, error) {
	if req.Kind == "savedView" {
		return nil, nil
	}
	q, err := a.requestQuery(a.ctx, req)
	if err != nil {
		return nil, err
	}
	return q.FolderIDs, nil
}

// foldersWithOlder loads the folder rows that still have messages below their
// sync window, dropping the ones already cached in full.
func (a *App) foldersWithOlder(folderIDs []int64) ([]storage.Folder, error) {
	var out []storage.Folder
	for _, id := range folderIDs {
		hasOlder, err := a.store.FolderHasOlderOnServer(a.ctx, id)
		if err != nil {
			return nil, err
		}
		if !hasOlder {
			continue
		}
		folder, err := a.store.GetFolder(a.ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *folder)
	}
	return out, nil
}

// enqueueSyncLimitDelta admits more newest messages per folder after the user
// raises sync_message_limit. Stubs first, then bodies for the expanded slice.
func (a *App) enqueueSyncLimitDelta(oldLimit, newLimit int) {
	if a.store == nil || !syncMessageLimitExpanded(oldLimit, newLimit) {
		return
	}
	batch := newLimit - oldLimit
	if newLimit == 0 {
		batch = math.MaxInt32
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		a.log.Error("list accounts for limit delta", "err", err)
		return
	}
	for _, account := range accounts {
		if account.Local {
			continue
		}
		folders, err := a.store.ListFolders(a.ctx, account.ID)
		if err != nil {
			a.log.Error("list folders for limit delta", "account", account.ID, "err", err)
			continue
		}
		var pending []storage.Folder
		for _, f := range folders {
			if f.SyncExcluded || !folderSelectable(f) {
				continue
			}
			if !a.protocolFor(account).limitNeedsBodies(a.ctx, f, newLimit) {
				continue
			}
			pending = append(pending, f)
		}
		if len(pending) == 0 {
			continue
		}
		a.enqueueAccountDelta(account, pending, batch, newLimit)
	}
}

// newLimit is the new total sync_message_limit (0 = all); the driver's
// backfillStep gets it as its body limit.
func (a *App) enqueueAccountDelta(account storage.Account, folders []storage.Folder, batch, newLimit int) {
	sched, err := a.ensureAccountScheduler(a.ctx, account.ID)
	if err != nil {
		// Held for removal or removed: nothing is left to sync.
		return
	}
	enqueueDeltaSync(sched, folders, func(ctx context.Context, folder storage.Folder, kind syncsched.JobKind) ([]string, error) {
		return a.protocolFor(account).backfillStep(ctx, account, folder, kind, batch, newLimit, nil)
	}, nil)
}
