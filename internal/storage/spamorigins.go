package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// spamOriginTTL is how long a spam mark remembers its source folder. Spam is
// rarely rescued after months, and the table would otherwise only grow.
const spamOriginTTL = 90 * 24 * time.Hour

// RecordSpamOrigin remembers that the message with this Message-ID was in
// folderID when it was marked as spam, replacing an earlier record for it. It
// also drops records older than the retention window.
func (d *DB) RecordSpamOrigin(ctx context.Context, accountID int64, messageID string, folderID int64) error {
	now := time.Now().UTC()
	if _, err := d.sql.ExecContext(ctx,
		`DELETE FROM spam_origins WHERE marked_at < ?`, formatTime(now.Add(-spamOriginTTL))); err != nil {
		return fmt.Errorf("storage: prune spam origins: %w", err)
	}
	const query = `
INSERT INTO spam_origins (account_id, message_id, folder_id, marked_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(account_id, message_id) DO UPDATE SET
    folder_id = excluded.folder_id,
    marked_at = excluded.marked_at`
	if _, err := d.sql.ExecContext(ctx, query, accountID, messageID, folderID, formatTime(now)); err != nil {
		return fmt.Errorf("storage: record spam origin: %w", err)
	}
	return nil
}

// SpamOrigin returns the folder a message was marked as spam from, and false
// when there is no record of it or the record is past the retention window.
func (d *DB) SpamOrigin(ctx context.Context, accountID int64, messageID string) (int64, bool, error) {
	var (
		folderID int64
		marked   string
	)
	err := d.sql.QueryRowContext(ctx,
		`SELECT folder_id, marked_at FROM spam_origins WHERE account_id = ? AND message_id = ?`,
		accountID, messageID).Scan(&folderID, &marked)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("storage: get spam origin: %w", err)
	}
	t, err := parseTime(marked)
	if err != nil {
		return 0, false, fmt.Errorf("storage: spam origin time: %w", err)
	}
	if time.Since(t) > spamOriginTTL {
		return 0, false, nil
	}
	return folderID, true, nil
}

// ForgetSpamOrigin drops the record for a message. Missing records are fine.
func (d *DB) ForgetSpamOrigin(ctx context.Context, accountID int64, messageID string) error {
	if _, err := d.sql.ExecContext(ctx,
		`DELETE FROM spam_origins WHERE account_id = ? AND message_id = ?`, accountID, messageID); err != nil {
		return fmt.Errorf("storage: forget spam origin: %w", err)
	}
	return nil
}
