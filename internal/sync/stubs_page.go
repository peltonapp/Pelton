package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// listSnapshot is one full server list of a folder.
type listSnapshot struct {
	headers                []Header
	stateToken, generation string
}

// StubPager carries one folder's paged stub list across SyncFolderStubsPage
// calls, in memory only. Nothing on disk marks a folder as half listed: a
// new pager lists again from the newest, and the stubs a lost pager stored
// are no longer missing, so they cost nothing.
type StubPager struct {
	// First is the size of the first page and Page of every later one.
	// Page <= 0 lists the folder in one call.
	First, Page int

	pages    int
	snap     *listSnapshot // an earlier call's list, for insert-only pages
	genReset bool
}

// Pages is how many pages this listing has stored so far.
func (p *StubPager) Pages() int { return p.pages }

func (p *StubPager) size() int {
	if p.pages == 0 && p.First > 0 {
		return p.First
	}
	return p.Page
}

func (p *StubPager) reset() {
	p.pages, p.snap, p.genReset = 0, nil, false
}

// SyncFolderStubsPage is SyncFolderStubs in bounded steps. While more new
// messages in the window lack a stub than fit one page, a call stores the
// newest page of them (insert only) and returns more. Deletes, flag changes,
// the cursor and the full-sync stamp wait for the call that finds at most a
// page left: it lists again and reconciles that fresh list in full, so a
// partial list never deletes. A delta from a stored cursor is small and
// runs in one call. Backfill is not paged; use SyncFolderStubs.
func (e *Engine) SyncFolderStubsPage(ctx context.Context, folder storage.Folder, p *StubPager) (StubSyncResult, bool, error) {
	if p.size() <= 0 {
		res, err := e.SyncFolderStubs(ctx, folder, 0)
		return res, false, err
	}
	started := time.Now()
	requestsBefore := e.requests()
	folder, state, localStates, err := e.loadFolder(ctx, folder)
	if err != nil {
		p.reset()
		return StubSyncResult{}, false, err
	}
	if p.snap == nil && e.useDelta(folder, state, 0) {
		res, unchanged, err := e.syncFolderDelta(ctx, folder, state, localStates)
		if !errors.Is(err, ErrNeedFullList) {
			mode := listModeDelta
			if unchanged {
				mode = listModeUnchanged
			}
			res, err = e.withIncompleteStubs(ctx, folder, res, err)
			e.logListed(folder, mode, started, requestsBefore, len(res.ToFetch))
			return res, false, err
		}
	}

	if p.snap != nil {
		res, more, err := e.storeListPage(ctx, folder, state, localStates, p.snap, p)
		if err != nil || more {
			return e.pageEnd(folder, p, res, more, err, started, requestsBefore)
		}
		// The old list has at most a page left: reconcile only on a list
		// taken now, so a stale one cannot bring back moved mail.
		p.snap = nil
	}
	headers, stateToken, generation, err := e.adapter.ListMessages(ctx, RemoteMailbox{
		RemoteID:   folder.RemoteID,
		Generation: storedGeneration(folder.UIDValidity),
	})
	if err != nil {
		p.reset()
		return StubSyncResult{}, false, fmt.Errorf("sync: list messages in %q: %w", folder.IMAPPath, err)
	}
	snap := &listSnapshot{headers: headers, stateToken: stateToken, generation: generation}
	res, more, err := e.storeListPage(ctx, folder, state, localStates, snap, p)
	if more && err == nil {
		p.snap = snap
	}
	if err != nil || more {
		return e.pageEnd(folder, p, res, more, err, started, requestsBefore)
	}

	genReset := p.genReset
	p.reset()
	folder, state, localStates, err = e.loadFolder(ctx, folder)
	if err != nil {
		return StubSyncResult{}, false, err
	}
	res, err = e.syncFolderFull(ctx, folder, state, localStates, 0, snap)
	res.GenerationReset = res.GenerationReset || genReset
	res, err = e.withIncompleteStubs(ctx, folder, res, err)
	e.logListed(folder, listModeFull, started, requestsBefore, len(res.ToFetch))
	return res, false, err
}

