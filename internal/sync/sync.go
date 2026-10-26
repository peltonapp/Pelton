package sync

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// mailClient is the slice of the imap client's public surface the sync engine
// uses. depending on an interface keeps the boundary explicit and lets tests
// substitute a fake without a live server.
type mailClient interface {
	Select(mailbox string) (*pimap.Mailbox, error)
	FetchAllFlags() ([]pimap.MessageHeader, error)
	// Addr is the server the client is talking to, host and port, for the log
	// lines the debug overlay shows.
	Addr() string
	FetchMessage(uid imap.UID) (*pimap.Message, error)
	// FetchMessages pulls a set of messages in one command, handing each to fn
	// as it arrives. A first sync is otherwise bound by the round trip to the
	// server rather than by bandwidth.
	FetchMessages(uids []imap.UID, fn func(uid imap.UID, msg *pimap.Message, err error) error) error
	AddFlags(uid imap.UID, flags ...imap.Flag) error
	DeleteMessages(uids ...imap.UID) error
	MoveMessages(uids []imap.UID, mailbox string) error
}

// Engine orchestrates one account's imap connection and the local store. It is
// created per connected account and is not safe for concurrent use, matching
// the imap client.
type Engine struct {
	client mailClient
	store  *storage.DB
	log    *slog.Logger
	// ColorSync, when true, adopts server-side flag colors (Thunderbird $LabelN
	// keywords) into the local cache during each folder sync.
	ColorSync bool
	// TrashPath and TrashFolderID identify the account's trash folder, which is
	// where a deleted message goes. Deleting from the trash itself, or from an
	// account with no trash folder (TrashPath empty), is the permanent delete
	// instead. The caller sets these because folder roles are resolved above
	// this layer.
	TrashPath     string
	TrashFolderID int64
	// InitialLimit caps how many of a folder's newest messages the first sync
	// fetches; the rest wait behind the folder's sync floor until a backfill asks
	// for them (see window.go). 0 means no cap, the pre-#175 behavior. It only
	// applies to a folder's first sync: once a folder is cached, lowering this
	// never discards anything.
	InitialLimit int
	// YieldTo, when set, is asked before each message body is fetched and the
	// sync waits while it says yes. The desktop points it at the outbox, so a
	// message the user pressed send on goes out during a first sync rather than
	// after it (#310).
	YieldTo func() bool
	// OnProgress, when set, is told how far the current folder has got: how many
	// message bodies this sync intends to fetch from it and how many are in.
	// The count comes from the reconcile plan, so it is what will actually be
	// downloaded rather than the size of the mailbox, and a resync of a cached
	// folder reports the handful it is really fetching (#313).
	OnProgress func(p FolderProgress)
	// OnStored, when set, is called as messages are stored rather than only when
	// the folder finishes, so a first sync fills the list as mail arrives
	// instead of showing nothing for several minutes. It is called with the
	// folder and the ids stored since the last call.
	OnStored func(folder storage.Folder, ids []int64)
}

// FolderProgress is how far a folder's fetch has got. Total is what the plan
// says is coming and never changes during a folder; Done counts up to it.
// A Total of 0 means there is nothing to fetch from this folder.
type FolderProgress struct {
	Folder storage.Folder
	Done   int
	Total  int
}

// fetchBatch is how many message bodies one command asks for. Big enough that
// the round trip stops being what a large sync is made of, small enough that a
// send waiting to go out is never more than a batch away from its turn, and
// that a failure costs one batch rather than a folder.
const fetchBatch = 50

// yieldPoll is how long the sync waits before asking again whether it may
// continue. It is a pause between messages, so the send it is standing aside
// for has the connection and the cpu to itself.
const yieldPoll = 250 * time.Millisecond

// NewEngine wires an imap client and the store together. A nil logger is
// replaced with a discarding default so callers need not pass one.
func NewEngine(client mailClient, store *storage.DB, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Engine{client: client, store: store, log: log}
}

// FolderSyncResult summarises one folder sync for logging and the cli.
type FolderSyncResult struct {
	New              int     // fetched from server into the cache
	NewIDs           []int64 // storage ids of the messages fetched this sync
	Deleted          int     // removed from the cache (server side or pushed delete)
	FlagUpdated      int     // server flag changes adopted locally
	Conflicts        int     // messages changed on both sides
	Pushed           int     // local flag or delete operations sent to the server
	UIDValidityReset bool    // the cache for the folder was dropped and refetched
	HasOlder         bool    // the server still holds messages below the sync window
	Repaired         int     // messages refetched because their cached text was broken
	RepairedIDs      []int64 // storage ids of those messages, for reindexing
}

