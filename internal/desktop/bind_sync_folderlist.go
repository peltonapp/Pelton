package desktop

import (
	"context"
	"time"
)

// listFolderOnce runs list unless a list of the same folder is already in
// flight on the account, by this runtime or one it replaced (the claims are
// the account's, see accountSessions). Then it only marks the folder dirty and returns nil
// at once, and the running list runs list once more when it ends, so a change
// the server reported meanwhile is still picked up. Any number of coalesced
// requests make one follow-up. Once the folder has a cursor that follow-up is
// a delta.
//
// A request that finds a list running returns without waiting, so a task
// never holds its session to wait for another. The follow-up runs inside the
// holder right after its list, on the same session.
//
// A cancelled list lets go of the folder without a follow-up, and so does a
// list that panics: the claims outlive the runtime, so one left behind would
// keep the folder from every later list of the account.
func (a *App) listFolderOnce(ctx context.Context, accountID, folderID int64, list func() error) error {
	fl := a.accountShared(accountID)
	if fl == nil {
		return list()
	}
	tok := fl.claimFolderList(folderID)
	if tok == 0 {
		return nil
	}
	defer fl.dropFolderList(folderID, tok) // a panic; a no-op once let go
	for {
		err := list()
		if ctx.Err() != nil {
			fl.dropFolderList(folderID, tok)
			return err
		}
		if !fl.releaseFolderList(folderID, tok) {
			return err
		}
	}
}

// Every holder of a folder-list claim installs a deferred
// dropFolderList(folderID, tok) right after it took the claim. On its normal
// paths the holder has let go by then and the deferred drop does nothing (a
// later holder's claim has another token); when a panic unwinds it, the drop
// frees the folder.

// claimFolderList marks a folder as being listed and returns the claim's
// token. When it already is, it marks it dirty instead and returns 0.
func (s *accountSessions) claimFolderList(folderID int64) uint64 {
	s.listMu.Lock()
	defer s.listMu.Unlock()
	return s.listing.claimOrMarkTok(folderID)
}

// claimFolderListNoMark marks a folder as being listed and returns the
// claim's token. When it already is, it returns 0 without marking it dirty.
func (s *accountSessions) claimFolderListNoMark(folderID int64) uint64 {
	s.listMu.Lock()
	defer s.listMu.Unlock()
	return s.listing.claimTok(folderID)
}

// folderListPoll is how often awaitFolderList looks whether the folder's
// running list has ended. Tests shorten it.
var folderListPoll = 100 * time.Millisecond

// awaitFolderList claims a folder for a list, waiting while another list of
// it runs, without marking it dirty. A full reconcile takes the folder this
// way: run beside a delta it would store the flags and cursor it read before
// the delta's newer ones.
// It returns the claim's token.
func (s *accountSessions) awaitFolderList(ctx context.Context, folderID int64) (uint64, error) {
	for {
		if tok := s.claimFolderListNoMark(folderID); tok != 0 {
			return tok, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(folderListPoll):
		}
	}
}

// releaseFolderList ends a folder's list. When the folder was marked dirty
// meanwhile it keeps the claim, clears the mark and returns true: the caller
// lists once more. tok is the holder's claim token: another holder's claim
// is left alone.
func (s *accountSessions) releaseFolderList(folderID int64, tok uint64) (again bool) {
	s.listMu.Lock()
	defer s.listMu.Unlock()
	return s.listing.releaseOwned(folderID, tok)
}

// dropFolderList ends the folder's list claim with token tok and forgets a
// dirty mark. Another holder's claim is left alone.
func (s *accountSessions) dropFolderList(folderID int64, tok uint64) {
	s.listMu.Lock()
	defer s.listMu.Unlock()
	s.listing.dropOwned(folderID, tok)
}
