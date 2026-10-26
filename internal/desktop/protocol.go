package desktop

import (
	"context"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

// mailProtocol is the protocol-specific half of syncing and changing a
// mailbox. imapProtocol is the only driver; call sites go through
// protocolFor rather than calling the IMAP code directly.
type mailProtocol interface {
	// manualSync syncs the account as normal-lane tasks; manual (the user's
	// Sync) runs them at the top class, a timed sync at the ordinary ones.
	manualSync(ctx context.Context, account storage.Account, manual bool) error
	// olderStep runs one unit of a "load older" or raised-limit listing on
	// the task's session: the ids that need a body, and whether more units
	// follow.
	olderStep(ctx context.Context, account storage.Account, folder storage.Folder, older *olderList) ([]string, bool, error)
	// onDemandBodies fetches the bodies of messages the user opened, on the
	// fast-lane task's session.
	onDemandBodies(ctx context.Context, account storage.Account, folder storage.Folder, remoteIDs []string) error
	// bodyBatch fetches one batch of a folder's missing bodies and returns
	// syncsched.ErrAgain while more are missing.
	bodyBatch(ctx context.Context, account storage.Account, folder storage.Folder) error
	// planDownload lists the download tasks and the pinned ids since the cutoff.
	planDownload(ctx context.Context, account storage.Account, folders []storage.Folder, since time.Time) ([]dlTask, []int64, error)
	// firstSync is the worker's first pass: watch, initial sync.
	// It returns when the first pass is done; the worker then waits for ctx.
	firstSync(ctx context.Context, account storage.Account)
	// watch starts the account's push watcher on its scheduler.
	watch(ctx context.Context, account storage.Account)
	// needsTimedSync reports whether timed auto-sync should touch the account.
	needsTimedSync(accountID int64) bool
	// lockPerFolder reports whether syncFolders holds the account lock per folder.
	lockPerFolder() bool
	// limitNeedsBodies reports whether a changed sync limit leaves the folder
	// work to do. A store error is logged and counts as no.
	limitNeedsBodies(ctx context.Context, folder storage.Folder, newLimit int) bool
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
	// createFolder creates a mailbox named name under parent (nil: top level)
	// at path (see folderPath), then runs apply with its remote id to store
	// it. The folder ops run apply right after the server command, in the
	// same task, so a folder discovery (a barrier) never sees the server
	// changed and the store not. apply's error is returned; it does not
	// count against the session.
	createFolder(ctx context.Context, account storage.Account, parent *storage.Folder, name, path string, apply func(remoteID string) error) error
	// renameFolder renames folder to name, then runs apply (see createFolder);
	// newPath is its stored path with the last segment swapped (renamedPath).
	renameFolder(ctx context.Context, account storage.Account, folder storage.Folder, name, newPath string, apply func() error) error
	// deleteFolders deletes the mailboxes on the server, in the given order,
	// then runs apply (see createFolder).
	deleteFolders(ctx context.Context, account storage.Account, folders []storage.Folder, apply func() error) error
	// download fetches one account's grouped download tasks batch by batch.
	download(ctx context.Context, account storage.Account, groups map[int64]*folderTasks, includeAttachments bool, prog *dlProgress) error
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
