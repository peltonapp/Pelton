package desktop

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// Fetching older mail on demand (#175). A first sync only caches a folder's
// newest messages, so reaching the end of the list is not necessarily the end
// of the mailbox: the rest is still on the server, behind the folder's sync
// window. FetchOlderMessages lowers that window by one page and syncs the
// newly-admitted range.

// syncMessageLimit is the sync window: how many of a folder's newest
// messages are kept with their bodies. Raising it fetches the newly admitted
// ones (enqueueSyncLimitDelta). A negative stored value is treated as
// unlimited, matching 0, so a hand-edited setting cannot produce a
// nonsensical window.
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

	// one scheduler per account, not a process-wide lock: each folder's page
	// is a fast-lane task, its bodies go to the normal lane.
	byAccount := make(map[int64][]storage.Folder, len(pending))
	for _, f := range pending {
		byAccount[f.AccountID] = append(byAccount[f.AccountID], f)
	}

	defer a.beginSyncState()()

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
		a.enqueueAccountDelta(account, pending, batch)
	}
}

// olderList is one folder's "load older" or raised-limit listing across its
// units. "Load older"'s first unit (fast) stores the page just below the
// window floor, bounded, without a full list (imapOlderPage). Then a unit
// lowers the floor by what is left of batch and stores a page of the newly
// admitted stubs; each next unit stores one more page from the same server
// list (pager), and the last reconciles on a fresh list. The cost is a
// fixed number of full lists whatever the batch, instead of one per page; a
// batch that fits the first page lists once.
type olderList struct {
	// batch is how many messages below the floor to admit; 0 is the rest of
	// the folder.
	batch int
	// fast marks "load older"'s fast-lane unit as the next one.
	fast    bool
	lowered bool
	pager   *psync.StubPager
	// started is set once lastSeen and before were read, at the first
	// unit and before the window moved: the highest UID the folder had seen
	// and its incomplete stubs then. Ids the listing finds above lastSeen
	// that were not stubs already are new mail (freshMark), even in a list
	// folded into it.
	started  bool
	lastSeen uint32
	before   map[string]struct{}
}

// newOlderList admits batch more messages (0 or less: the rest of the
// folder), first page first, then pages of page.
func newOlderList(batch, first, page int) *olderList {
	if batch <= 0 || batch >= math.MaxInt32 {
		batch = 0
	} else {
		first = min(first, batch)
	}
	return &olderList{batch: batch, pager: &psync.StubPager{First: first, Page: page}}
}

// backfillAccount lists one page of older stubs per folder, each as a
// fast-lane task ("load older"), and waits for those pages only. The rest
// of the batch is listed a page per normal-lane unit from the same server
// list, then the folder's body generator fills the bodies, without new-mail
// notifications. Fetched counts the stubs the first pages admitted.
func (a *App) backfillAccount(accountID int64, folders []storage.Folder) (int, bool, error) {
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return 0, false, err
	}
	rt, err := a.ensureAccountSync(a.ctx, accountID)
	if errors.Is(err, errAccountSyncHeld) {
		// The account is being removed.
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	tun := rt.sched.Tunables()
	p := a.protocolFor(*account)
	fetched, hasOlder := 0, false
	for _, folder := range folders {
		older := newOlderList(a.backfillBatch(), tun.FirstListPage, tun.ListPage)
		older.fast = true
		var ids []string
		var more bool
		err := a.runNowKey(a.ctx, *account, syncsched.KindLoadOlder, folder.ID, folderKey(syncsched.KindLoadOlder, folder.ID),
			func(ctx context.Context, _ mailClient) error {
				var err error
				ids, more, err = p.olderStep(ctx, *account, folder, older)
				return err
			})
		if errors.Is(err, errAccountSyncHeld) {
			return fetched, hasOlder, nil
		}
		if err != nil {
			// A failure on one folder must not lose another's page.
			a.log.Error("backfill folder", "account", accountID, "folder", folder.Name, "err", err)
			continue
		}
		fetched += len(ids)
		rt.addBodyWant(folder.ID, ids, false)
		if left, err := a.store.FolderHasOlderOnServer(a.ctx, folder.ID); err == nil {
			hasOlder = hasOlder || left
		}
		if more {
			a.enqueueOlderPages(*account, folder, older)
			continue
		}
		a.enqueueBodies(a.ctx, *account, folder, syncsched.ClassAuto, false, nil)
	}
	return fetched, hasOlder, nil
}

// enqueueOlderPages lists the rest of older's batch in the normal lane, a
// list page per unit, admits their bodies, then runs the folder's body
// generator. None of it is new mail.
func (a *App) enqueueOlderPages(account storage.Account, folder storage.Folder, older *olderList) {
	rt, err := a.ensureAccountSync(a.ctx, account.ID)
	if err != nil {
		// Held for removal or removed: nothing is left to sync.
		return
	}
	p := a.protocolFor(account)
	rt.sched.Enqueue(imapTask{
		Kind: syncsched.KindListStubs, FolderID: folder.ID,
		Key: fmt.Sprintf("older:%d", folder.ID),
		Run: func(ctx context.Context, c mailClient) error {
			ids, more, err := p.olderStep(withTaskSession(ctx, account.ID, c), account, folder, older)
			if err != nil {
				return err
			}
			rt.addBodyWant(folder.ID, ids, false)
			if more {
				return syncsched.ErrAgain
			}
			return nil
		},
		Done: func(err error) {
			if taskEnded(err) {
				// Stopped (a profile switch, say) or dropped: the next sync
				// finds what is missing.
				return
			}
			if err != nil {
				a.log.Error("list older messages", "folder", folder.Name, "err", err)
			}
			a.enqueueBodiesOn(rt, account, folder, syncsched.ClassAuto, false, nil)
		},
	})
}

// enqueueAccountDelta lists batch more of each folder's older messages in
// the normal lane after the limit was raised, a page per unit, then fills
// their bodies. As with "load older", the first page is short
// (FirstListPage), so the first older stubs show quickly; then full pages.
func (a *App) enqueueAccountDelta(account storage.Account, folders []storage.Folder, batch int) {
	first, page := syncsched.FirstListPage, syncsched.ListPage
	if rt := a.accountSync(account.ID); rt != nil {
		t := rt.sched.Tunables()
		first, page = t.FirstListPage, t.ListPage
	}
	for _, folder := range foldersInSyncOrder(folders) {
		a.enqueueOlderPages(account, folder, newOlderList(batch, first, page))
	}
}
