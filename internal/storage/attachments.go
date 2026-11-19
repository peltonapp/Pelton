package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// filePerm is used for attachment files written to disk.
	filePerm = 0o644

	// dupSeparator splits a filename stem from the counter we append when two
	// attachments in the same message share a name, e.g. "report (1).pdf".
	dupSuffixOpen  = " ("
	dupSuffixClose = ")"

	// fallbackFilename is used when sanitizing leaves nothing usable.
	fallbackFilename = "attachment"

	// maxDuplicateAttempts caps the dedupe counter so a pathological directory
	// cannot loop forever.
	maxDuplicateAttempts = 10000
)

// ErrAttachmentNotLocal is returned for an attachment whose bytes were never
// stored, because its message was cached without attachments.
var ErrAttachmentNotLocal = errors.New("storage: attachment not downloaded")

// Attachment is attachment metadata. The bytes live on disk at DiskPath, which
// is relative to the attachments root so the config dir stays portable.
type Attachment struct {
	ID          int64
	MessageID   int64
	Filename    string
	ContentType string
	SizeBytes   int64
	ContentID   string
	DiskPath    string
}

// ListAttachments returns the attachment rows for a message. A message in a
// vault the context may not see has none; a sealed one the context may see
// comes back with its names and types opened.
func (d *DB) ListAttachments(ctx context.Context, messageID int64) ([]Attachment, error) {
	_, env, _, err := d.sealedKey(ctx, messageID)
	if errors.Is(err, ErrMessageNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	attachments, err := attachmentRows(ctx, d.sql, messageID)
	if err != nil || env == nil {
		return attachments, err
	}
	for i := range attachments {
		if i >= len(env.Attachments) {
			return nil, ErrSealedTampered
		}
		meta := env.Attachments[i]
		attachments[i].Filename, attachments[i].ContentType = meta.Filename, meta.ContentType
		attachments[i].ContentID, attachments[i].SizeBytes = meta.ContentID, meta.Size
	}
	return attachments, nil
}

// OpenAttachment opens an attachment's bytes for reading. A sealed
// attachment opens only through a context its unlocked vault admits, and
// comes back decrypted in memory. The caller must close the returned reader.
func (d *DB) OpenAttachment(ctx context.Context, att Attachment) (io.ReadCloser, error) {
	if att.DiskPath == "" {
		return nil, ErrAttachmentNotLocal
	}
	full := filepath.Join(d.attachmentsDir, filepath.FromSlash(att.DiskPath))
	if idx, sealed := sealedFileIndex(att.DiskPath); sealed {
		mk, _, _, err := d.sealedKey(ctx, att.MessageID)
		if err != nil {
			return nil, err
		}
		if mk == nil {
			return nil, ErrSealedTampered
		}
		box, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("storage: open attachment %q: %w", att.DiskPath, err)
		}
		plain, err := mk.Open(box, attachmentBinding(att.MessageID, idx)...)
		if err != nil {
			return nil, ErrSealedTampered
		}
		return io.NopCloser(bytes.NewReader(plain)), nil
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("storage: open attachment %q: %w", att.DiskPath, err)
	}
	return f, nil
}

// DeleteAttachmentFilesForMessage removes the on disk attachment directory for a
// message. Call it after DeleteMessage, whose cascade only clears the rows.
func (d *DB) DeleteAttachmentFilesForMessage(accountID, messageID int64) error {
	dir := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(messageID))
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("storage: remove attachment dir for message %d: %w", messageID, err)
	}
	return nil
}

// DeleteAttachmentFilesForAccount removes the on disk attachment directory for a
// whole account. Call it after DeleteAccount, whose cascade only clears the rows.
func (d *DB) DeleteAttachmentFilesForAccount(accountID int64) error {
	dir := filepath.Join(d.attachmentsDir, accountSegment(accountID))
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("storage: remove attachment dir for account %d: %w", accountID, err)
	}
	return nil
}

// writeAttachmentFile writes content under
// attachmentsDir/{account_id}/{message_id}/{filename}, creating directories as
// needed, sanitizing the filename and resolving duplicates. It returns an
// Attachment carrying the relative DiskPath and the byte count.
func (d *DB) writeAttachmentFile(accountID, messageID int64, filename string, content io.Reader) (*Attachment, error) {
	dir := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(messageID))
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("storage: create attachment dir: %w", err)
	}

	safeName := sanitizeFilename(filename)
	finalName, err := uniqueFilename(dir, safeName)
	if err != nil {
		return nil, err
	}

	full := filepath.Join(dir, finalName)
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return nil, fmt.Errorf("storage: create attachment file %q: %w", finalName, err)
	}
	written, copyErr := io.Copy(f, content)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(full)
		return nil, fmt.Errorf("storage: write attachment %q: %w", finalName, copyErr)
	}
	if closeErr != nil {
		os.Remove(full)
		return nil, fmt.Errorf("storage: close attachment %q: %w", finalName, closeErr)
	}

	// store the path relative to the attachments root, using forward slashes so
	// the value is portable across operating systems.
	rel := filepath.ToSlash(filepath.Join(accountSegment(accountID), messageSegment(messageID), finalName))
	return &Attachment{
		Filename:  finalName,
		SizeBytes: written,
		DiskPath:  rel,
	}, nil
}

// removeAttachmentFiles deletes already written files during a rollback. Errors
// are ignored: cleanup is best effort and the rollback error is what matters.
func (d *DB) removeAttachmentFiles(relPaths []string) {
	for _, rel := range relPaths {
		os.Remove(filepath.Join(d.attachmentsDir, filepath.FromSlash(rel)))
	}
}

func insertAttachment(ctx context.Context, ex execer, a *Attachment) error {
	const query = `
INSERT INTO attachments (message_id, filename, content_type, size_bytes, content_id, disk_path)
VALUES (?, ?, ?, ?, ?, ?)`
	res, err := ex.ExecContext(ctx, query,
		a.MessageID, a.Filename, a.ContentType, a.SizeBytes, a.ContentID, a.DiskPath)
	if err != nil {
		return fmt.Errorf("storage: insert attachment %q: %w", a.Filename, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("storage: attachment insert id: %w", err)
	}
	a.ID = id
	return nil
}

func accountSegment(accountID int64) string {
	return strconv.FormatInt(accountID, 10)
}

func messageSegment(messageID int64) string {
	return strconv.FormatInt(messageID, 10)
}

// sanitizeFilename strips any directory components and path traversal so a
// malicious filename like "../../etc/passwd" cannot escape the message dir,
// and any characters or reserved names a Windows filesystem would reject, so
// the same attachment store works regardless of host OS (and stays usable if
// synced onto a Windows machine later).
func sanitizeFilename(name string) string {
	// drop everything up to the last path separator from either os convention.
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." || isWindowsReservedName(name) {
		return fallbackFilename
	}
	return name
}

func isWindowsReservedName(name string) bool {
	stem, _ := splitExt(name)
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// uniqueFilename appends " (n)" before the extension until the name is free in
// dir, handling two attachments on one message that share a filename.
func uniqueFilename(dir, name string) (string, error) {
	if !fileExists(filepath.Join(dir, name)) {
		return name, nil
	}
	stem, ext := splitExt(name)
	for i := 1; i <= maxDuplicateAttempts; i++ {
		candidate := stem + dupSuffixOpen + strconv.Itoa(i) + dupSuffixClose + ext
		if !fileExists(filepath.Join(dir, candidate)) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("storage: too many duplicate attachments named %q", name)
}

func splitExt(name string) (stem, ext string) {
	ext = filepath.Ext(name)
	return name[:len(name)-len(ext)], ext
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
