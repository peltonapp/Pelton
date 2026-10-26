package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/peltonapp/Pelton/internal/storage"
)

// PartialBody is one message being fetched in chunks. It lives in memory
// only: after a restart the stub is still incomplete and is fetched again.
type PartialBody struct {
	RemoteID   string
	head       Fetched // flags and envelope from the latest chunk
	raw        []byte
	generation string // the folder generation the first chunk came from
	rowID      int64  // the stub the first chunk was for; 0 when it had none
}

// NewPartialBody starts a chunked fetch of remoteID.
func NewPartialBody(remoteID string) *PartialBody {
	return &PartialBody{RemoteID: remoteID}
}

// Fetched is how many bytes are in so far.
func (p *PartialBody) Fetched() int64 { return int64(len(p.raw)) }

// ErrNoChunks means the adapter cannot fetch a message in chunks.
var ErrNoChunks = errors.New("sync: adapter has no partial fetch")

// FetchBodyChunk fetches the next size bytes of p's message. A short chunk
// that reaches the stub's known size (or any short chunk when the size is
// unknown) ends the message: it is parsed and fills its stub (never inserts), done is
// true and res names the stored row. A message gone from the server is done
// with nothing stored. A changed generation (UIDVALIDITY) is done with
// ErrGenerationChanged and nothing stored: the caller must stop. One call is one command, so a caller can let other
// work run between chunks.
func (e *Engine) FetchBodyChunk(ctx context.Context, folder storage.Folder, p *PartialBody, size int64) (done bool, res FolderSyncResult, err error) {
	cf, ok := e.adapter.(ChunkFetcher)
	if !ok {
		return false, res, ErrNoChunks
	}
	// Chunks from two generations would join two different messages. The
	// folder row may have been reset between calls, and the adapter checks
	// the mailbox against the row; either way the partial body is dropped.
	gen := storedGeneration(folder.UIDValidity)
	if p.Fetched() > 0 && gen != p.generation {
		return true, res, fmt.Errorf("sync: chunked fetch of %q: %w", p.RemoteID, ErrGenerationChanged)
	}
	if p.Fetched() == 0 {
		// The parts belong to this row: a stub listed again for the id
		// later, even in the same generation, does not take them.
		p.rowID = 0
		if id, err := e.store.MessageIDByRemoteID(ctx, folder.ID, p.RemoteID); err == nil {
			p.rowID = id
		} else if !errors.Is(err, storage.ErrMessageNotFound) {
			return false, res, fmt.Errorf("sync: chunked fetch of %q: %w", p.RemoteID, err)
		}
	}
	chunk, err := cf.FetchChunk(folderContext(ctx, folder), folder.RemoteID, p.RemoteID, p.Fetched(), size)
	if errors.Is(err, ErrNotOnServer) {
		return true, res, nil
	}
	if errors.Is(err, ErrGenerationChanged) {
		return true, res, fmt.Errorf("sync: fetch chunk of %q at %d: %w", p.RemoteID, p.Fetched(), err)
	}
	if err != nil {
		return false, res, fmt.Errorf("sync: fetch chunk of %q at %d: %w", p.RemoteID, p.Fetched(), err)
	}
	if p.Fetched() == 0 {
		p.generation = gen
	}
	got := int64(len(chunk.Raw))
	p.raw = append(p.raw, chunk.Raw...)
	chunk.Raw = nil
	p.head = chunk
	if got >= size {
		return false, res, nil
	}
	// A short chunk ends the message, unless the stub's size (RFC822.SIZE)
	// says there is more: a server may cap a partial fetch below what was
	// asked, and taking that for the end would store a truncated body as
	// complete. Only an empty chunk short of the size ends it anyway (the
	// size was wrong); that is logged.
	if want := e.stubSize(ctx, folder, p.RemoteID); want > 0 && p.Fetched() < want {
		if got > 0 {
			return false, res, nil
		}
		e.log.Warn("chunked body shorter than its size", "folder", folder.IMAPPath,
			"remote_id", p.RemoteID, "fetched", p.Fetched(), "size", want)
	}

	msg := p.head
	msg.Raw = p.raw
	msg, err = cf.ParseChunked(msg)
	if err != nil {
		return true, res, fmt.Errorf("sync: chunked message in %q: %w", folder.IMAPPath, err)
	}
	// A folder reset since the first chunk, or a replaced row, drops the
	// body here (id 0), in the transaction that would have written it.
	id, err := e.storeMessage(ctx, folder, msg, false, p.rowID)
	if err != nil {
		return true, res, err
	}
	if id != 0 {
		res.New = 1
		res.NewIDs = []int64{id}
		e.announce(folder, res.NewIDs)
	}
	return true, res, nil
}

// stubSize is the size the stub for remoteID was listed with, or 0 when
// unknown.
func (e *Engine) stubSize(ctx context.Context, folder storage.Folder, remoteID string) int64 {
	id, err := e.store.MessageIDByRemoteID(ctx, folder.ID, remoteID)
	if err != nil {
		return 0
	}
	m, err := e.store.GetMessage(ctx, id)
	if err != nil {
		return 0
	}
	return m.SizeBytes
}
