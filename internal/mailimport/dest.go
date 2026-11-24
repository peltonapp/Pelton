package mailimport

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/rfc822"
	"github.com/peltonapp/Pelton/internal/storage"
)

// LocalDestination keeps imported mail on this computer: in an Imported
// folder of the Local Folders account, with the source's folders below it.
// Messages are stored through the same insert as synced mail, so a folder the
// user has put in a mail vault seals what lands in it.
type LocalDestination struct {
	store   *storage.DB
	account int64
}

// NewLocalDestination opens the Local Folders account, creating it on first
// use.
func NewLocalDestination(ctx context.Context, store *storage.DB) (*LocalDestination, error) {
	acc, err := store.EnsureLocalAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &LocalDestination{store: store, account: acc.ID}, nil
}

// Folder returns Imported/<path>, creating the levels that are missing.
func (d *LocalDestination) Folder(ctx context.Context, path []string) (DestFolder, error) {
	f, err := d.store.EnsureLocalFolderPath(ctx, d.account, append([]string{importedFolderName}, path...))
	if err != nil {
		return DestFolder{}, err
	}
	uid, err := d.store.NextLocalUID(ctx, f.ID)
	if err != nil {
		return DestFolder{}, err
	}
	return DestFolder{Key: "local:" + strconv.FormatInt(f.ID, 10), ID: f.ID, Mailbox: f.IMAPPath, nextUID: uid}, nil
}

// Deliver parses and stores one message with its attachments.
func (d *LocalDestination) Deliver(ctx context.Context, f *DestFolder, item Item) (int64, error) {
	parsed, err := rfc822.Parse(item.Raw)
	if err != nil {
		return 0, err
	}
	msg := messageFor(parsed, d.account, f.ID, f.nextUID, item.Flags, item.Received)
	id, err := d.store.InsertMessageWithAttachments(ctx, msg, attachmentsFor(parsed))
	if err != nil {
		return 0, err
	}
	f.nextUID++
	return id, nil
}

// Uploader appends messages to the user's own mail server.
type Uploader interface {
	// Delimiter is the server's folder hierarchy separator.
	Delimiter(ctx context.Context) (string, error)
	// EnsureMailbox creates a folder unless it exists.
	EnsureMailbox(ctx context.Context, name string) error
	// Append stores one message in a folder with its flags and arrival date.
	Append(ctx context.Context, mailbox string, raw []byte, flags storage.Flag, date time.Time) error
}

// errUpload marks a message the server would not take.
var errUpload = errors.New("mailimport: upload failed")

// UploadDestination puts imported mail into a folder of one of the user's
// accounts, at a limited rate so a large import does not trip the server's
// own limits.
type UploadDestination struct {
	up        Uploader
	accountID int64
	base      string
	delim     string
	limit     *Limiter
}

// NewUploadDestination uploads below base (a server folder path, empty for
// the top of the account) of the given account.
func NewUploadDestination(ctx context.Context, up Uploader, accountID int64, base string, limit *Limiter) (*UploadDestination, error) {
	delim, err := up.Delimiter(ctx)
	if err != nil {
		return nil, err
	}
	return &UploadDestination{up: up, accountID: accountID, base: base, delim: delim, limit: limit}, nil
}

// Folder maps a source path onto a server folder below the base, creating
// it. A separator inside a folder name is replaced so it does not add a
// level.
func (d *UploadDestination) Folder(ctx context.Context, path []string) (DestFolder, error) {
	var parts []string
	if d.base != "" {
		parts = append(parts, d.base)
	}
	for _, p := range path {
		if d.delim != "" {
			p = strings.ReplaceAll(p, d.delim, "_")
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		parts = []string{importedFolderName}
	}
	sep := d.delim
	if sep == "" {
		sep = "/"
	}
	mailbox := strings.Join(parts, sep)
	if err := d.up.EnsureMailbox(ctx, mailbox); err != nil {
		return DestFolder{}, err
	}
	return DestFolder{Key: fmt.Sprintf("account:%d:%s", d.accountID, mailbox), Mailbox: mailbox}, nil
}

// Deliver appends one message, keeping its flags and its date.
func (d *UploadDestination) Deliver(ctx context.Context, f *DestFolder, item Item) (int64, error) {
	if d.limit != nil {
		if err := d.limit.Wait(ctx, len(item.Raw)); err != nil {
			return 0, err
		}
	}
	date := item.Received
	if t, err := mail.ParseDate(headerValue(headerBlock(item.Raw), "Date")); err == nil {
		date = t
	}
	if err := d.up.Append(ctx, f.Mailbox, item.Raw, item.Flags, date); err != nil {
		return 0, fmt.Errorf("%w: %w", errUpload, err)
	}
	return 0, nil
}

// Limiter spaces uploads out: at most Rate bytes a second, and no closer
// together than Gap.
type Limiter struct {
	Rate int64
	Gap  time.Duration

	mu   sync.Mutex
	next time.Time
}

// Wait blocks until n more bytes may be sent, or ctx ends.
func (l *Limiter) Wait(ctx context.Context, n int) error {
	l.mu.Lock()
	now := time.Now()
	start := l.next
	if start.Before(now) {
		start = now
	}
	cost := l.Gap
	if l.Rate > 0 {
		cost = max(cost, time.Duration(int64(n)*int64(time.Second)/l.Rate))
	}
	l.next = start.Add(cost)
	l.mu.Unlock()
	wait := time.Until(start)
	if wait <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
