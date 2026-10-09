// Package syncsched is the per-account priority queue for mail sync.
// It does not import IMAP. Desktop injects each job's Run func.
// The scheduler checks a pool slot out around that func.
package syncsched

import "context"

// Priority is how urgently a job should run. Higher values run first.
type Priority int

const (
	// PriorityBackgroundReconcile is P3: a full re-list of a folder behind the
	// delta sync, as a safety net. It starts only when nothing else is queued.
	PriorityBackgroundReconcile Priority = iota
	// PriorityBackgroundBody is P2: body fill and delta body campaigns.
	PriorityBackgroundBody
	// PriorityBackgroundStubs is P1: list stubs.
	PriorityBackgroundStubs
	// PriorityLive is P0: new mail, manual Sync, and on-demand body.
	PriorityLive
)

// numPriorities sizes the per-priority counters.
const numPriorities = int(PriorityLive) + 1

// JobKind identifies the sync operation. The scheduler orders by Priority,
// not by kind.
type JobKind int

const (
	// JobListStubs lists a folder's message headers and stores list stubs.
	JobListStubs JobKind = iota
	// JobFetchBodies downloads bodies for stubs that have none yet.
	JobFetchBodies
	// JobNewMail checks an account for new mail after a push or poll.
	JobNewMail
	// JobManualSync is a user-requested sync of an account.
	JobManualSync
	// JobOnDemandBody downloads the bodies of specific messages the user opened.
	JobOnDemandBody
	// JobFullReconcile re-lists a whole folder and reconciles it in full.
	JobFullReconcile
)

// Job is one unit of sync work. Run should honor ctx cancellation: Stop
// cancels ctx for shutdown and account removal.
//
// RemoteIDs is the body or on-demand id list for this attempt. After
// ErrSoftPaused the scheduler replaces it with the ids that were not started,
// when the error carries them, and runs the job again. Read the attempt's
// ids with RemoteIDs(ctx). A plain ErrSoftPaused requeues the same list.
type Job struct {
	Priority  Priority
	Kind      JobKind
	FolderID  int64
	RemoteIDs []string
	Run       func(ctx context.Context) error
}

type remoteIDsKey struct{}

// RemoteIDs returns the remote ids for the job attempt running in ctx.
// The slice is a copy. It is nil when ctx is not a running job.
func RemoteIDs(ctx context.Context) []string {
	ids, _ := ctx.Value(remoteIDsKey{}).([]string)
	if len(ids) == 0 {
		return nil
	}
	return append([]string(nil), ids...)
}
