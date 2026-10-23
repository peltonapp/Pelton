package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// OlderLister is an optional Adapter capability: a bounded look just below a
// folder's window, for "load older"'s fast-lane page. Unlike ListMessages
// its cost does not grow with the folder.
type OlderLister interface {
	// ListBelow returns, newest first and with list meta, up to n of the
	// newest messages whose ids sort below the id below. It looks a bounded
	// number of times, so it may return fewer than n though more lie further
	// down; it never skips one: no server message sorts between the last
	// header returned and below but those returned (and any that left the
	// server meanwhile). A mailbox whose generation is not box.Generation
	// returns ErrGenerationChanged.
	ListBelow(ctx context.Context, box RemoteMailbox, below string, n int) (OlderPage, error)
}

// OlderPage is what ListBelow found.
type OlderPage struct {
	// Headers are the messages found, newest first, with list meta.
	Headers []Header
	// Bottom is set when nothing on the server sorts below the last header
	// (or below the id asked for, when there are none).
	Bottom bool
}

// OlderPageResult is what SyncOlderPage stored.
type OlderPageResult struct {
	// ToFetch is the ids it stored as stubs, newest first: they need bodies.
	ToFetch []string
	// Admitted is how many server messages the window floor moved over:
	// those stored and those that already had a row.
	Admitted int
	// HasOlder reports that the window still has a floor.
	HasOlder bool
}

// listModeOlder logs SyncOlderPage.
const listModeOlder = "older page"

// SyncOlderPage stores, insert only, up to n of the newest server messages
// just below the folder's window floor (OlderLister), and lowers the floor
// to the lowest of them it stored, so the window never covers mail without
// a row. It lists nothing in full and costs the same in any folder: flags
// of older rows, deletes and the cursor wait for a full list. It returns
// ErrNeedFullList when it cannot run: the adapter has no OlderLister, or the
// mailbox's generation changed. A folder without a floor has nothing below
// its window, and nothing is done.
func (e *Engine) SyncOlderPage(ctx context.Context, folder storage.Folder, n int) (OlderPageResult, error) {
	var res OlderPageResult
	lister, ok := e.adapter.(OlderLister)
	if !ok || n <= 0 {
		return res, ErrNeedFullList
	}
	started := time.Now()
	requestsBefore := e.requests()
	// The folder row and its floor only: the local rows this page needs are
	// read after the bounded remote page, by its ids, so the unit costs the
	// same in any folder. Whole-folder scans stay in the normal lane.
	folder, err := refreshFolder(ctx, e.store, folder)
	if err != nil {
		return res, err
	}
	state, err := loadFolderSyncState(ctx, e.store, folder)
	if err != nil {
		return res, err
	}
	if state.SyncFloorID == "" {
		return res, nil
	}
	if !folderLooksInitialized(state, 0) {
		has, err := e.states.FolderHasMessages(ctx, folder.ID)
		if err != nil {
			return res, err
		}
		if !has {
			return res, nil
		}
	}
	page, err := lister.ListBelow(ctx, RemoteMailbox{
		RemoteID:   folder.RemoteID,
		Generation: storedGeneration(folder.UIDValidity),
	}, state.SyncFloorID, n)
	if errors.Is(err, ErrGenerationChanged) {
		return res, ErrNeedFullList
	}
	if err != nil {
		return res, fmt.Errorf("sync: list older in %q: %w", folder.IMAPPath, err)
	}
	if len(page.Headers) > n {
		page.Headers, page.Bottom = page.Headers[:n], false
	}

	pageIDs := make([]string, 0, len(page.Headers))
	for _, h := range page.Headers {
		pageIDs = append(pageIDs, h.RemoteID)
	}
	localStates, err := e.states.MessageStatesByRemoteID(ctx, folder.ID, pageIDs)
	if err != nil {
		return res, fmt.Errorf("sync: load local states for older page in %q: %w", folder.IMAPPath, err)
	}
	local := make(map[string]struct{}, len(localStates))
	for _, s := range localStates {
		local[s.RemoteID] = struct{}{}
	}
	// Store newest first and stop at the first failure: the floor goes down
	// only over messages that have a row.
	fresh := make([]string, 0, len(page.Headers))
	byID := make(map[string]Header, len(page.Headers))
	for _, h := range page.Headers {
		if _, ok := local[h.RemoteID]; !ok && h.HasListMeta {
			fresh = append(fresh, h.RemoteID)
			byID[h.RemoteID] = h
		}
	}
	w := e.writeStubs(ctx, folder, fresh, byID, true)
	stored := w.ids
	failed := false
	var lowest Header
	next := 0 // w.stored[next] is the next stored stub in page order
	for _, h := range page.Headers {
		if _, ok := byID[h.RemoteID]; ok {
			if next == len(w.stored) || w.stored[next] != h.RemoteID {
				// Not stored: it failed, or a cancel came first (and
				// the cancel returns below).
				failed = true
				break
			}
			next++
			res.ToFetch = append(res.ToFetch, h.RemoteID)
		}
		lowest = h
		res.Admitted++
	}
	e.announce(folder, stored)
	if err := ctx.Err(); err != nil {
		return res, err
	}

	res.HasOlder = true
	switch {
	case !failed && page.Bottom:
		// Everything below the old floor has a row: the window is the
		// whole folder.
		lowest, res.HasOlder = Header{}, false
	case res.Admitted == 0:
		lowest.RemoteID = "" // the floor stays
	}
	if lowest.RemoteID != "" || !res.HasOlder {
		if err := e.store.SetFolderSyncWindow(ctx, folder.ID, lowest.RemoteID, true); err != nil {
			return res, err
		}
		if err := e.store.SetFolderSyncFloorUID(ctx, folder.ID, lowest.LegacyUID); err != nil {
			return res, err
		}
	}
	e.logListed(folder, listModeOlder, started, requestsBefore, len(res.ToFetch))
	if failed {
		return res, fmt.Errorf("sync: an older stub in %q was not stored", folder.IMAPPath)
	}
	return res, nil
}
