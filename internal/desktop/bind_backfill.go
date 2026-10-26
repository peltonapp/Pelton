package desktop

import (
	"fmt"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

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
// It follows the same setting as the initial limit, so "sync 50 messages" means
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
		return FetchOlderResult{}, nil
	}

	// one imap session per account, not per folder: a unified view spans an
	// inbox per account and logging in once each is the difference between a
	// couple of round trips and a dozen.
	byAccount := make(map[int64][]storage.Folder, len(pending))
	for _, f := range pending {
		byAccount[f.AccountID] = append(byAccount[f.AccountID], f)
	}

	syncMu.Lock()
	defer syncMu.Unlock()

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

// backfillAccount opens one session for an account and backfills each of its
// folders in the selection.
func (a *App) backfillAccount(accountID int64, folders []storage.Folder) (int, bool, error) {
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return 0, false, err
	}
	cfg, err := a.resolveIMAP(*account)
	if err != nil {
		return 0, false, err
	}

	client, err := a.connectIMAP(cfg)
	if err != nil {
		return 0, false, err
	}
	defer client.Close()
	if err := client.Login(); err != nil {
		return 0, false, err
	}
	defer client.Logout()

	engine := a.newSyncEngine(pimap.NewAdapter(client), accountID)
	email := a.accountEmail(accountID)
	// a backfill is a sync run of its own as far as the progress bar is
	// concerned: a known number of older messages, fetched now.
	a.syncTally.begin(len(folders))

	batch := a.backfillBatch()
	var (
		fetched  int
		hasOlder bool
	)
	for i, f := range folders {
		a.syncTally.enterFolder(i, f.Name)
		a.emitSyncProgress(accountID, email, client.Addr(), a.syncTally.counts())
		res, err := engine.BackfillFolder(a.ctx, f, batch)
		if psync.IsSoftError(err) {
			// an unreadable message stays a stub; the page itself landed.
			a.log.Warn("backfill folder", "folder", f.Name, "err", err)
			err = nil
		}
		if err != nil {
			a.log.Error("backfill folder", "folder", f.Name, "err", err)
			continue
		}
		fetched += res.New
		hasOlder = hasOlder || res.HasOlder
	}
	final := a.syncTally.counts()
	final.Folder = ""
	final.FoldersDone = len(folders)
	a.emitSyncProgress(accountID, email, client.Addr(), final)
	return fetched, hasOlder, nil
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
		floor, err := a.store.FolderSyncFloorUID(a.ctx, id)
		if err != nil {
			return nil, fmt.Errorf("desktop: sync floor for folder %d: %w", id, err)
		}
		if floor == 0 {
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