// SyncAccount syncs every cached folder for an account. A failure on one folder
// is logged and does not stop the others, so one broken mailbox cannot block a
// whole account.
func (e *Engine) SyncAccount(ctx context.Context, accountID int64) error {
	folders, err := e.store.ListFolders(ctx, accountID)
	if err != nil {
		return fmt.Errorf("sync: list folders for account %d: %w", accountID, err)
	}
	for _, folder := range folders {
		if err := ctx.Err(); err != nil {
			return err
		}
		// a folder the user unchecked is never opened at all (#173).
		if folder.SyncExcluded {
			continue
		}
		res, err := e.SyncFolder(ctx, folder)
		if err != nil {
			e.log.Error("folder sync failed", "folder", folder.IMAPPath, "err", err)
			continue
		}
		e.log.Info("folder synced",
			"folder", folder.IMAPPath, "server", e.client.Addr(),
			"new", res.New, "deleted", res.Deleted, "flag_updated", res.FlagUpdated,
			"conflicts", res.Conflicts, "pushed", res.Pushed, "uidvalidity_reset", res.UIDValidityReset)
	}
	return nil
}

// SyncFolder runs a full bidirectional sync of one folder and returns a summary.
func (e *Engine) SyncFolder(ctx context.Context, folder storage.Folder) (FolderSyncResult, error) {
	return e.syncFolder(ctx, folder, 0)
}

// BackfillFolder lowers the folder's sync window by batch messages and syncs,
// fetching that many older messages from the server. It is a no-op returning a
// zero result when the folder has no floor, i.e. it is already cached in full.
// Callers use HasOlder on the result to decide whether more remain.
func (e *Engine) BackfillFolder(ctx context.Context, folder storage.Folder, batch int) (FolderSyncResult, error) {
	return e.syncFolder(ctx, folder, batch)
}

// syncFolder is the shared body of SyncFolder and BackfillFolder. backfill is
// how many older messages to admit into the window before planning; 0 keeps the
// window where it is, which is every ordinary sync.
func (e *Engine) syncFolder(ctx context.Context, folder storage.Folder, backfill int) (FolderSyncResult, error) {
	var res FolderSyncResult

	mbox, err := e.client.Select(folder.IMAPPath)
	if err != nil {
		return res, fmt.Errorf("sync: select folder %q: %w", folder.IMAPPath, err)
	}
	e.log.Debug("folder selected", "folder", folder.IMAPPath, "server", e.client.Addr(), "messages", mbox.NumMessages)

	state, err := loadFolderSyncState(ctx, e.store, folder)
	if err != nil {
		return res, err
	}

	folder, reset, err := e.handleUIDValidity(ctx, folder, state.StoredUIDValidity, mbox.UIDValidity)
	if err != nil {
		return res, err
	}
	res.UIDValidityReset = reset
	// storedFloor is what the folder row actually holds, which the reset below
	// deliberately stops tracking; the write at the end compares against this so
	// a reset back to no floor still reaches the database.
	storedFloor := state.SyncFloorUID
	if reset {
		// the cache was dropped, so this is a first sync again and the window has
		// to be recomputed from scratch rather than kept at a floor that refers to
		// uids from the old UIDVALIDITY generation.
		state.SyncFloorUID = 0
		state.LastSeenUID = 0
	}

	localStates, err := e.store.ListMessageStates(ctx, folder.ID)
	if err != nil {
		return res, fmt.Errorf("sync: load local states for folder %q: %w", folder.IMAPPath, err)
	}
	locals, localByUID := localView(localStates)

	servers, serverColors, err := loadServerView(e.client)
	if err != nil {
		return res, err
	}

	floor := e.windowFloor(state, servers, len(localStates), backfill)
	if floor != storedFloor {
		if err := e.store.SetFolderSyncFloorUID(ctx, folder.ID, floor); err != nil {
			return res, err
		}
	}
	res.HasOlder = floor > 0

	plan := BuildPlan(locals, servers, floor)
	e.executePlan(ctx, folder, plan, localByUID, &res)

	// mail cached before charset detection existed is stored with bytes that
	// are not valid utf-8 and cannot be fixed locally, so it comes from the
	// server again. A few per sync, after the plan, so it never delays new mail.
	e.repairMangled(ctx, folder, &res)

	// adopt server-side color labels when color syncing is on. this runs after the
	// plan so newly fetched messages already have local rows to color.
	if e.ColorSync {
		e.adoptColors(ctx, folder, serverColors)
	}

	newHigh := max(highestUID(servers), state.LastSeenUID)
	if err := e.store.SetFolderLastSeenUID(ctx, folder.ID, newHigh); err != nil {
		return res, err
	}
	return res, nil
}

