package sync

import (
	"context"
	"errors"
)

// ErrNeedFullList means an adapter cannot compute a delta from the stored
// cursor: there is none, the server no longer has its history, the mailbox
// was reset (IMAP UIDVALIDITY), or the
// server tracks no changes (IMAP without CONDSTORE). The engine then lists the
// folder in full.
var ErrNeedFullList = errors.New("sync: delta unavailable, full list needed")

// Delta is what changed in one mailbox since a stored cursor.
type Delta struct {
	// Changed holds messages created or updated since the cursor that are in
	// this mailbox now, newest first, plus the current state of every ensure id
	// still in the mailbox. Flags are the server's current flags.
	Changed []Header
	// Removed holds ids that left this mailbox or no longer exist. Ids the
	// cache never had are harmless.
	Removed []string
	// Members, when non-nil, is every message in the mailbox, with only
	// RemoteID and LegacyUID set. Protocols that cannot list members cheaply
	// leave it nil and report removals in Removed.
	Members []Header
	// Cursor is the adapter's position after this delta. The engine stores it
	// as the folder's state token.
	Cursor string
	// Unchanged means the server proved nothing in the mailbox changed. It is
	// only set when no ensure ids were asked for.
	Unchanged bool
}

// DeltaLister is an optional Adapter capability. ListChanges reports what
// changed in a mailbox since box.StateToken. ensure holds ids whose current server state must be reported even when
// unchanged: local flag changes and deletes that are not on the server yet,
// which reconcile can only merge against the server's real flags. It returns
// ErrNeedFullList when no delta can be computed.
type DeltaLister interface {
	ListChanges(ctx context.Context, box RemoteMailbox, ensure []string) (Delta, error)
}

// ListMetaFetcher is an optional Adapter capability for protocols whose list
// carries no envelope fields (IMAP). FetchListMeta returns headers with flags
// and HasListMeta set for the given ids; ids the server no longer has are left
// out.
type ListMetaFetcher interface {
	FetchListMeta(ctx context.Context, box RemoteMailbox, ids []string) ([]Header, error)
}

// RequestCounter is an optional Adapter capability that reports how many
// server round trips the adapter has made so far, for the sync log.
type RequestCounter interface {
	Requests() int64
}
