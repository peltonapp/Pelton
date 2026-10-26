package sync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// ErrNotOnServer marks a Fetch failure for a message the server no longer has,
// as opposed to one that could work on a later try.
var ErrNotOnServer = errors.New("sync: message not on the server")

// ErrGenerationChanged means the mailbox's generation (IMAP UIDVALIDITY) is
// not the one the caller's remote ids belong to: the ids may now name other
// messages. Nothing was fetched or changed. A campaign working from stored
// ids must stop; the next full list resets the folder.
var ErrGenerationChanged = errors.New("sync: mailbox generation changed")

// ErrRefused marks the server answering a request with a refusal (IMAP NO or
// BAD), as opposed to a transport failure or a local error. The connection is
// still good; the request itself was not accepted. Adapters wrap such errors
// so errors.Is(err, ErrRefused) holds and the original text is kept.
var ErrRefused = errors.New("sync: server refused the request")

type generationKey struct{ mailboxID string }

// WithGeneration tells the adapter which generation the remote ids of
// mailboxID used under ctx belong to, for the calls that take no
// RemoteMailbox (Fetch, SetFlags, FetchChunk). An adapter that selects the
// mailbox compares and returns ErrGenerationChanged on a mismatch. An empty
// generation checks nothing.
func WithGeneration(ctx context.Context, mailboxID, generation string) context.Context {
	if generation == "" {
		return ctx
	}
	return context.WithValue(ctx, generationKey{mailboxID}, generation)
}

// ExpectedGeneration returns the generation WithGeneration put on ctx for
// mailboxID, or "".
func ExpectedGeneration(ctx context.Context, mailboxID string) string {
	g, _ := ctx.Value(generationKey{mailboxID}).(string)
	return g
}

// folderContext carries the folder's stored generation for the adapter.
func folderContext(ctx context.Context, folder storage.Folder) context.Context {
	return WithGeneration(ctx, folder.RemoteID, storedGeneration(folder.UIDValidity))
}

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
	// Messages the adapter could not read are reported as a *MessageError, so
	// the engine goes on with the next batch.
	Fetch(ctx context.Context, mailboxID string, remoteIDs []string) ([]Fetched, error)
	// SetFlags adds flags to one message on the server and never removes
	// any: the sync pushes the union of local and server flags, so a flag
	// the server has that flags lacks was set by another client after the
	// snapshot (see the policy in reconcile.go). The name is kept for the
	// implementations outside this package.
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

// MessageError is a Fetch error that concerns single messages, such as a body
// that does not parse, while the connection is fine. Those messages stay
// incomplete stubs and are tried again on a later sync.
type MessageError struct {
	RemoteIDs []string
	Err       error
}

func (e *MessageError) Error() string {
	return fmt.Sprintf("sync: %d messages could not be read (%s): %v", len(e.RemoteIDs), strings.Join(e.RemoteIDs, ", "), e.Err)
}

func (e *MessageError) Unwrap() error { return e.Err }

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

// ChunkFetcher is an optional Adapter capability for a message too large to
// fetch in one piece. FetchChunk returns up to length bytes of the source
// from offset in Raw, with the message's flags and envelope fields; fewer
// bytes than length mean the end. A message the server no longer has
// returns ErrNotOnServer. ParseChunked fills the body fields of a Fetched
// whose Raw is the whole source.
type ChunkFetcher interface {
	FetchChunk(ctx context.Context, mailboxID, remoteID string, offset, length int64) (Fetched, error)
	ParseChunked(f Fetched) (Fetched, error)
}