// fetchNew downloads the bodies of the given uids, newest first, in batches.
//
// One command per batch rather than one per message: the bytes are the same
// either way, but 14k separate fetches means paying the round trip to the
// server 14k times before anything else can happen on the connection (#310).
// Between batches the sync stands aside for anything the user asked for and
// tells the ui what has arrived, so a long download stays interruptible and
// visible rather than being one opaque stretch.
func (e *Engine) fetchNew(ctx context.Context, folder storage.Folder, uids []uint32, res *FolderSyncResult) {
	// reported before the first batch so the bar starts at 0 of n rather than
	// appearing part way through, and reported for an empty plan too so a
	// folder with nothing to do does not leave the previous folder's numbers on
	// screen.
	e.report(folder, 0, len(uids))
	done := 0
	for start := 0; start < len(uids); start += fetchBatch {
		if err := ctx.Err(); err != nil {
			e.log.Warn("sync cancelled mid-folder", "folder", folder.IMAPPath)
			return
		}
		// anything the user asked for goes first. A body fetch is the expensive
		// step in a sync, so between batches is the point to stand aside at.
		e.yield(ctx)

		end := min(start+fetchBatch, len(uids))
		ids, err := e.fetchBatch(ctx, folder, uids[start:end])
		if err != nil {
			e.log.Error("fetch batch failed", "folder", folder.IMAPPath, "err", err)
		}
		res.New += len(ids)
		res.NewIDs = append(res.NewIDs, ids...)
		e.announce(folder, ids)
		// counted by what the batch asked for, not by what came back: a message
		// the server refused is still one fewer left to wait for, and a bar that
		// stops short of its total is worse than one that counts a skip.
		done += end - start
		e.report(folder, done, len(uids))
		if err != nil {
			// the connection is in an unknown state after a failed fetch, so the
			// rest of this folder waits for the next sync rather than piling more
			// commands onto it.
			return
		}
	}
}

// yield waits while the caller's YieldTo says something more important is
// happening. A cancelled context ends the wait, since the sync is stopping
// anyway.
func (e *Engine) yield(ctx context.Context) {
	if e.YieldTo == nil {
		return
	}
	waited := false
	for e.YieldTo() {
		if !waited {
			e.log.Debug("sync standing aside", "reason", "outbox")
			waited = true
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(yieldPoll):
		}
	}
}

// report tells the caller how far this folder has got, if anyone is listening.
func (e *Engine) report(folder storage.Folder, done, total int) {
	if e.OnProgress == nil {
		return
	}
	e.OnProgress(FolderProgress{Folder: folder, Done: done, Total: total})
}

// announce hands a batch of freshly stored ids to OnStored, if anyone is
// listening and there is anything to hand over.
func (e *Engine) announce(folder storage.Folder, ids []int64) {
	if e.OnStored == nil || len(ids) == 0 {
		return
	}
	e.OnStored(folder, append([]int64(nil), ids...))
}

// windowFloor decides the folder's sync floor for this run: a backfill lowers
// the existing one, a first sync of a folder bigger than InitialLimit
// establishes one, and everything else keeps what is stored (minus a floor that
// no longer holds anything back).
//
// "First sync" is both an empty high water mark and an empty cache. Checking
// only the high water mark would re-cap a folder whose messages were all
// deleted server side, and checking only the cache would re-cap a folder the
// user had legitimately emptied.
func (e *Engine) windowFloor(state FolderSyncState, servers []ServerMessage, cached, backfill int) uint32 {
	if backfill > 0 {
		return lowerFloor(servers, state.SyncFloorUID, backfill)
	}
	if state.SyncFloorUID == 0 && state.LastSeenUID == 0 && cached == 0 {
		return floorForLimit(servers, e.InitialLimit)
	}
	return normalizeFloor(servers, state.SyncFloorUID)
}

// handleUIDValidity drops and refetches the folder cache if the server's
// UIDVALIDITY changed, which means every cached uid for the folder is stale.
// this is destructive but correct, so it is logged loudly. returns the folder
// with its updated uid_validity.
func (e *Engine) handleUIDValidity(ctx context.Context, folder storage.Folder, stored, server uint32) (storage.Folder, bool, error) {
	if stored == server {
		return folder, false, nil
	}

	reset := false
	// stored == 0 is a first sync, there is nothing cached to drop.
	if stored != 0 {
		e.log.Warn("uidvalidity changed, dropping stale cache for folder",
			"folder", folder.IMAPPath, "stored", stored, "server", server)
		n, err := e.store.PurgeFolderMessages(ctx, folder.AccountID, folder.ID)
		if err != nil {
			return folder, false, err
		}
		e.log.Warn("purged stale cached messages", "folder", folder.IMAPPath, "count", n)
		reset = true
	}

	if err := e.store.SetFolderUIDValidity(ctx, folder.ID, server); err != nil {
		return folder, false, err
	}
	if err := e.store.SetFolderLastSeenUID(ctx, folder.ID, 0); err != nil {
		return folder, false, err
	}
	folder.UIDValidity = server
	return folder, reset, nil
}

