package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Engine orchestrates one account's protocol adapter and the local store. It is
// created per connected account and is not safe for concurrent use, matching
// the underlying connection.
type Engine struct {
	adapter Adapter
	store   *storage.DB
	log     *slog.Logger
	// ColorSync, when true, adopts server-side flag colors into the local cache
	// during each folder sync when the adapter implements ColorSource.
	ColorSync bool
	// TrashRemoteID and TrashFolderID identify the account's trash folder, which
	// is where a deleted message goes. Deleting from the trash itself, or from
	// an account with no trash folder (TrashRemoteID empty), is the permanent
	// delete instead. The caller sets these because folder roles are resolved
	// above this layer.
	TrashRemoteID string
	TrashFolderID int64
	// InitialLimit caps how many of a folder's newest messages the first sync
	// fetches; the rest wait behind the folder's sync floor until a backfill asks
	// for them (see window.go). 0 means no cap. It only applies to a folder's
	// first sync: once SyncInitialized is set, lowering this never discards
	// anything.
	InitialLimit int
	// FullList drops a stored sync floor for this run so the stub list covers
	// every server message. A list-stub phase sets it: a push may have
	// floored the folder at X already, and InitialLimit is ignored after that.
	// Bodies stay a later pass. IMAP leaves it false.
	FullList bool
	// ForceFull lists the folder in full even when a delta is possible: the
	// background full reconcile and manual Sync's follow-up set it. Any clean
	// full list stamps the folder's last_full_sync_at, forced or not. Only a
	// forced list checks PauseCheck between list pages and envelope batches.
	ForceFull bool
	// PauseCheck, when set, is asked after each successful body batch. If it
	// returns true and ids remain, the engine returns ErrSoftPaused and does
	// not start the next batch. The batch already in flight is never cancelled.
	// Nil means never pause. This is cooperative: it does not abort the context.
	PauseCheck func() bool
	// ReleaseLock, when set, is called between body-fetch batches so the caller
	// can drop a process-wide sync mutex. A long body backfill must not freeze
	// watch / TriggerSync: one SQLite writer is fine; holding syncMu across
	// thousands of downloads is not. The callback must re-acquire before
	// returning. Nil for IMAP, where the connection must stay exclusive.
	ReleaseLock func()
	// OnProgress, when set, is told how far the current folder has got: how many
	// message bodies this sync intends to fetch from it and how many are in.
	OnProgress func(p FolderProgress)
	// OnStored, when set, is called as messages are stored rather than only when
	// the folder finishes, so a first sync fills the list as mail arrives.
	OnStored func(folder storage.Folder, ids []int64)
	// beforeListStub, when set, runs before each list-stub upsert; a non-nil
	// error counts as a failed upsert. Tests cancel the context or fail a
	// write here. Nil in production.
	beforeListStub func(remoteID string) error
	// FetchBatchSize overrides fetchBatch for this engine when > 0. A smaller
	// value makes soft-pause happen more often; IMAP leaves 0.
	FetchBatchSize int
	// AbsorbArrivals, when true (default), re-lists between body batches so new
	// mail lands as stubs during a long fetch. Background body jobs may set
	// false; push and manual sync still reconcile.
	AbsorbArrivals bool
}

// FolderProgress is how far a folder's fetch has got. Total is what the plan
// says is coming and never changes during a folder; Done counts up to it.
// A Total of 0 means there is nothing to fetch from this folder.
type FolderProgress struct {
	Folder storage.Folder
	Done   int
	Total  int
}

// fetchBatch is how many message bodies one command asks for.
// Tests may lower it so a single batch boundary can be observed.
var fetchBatch = 50

// NewEngine wires a protocol adapter and the store together. A nil logger is
// replaced with a discarding default so callers need not pass one.
func NewEngine(adapter Adapter, store *storage.DB, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Engine{adapter: adapter, store: store, log: log, AbsorbArrivals: true}
}

func (e *Engine) batchSize() int {
	if e != nil && e.FetchBatchSize > 0 {
		return e.FetchBatchSize
	}
	return fetchBatch
}

// FolderSyncResult summarises one folder sync for logging and the cli.
type FolderSyncResult struct {
	New             int     // fetched from server into the cache
	NewIDs          []int64 // storage ids of the messages fetched this sync
	Deleted         int     // removed from the cache (server side or pushed delete)
	FlagUpdated     int     // server flag changes adopted locally
	Conflicts       int     // messages changed on both sides
	Pushed          int     // local flag or delete operations sent to the server
	GenerationReset bool    // the cache for the folder was dropped and refetched
	HasOlder        bool    // the server still holds messages below the sync window
	Repaired        int     // messages refetched because their cached text was broken
	RepairedIDs     []int64 // storage ids of those messages, for reindexing
	// removed is the remote ids executePlan deleted from the cache. Not part
	// of the public result; absorbArrivals uses it to drop them from the queue.
	removed []string
}

// StubSyncResult is the list/reconcile half of a folder sync. ToFetch is the
// remote ids that still need bodies, newest first. Those ids are already
// stored as list stubs when the headers carried list metadata. Headers without
// HasListMeta leave no stub row and are still listed in ToFetch.
type StubSyncResult struct {
	FloorID         string
	HasOlder        bool
	ToFetch         []string
	Deleted         int
	FlagUpdated     int
	Conflicts       int
	Pushed          int
	GenerationReset bool
	removed         []string // remote ids deleted locally by the delta plan
}

