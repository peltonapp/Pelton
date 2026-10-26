package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	// stagingLockName is the file in a staging tree that its owner holds
	// locked from creation until the tree is gone, so a recovery can tell a
	// change in flight from one whose process died.
	stagingLockName = ".owner"
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

// withStagedMessageDirs begins a transaction, renames each message's
// attachment directory into a staging tree, runs fn, restores the dirs on any
// failure, and removes the staging tree after a successful commit. The
// transaction is immediate (see dataSourceName), so it holds the write lock
// before any directory moves: a concurrent fill or delete of the same message
// waits instead of staging files this change is about to commit.
func (d *DB) withStagedMessageDirs(ctx context.Context, accountID int64, messageIDs []int64, fn func(tx *sql.Tx) error) error {
	return d.withStagedMessageDirsTx(ctx, accountID, func(*sql.Tx) ([]int64, error) { return messageIDs, nil }, fn)
}

// withStagedMessageDirsTx is withStagedMessageDirs with the ids read inside
// the transaction, so a message added after the caller looked cannot be
// deleted with its directory left behind.
func (d *DB) withStagedMessageDirsTx(ctx context.Context, accountID int64, ids func(tx *sql.Tx) ([]int64, error), fn func(tx *sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin staged delete: %w", err)
	}
	defer tx.Rollback()
	messageIDs, err := ids(tx)
	if err != nil {
		return err
	}
	staged, tree, err := d.stageMessageDirs(accountID, messageIDs)
	if err != nil {
		return err
	}
	return d.commitStagedDelete(ctx, tx, staged, tree, fn)
}

// TestingSetDeleteTxHook injects a callback that runs after staging and before
// commit. Tests use it to force a rollback; production leaves it unset.
func (d *DB) TestingSetDeleteTxHook(fn func(ctx context.Context) error) {
	d.deleteTxHook = fn
}

// TestingSetBodyFillLookupHook injects a callback that runs after a body fill
// looked up its row and before its write transaction begins.
func (d *DB) TestingSetBodyFillLookupHook(fn func()) {
	d.bodyFillLookupHook = fn
}

// withStagedAccountCache begins a transaction, renames the whole account
// attachment directory aside, runs fn, restores on failure, and removes
// staging after commit.
func (d *DB) withStagedAccountCache(ctx context.Context, accountID int64, fn func(tx *sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin staged delete: %w", err)
	}
	defer tx.Rollback()
	live := filepath.Join(d.attachmentsDir, accountSegment(accountID))
	if _, err := os.Stat(live); err != nil {
		if os.IsNotExist(err) {
			return d.commitStagedDelete(ctx, tx, nil, nil, fn)
		}
		return fmt.Errorf("storage: stat account attachment dir %d: %w", accountID, err)
	}
	tree, err := d.newStagingTree()
	if err != nil {
		return err
	}
	staged := filepath.Join(tree.path, accountSegment(accountID))
	if err := os.Rename(live, staged); err != nil {
		tree.remove()
		return fmt.Errorf("storage: stage account attachment dir %d: %w", accountID, err)
	}
	return d.commitStagedDelete(ctx, tx, []stagedPath{{live: live, staged: staged}}, tree, fn)
}

// stagingTree is a staging root with the lock its owner holds on it. A nil
// tree is no staging at all.
type stagingTree struct {
	path string
	lock *os.File
}

