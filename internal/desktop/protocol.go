package desktop

import (
	"context"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/desktop/syncsched"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// mailProtocol is the protocol-specific half of syncing and changing a
// mailbox. imapProtocol is the only driver; call sites go through
// protocolFor rather than calling the IMAP code directly.
type mailProtocol interface {
	// manualSync refreshes the newest mail on the live slot (syncAccountOnceCtx).
	manualSync(ctx context.Context, account storage.Account) error
	// backfillStep runs one stub or body job of a folder backfill.
	backfillStep(ctx context.Context, account storage.Account, folder storage.Folder, kind syncsched.JobKind, batch, bodyLimit int, stats *imapStepStats) ([]string, error)
	// onDemandBodies fetches the bodies the user asked for in a folder.
	onDemandBodies(ctx context.Context, account storage.Account, folder storage.Folder) error
	// reconcileBodies fetches the bodies a full reconcile left missing.
	reconcileBodies(ctx context.Context, account storage.Account, folder storage.Folder) error
	// planDownload lists the download tasks and the pinned ids since the cutoff.
	planDownload(ctx context.Context, account storage.Account, folders []storage.Folder, since time.Time) ([]dlTask, []int64, error)
	// backfillBodyLimit is the body cap of one backfill step (0 = none).
	backfillBodyLimit(ctx context.Context, folder storage.Folder, kind syncsched.JobKind, batch int) (int, error)
	// firstSync is the worker's first pass: watch, initial sync, due reconcile.
	// It returns when the first pass is done; the worker then waits for ctx.
	firstSync(ctx context.Context, account storage.Account)
	// watch parks the account on imap idle until ctx ends.
	watch(ctx context.Context, account storage.Account)
	// needsTimedSync reports whether timed auto-sync should touch the account.
	needsTimedSync(accountID int64) bool
	// lockPerFolder reports whether syncFolders holds the account lock per folder.
	lockPerFolder() bool
	// limitNeedsBodies reports whether a changed sync limit leaves the folder
	// work to do. A store error is logged and counts as no.
	limitNeedsBodies(ctx context.Context, folder storage.Folder, newLimit int) bool
	// withListEngine runs fn with the engine that lists the account's folders
	// for a full reconcile.
	withListEngine(ctx context.Context, account storage.Account, fn func(*psync.Engine) error) error
	// transmit sends one queued outbox message for the account.
	transmit(ctx context.Context, account storage.Account, m outbox.Message) error
	// moveMessage moves a cached message to dest on the server, then drops the
	// local row (the core of archive and move). The driver takes the account
	// lock itself, so callers must not hold it.
	moveMessage(m *storage.Message, source, dest storage.Folder, account storage.Account) (ArchiveUndoDTO, error)
	// moveBack undoes a move: from is where the action put the message.
	moveBack(rfcMessageID string, from, dest storage.Folder, account storage.Account) error
	// source fetches a message's raw RFC 822 source.
	source(m storage.Message, folder storage.Folder, account storage.Account) (string, error)
	// setColor reflects a colour change onto the server. Failures are logged only.
	setColor(m storage.Message, folder storage.Folder, account storage.Account, color int)
	// withAdapter runs fn against a sync adapter for the account. The caller
	// holds the account lock.
	withAdapter(account *storage.Account, fn func(psync.Adapter) error) error
	// download fetches one account's grouped download tasks batch by batch.
	download(ctx context.Context, account storage.Account, groups map[int64]*folderTasks, includeAttachments bool, done *int, total int, start time.Time) error
	// checkPassword tries a typed password against the server without storing
	// it. A refusal and an unreachable server are results, not errors.
	checkPassword(account storage.Account, password string) PasswordCheckDTO
	// routeServers is the servers a route test connects to.
	routeServers(account storage.Account, requested []routeServer) ([]routeServer, error)
	// discoverFolders lists the server's mailboxes into folder rows.
	discoverFolders(ctx context.Context, account storage.Account) error
	// folderPath is the path of a new folder named name under parent. IMAP
	// refuses a parent on a flat mailbox list.
	folderPath(parent storage.Folder, name string) (string, error)
	// createdPath is the path stored for a folder the server created, given
	// the path folderPath chose and the remote id the server returned.
	createdPath(path, remoteID string) string
	// renameKeepsPath reports whether a rename leaves the stored path alone.
	renameKeepsPath(folder storage.Folder, remoteID string) bool
}

// routeServer is one server a route test connects to.
type routeServer struct {
	name string
	host string
	port int
}

// imapProtocol carries the App so its methods can use its store, logger,
// locks and test hooks.
type imapProtocol struct{ a *App }

// protocolFor returns the driver for the account's mail protocol.
func (a *App) protocolFor(storage.Account) mailProtocol {
	return imapProtocol{a}
}

// knownProtocol reports whether p names a protocol Pelton can use.
func knownProtocol(p string) bool {
	return normalizeProtocol(p) == "imap"
}

// normalizeProtocol lowercases p and trims it; empty means imap.
func normalizeProtocol(p string) string {
	p = strings.TrimSpace(strings.ToLower(p))
	if p == "" {
		return "imap"
	}
	return p
}
