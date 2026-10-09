package desktop

import (
	"context"
	"time"

	"github.com/emersion/go-imap/v2"

	pimap "github.com/peltonapp/Pelton/internal/imap"
)

// mailClient is the slice of the imap client's public surface the bindings use.
// internal/sync has had the same seam since it was written, which is why its
// reconcile, backfill and delete paths can be driven by a fake; the bindings
// called pimap.Connect directly, so a test could reach a binding's validation
// checks and nothing past them.
//
// It is a superset of the IMAP surface the sync adapter wraps, so a connection
// opened here can still be handed to imap.NewAdapter for psync.NewEngine.
type mailClient interface {
	Login() error
	Logout() error
	Close() error
	// Addr is the server the client is talking to, host and port, for the log
	// lines the debug overlay shows.
	Addr() string

	Select(mailbox string) (*pimap.Mailbox, error)
	ListFolders() ([]pimap.Folder, error)
	CreateFolder(path string) error
	RenameFolder(path, newPath string) error
	DeleteFolder(path string) error

	// AppendToSent puts a copy of a message that has just been sent in the
	// account's Sent folder, and reports which folder it used.
	AppendToSent(raw []byte) (string, error)

	FetchMessages(uids []imap.UID, fn func(uid imap.UID, msg *pimap.Message, err error) error) error
	FetchRawMessage(uid imap.UID) ([]byte, error)
	FetchAllFlags() ([]pimap.MessageHeader, error)
	FetchFlagsChangedSince(modSeq uint64) ([]pimap.MessageHeader, error)
	SearchAllUIDs() ([]imap.UID, error)
	FetchHeaders(uids []imap.UID, options *imap.FetchOptions) ([]pimap.MessageHeader, error)

	AddFlags(uid imap.UID, flags ...imap.Flag) error
	RemoveFlags(uid imap.UID, flags ...imap.Flag) error
	Move(uid imap.UID, mailbox string) error
	MoveMessages(uids []imap.UID, mailbox string) error
	DeleteMessages(uids ...imap.UID) error

	SearchByMessageID(messageID string) ([]imap.UID, error)
	SearchSince(since time.Time) ([]imap.UID, error)

	SupportsIdle() bool
	IdleUntil(ctx context.Context) (bool, error)
}

var _ mailClient = (*pimap.Client)(nil)

// connectIMAP opens a connection for cfg. It is the single place the bindings
// reach the network, so a test can substitute newIMAPClient and drive the rest
// of a binding without a server.
func (a *App) connectIMAP(cfg pimap.Config) (mailClient, error) {
	if a.newIMAPClient != nil {
		return a.newIMAPClient(cfg)
	}
	client, err := pimap.Connect(cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}