// newStagingTree creates a staging root and locks it for this change.
func (d *DB) newStagingTree() (*stagingTree, error) {
	path, err := os.MkdirTemp(d.attachmentsDir, cacheStagingDirName+"-")
	if err != nil {
		return nil, fmt.Errorf("storage: create cache staging dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(path, stagingLockName), os.O_RDWR|os.O_CREATE|os.O_EXCL, filePerm)
	if err == nil {
		var busy bool
		if busy, err = tryLockFile(f); busy {
			err = errors.New("locked by another process")
		}
		if err != nil {
			f.Close()
		}
	}
	if err != nil {
		os.RemoveAll(path)
		return nil, fmt.Errorf("storage: lock cache staging dir: %w", err)
	}
	return &stagingTree{path: path, lock: f}, nil
}

// remove deletes the tree after its change committed or was fully restored,
// then drops the lock. The staged dirs go first and the lock file last, so a
// recovery never finds staged dirs without a held lock. Where an open file
// cannot be deleted (Windows) the lock file goes after the lock is dropped.
func (t *stagingTree) remove() {
	if t == nil {
		return
	}
	entries, _ := os.ReadDir(t.path)
	for _, e := range entries {
		if e.Name() != stagingLockName {
			os.RemoveAll(filepath.Join(t.path, e.Name()))
		}
	}
	err := os.RemoveAll(t.path)
	t.lock.Close()
	if err != nil {
		err = os.RemoveAll(t.path)
	}
	if err != nil {
		// Best-effort: retain the path in the log for later cleanup.
		slog.Warn("storage: remove cache staging dir", "path", t.path, "err", err)
	}
}

// keep drops the lock and leaves the tree for recoverCacheStaging.
func (t *stagingTree) keep() {
	if t != nil {
		t.lock.Close()
	}
}

func (d *DB) stageMessageDirs(accountID int64, messageIDs []int64) ([]stagedPath, *stagingTree, error) {
	var toStage []int64
	for _, id := range messageIDs {
		live := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(id))
		if _, err := os.Stat(live); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, fmt.Errorf("storage: stat attachment dir for message %d: %w", id, err)
		}
		toStage = append(toStage, id)
	}
	if len(toStage) == 0 {
		return nil, nil, nil
	}
	tree, err := d.newStagingTree()
	if err != nil {
		return nil, nil, err
	}
	staged := make([]stagedPath, 0, len(toStage))
	for _, id := range toStage {
		live := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(id))
		dest := filepath.Join(tree.path, accountSegment(accountID), messageSegment(id))
		if err := os.MkdirAll(filepath.Dir(dest), dirPerm); err != nil {
			d.abandonStaged(staged, tree)
			return nil, nil, fmt.Errorf("storage: create staging path for message %d: %w", id, err)
		}
		if err := os.Rename(live, dest); err != nil {
			d.abandonStaged(staged, tree)
			return nil, nil, fmt.Errorf("storage: stage attachment dir for message %d: %w", id, err)
		}
		staged = append(staged, stagedPath{live: live, staged: dest})
	}
	return staged, tree, nil
}