// SyncMailboxes lists remote mailboxes and upserts them by remote id. Local
// folders whose remote_id is absent from the server list are deleted.
func (e *Engine) SyncMailboxes(ctx context.Context, accountID int64) error {
	mailboxes, err := e.adapter.ListMailboxes(ctx)
	if err != nil {
		return fmt.Errorf("sync: list mailboxes for account %d: %w", accountID, err)
	}

	seen := make(map[string]struct{}, len(mailboxes))
	byRemote := make(map[string]int64, len(mailboxes))
	for _, mb := range mailboxes {
		seen[mb.RemoteID] = struct{}{}
		attrs := mailboxAttributes(mb)
		folder := &storage.Folder{
			AccountID:   accountID,
			Name:        mb.Name,
			IMAPPath:    mb.RemoteID,
			RemoteID:    mb.RemoteID,
			Attributes:  attrs,
			UIDValidity: generationToUIDValidity(mb.Generation),
		}
		if err := e.store.UpsertFolderByRemoteID(ctx, folder); err != nil {
			return fmt.Errorf("sync: upsert mailbox %q: %w", mb.RemoteID, err)
		}
		byRemote[mb.RemoteID] = folder.ID
	}

	for _, mb := range mailboxes {
		id := byRemote[mb.RemoteID]
		var parent *int64
		if mb.ParentID != "" {
			if pid, ok := byRemote[mb.ParentID]; ok {
				parent = &pid
			}
		}
		if err := e.store.SetFolderParent(ctx, id, parent); err != nil {
			return fmt.Errorf("sync: set parent for mailbox %q: %w", mb.RemoteID, err)
		}
	}

	local, err := e.store.ListFolders(ctx, accountID)
	if err != nil {
		return fmt.Errorf("sync: list local folders for account %d: %w", accountID, err)
	}
	for _, f := range local {
		if _, ok := seen[f.RemoteID]; ok {
			continue
		}
		if err := e.store.DeleteFolder(ctx, f.ID); err != nil {
			return fmt.Errorf("sync: delete stale folder %q: %w", f.RemoteID, err)
		}
	}
	return nil
}

func mailboxAttributes(mb Mailbox) []string {
	var attrs []string
	if mb.Role != "" {
		attrs = append(attrs, mb.Role)
	}
	if !mb.Selectable {
		attrs = append(attrs, `\Noselect`)
	}
	return attrs
}

