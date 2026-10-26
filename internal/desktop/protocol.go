package desktop

import (
	"context"
	"strings"

	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
	psync "github.com/peltonapp/Pelton/internal/sync"
)

// mailProtocol is the protocol-specific half of changing a mailbox.
// imapProtocol is the only driver; call sites go through protocolFor rather
// than calling the IMAP code directly.
type mailProtocol interface {
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
	// at path (see folderPath) and returns its remote id.
	createFolder(ctx context.Context, account storage.Account, parent *storage.Folder, name, path string) (string, error)
	// renameFolder renames folder to name; newPath is its stored path with
	// the last segment swapped (renamedPath).
	renameFolder(ctx context.Context, account storage.Account, folder storage.Folder, name, newPath string) error
	// deleteFolders deletes the mailboxes on the server, in the given order.
	deleteFolders(ctx context.Context, account storage.Account, folders []storage.Folder) error
	// withAdapter runs fn against a sync adapter for the account. The driver
	// takes the account lock itself, so callers must not hold it.
	withAdapter(account *storage.Account, fn func(psync.Adapter) error) error
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
