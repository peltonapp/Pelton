package storage

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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

	// cacheStagingDirName holds attachment directories renamed aside before a
	// cache-delete transaction so a rollback can restore them.
	cacheStagingDirName = ".cache-staging"
)

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

// ListAttachments returns the attachment rows for a message.
func (d *DB) ListAttachments(ctx context.Context, messageID int64) ([]Attachment, error) {
	const query = `
SELECT id, message_id, filename, content_type, size_bytes, content_id, disk_path
FROM attachments WHERE message_id = ? ORDER BY id`
	rows, err := d.sql.QueryContext(ctx, query, messageID)
	if err != nil {
		return nil, fmt.Errorf("storage: list attachments for message %d: %w", messageID, err)
	}
	defer rows.Close()

	var attachments []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType,
			&a.SizeBytes, &a.ContentID, &a.DiskPath); err != nil {
			return nil, fmt.Errorf("storage: scan attachment: %w", err)
		}
		attachments = append(attachments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate attachments: %w", err)
	}
	return attachments, nil
}

// OpenAttachment opens an attachment file for reading given its stored relative
// DiskPath. The caller must close the returned reader.
func (d *DB) OpenAttachment(diskPath string) (io.ReadCloser, error) {
	full := filepath.Join(d.attachmentsDir, filepath.FromSlash(diskPath))
	f, err := os.Open(full)
	if err != nil {
		return nil, fmt.Errorf("storage: open attachment %q: %w", diskPath, err)
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

// stagedPath is one attachment directory renamed into the staging tree, with
// enough information to rename it back on rollback.
type stagedPath struct {
	live   string
	staged string
}

// withStagedMessageDirs renames each message's attachment directory into a
// staging tree, runs fn inside a transaction, restores the dirs on any failure,
// and removes the staging tree after a successful commit.
func (d *DB) withStagedMessageDirs(ctx context.Context, accountID int64, messageIDs []int64, fn func(tx *sql.Tx) error) error {
	staged, stagingRoot, err := d.stageMessageDirs(accountID, messageIDs)
	if err != nil {
		return err
	}
	return d.commitStagedDelete(ctx, staged, stagingRoot, fn)
}

// TestingSetDeleteTxHook injects a callback that runs after staging and before
// commit. Tests use it to force a rollback; production leaves it unset.
func (d *DB) TestingSetDeleteTxHook(fn func(ctx context.Context) error) {
	d.deleteTxHook = fn
}

// withStagedAccountCache renames the whole account attachment directory aside,
// runs fn inside a transaction, restores on failure, and removes staging after
// commit.
func (d *DB) withStagedAccountCache(ctx context.Context, accountID int64, fn func(tx *sql.Tx) error) error {
	live := filepath.Join(d.attachmentsDir, accountSegment(accountID))
	if _, err := os.Stat(live); err != nil {
		if os.IsNotExist(err) {
			return d.commitStagedDelete(ctx, nil, "", fn)
		}
		return fmt.Errorf("storage: stat account attachment dir %d: %w", accountID, err)
	}
	stagingRoot, err := os.MkdirTemp(d.attachmentsDir, cacheStagingDirName+"-")
	if err != nil {
		return fmt.Errorf("storage: create cache staging dir: %w", err)
	}
	staged := filepath.Join(stagingRoot, accountSegment(accountID))
	if err := os.Rename(live, staged); err != nil {
		os.RemoveAll(stagingRoot)
		return fmt.Errorf("storage: stage account attachment dir %d: %w", accountID, err)
	}
	return d.commitStagedDelete(ctx, []stagedPath{{live: live, staged: staged}}, stagingRoot, fn)
}

func (d *DB) stageMessageDirs(accountID int64, messageIDs []int64) ([]stagedPath, string, error) {
	var toStage []int64
	for _, id := range messageIDs {
		live := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(id))
		if _, err := os.Stat(live); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("storage: stat attachment dir for message %d: %w", id, err)
		}
		toStage = append(toStage, id)
	}
	if len(toStage) == 0 {
		return nil, "", nil
	}
	stagingRoot, err := os.MkdirTemp(d.attachmentsDir, cacheStagingDirName+"-")
	if err != nil {
		return nil, "", fmt.Errorf("storage: create cache staging dir: %w", err)
	}
	staged := make([]stagedPath, 0, len(toStage))
	for _, id := range toStage {
		live := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(id))
		dest := filepath.Join(stagingRoot, accountSegment(accountID), messageSegment(id))
		if err := os.MkdirAll(filepath.Dir(dest), dirPerm); err != nil {
			d.restoreStaged(staged)
			os.RemoveAll(stagingRoot)
			return nil, "", fmt.Errorf("storage: create staging path for message %d: %w", id, err)
		}
		if err := os.Rename(live, dest); err != nil {
			d.restoreStaged(staged)
			os.RemoveAll(stagingRoot)
			return nil, "", fmt.Errorf("storage: stage attachment dir for message %d: %w", id, err)
		}
		staged = append(staged, stagedPath{live: live, staged: dest})
	}
	return staged, stagingRoot, nil
}

