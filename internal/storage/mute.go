package storage

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotThreaded is returned for a conversation action on a message the
// threading pass has not reached yet.
var ErrNotThreaded = errors.New("storage: message has no conversation yet")

// SetThreadMuted mutes or unmutes a conversation. Muted mail stays out of the
// inbox views and raises no notification.
func (d *DB) SetThreadMuted(ctx context.Context, threadID int64, muted bool) error {
	if threadID <= 0 {
		return ErrNotThreaded
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE threads SET muted = ? WHERE id = ?`, boolToInt(muted), threadID)
	if err != nil {
		return fmt.Errorf("storage: mute thread %d: %w", threadID, err)
	}
	return requireOneRow(res, ErrNotThreaded)
}

// ThreadMuted reports whether a conversation is muted. Thread 0 never is.
func (d *DB) ThreadMuted(ctx context.Context, threadID int64) (bool, error) {
	if threadID <= 0 {
		return false, nil
	}
	var muted int
	err := d.sql.QueryRowContext(ctx, `SELECT COALESCE(MAX(muted), 0) FROM threads WHERE id = ?`, threadID).Scan(&muted)
	if err != nil {
		return false, fmt.Errorf("storage: thread %d muted: %w", threadID, err)
	}
	return muted != 0, nil
}

// FolderCountsHidingMuted is FolderCounts for an inbox, leaving out the mail of
// muted conversations so the badge agrees with the list.
func (d *DB) FolderCountsHidingMuted(ctx context.Context, folderID int64) (total, unread int, err error) {
	const query = `
SELECT COUNT(*), COALESCE(SUM(CASE WHEN flags & ? = 0 THEN 1 ELSE 0 END), 0)
FROM messages WHERE folder_id = ? AND pending_delete = 0 AND snooze_hidden = 0
AND thread_id NOT IN (SELECT id FROM threads WHERE muted = 1)`
	if err := d.sql.QueryRowContext(ctx, query, uint8(FlagSeen), folderID).Scan(&total, &unread); err != nil {
		return 0, 0, fmt.Errorf("storage: folder counts %d: %w", folderID, err)
	}
	return total, unread, nil
}

// UnreadCountFor counts the unread messages q lists, ignoring its paging.
func (d *DB) UnreadCountFor(ctx context.Context, q MessageQuery) (int, error) {
	q.ExcludeFlags |= FlagSeen
	return d.CountMessages(ctx, q)
}
