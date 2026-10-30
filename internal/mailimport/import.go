// Package mailimport reads mail out of files another client wrote and stores
// it locally: single .eml messages, .mbox archives, and Thunderbird's profile
// (its account settings and its on-disk Local Folders).
//
// The source is strictly read-only. Nothing here creates, rewrites, renames or
// deletes a file in another program's mail store, and nothing writes a cache or
// marker beside one: os.Open, os.ReadDir and os.Stat are the only filesystem
// calls in the package. That store often belongs to a client the user still
// runs, so an import must be safe to point at a live profile and safe to run
// twice. readonly_test.go fingerprints a profile before and after an import and
// fails on any difference, down to the modification times.
//
// Everything it produces lands in Pelton's Local Folders account, which sync
// never touches, so imported mail is never uploaded anywhere either. The
// package only reads paths the caller was given by the user; it never scans the
// home directory on its own except to look for Thunderbird profiles in their
// known locations, and it opens no network connection at all.
package mailimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/peltonapp/Pelton/internal/rfc822"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Importer writes parsed messages into the local store.
type Importer struct {
	store *storage.DB
	log   *slog.Logger
	// OnProgress, when set, is called after every message so a long import can
	// report where it is. It runs on the importing goroutine, so it must not
	// block.
	OnProgress func(Progress)
}

// Progress is one update from a running import.
type Progress struct {
	// Folder is the local folder currently being filled.
	Folder string
	// Imported and Skipped count messages across the whole import, not just
	// the current file. Skipped are the ones already present.
	Imported int
	Skipped  int
	// BytesDone and BytesTotal track the source files, which is the only
	// measure available up front: an mbox does not say how many messages it
	// holds without reading all of it.
	BytesDone  int64
	BytesTotal int64
	// FileIndex is which source is being read, counting from 1, out of
	// FileTotal. Bytes alone say how far along the import is; these say where
	// it is, which is what makes a long import feel like it is moving.
	FileIndex int
	FileTotal int
}

// Result summarises a finished import.
type Result struct {
	Imported int
	Skipped  int
	// Folders are the local folders that were written to, in the order they
	// were created.
	Folders []string
	// Failed counts messages that could not be parsed and were left out. The
	// import continues past them: one unreadable message in a large archive
	// should not cost the user the rest of it.
	Failed int
}

// New creates an importer over the local store. A nil logger is replaced with
// a discarding one.
func New(store *storage.DB, log *slog.Logger) *Importer {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Importer{store: store, log: log}
}

// Source is one file to import and the local folder it should land in.
type Source struct {
	Path   string
	Folder string
}

// Import reads every source in turn into the Local Folders account, creating
// it on first use. It reports progress as it goes and stops early if ctx is
// cancelled, keeping whatever it has already stored.
func (im *Importer) Import(ctx context.Context, sources []Source) (Result, error) {
	var result Result
	if len(sources) == 0 {
		return result, nil
	}

	account, err := im.store.EnsureLocalAccount(ctx)
	if err != nil {
		return result, err
	}

	total := totalBytes(sources)
	var done int64
	for i, source := range sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		base := progressBase{done: done, total: total, index: i + 1, count: len(sources)}
		if err := im.importFile(ctx, account.ID, source, &result, base); err != nil {
			return result, err
		}
		done += fileSize(source.Path)
	}
	return result, nil
}

// progressBase carries the byte counters that span the whole import, so a
// per-file callback can still report overall progress.
type progressBase struct {
	done  int64
	total int64
	// index is the 1-based position of the file being read, out of count.
	index int
	count int
}

func (im *Importer) importFile(ctx context.Context, accountID int64, source Source, result *Result, base progressBase) error {
	f, err := os.Open(source.Path)
	if err != nil {
		return fmt.Errorf("mailimport: open %s: %w", source.Path, err)
	}
	defer f.Close()

	folder, err := im.store.EnsureLocalFolder(ctx, accountID, source.Folder)
	if err != nil {
		return err
	}
	if !slices.Contains(result.Folders, folder.Name) {
		result.Folders = append(result.Folders, folder.Name)
	}
	uid, err := im.store.NextLocalUID(ctx, folder.ID)
	if err != nil {
		return err
	}

	messages, err := messageReader(f)
	if err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, err := messages.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		switch err := im.storeMessage(ctx, accountID, folder, uid, raw); {
		case errors.Is(err, errAlreadyStored):
			result.Skipped++
		case err != nil:
			im.log.Warn("skipping unreadable message", "file", source.Path, "err", err)
			result.Failed++
		default:
			result.Imported++
			uid++
		}

		if im.OnProgress != nil {
			im.OnProgress(Progress{
				Folder:     folder.Name,
				Imported:   result.Imported,
				Skipped:    result.Skipped,
				BytesDone:  base.done + messages.consumed(),
				BytesTotal: base.total,
				FileIndex:  base.index,
				FileTotal:  base.count,
			})
		}
	}
}