// adoptColors makes the server authoritative for flag colors: for each cached
// message whose stored color differs from the server keyword, it writes the
// server's color (0 clears). It only writes on a difference, so a steady state
// costs no writes.
func (e *Engine) adoptColors(ctx context.Context, folder storage.Folder, serverColors map[uint32]int) {
	states, err := e.store.ListMessageStates(ctx, folder.ID)
	if err != nil {
		e.log.Error("color sync: list states", "folder", folder.IMAPPath, "err", err)
		return
	}
	current, err := e.store.FolderFlagColors(ctx, folder.ID)
	if err != nil {
		e.log.Error("color sync: current colors", "folder", folder.IMAPPath, "err", err)
		return
	}
	for _, s := range states {
		desired, ok := serverColors[s.UID]
		if !ok {
			continue
		}
		if current[s.UID] != desired {
			if err := e.store.SetFlagColor(ctx, s.ID, desired); err != nil {
				e.log.Error("color sync: set color", "uid", s.UID, "err", err)
			}
		}
	}
}

// executePlan applies a reconciled plan. pull actions and flag pushes run
// inline; local deletions are batched so they cost two server round trips total.
// a failure on one message is logged and skipped so it cannot corrupt the cache
// or block the rest of the folder.
func (e *Engine) executePlan(ctx context.Context, folder storage.Folder, plan []Decision, localByUID map[uint32]storage.MessageState, res *FolderSyncResult) {
	var pendingDeletes []storage.MessageState
	// uids whose bodies have to come down, gathered here and fetched in batches
	// once the cheap decisions are out of the way.
	var toFetch []uint32

	// walked back to front, because BuildPlan orders by ascending uid and a full
	// body fetch is the expensive step here. Front to back means the oldest mail
	// downloads first and a large mailbox shows years-old messages for as long as
	// it takes to reach the present (#175); back to front puts the newest mail on
	// screen within seconds. Nothing else in the plan is order-sensitive: the
	// decisions are independent per uid and deletes are batched at the end.
	for _, d := range slices.Backward(plan) {
		if d.Conflict {
			res.Conflicts++
		}
		if err := ctx.Err(); err != nil {
			e.log.Warn("sync cancelled mid-folder", "folder", folder.IMAPPath)
			return
		}

		switch d.Action {
		case ActionNone:
			// already in agreement

		case ActionFetchNew:
			// collected and fetched below, in one command per batch rather than
			// one per message.
			toFetch = append(toFetch, d.UID)

		case ActionDeleteLocal:
			if err := e.deleteLocal(ctx, folder, localByUID[d.UID]); err != nil {
				e.log.Error("delete local message failed", "uid", d.UID, "err", err)
				continue
			}
			res.Deleted++

		case ActionAdoptServerFlags:
			if err := e.adoptServerFlags(ctx, localByUID[d.UID], d.Flags); err != nil {
				e.log.Error("adopt server flags failed", "uid", d.UID, "err", err)
				continue
			}
			res.FlagUpdated++

		case ActionPushFlags:
			if err := e.pushFlags(ctx, localByUID[d.UID], d.Flags); err != nil {
				e.log.Error("push flags failed", "uid", d.UID, "err", err)
				continue
			}
			res.Pushed++

		case ActionClearPending:
			if err := e.clearPending(ctx, localByUID[d.UID], d.Flags); err != nil {
				e.log.Error("clear pending flags failed", "uid", d.UID, "err", err)
				continue
			}

		case ActionPushDelete:
			pendingDeletes = append(pendingDeletes, localByUID[d.UID])
		}
	}

	e.fetchNew(ctx, folder, toFetch, res)

	if len(pendingDeletes) > 0 {
		if err := e.pushDeletes(ctx, folder, pendingDeletes); err != nil {
			e.log.Error("push deletes failed", "folder", folder.IMAPPath, "count", len(pendingDeletes), "err", err)
			return
		}
		res.Pushed += len(pendingDeletes)
		res.Deleted += len(pendingDeletes)
	}
}
