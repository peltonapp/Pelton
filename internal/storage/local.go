package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Local Folders is where imported mail lives: mail read out of an .eml or
// .mbox file, or out of another client's on-disk store, which has no imap
// server behind it. It is a normal account row flagged is_local, so folders,
// messages, attachments and search all work unchanged, and the one thing that
// differs is that nothing here is ever synced or uploaded.

// EnsureLocalAccount returns the Local Folders account, creating it on first
// use. There is at most one: a second call returns the existing row.
func (d *DB) EnsureLocalAccount(ctx context.Context) (Account, error) {
	account, err := d.LocalAccount(ctx)
	if err == nil {
		return account, nil
	}
	if !errors.Is(err, ErrAccountNotFound) {
		return Account{}, err
	}
	created := Account{Email: LocalAccountEmail, DisplayName: LocalAccountName, Local: true}
	if _, err := d.CreateAccount(ctx, &created); err != nil {
		return Account{}, err
	}
	return created, nil
}

// LocalAccount returns the Local Folders account, or ErrAccountNotFound when
// nothing has been imported yet. The sidebar shows the section only once this
// exists, so an install that never imported anything never sees it.
func (d *DB) LocalAccount(ctx context.Context) (Account, error) {
	const query = `SELECT ` + accountColumns + accountFrom + `
WHERE a.is_local = 1 ORDER BY a.id LIMIT 1`
	a, err := scanAccount(d.sql.QueryRowContext(ctx, query, d.layoutProfile()))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("storage: get local account: %w", err)
	}
	return *a, nil
}

// EnsureLocalFolder returns the folder named name under the local account,
// creating it if it does not exist. Names are matched exactly, so importing
// twice into the same name appends rather than making a second folder.
func (d *DB) EnsureLocalFolder(ctx context.Context, accountID int64, name string) (Folder, error) {
	const query = `SELECT ` + folderColumns + folderFrom + `
WHERE f.account_id = ? AND f.imap_path = ?`
	f, err := scanFolder(d.sql.QueryRowContext(ctx, query, d.layoutProfile(), accountID, name))
	if err == nil {
		return *f, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Folder{}, fmt.Errorf("storage: look up local folder %q: %w", name, err)
	}
	created := Folder{AccountID: accountID, Name: name, IMAPPath: name}
	if _, err := d.CreateFolder(ctx, &created); err != nil {
		return Folder{}, err
	}
	return created, nil
}

// NextLocalUID returns the uid to give the next message inserted into a local
// folder. Imported mail has no server uid, but the messages table keys on
// (folder_id, uid), so local folders number their own from 1 upwards.
func (d *DB) NextLocalUID(ctx context.Context, folderID int64) (uint32, error) {
	const query = `SELECT coalesce(max(uid), 0) + 1 FROM messages WHERE folder_id = ?`
	var uid uint32
	if err := d.sql.QueryRowContext(ctx, query, folderID).Scan(&uid); err != nil {
		return 0, fmt.Errorf("storage: next local uid for folder %d: %w", folderID, err)
	}
	return uid, nil
}

// HasLocalMessage reports whether a message with this Message-ID is already in
// the folder, so re-importing the same file does not duplicate its contents.
// A message with no Message-ID header always reports false: there is nothing
// to match on, and dropping it would be worse than storing it twice.
func (d *DB) HasLocalMessage(ctx context.Context, folderID int64, messageID string) (bool, error) {
	if messageID == "" {
		return false, nil
	}
	const query = `SELECT 1 FROM messages WHERE folder_id = ? AND message_id = ? AND ` + notVaulted + ` LIMIT 1`
	var found int
	err := d.sql.QueryRowContext(ctx, query, folderID, messageID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: look up local message %q: %w", messageID, err)
	}
	return true, nil
}