// pageEnd ends a call that stored a page or failed. A failure drops the
// pager's list, so the next call starts from a fresh one.
func (e *Engine) pageEnd(folder storage.Folder, p *StubPager, res StubSyncResult, more bool, err error, started time.Time, requestsBefore int64) (StubSyncResult, bool, error) {
	if err != nil {
		p.reset()
		return res, false, err
	}
	e.logListed(folder, listModePage, started, requestsBefore, len(res.ToFetch))
	return res, more, nil
}

// storeListPage stores, insert only, the newest page of the window's
// messages that have no local row, when more of them are missing than fit
// one page; it reports whether it did. With a page or fewer missing it
// writes nothing and the caller reconciles in full. The generation and the
// window floor are settled first, as the full path does, so the next call
// sees the same window.
func (e *Engine) storeListPage(ctx context.Context, folder storage.Folder, state FolderSyncState, localStates []storage.MessageState, snap *listSnapshot, p *StubPager) (StubSyncResult, bool, error) {
	var res StubSyncResult
	limit := p.size()
	stored := storedGeneration(state.StoredUIDValidity)
	newGen := !generationsEqual(stored, snap.generation)
	if newGen && normalizeGeneration(stored) != "" {
		// handleGeneration will drop the folder's rows and window.
		localStates = nil
		state = FolderSyncState{StoredUIDValidity: state.StoredUIDValidity}
	}
	locals, localByID := localView(localStates)
	servers := headersToServers(snap.headers)
	floorID := e.windowFloor(state, servers, localByID, 0)
	var missing []string
	for _, d := range BuildPlan(locals, servers, floorID) {
		if d.Action == ActionFetchNew {
			missing = append(missing, d.RemoteID)
		}
	}
	if len(missing) <= limit {
		return res, false, nil
	}

	if newGen {
		var purged bool
		var err error
		folder, purged, err = e.handleGeneration(ctx, folder, stored, snap.generation)
		if err != nil {
			return res, false, err
		}
		p.genReset = p.genReset || purged
	}
	if floorID != folder.SyncFloorID || !state.SyncInitialized {
		if err := e.store.SetFolderSyncWindow(ctx, folder.ID, floorID, true); err != nil {
			return res, false, err
		}
		if err := e.store.SetFolderSyncFloorUID(ctx, folder.ID, legacyFloorUID(snap.headers, floorID)); err != nil {
			return res, false, err
		}
	}
	res.FloorID = floorID
	res.HasOlder = floorID != ""

	page := missing[:min(limit, len(missing))]
	byID := make(map[string]Header, len(page))
	for _, h := range snap.headers {
		byID[h.RemoteID] = h
	}
	if err := e.fillListMeta(ctx, folder, page, byID); err != nil {
		return res, false, err
	}
	ids, failed := e.storeListStubs(ctx, folder, page, byID)
	e.announce(folder, ids)
	if failed > 0 {
		return res, false, fmt.Errorf("sync: %d list stubs in %q were not stored", failed, folder.IMAPPath)
	}
	if err := ctx.Err(); err != nil {
		return res, false, err
	}
	if len(ids) == 0 {
		// Nothing of the page could be stored (gone from the server, or no
		// envelope): no progress, so let the full path settle it.
		return res, false, nil
	}
	for _, id := range page {
		if h := byID[id]; h.HasListMeta {
			res.ToFetch = append(res.ToFetch, id)
		}
	}
	p.pages++
	return res, true, nil
}

// loadFolder reads a folder's row, sync state and local message states.
func (e *Engine) loadFolder(ctx context.Context, folder storage.Folder) (storage.Folder, FolderSyncState, []storage.MessageState, error) {
	folder, err := refreshFolder(ctx, e.store, folder)
	if err != nil {
		return folder, FolderSyncState{}, nil, err
	}
	state, err := loadFolderSyncState(ctx, e.store, folder)
	if err != nil {
		return folder, FolderSyncState{}, nil, err
	}
	localStates, err := e.states.ListMessageStates(ctx, folder.ID)
	if err != nil {
		return folder, FolderSyncState{}, nil, fmt.Errorf("sync: load local states for folder %q: %w", folder.IMAPPath, err)
	}
	return folder, state, localStates, nil
}