// SyncAccount syncs every selectable, non-excluded folder for an account.
// Failures on one folder are logged and do not stop the others; the first
// error is returned after later folders have run.
func (e *Engine) SyncAccount(ctx context.Context, accountID int64) error {
	folders, err := e.store.ListFolders(ctx, accountID)
	if err != nil {
		return fmt.Errorf("sync: list folders for account %d: %w", accountID, err)
	}
	var firstErr error
	for _, folder := range folders {
		if err := ctx.Err(); err != nil {
			if firstErr == nil {
				return err
			}
			return firstErr
		}
		if folder.SyncExcluded || !selectable(folder) {
			continue
		}
		res, err := e.SyncFolder(ctx, folder)
		if err != nil {
			e.log.Error("folder sync failed", "folder", folder.IMAPPath, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		e.log.Info("folder synced",
			"folder", folder.IMAPPath, "server", e.adapter.Addr(),
			"new", res.New, "deleted", res.Deleted, "flag_updated", res.FlagUpdated,
			"conflicts", res.Conflicts, "pushed", res.Pushed, "generation_reset", res.GenerationReset)
	}
	return firstErr
}

// SyncFolder runs a full bidirectional sync of one folder and returns a summary.
func (e *Engine) SyncFolder(ctx context.Context, folder storage.Folder) (FolderSyncResult, error) {
	return e.syncFolder(ctx, folder, 0)
}

// BackfillFolder lowers the folder's sync window by batch messages and syncs,
// fetching that many older messages from the server. It is a no-op returning a
// zero result when the folder has no floor, i.e. it is already cached in full.
func (e *Engine) BackfillFolder(ctx context.Context, folder storage.Folder, batch int) (FolderSyncResult, error) {
	return e.syncFolder(ctx, folder, batch)
}

// syncFolder is the shared body of SyncFolder and BackfillFolder. backfill is
// how many older messages to admit into the window before planning; 0 keeps the
// window where it is, which is every ordinary sync.
//
// Stubs and bodies are separate: SyncFolderStubs reconciles and announces list
// rows, then FetchBodies downloads bodies. Soft pause is FetchBodies' boundary;
// this method returns that error unchanged so SoftPauseRemaining still lists
// the ids that were not started.
func (e *Engine) syncFolder(ctx context.Context, folder storage.Folder, backfill int) (FolderSyncResult, error) {
	var res FolderSyncResult

	stub, stubErr := e.SyncFolderStubs(ctx, folder, backfill)
	res.Deleted = stub.Deleted
	res.FlagUpdated = stub.FlagUpdated
	res.Conflicts = stub.Conflicts
	res.Pushed = stub.Pushed
	res.GenerationReset = stub.GenerationReset
	res.HasOlder = stub.HasOlder

	// A list failure has nothing to fetch. A reconcile error recorded while
	// building ToFetch still downloads bodies; the earlier error wins below.
	// A cancelled list/plan must not start FetchBodies.
	var bodyErr error
	if ctx.Err() == nil && (stubErr == nil || len(stub.ToFetch) > 0) {
		fresh, rerr := refreshFolder(ctx, e.store, folder)
		if rerr != nil {
			if stubErr == nil {
				stubErr = rerr
			}
		} else {
			folder = fresh
		}
		if rerr == nil {
			var body FolderSyncResult
			body, bodyErr = e.FetchBodies(ctx, folder, stub.ToFetch)
			res.New += body.New
			res.NewIDs = append(res.NewIDs, body.NewIDs...)
		}
	}

	post, postErr := e.CompleteFolder(ctx, folder)
	res.Repaired += post.Repaired
	res.RepairedIDs = append(res.RepairedIDs, post.RepairedIDs...)
	if postErr != nil {
		return res, postErr
	}

	if stubErr != nil {
		return res, stubErr
	}
	if bodyErr != nil {
		return res, bodyErr
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	return res, nil
}

// CompleteFolder runs the SyncFolder steps that follow a stub list and a body
// fetch: mangled-text repair, optional flag-color adoption, and the state-token
// refresh. The initial sync and scroll backfill call it so those passes still repair
// and color the way a full SyncFolder does. A cancelled context skips the token
// write. The returned error is a token save failure; repair and color log their
// own errors and still fill RepairedIDs.
func (e *Engine) CompleteFolder(ctx context.Context, folder storage.Folder) (FolderSyncResult, error) {
	var res FolderSyncResult
	e.repairMangled(ctx, folder, &res)
	if e.ColorSync {
		if src, ok := e.adapter.(ColorSource); ok {
			e.adoptColors(ctx, folder, src.Colors(folder.RemoteID))
		}
	}
	// Refresh the token after bodies in case absorbArrivals advanced it. A
	// mid-folder cancel leaves the early token in place so the next sync can
	// still diff.
	if ctx.Err() == nil {
		if latest, err := refreshFolder(ctx, e.store, folder); err == nil && latest.StateToken != "" {
			if err := e.store.SetFolderStateToken(ctx, folder.ID, latest.StateToken); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// listMetaBatch is how many envelopes one FetchListMeta call asks for.
const listMetaBatch = 200

// List modes for the folder listed log line.
const (
	listModeFull      = "full"
	listModeDelta     = "delta"
	listModeUnchanged = "unchanged"
)

func (e *Engine) requests() int64 {
	if rc, ok := e.adapter.(RequestCounter); ok {
		return rc.Requests()
	}
	return 0
}

func (e *Engine) logListed(folder storage.Folder, mode string, started time.Time, requestsBefore int64, toFetch int) {
	e.log.Info("folder listed",
		"folder", folder.IMAPPath, "server", e.adapter.Addr(), "mode", mode,
		"duration_ms", time.Since(started).Milliseconds(),
		"requests", e.requests()-requestsBefore, "to_fetch", toFetch)
}

// useDelta reports whether this run may ask the adapter for changes instead
// of a full list. A first sync, a backfill, a forced full reconcile and a
// FullList pass that still has to widen a floored folder all need the full
// snapshot.
func (e *Engine) useDelta(folder storage.Folder, state FolderSyncState, backfill int) bool {
	if e.ForceFull || backfill > 0 || folder.StateToken == "" || !state.SyncInitialized {
		return false
	}
	if e.FullList && state.SyncFloorID != "" {
		return false
	}
	_, ok := e.adapter.(DeltaLister)
	return ok
}

// SyncFolderStubs lists one folder, reconciles flags and deletes, and stores
// list stubs. It does not download bodies. ToFetch is the newest-first remote
// ids a later FetchBodies should fill. With a stored cursor and an adapter
// that implements DeltaLister it only reconciles what changed; otherwise, or
// when the adapter returns ErrNeedFullList, it lists the folder in full.
func (e *Engine) SyncFolderStubs(ctx context.Context, folder storage.Folder, backfill int) (StubSyncResult, error) {
	started := time.Now()
	requestsBefore := e.requests()
	folder, err := refreshFolder(ctx, e.store, folder)
	if err != nil {
		return StubSyncResult{}, err
	}
	state, err := loadFolderSyncState(ctx, e.store, folder)
	if err != nil {
		return StubSyncResult{}, err
	}
	localStates, err := e.store.ListMessageStates(ctx, folder.ID)
	if err != nil {
		return StubSyncResult{}, fmt.Errorf("sync: load local states for folder %q: %w", folder.IMAPPath, err)
	}
	if e.useDelta(folder, state, backfill) {
		res, unchanged, err := e.syncFolderDelta(ctx, folder, state, localStates)
		if !errors.Is(err, ErrNeedFullList) {
			mode := listModeDelta
			if unchanged {
				mode = listModeUnchanged
			}
			e.logListed(folder, mode, started, requestsBefore, len(res.ToFetch))
			return res, err
		}
	}
	res, err := e.syncFolderFull(ctx, folder, state, localStates, backfill)
	e.logListed(folder, listModeFull, started, requestsBefore, len(res.ToFetch))
	return res, err
}

// syncFolderFull is the full-list path of SyncFolderStubs.
func (e *Engine) syncFolderFull(ctx context.Context, folder storage.Folder, state FolderSyncState, localStates []storage.MessageState, backfill int) (StubSyncResult, error) {
	var res StubSyncResult
	var err error

	listBox := RemoteMailbox{
		RemoteID:   folder.RemoteID,
		Generation: storedGeneration(folder.UIDValidity),
	}
	var (
		headers      []Header
		stateToken   string
		generation   string
		pageStored   map[string]struct{}
		stubFailures int
	)
	if pl, ok := e.adapter.(PagedLister); ok && e.FullList && backfill == 0 {
		// FullList without a backfill clears the floor (windowFloor), so every
		// listed id is a row reconcile would store anyway; inserting it per page
		// cannot land mail below a floor. A backfill lowers the floor by a page
		// instead, even with FullList set (scroll backfill), so it and runs
		// without FullList (IMAP-style windows) take the plain ListMessages path.
		pageStored = make(map[string]struct{})
		headers, stateToken, generation, err = pl.ListMessagesPaged(ctx, listBox, e.pageStubWriter(ctx, folder, localStates, pageStored, &stubFailures))
	} else {
		headers, stateToken, generation, err = e.adapter.ListMessages(ctx, listBox)
	}
	if err != nil {
		// Page stubs already stored stay: they are real server mail, and the
		// next successful list reconciles deletes and flags. Nothing is
		// deleted and the state token does not advance on a partial list.
		return res, fmt.Errorf("sync: list messages in %q: %w", folder.IMAPPath, err)
	}
	if len(pageStored) > 0 {
		// Reconcile must see the page stubs as local so it neither inserts
		// them again nor treats them as new.
		localStates, err = e.store.ListMessageStates(ctx, folder.ID)
		if err != nil {
			return res, fmt.Errorf("sync: reload local states for folder %q: %w", folder.IMAPPath, err)
		}
	}

	folder, reset, err := e.handleGeneration(ctx, folder, storedGeneration(state.StoredUIDValidity), generation)
	if err != nil {
		return res, err
	}
	res.GenerationReset = reset
	storedFloorID := state.SyncFloorID
	if reset {
		// A generation change purges the folder, page stubs included, and the
		// plan below stores every listed id again from the new snapshot. No
		// row from the old generation survives. (A paged adapter may report no
		// generation, so this only matters for one that grows one.)
		pageStored = nil
		state.SyncFloorID = ""
		state.SyncFloorUID = 0
		state.LastSeenUID = 0
		state.SyncInitialized = false
		localStates, err = e.store.ListMessageStates(ctx, folder.ID)
		if err != nil {
			return res, fmt.Errorf("sync: reload local states for folder %q: %w", folder.IMAPPath, err)
		}
	}

	locals, localByID := localView(localStates)
	servers := headersToServers(headers)

	floorID := e.windowFloor(state, servers, len(localStates), backfill)
	if floorID != storedFloorID || !state.SyncInitialized {
		if err := e.store.SetFolderSyncWindow(ctx, folder.ID, floorID, true); err != nil {
			return res, err
		}
		if err := e.store.SetFolderSyncFloorUID(ctx, folder.ID, legacyFloorUID(headers, floorID)); err != nil {
			return res, err
		}
	}
	res.FloorID = floorID
	res.HasOlder = floorID != ""

	plan := BuildPlan(locals, servers, floorID)

	var planRes FolderSyncResult
	outcome := e.executePlan(ctx, folder, plan, localByID, headersByRemoteID(headers), &planRes)
	stubFailures += outcome.stubFailures
	planErr := outcome.err()
	res.ToFetch = withPageStubs(outcome.toFetch, headers, pageStored)
	res.Deleted = planRes.Deleted
	res.FlagUpdated = planRes.FlagUpdated
	res.Conflicts = planRes.Conflicts
	res.Pushed = planRes.Pushed

	if high := highestLegacyUID(headers); high != 0 {
		if err := e.store.SetFolderLastSeenUID(ctx, folder.ID, high); err != nil {
			return res, err
		}
	}

	// The state token is the incremental cursor. Save it only after this
	// snapshot's stub rows are stored, and skip the save when the context is
	// already cancelled or any local write failed. A cancel or failed write
	// must not advance the cursor, or the next Email/changes pass drops mail
	// that never landed. A server refusing a push does not hold it (see
	// planOutcome). An empty token means the adapter has no cursor.
	clean := ctx.Err() == nil && outcome.clean() && stubFailures == 0
	if clean && stateToken != "" {
		if err := e.store.SetFolderStateToken(ctx, folder.ID, stateToken); err != nil {
			return res, err
		}
		folder.StateToken = stateToken
	}
	if clean {
		if err := e.store.SetFolderFullSyncAt(ctx, folder.ID, time.Now()); err != nil {
			return res, err
		}
	}
	if planErr != nil {
		return res, planErr
	}
	if stubFailures > 0 {
		return res, fmt.Errorf("sync: %d list stubs in %q were not stored", stubFailures, folder.IMAPPath)
	}
	if err := ctx.Err(); err != nil {
		return res, err
	}
	return res, nil
}

// syncFolderDelta is SyncFolderStubs for a folder with a stored cursor and an
// adapter that lists changes. ErrNeedFullList is returned untouched so the
// caller can fall back to the full list. The cursor only moves when every
// local write and every stub upsert went through; repeating a delta is
// harmless, skipping one loses mail. A refused server push is reported but
// does not hold the cursor. unchanged reports the adapter's Unchanged flag.
func (e *Engine) syncFolderDelta(ctx context.Context, folder storage.Folder, state FolderSyncState, localStates []storage.MessageState) (res StubSyncResult, unchanged bool, err error) {
	box := RemoteMailbox{RemoteID: folder.RemoteID, StateToken: folder.StateToken, Generation: storedGeneration(folder.UIDValidity)}
	delta, err := e.adapter.(DeltaLister).ListChanges(ctx, box, pendingRemoteIDs(localStates))
	if errors.Is(err, ErrNeedFullList) {
		return res, false, err
	}
	if err != nil {
		return res, false, fmt.Errorf("sync: list changes in %q: %w", folder.IMAPPath, err)
	}
	res.FloorID = state.SyncFloorID
	res.HasOlder = state.SyncFloorID != ""

	locals, localByID := localView(localStates)
	plan := BuildDeltaPlan(locals, delta, state.SyncFloorUID)
	byID := make(map[string]Header, len(delta.Members)+len(delta.Changed))
	for _, h := range delta.Members {
		byID[h.RemoteID] = h
	}
	for _, h := range delta.Changed {
		byID[h.RemoteID] = h
	}

	var planRes FolderSyncResult
	outcome := e.executePlan(ctx, folder, plan, localByID, byID, &planRes)
	stubFailures, planErr := outcome.stubFailures, outcome.err()
	res.ToFetch = outcome.toFetch
	res.Deleted = planRes.Deleted
	res.FlagUpdated = planRes.FlagUpdated
	res.Conflicts = planRes.Conflicts
	res.Pushed = planRes.Pushed
	res.removed = planRes.removed

	if high := max(highestLegacyUID(delta.Changed), highestLegacyUID(delta.Members)); high > state.LastSeenUID {
		if err := e.store.SetFolderLastSeenUID(ctx, folder.ID, high); err != nil {
			return res, delta.Unchanged, err
		}
	}
	if ctx.Err() == nil && outcome.clean() && delta.Cursor != "" && delta.Cursor != folder.StateToken {
		if err := e.store.SetFolderStateToken(ctx, folder.ID, delta.Cursor); err != nil {
			return res, delta.Unchanged, err
		}
	}
	if planErr != nil {
		return res, delta.Unchanged, planErr
	}
	if stubFailures > 0 {
		return res, delta.Unchanged, fmt.Errorf("sync: %d list stubs in %q were not stored", stubFailures, folder.IMAPPath)
	}
	return res, delta.Unchanged, ctx.Err()
}

// fetchNew downloads the bodies of the given remote ids, newest first, in
// batches chosen by the engine and handed to the adapter. Between batches it
// releases the caller's sync lock (when ReleaseLock is set) and absorbs any
// mail that arrived since the list snapshot so new stubs land without waiting
// for the rest of the body campaign.
func (e *Engine) fetchNew(ctx context.Context, folder storage.Folder, remoteIDs []string, res *FolderSyncResult) error {
	queue := append([]string(nil), remoteIDs...)
	planned := len(queue)
	e.report(folder, 0, planned)
	done := 0
	var firstErr error
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			e.log.Warn("sync cancelled mid-folder", "folder", folder.IMAPPath)
			return err
		}
		end := min(e.batchSize(), len(queue))
		batch := queue[:end]
		queue = queue[end:]
		ids, err := e.fetchBatch(ctx, folder, batch)
		if err != nil {
			e.log.Error("fetch batch failed", "folder", folder.IMAPPath, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		res.New += len(ids)
		res.NewIDs = append(res.NewIDs, ids...)
		e.announce(folder, ids)
		done += len(batch)
		e.report(folder, done, planned)
		if err != nil {
			return firstErr
		}
		if len(queue) > 0 && e.pauseRequested() {
			return newSoftPaused(queue)
		}

		// After each batch: drop the caller's sync lock so watch / refresh can
		// run, then pick up any headers that arrived while we were downloading.
		if len(queue) == 0 {
			break
		}
		if e.ReleaseLock != nil {
			e.ReleaseLock()
		}
		if e.AbsorbArrivals {
			arrived, removed := e.absorbArrivals(ctx, &folder)
			if len(removed) > 0 {
				gone := make(map[string]struct{}, len(removed))
				for _, id := range removed {
					gone[id] = struct{}{}
				}
				kept := queue[:0]
				for _, id := range queue {
					if _, ok := gone[id]; !ok {
						kept = append(kept, id)
					}
				}
				planned -= len(queue) - len(kept)
				queue = kept
			}
			if len(arrived) > 0 {
				queue = append(arrived, queue...)
				planned += len(arrived)
			}
			if len(removed) > 0 || len(arrived) > 0 {
				e.report(folder, done, planned)
			}
		}
	}
	return firstErr
}

// FetchBodies downloads bodies for the given remote ids, newest-first as
// supplied, in engine batch-sized chunks. Ids that already have body_complete
// set are left out, so a retry of a partial batch does not fetch them again.
// A soft pause returns ErrSoftPaused after the current chunk;
// SoftPauseRemaining lists the ids not started, not the ones already stored.
func (e *Engine) FetchBodies(ctx context.Context, folder storage.Folder, remoteIDs []string) (FolderSyncResult, error) {
	var res FolderSyncResult
	ids := remoteIDs
	if e.store != nil && len(remoteIDs) > 0 {
		need, err := e.store.RemoteIDsNeedingBody(ctx, folder.ID, remoteIDs)
		if err != nil {
			return res, err
		}
		ids = need
	}
	err := e.fetchNew(ctx, folder, ids, &res)
	return res, err
}

// absorbArrivals applies the folder's delta between body batches, so mail that
// arrives during a long body fill lands as stubs right away and flag changes
// and deletes are not held back either. It returns the new ids newest-first
// so fetchNew can download them ahead of older bodies, and the ids the delta
// removed from the cache so fetchNew stops queueing their bodies. No-op without a
// DeltaLister or a stored cursor; a failed delta waits for the next sync.
func (e *Engine) absorbArrivals(ctx context.Context, folder *storage.Folder) (arrived, removed []string) {
	if _, ok := e.adapter.(DeltaLister); !ok {
		return nil, nil
	}
	latest, err := refreshFolder(ctx, e.store, *folder)
	if err != nil {
		e.log.Debug("absorb arrivals: refresh folder", "err", err)
		return nil, nil
	}
	*folder = latest
	if folder.StateToken == "" {
		return nil, nil
	}
	state, err := loadFolderSyncState(ctx, e.store, *folder)
	if err != nil {
		return nil, nil
	}
	localStates, err := e.store.ListMessageStates(ctx, folder.ID)
	if err != nil {
		e.log.Debug("absorb arrivals: local states", "err", err)
		return nil, nil
	}
	res, _, err := e.syncFolderDelta(ctx, *folder, state, localStates)
	if err != nil {
		e.log.Debug("absorb arrivals", "folder", folder.IMAPPath, "err", err)
	}
	if fresh, err := refreshFolder(ctx, e.store, *folder); err == nil {
		*folder = fresh
	}
	return res.ToFetch, res.removed
}

func (e *Engine) report(folder storage.Folder, done, total int) {
	if e.OnProgress == nil {
		return
	}
	e.OnProgress(FolderProgress{Folder: folder, Done: done, Total: total})
}

func (e *Engine) announce(folder storage.Folder, ids []int64) {
	if e.OnStored == nil || len(ids) == 0 {
		return
	}
	e.OnStored(folder, append([]int64(nil), ids...))
}

// windowFloor decides the folder's sync floor for this run: a backfill lowers
// the existing one, a first sync of a folder bigger than InitialLimit
// establishes one, and everything else keeps what is stored (minus a floor that
// no longer holds anything back). FullList clears the floor so a later stub
// pass can widen a folder that push already capped.
func (e *Engine) windowFloor(state FolderSyncState, servers []ServerMessage, cached, backfill int) string {
	if backfill > 0 {
		return lowerFloor(servers, state.SyncFloorID, backfill)
	}
	if e.FullList {
		return ""
	}
	if !folderLooksInitialized(state, cached) {
		return floorForLimit(servers, e.InitialLimit)
	}
	return normalizeFloor(servers, state.SyncFloorID)
}

// folderLooksInitialized is true once SyncInitialized is set, or when an
// upgraded pre-migration folder already has local mail / numeric sync cursors
// (sync_initialized defaults to 0 after migration 0037).
func folderLooksInitialized(state FolderSyncState, cached int) bool {
	if state.SyncInitialized {
		return true
	}
	return cached > 0 || state.LastSeenUID != 0 || state.SyncFloorUID != 0
}

// handleGeneration drops and refetches the folder cache if the server's
// generation changed. "" and "0" match (choice 2). Returns the folder with its
// updated uid_validity.
func (e *Engine) handleGeneration(ctx context.Context, folder storage.Folder, stored, server string) (storage.Folder, bool, error) {
	if generationsEqual(stored, server) {
		return folder, false, nil
	}

	reset := false
	if normalizeGeneration(stored) != "" {
		e.log.Warn("generation changed, dropping stale cache for folder",
			"folder", folder.IMAPPath, "stored", stored, "server", server)
		n, err := e.store.PurgeFolderMessages(ctx, folder.AccountID, folder.ID)
		if err != nil {
			return folder, false, err
		}
		e.log.Warn("purged stale cached messages", "folder", folder.IMAPPath, "count", n)
		reset = true
	}

	uidValidity := generationToUIDValidity(server)
	if err := e.store.SetFolderUIDValidity(ctx, folder.ID, uidValidity); err != nil {
		return folder, false, err
	}
	if err := e.store.SetFolderLastSeenUID(ctx, folder.ID, 0); err != nil {
		return folder, false, err
	}
	if err := e.store.SetFolderSyncFloorUID(ctx, folder.ID, 0); err != nil {
		return folder, false, err
	}
	if err := e.store.SetFolderSyncWindow(ctx, folder.ID, "", false); err != nil {
		return folder, false, err
	}
	folder.UIDValidity = uidValidity
	folder.SyncFloorID = ""
	folder.SyncInitialized = false
	return folder, reset, nil
}

// adoptColors makes the server authoritative for flag colors keyed by remote id.
func (e *Engine) adoptColors(ctx context.Context, folder storage.Folder, serverColors map[string]int) {
	if len(serverColors) == 0 {
		return
	}
	states, err := e.store.ListMessageStates(ctx, folder.ID)
	if err != nil {
		e.log.Error("color sync: list states", "folder", folder.IMAPPath, "err", err)
		return
	}
	colorsByUID, err := e.store.FolderFlagColors(ctx, folder.ID)
	if err != nil {
		e.log.Error("color sync: current colors", "folder", folder.IMAPPath, "err", err)
		return
	}
	current := make(map[string]int, len(states))
	for _, s := range states {
		current[s.RemoteID] = colorsByUID[s.UID]
	}
	for _, s := range states {
		desired, ok := serverColors[s.RemoteID]
		if !ok {
			continue
		}
		if current[s.RemoteID] != desired {
			if err := e.store.SetFlagColor(ctx, s.ID, desired); err != nil {
				e.log.Error("color sync: set color", "remote_id", s.RemoteID, "err", err)
			}
		}
	}
}

// planOutcome is what executePlan left to do and what went wrong. A failed
// local write (stub upsert, local delete, adopted or cleared flags, list
// metadata) holds the folder's cursor and full-sync stamp back, since moving
// on would skip the change for good. A server refusing a push does not: the
// row keeps its pending marker and every later sync re-reads it.
type planOutcome struct {
	toFetch      []string
	stubFailures int
	localErr     error
	pushErr      error
}

// clean reports whether every local write of the plan went through.
func (o planOutcome) clean() bool {
	return o.stubFailures == 0 && o.localErr == nil
}

// err is the error to report for the plan, local failures first. Stub
// failures are reported by the caller, which names the folder.
func (o planOutcome) err() error {
	if o.localErr != nil {
		return o.localErr
	}
	return o.pushErr
}

// executePlan applies a reconciled plan front to back (newest first). It stores
// list stubs once and returns the remote ids that still need bodies. Body
// download is FetchBodies, so a soft pause is not decided here.
func (e *Engine) executePlan(ctx context.Context, folder storage.Folder, plan []Decision, localByID map[string]storage.MessageState, headersByID map[string]Header, res *FolderSyncResult) planOutcome {
	var pendingDeletes []storage.MessageState
	var out planOutcome
	fail := func(err error) {
		if isPushRejected(err) {
			if out.pushErr == nil {
				out.pushErr = err
			}
			return
		}
		if out.localErr == nil {
			out.localErr = err
		}
	}

	for _, d := range plan {
		if d.Conflict {
			res.Conflicts++
		}
		if err := ctx.Err(); err != nil {
			e.log.Warn("sync cancelled mid-folder", "folder", folder.IMAPPath)
			out.localErr = err
			return out
		}

		switch d.Action {
		case ActionNone:

		case ActionFetchNew:
			out.toFetch = append(out.toFetch, d.RemoteID)

		case ActionDeleteLocal:
			if err := e.deleteLocal(ctx, folder, localByID[d.RemoteID]); err != nil {
				e.log.Error("delete local message failed", "remote_id", d.RemoteID, "err", err)
				fail(err)
				continue
			}
			res.Deleted++
			res.removed = append(res.removed, d.RemoteID)

		case ActionAdoptServerFlags:
			applied, err := e.adoptServerFlags(ctx, localByID[d.RemoteID], d.Flags)
			if err != nil {
				e.log.Error("adopt server flags failed", "remote_id", d.RemoteID, "err", err)
				fail(err)
				continue
			}
			if applied {
				res.FlagUpdated++
			}

		case ActionPushFlags:
			if err := e.pushFlags(ctx, folder, localByID[d.RemoteID], d.Flags); err != nil {
				e.log.Error("push flags failed", "remote_id", d.RemoteID, "err", err)
				fail(err)
				continue
			}
			res.Pushed++

		case ActionClearPending:
			if err := e.resolvePending(ctx, localByID[d.RemoteID], d.Flags); err != nil {
				e.log.Error("clear pending flags failed", "remote_id", d.RemoteID, "err", err)
				fail(err)
				continue
			}

		case ActionPushDelete:
			pendingDeletes = append(pendingDeletes, localByID[d.RemoteID])
		}
	}

	// Headers-first: fill missing envelopes, then upsert list rows and announce
	// once. FetchBodies announces again only for rows whose bodies it stored.
	if err := e.fillListMeta(ctx, folder, out.toFetch, headersByID); err != nil {
		if errors.Is(err, ErrSoftPaused) || ctx.Err() != nil {
			out.localErr = err
			return out
		}
		e.log.Error("fetch list meta failed", "folder", folder.IMAPPath, "err", err)
		fail(err)
	}
	stubIDs, stubFailures := e.storeListStubs(ctx, folder, out.toFetch, headersByID)
	out.stubFailures = stubFailures
	if len(stubIDs) > 0 {
		e.announce(folder, stubIDs)
	}

	// Deletes are part of reconcile, not body fetch, so a later FetchBodies
	// failure cannot skip a delete the user already marked.
	if len(pendingDeletes) > 0 {
		if err := e.pushDeletes(ctx, folder, pendingDeletes); err != nil {
			e.log.Error("push deletes failed", "folder", folder.IMAPPath, "count", len(pendingDeletes), "err", err)
			fail(err)
		} else {
			res.Pushed += len(pendingDeletes)
			res.Deleted += len(pendingDeletes)
			for _, m := range pendingDeletes {
				res.removed = append(res.removed, m.RemoteID)
			}
		}
	}
	return out
}

// fillListMeta asks a ListMetaFetcher for the envelope of every id in ids
// whose header came without one and writes the result into headersByID. Only
// messages about to become stubs are asked for, so thousands of uncached
// messages below a sync floor cost nothing. A forced full list stops between
// batches when PauseCheck asks.
func (e *Engine) fillListMeta(ctx context.Context, folder storage.Folder, ids []string, headersByID map[string]Header) error {
	mf, ok := e.adapter.(ListMetaFetcher)
	if !ok {
		return nil
	}
	var need []string
	for _, id := range ids {
		if h, ok := headersByID[id]; !ok || !h.HasListMeta {
			need = append(need, id)
		}
	}
	box := RemoteMailbox{RemoteID: folder.RemoteID, StateToken: folder.StateToken, Generation: storedGeneration(folder.UIDValidity)}
	for start := 0; start < len(need); start += listMetaBatch {
		if err := ctx.Err(); err != nil {
			return err
		}
		if start > 0 && e.ForceFull && e.pauseRequested() {
			return ErrSoftPaused
		}
		got, err := mf.FetchListMeta(ctx, box, need[start:min(start+listMetaBatch, len(need))])
		if err != nil {
			return fmt.Errorf("sync: list meta in %q: %w", folder.IMAPPath, err)
		}
		for _, h := range got {
			headersByID[h.RemoteID] = h
		}
	}
	return nil
}

func headersByRemoteID(headers []Header) map[string]Header {
	out := make(map[string]Header, len(headers))
	for _, h := range headers {
		out[h.RemoteID] = h
	}
	return out
}

// ReconcileAndStoreStubs upserts list-meta rows for remote ids whose headers
// carry list metadata and announces those rows. Ids without HasListMeta are
// skipped. It does not fetch bodies; the scheduler calls FetchBodies later.
func (e *Engine) ReconcileAndStoreStubs(ctx context.Context, folder storage.Folder, remoteIDs []string, headersByID map[string]Header) []int64 {
	ids, _ := e.storeListStubs(ctx, folder, remoteIDs, headersByID)
	e.announce(folder, ids)
	return ids
}

// pageStubWriter returns the onPage callback for a paged full list. It stores
// and announces stubs only for ids with no local row (insert-only: flags and
// deletes wait for reconcile on the complete snapshot) and records them in
// stored; failed counts stubs that did not store. A forced full list stops at a
// page boundary when PauseCheck asks. It runs on the list call's goroutine.
func (e *Engine) pageStubWriter(ctx context.Context, folder storage.Folder, localStates []storage.MessageState, stored map[string]struct{}, failed *int) func([]Header) error {
	local := make(map[string]struct{}, len(localStates))
	for _, s := range localStates {
		if s.RemoteID != "" {
			local[s.RemoteID] = struct{}{}
		}
	}
	return func(page []Header) error {
		fresh := make([]string, 0, len(page))
		byID := make(map[string]Header, len(page))
		for _, h := range page {
			if _, ok := local[h.RemoteID]; ok || !h.HasListMeta {
				continue
			}
			local[h.RemoteID] = struct{}{}
			fresh = append(fresh, h.RemoteID)
			byID[h.RemoteID] = h
		}
		ids, n := e.storeListStubs(ctx, folder, fresh, byID)
		*failed += n
		for _, rid := range fresh {
			stored[rid] = struct{}{}
		}
		e.announce(folder, ids)
		if e.ForceFull && e.pauseRequested() {
			return ErrSoftPaused
		}
		return ctx.Err()
	}
}

// withPageStubs adds ids stored by a paged list back into toFetch in snapshot
// order. Reconcile sees those rows as local, so it no longer lists them, but
// they still need bodies.
func withPageStubs(toFetch []string, headers []Header, pageStored map[string]struct{}) []string {
	if len(pageStored) == 0 {
		return toFetch
	}
	want := make(map[string]struct{}, len(toFetch)+len(pageStored))
	for _, id := range toFetch {
		want[id] = struct{}{}
	}
	for id := range pageStored {
		want[id] = struct{}{}
	}
	out := make([]string, 0, len(want))
	for _, h := range headers {
		if _, ok := want[h.RemoteID]; ok {
			out = append(out, h.RemoteID)
			delete(want, h.RemoteID)
		}
	}
	return out
}

// storeListStubs writes envelope rows for messages that have list metadata
// so the message list can render before bodies arrive. Headers without
// HasListMeta are skipped. It returns the stored row ids and how many
// upserts failed, so callers can hold the cursor back.
func (e *Engine) storeListStubs(ctx context.Context, folder storage.Folder, remoteIDs []string, headersByID map[string]Header) ([]int64, int) {
	ids := make([]int64, 0, len(remoteIDs))
	failed := 0
	for _, rid := range remoteIDs {
		if err := ctx.Err(); err != nil {
			return ids, failed
		}
		h, ok := headersByID[rid]
		if !ok || !h.HasListMeta {
			continue
		}
		if e.beforeListStub != nil {
			if err := e.beforeListStub(rid); err != nil {
				e.log.Error("store list stub failed", "remote_id", rid, "err", err)
				failed++
				continue
			}
			if err := ctx.Err(); err != nil {
				return ids, failed
			}
		}
		id, err := e.store.UpsertMessageListMeta(ctx, &storage.Message{
			AccountID:      folder.AccountID,
			FolderID:       folder.ID,
			UID:            h.LegacyUID,
			RemoteID:       h.RemoteID,
			Subject:        h.Subject,
			FromAddress:    h.From,
			FromName:       h.FromName,
			ToAddresses:    h.To,
			Date:           h.Date,
			Flags:          h.Flags,
			BodyPlain:      h.Preview,
			HasAttachments: h.HasAttachment,
			SizeBytes:      h.Size,
		})
		if err != nil {
			e.log.Error("store list stub failed", "remote_id", rid, "err", err)
			failed++
			continue
		}
		ids = append(ids, id)
	}
	return ids, failed
}