// errAlreadyStored marks a message the folder already holds, which is a skip
// rather than a failure.
var errAlreadyStored = errors.New("mailimport: message already imported")

func (im *Importer) storeMessage(ctx context.Context, accountID int64, folder storage.Folder, uid uint32, raw []byte) error {
	parsed, err := rfc822.Parse(raw)
	if err != nil {
		return err
	}
	duplicate, err := im.store.HasLocalMessage(ctx, folder.ID, parsed.MessageID)
	if err != nil {
		return err
	}
	if duplicate {
		return errAlreadyStored
	}

	message := &storage.Message{
		AccountID:           accountID,
		FolderID:            folder.ID,
		UID:                 uid,
		MessageID:           parsed.MessageID,
		InReplyTo:           parsed.InReplyTo,
		References:          parsed.References,
		Subject:             parsed.Subject,
		FromAddress:         parsed.From,
		ToAddresses:         parsed.To,
		CcAddresses:         parsed.Cc,
		Date:                parsed.Date,
		Flags:               importedFlags(parsed.Header.Get("X-Mozilla-Status")),
		BodyPlain:           parsed.Text,
		BodyHTML:            parsed.HTML,
		SizeBytes:           parsed.Size,
		ListUnsubscribe:     parsed.ListUnsubscribe,
		ListUnsubscribePost: parsed.ListUnsubscribePost,
		CharsetGuess:        parsed.CharsetGuess,
	}

	attachments := make([]storage.IncomingAttachment, 0, len(parsed.Attachments))
	for _, a := range parsed.Attachments {
		attachments = append(attachments, storage.IncomingAttachment{
			Filename:    a.Filename,
			ContentType: a.ContentType,
			ContentID:   a.ContentID,
			Content:     bytes.NewReader(a.Content),
		})
	}
	_, err = im.store.InsertMessageWithAttachments(ctx, message, attachments)
	return err
}

// Thunderbird records per-message state in an X-Mozilla-Status header, a hex
// bitmask it writes into the mbox itself.
const (
	mozillaStatusRead    = 0x0001
	mozillaStatusFlagged = 0x0004
)

// importedFlags maps Thunderbird's status header onto Pelton's flags. Mail
// with no status header (a plain .eml, or an archive from another client)
// imports as read: an archive is something the user already dealt with, and
// marking thousands of old messages unread would bury whatever is actually new.
func importedFlags(status string) storage.Flag {
	if status == "" {
		return storage.FlagSeen
	}
	bits, err := strconv.ParseUint(strings.TrimSpace(status), 16, 32)
	if err != nil {
		return storage.FlagSeen
	}
	var flags storage.Flag
	if bits&mozillaStatusRead != 0 {
		flags |= storage.FlagSeen
	}
	if bits&mozillaStatusFlagged != 0 {
		flags |= storage.FlagFlagged
	}
	return flags
}

// source is one file's worth of messages, whichever format it turned out to be.
type source interface {
	next() ([]byte, error)
	// consumed reports how many bytes of the file have been read, for progress.
	consumed() int64
}

// messageReader sniffs the format. An mbox begins with a "From " separator
// line; anything else is treated as a single message, which covers .eml
// whatever extension it was saved under.
func messageReader(f *os.File) (source, error) {
	head := make([]byte, len(fromPrefix))
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("mailimport: read %s: %w", f.Name(), err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("mailimport: rewind %s: %w", f.Name(), err)
	}

	counter := &countingReader{r: f}
	if n == len(fromPrefix) && bytes.Equal(head, fromPrefix) {
		return &mboxSource{reader: NewMboxReader(counter), counter: counter}, nil
	}
	return &emlSource{file: counter}, nil
}

type mboxSource struct {
	reader  *MboxReader
	counter *countingReader
}

func (s *mboxSource) next() ([]byte, error) { return s.reader.Next() }
func (s *mboxSource) consumed() int64       { return s.counter.n }

type emlSource struct {
	file *countingReader
	done bool
}

func (s *emlSource) next() ([]byte, error) {
	if s.done {
		return nil, io.EOF
	}
	s.done = true
	raw, err := io.ReadAll(s.file)
	if err != nil {
		return nil, fmt.Errorf("mailimport: read message: %w", err)
	}
	if len(raw) == 0 {
		return nil, io.EOF
	}
	return raw, nil
}

func (s *emlSource) consumed() int64 { return s.file.n }

// countingReader counts bytes read so progress can be reported against the
// file size, which is the only total known before the file is parsed.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func totalBytes(sources []Source) int64 {
	var total int64
	for _, s := range sources {
		total += fileSize(s.Path)
	}
	return total
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
