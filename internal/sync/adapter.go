package sync

import (
	"context"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Mailbox is one mailbox on the remote server.
type Mailbox struct {
	RemoteID   string
	Name       string
	ParentID   string // empty at the top
	Role       string // "\Inbox", "\Sent", "\Drafts", "\Trash", "\Junk", "\Archive", or ""
	Generation string // decimal UIDVALIDITY, or ""
	Selectable bool
}

// Header is the lightweight per-message view ListMessages returns.
type Header struct {
	RemoteID  string
	LegacyUID uint32 // IMAP adapter only; zero for adapters without uids
	Flags     storage.Flag

	// HasListMeta is true when the adapter filled the envelope fields below
	// (IMAP ENVELOPE). When it is false the engine skips
	// stub upsert and only stores the message after a full Fetch.
	HasListMeta   bool
	Subject       string
	From          string // display form, e.g. "Ada <ada@ex>" or "ada@ex"
	FromName      string
	To            string
	Date          time.Time
	Size          int64
	HasAttachment bool
	Preview       string // short text for the list row before the body arrives
}

// Attachment is a MIME part the adapter fetched with a message body.
type Attachment struct {
	Filename, ContentType, ContentID string
	Content                          []byte
}

// Fetched is a full message body returned by Adapter.Fetch.
type Fetched struct {
	RemoteID                                                                             string
	LegacyUID                                                                            uint32 // IMAP adapter only; zero for adapters without uids
	Flags                                                                                storage.Flag
	Raw                                                                                  []byte
	MessageID, Subject, From, To, Cc, Text, HTML, ListUnsubscribe, ReplyTo, CharsetGuess string
	Date                                                                                 time.Time
	Size                                                                                 int64
	ListUnsubscribePost                                                                  bool
	AuthResults                                                                          []string
	Attachments                                                                          []Attachment
}

// RemoteMailbox identifies a mailbox for ListMessages and ListChanges.
// StateToken is the stored cursor a delta starts from; a full list ignores it.
type RemoteMailbox struct {
	RemoteID   string
	StateToken string
	Generation string
}

// Adapter is the protocol-facing surface the sync engine drives. IMAP provides
// one; the engine never imports a protocol package.
type Adapter interface {
	// Addr returns the server address, used to label sync status.
	Addr() string
	// ListMailboxes returns every mailbox on the server with its parent and role.
	ListMailboxes(ctx context.Context) ([]Mailbox, error)
	// ListMessages lists the whole mailbox: every header newest first, plus
	// the state token and generation to store for the next call. It is always
	// a full list; deltas from a stored token go through DeltaLister. IMAP
	// reports a generation (UIDVALIDITY) and a CONDSTORE cursor when the
	// server has one; an adapter may report a state token and no generation. A changed
	// generation means the stored ids are no longer valid.
	ListMessages(ctx context.Context, box RemoteMailbox) (headers []Header, stateToken, generation string, err error)
	// Fetch may return both successful messages and an error. The engine stores
	// every returned message and carries the first error to the account result.
	Fetch(ctx context.Context, mailboxID string, remoteIDs []string) ([]Fetched, error)
	// SetFlags makes the server flags of one message match flags.
	SetFlags(ctx context.Context, mailboxID, remoteID string, flags storage.Flag) error
	// Move moves the messages from sourceMailboxID to destMailboxID.
	Move(ctx context.Context, sourceMailboxID string, remoteIDs []string, destMailboxID string) error
	// Delete permanently removes the messages from the mailbox.
	Delete(ctx context.Context, mailboxID string, remoteIDs []string) error
	// CreateMailbox creates name under parentID (top level when empty) and
	// returns the new mailbox.
	CreateMailbox(ctx context.Context, name, parentID string) (Mailbox, error)
	// RenameMailbox renames a mailbox in place.
	RenameMailbox(ctx context.Context, remoteID, name string) error
	// DeleteMailbox deletes a mailbox on the server.
	DeleteMailbox(ctx context.Context, remoteID string) error
}

// ColorSource is an optional Adapter capability that reports server-side flag
// colors keyed by remote message id for one mailbox.
type ColorSource interface {
	Colors(mailboxID string) map[string]int
}

// PagedLister is an optional Adapter capability. ListMessagesPaged returns the
// same snapshot as ListMessages, and calls onPage with each page of headers as
// soon as it is read, newest first, so the engine can store list stubs early.
// An error from onPage stops the list and is returned. A page delivered before
// a later failure is real server mail; the returned snapshot is only complete
// when err is nil.
type PagedLister interface {
	ListMessagesPaged(ctx context.Context, box RemoteMailbox, onPage func([]Header) error) (headers []Header, stateToken, generation string, err error)
}