// commitStagedDelete runs fn in tx after the dirs were staged, commits, and
// removes the staging tree; on any failure the dirs go back.
func (d *DB) commitStagedDelete(ctx context.Context, tx *sql.Tx, staged []stagedPath, tree *stagingTree, fn func(tx *sql.Tx) error) error {
	if err := fn(tx); err != nil {
		d.abandonStaged(staged, tree)
		return err
	}
	if d.deleteTxHook != nil {
		if err := d.deleteTxHook(ctx); err != nil {
			d.abandonStaged(staged, tree)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		d.abandonStaged(staged, tree)
		return fmt.Errorf("storage: commit staged delete: %w", err)
	}
	tree.remove()
	return nil
}

// abandonStaged moves staged dirs back and removes the staging tree. A tree
// with a dir that could not go back is kept for recoverCacheStaging.
func (d *DB) abandonStaged(staged []stagedPath, tree *stagingTree) {
	if d.restoreStaged(staged) {
		tree.remove()
	} else {
		tree.keep()
	}
}

// recoverCacheStaging cleans up staging trees left by a staged cache change
// that never finished: the app died between the rename and the commit, or
// before the tree was removed. A staged message directory goes back to its
// live path when the message row still exists and the path is free; anything
// else is stale. A tree is kept for the next start if the database cannot
// answer or a rename fails.
//
// Another process (a CLI tool, a second instance on the same database) may
// have a change in flight. Staging happens only under the write lock, so the
// recovery holds it too; a change that has committed and not yet removed its
// tree still holds the tree's lock file, and such a tree is skipped. A tree
// whose lock is free (its process died) is recovered at once. A tree without
// a lock file is from a build before the lock and is recovered too.
func (d *DB) recoverCacheStaging(ctx context.Context) {
	roots, _ := filepath.Glob(filepath.Join(d.attachmentsDir, cacheStagingDirName+"-*"))
	if len(roots) == 0 {
		return
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		slog.Warn("storage: lock for cache staging recovery", "err", err)
		return
	}
	defer tx.Rollback()
	for _, root := range roots {
		tree, ok := claimStagingTree(root)
		if !ok {
			continue
		}
		if err := d.recoverStagingRoot(ctx, tx, root); err != nil {
			slog.Warn("storage: recover cache staging dir", "path", root, "err", err)
			tree.keep()
			continue
		}
		tree.remove()
	}
}

// claimStagingTree takes the lock of a staging tree left behind. ok is false
// while its owner still holds the lock, or when the lock cannot be read.
func claimStagingTree(root string) (tree *stagingTree, ok bool) {
	// A tree from a build before the lock gets a lock file here. Its owner
	// cannot be creating one at the same time: staging needs the write lock
	// the recovery holds.
	f, err := os.OpenFile(filepath.Join(root, stagingLockName), os.O_RDWR|os.O_CREATE, filePerm)
	if err != nil {
		slog.Warn("storage: open cache staging lock", "path", root, "err", err)
		return nil, false
	}
	busy, err := tryLockFile(f)
	if busy || err != nil {
		if err != nil {
			slog.Warn("storage: lock cache staging dir", "path", root, "err", err)
		}
		f.Close()
		return nil, false
	}
	return &stagingTree{path: root, lock: f}, true
}

// recoverStagingRoot restores what is still live from one staging tree, laid
// out as <root>/<account>/<message>.
func (d *DB) recoverStagingRoot(ctx context.Context, tx *sql.Tx, root string) error {
	accounts, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		accountID, err := strconv.ParseInt(a.Name(), 10, 64)
		if err != nil || !a.IsDir() {
			continue
		}
		messages, err := os.ReadDir(filepath.Join(root, a.Name()))
		if err != nil {
			return err
		}
		for _, m := range messages {
			messageID, err := strconv.ParseInt(m.Name(), 10, 64)
			if err != nil || !m.IsDir() {
				continue
			}
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE id = ? AND account_id = ?`,
				messageID, accountID).Scan(&n); err != nil {
				return err
			}
			live := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(messageID))
			if _, err := os.Lstat(live); n == 0 || !errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(live), dirPerm); err != nil {
				return err
			}
			if err := os.Rename(filepath.Join(root, a.Name(), m.Name()), live); err != nil {
				return err
			}
		}
	}
	return nil
}

// restoreStaged renames staged dirs back to their live paths and reports
// whether all of them went back.
func (d *DB) restoreStaged(staged []stagedPath) bool {
	ok := true
	for _, s := range slices.Backward(staged) {
		_ = os.MkdirAll(filepath.Dir(s.live), dirPerm)
		if err := os.Rename(s.staged, s.live); err != nil {
			slog.Warn("storage: restore staged attachment dir", "path", s.live, "err", err)
			ok = false
		}
	}
	return ok
}

// commitStagedReplace begins a transaction, runs check against the row, and
// only then renames the message's attachment dir aside and runs fn, which may
// write a new directory at the live path. Taking the (immediate) transaction
// first means a concurrent fill or delete of the same message waits for the
// write lock before touching any file, and sees this change committed. On
// any failure the new files are removed and the staged dir is restored.
// After commit the staging tree (the previous files) is deleted.
func (d *DB) commitStagedReplace(ctx context.Context, accountID, messageID int64, check func(tx *sql.Tx) error, fn func(tx *sql.Tx) (written []string, err error)) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin staged replace: %w", err)
	}
	defer tx.Rollback()
	if err := check(tx); err != nil {
		return err
	}
	staged, tree, err := d.stageMessageDirs(accountID, []int64{messageID})
	if err != nil {
		return err
	}

	written, err := fn(tx)
	if err != nil {
		d.abandonStagedReplace(accountID, messageID, written, staged, tree)
		return err
	}
	if d.deleteTxHook != nil {
		if err := d.deleteTxHook(ctx); err != nil {
			d.abandonStagedReplace(accountID, messageID, written, staged, tree)
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		d.abandonStagedReplace(accountID, messageID, written, staged, tree)
		return fmt.Errorf("storage: commit staged replace: %w", err)
	}
	tree.remove()
	return nil
}

// abandonStagedReplace drops files written for a fill that did not commit,
// moves the staged attachment directory back to its live path and removes
// the staging tree. It runs under the write lock, so the live directory holds
// only this fill's files.
func (d *DB) abandonStagedReplace(accountID, messageID int64, written []string, staged []stagedPath, tree *stagingTree) {
	d.removeAttachmentFiles(written)
	live := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(messageID))
	if len(staged) == 0 {
		_ = os.Remove(live) // only if this fill created it and left it empty
		return
	}
	// The live directory was recreated for the new files. It has to be gone
	// before the staged directory can be renamed back onto that path.
	_ = os.RemoveAll(live)
	d.abandonStaged(staged, tree)
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