func (d *DB) commitStagedDelete(ctx context.Context, staged []stagedPath, stagingRoot string, fn func(tx *sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		d.restoreStaged(staged)
		return fmt.Errorf("storage: begin staged delete: %w", err)
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		d.restoreStaged(staged)
		return err
	}
	if d.deleteTxHook != nil {
		if err := d.deleteTxHook(ctx); err != nil {
			d.restoreStaged(staged)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		d.restoreStaged(staged)
		return fmt.Errorf("storage: commit staged delete: %w", err)
	}
	if stagingRoot != "" {
		if err := os.RemoveAll(stagingRoot); err != nil {
			// Best-effort: retain the path in the log for later cleanup.
			slog.Warn("storage: remove cache staging dir", "path", stagingRoot, "err", err)
		}
	}
	return nil
}

func (d *DB) restoreStaged(staged []stagedPath) {
	for _, s := range slices.Backward(staged) {

		_ = os.MkdirAll(filepath.Dir(s.live), dirPerm)
		_ = os.Rename(s.staged, s.live)
	}
}

// commitStagedReplace runs fn after message attachment dirs were renamed
// aside. fn may write a new directory at the live path. On any failure the
// new files are removed and the staged dirs are restored. After commit the
// staging tree (the previous files) is deleted.
func (d *DB) commitStagedReplace(ctx context.Context, accountID, messageID int64, staged []stagedPath, stagingRoot string, fn func(tx *sql.Tx) (written []string, err error)) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		d.restoreStaged(staged)
		return fmt.Errorf("storage: begin staged replace: %w", err)
	}
	defer tx.Rollback()

	written, err := fn(tx)
	if err != nil {
		d.abandonStagedReplace(accountID, messageID, written, staged)
		return err
	}
	if d.deleteTxHook != nil {
		if err := d.deleteTxHook(ctx); err != nil {
			d.abandonStagedReplace(accountID, messageID, written, staged)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		d.abandonStagedReplace(accountID, messageID, written, staged)
		return fmt.Errorf("storage: commit staged replace: %w", err)
	}
	if stagingRoot != "" {
		if err := os.RemoveAll(stagingRoot); err != nil {
			slog.Warn("storage: remove cache staging dir", "path", stagingRoot, "err", err)
		}
	}
	return nil
}

// abandonStagedReplace drops files written for a fill that did not commit
// and moves the staged attachment directory back to its live path.
func (d *DB) abandonStagedReplace(accountID, messageID int64, written []string, staged []stagedPath) {
	d.removeAttachmentFiles(written)
	if len(staged) == 0 {
		return
	}
	// The live directory was recreated for the new files. It has to be gone
	// before the staged directory can be renamed back onto that path.
	live := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(messageID))
	_ = os.RemoveAll(live)
	d.restoreStaged(staged)
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
	if _, last, ok := strings.CutLast(name, "/"); ok {
		name = last
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
