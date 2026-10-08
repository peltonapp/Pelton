package storage

import (
	"context"
	"fmt"
)

// MessageState is the lightweight per-message view the sync engine needs:
// identity, flags and whether a local change is waiting to be pushed. it
// deliberately carries no bodies so a full-folder scan stays cheap.
type MessageState struct {
	ID            int64
	UID           uint32
	Flags         Flag
	PendingFlags  bool
	PendingDelete bool
}

// ListMessageStates returns the sync state of every cached message in a folder,
// ordered by uid.
func (d *DB) ListMessageStates(ctx context.Context, folderID int64) ([]MessageState, error) {
	const query = `
SELECT id, uid, flags, pending_flags, pending_delete
FROM messages WHERE folder_id = ? ORDER BY uid`
	rows, err := d.sql.QueryContext(ctx, query, folderID)
	if err != nil {
		return nil, fmt.Errorf("storage: list message states for folder %d: %w", folderID, err)
	}
	defer rows.Close()

	var states []MessageState
	for rows.Next() {
		var (
			s             MessageState
			flags         uint8
			pendingFlags  int
			pendingDelete int
		)
		if err := rows.Scan(&s.ID, &s.UID, &flags, &pendingFlags, &pendingDelete); err != nil {
			return nil, fmt.Errorf("storage: scan message state: %w", err)
		}
		s.Flags = Flag(flags)
		s.PendingFlags = pendingFlags != 0
		s.PendingDelete = pendingDelete != 0
		states = append(states, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate message states: %w", err)
	}
	return states, nil
}

// MarkFlagsPending records a local flag change: it stores the new flags and
// marks the row so the next sync pushes them to the server.
func (d *DB) MarkFlagsPending(ctx context.Context, id int64, flags Flag) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET flags = ?, pending_flags = 1 WHERE id = ?`, uint8(flags), id)
	if err != nil {
		return fmt.Errorf("storage: mark flags pending on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// MarkCopiesFlagPending sets or clears one flag bit on the other copies of a
// message in the same account, the rows sharing its Message-ID, and marks the
// ones it changes for the next sync to push. A message without a Message-ID has
// no copies to find.
func (d *DB) MarkCopiesFlagPending(ctx context.Context, accountID, exceptID int64, messageID string, flag Flag, on bool) error {
	if messageID == "" {
		return nil
	}
	set, differs := `flags = flags | ?`, `flags & ? = 0`
	if !on {
		set, differs = `flags = flags & ~?`, `flags & ? != 0`
	}
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET `+set+`, pending_flags = 1
WHERE account_id = ? AND message_id = ? AND id != ? AND pending_delete = 0 AND `+differs,
		uint8(flag), accountID, messageID, exceptID, uint8(flag))
	if err != nil {
		return fmt.Errorf("storage: mark copies of message %d: %w", exceptID, err)
	}
	return nil
}

// ClearFlagsPending clears the pending flag marker after a successful push.
func (d *DB) ClearFlagsPending(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET pending_flags = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: clear flags pending on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// MarkDeletePending records a local deletion to be pushed on the next sync. The
// row is kept until the server delete succeeds.
func (d *DB) MarkDeletePending(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET pending_delete = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: mark delete pending on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// MarkFolderDeletePending marks every cached message in a folder for deletion
// in one statement and returns how many rows it marked. Rows already pending
// are left alone and not counted again, so emptying a folder twice reports 0
// the second time rather than double counting what is already on its way out.
func (d *DB) MarkFolderDeletePending(ctx context.Context, folderID int64) (int, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET pending_delete = 1 WHERE folder_id = ? AND pending_delete = 0`, folderID)
	if err != nil {
		return 0, fmt.Errorf("storage: mark folder %d delete pending: %w", folderID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: rows affected: %w", err)
	}
	return int(n), nil
}

// ClearDeletePending undoes a pending local deletion, as long as the row is still
// cached (a sync has not yet expunged it on the server and dropped it locally).
func (d *DB) ClearDeletePending(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET pending_delete = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: clear delete pending on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// FolderLastSeenUID returns the high water mark recorded for a folder.
func (d *DB) FolderLastSeenUID(ctx context.Context, folderID int64) (uint32, error) {
	var uid uint32
	err := d.sql.QueryRowContext(ctx,
		`SELECT last_seen_uid FROM folders WHERE id = ?`, folderID).Scan(&uid)
	if err != nil {
		return 0, fmt.Errorf("storage: get last_seen_uid for folder %d: %w", folderID, err)
	}
	return uid, nil
}

// SetFolderLastSeenUID updates the high water mark for a folder.
func (d *DB) SetFolderLastSeenUID(ctx context.Context, folderID int64, uid uint32) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET last_seen_uid = ? WHERE id = ?`, uid, folderID)
	if err != nil {
		return fmt.Errorf("storage: set last_seen_uid for folder %d: %w", folderID, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// FolderSyncFloorUID returns the lowest uid a folder's cache covers. 0 means
// there is no floor and the folder is cached in full.
func (d *DB) FolderSyncFloorUID(ctx context.Context, folderID int64) (uint32, error) {
	var uid uint32
	err := d.sql.QueryRowContext(ctx,
		`SELECT sync_floor_uid FROM folders WHERE id = ?`, folderID).Scan(&uid)
	if err != nil {
		return 0, fmt.Errorf("storage: get sync_floor_uid for folder %d: %w", folderID, err)
	}
	return uid, nil
}

// SetFolderSyncFloorUID updates the folder's sync floor. Pass 0 to clear it,
// meaning nothing older is left to fetch.
func (d *DB) SetFolderSyncFloorUID(ctx context.Context, folderID int64, uid uint32) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE folders SET sync_floor_uid = ? WHERE id = ?`, uid, folderID)
	if err != nil {
		return fmt.Errorf("storage: set sync_floor_uid for folder %d: %w", folderID, err)
	}
	return requireOneRow(res, ErrFolderNotFound)
}

// AnyFolderHasOlder reports whether any of the given folders still has messages
// on the server below its sync floor, i.e. whether a backfill would fetch
// anything. An empty list is false.
func (d *DB) AnyFolderHasOlder(ctx context.Context, folderIDs []int64) (bool, error) {
	if len(folderIDs) == 0 {
		return false, nil
	}
	marks, args := inClause(folderIDs)
	query := `SELECT COUNT(*) FROM folders WHERE sync_floor_uid > 0 AND id IN (` + marks + `)`
	var n int
	if err := d.sql.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return false, fmt.Errorf("storage: check older messages for folders: %w", err)
	}
	return n > 0, nil
}

// PurgeFolderMessages removes every cached message and its attachment files for
// a folder. It is used when the server's UIDVALIDITY changed and the whole
// cache for the folder is stale. Attachment directories are removed per message
// first, then the rows are dropped in one statement. Returns the row count.
func (d *DB) PurgeFolderMessages(ctx context.Context, accountID, folderID int64) (int, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id FROM messages WHERE folder_id = ?`, folderID)
	if err != nil {
		return 0, fmt.Errorf("storage: list messages to purge for folder %d: %w", folderID, err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("storage: scan message id to purge: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("storage: iterate messages to purge: %w", err)
	}
	rows.Close()

	// remove the files first. best effort: a leftover file is harmless, a
	// dangling db row is not, so the row delete is what must succeed.
	for _, id := range ids {
		if err := d.DeleteAttachmentFilesForMessage(accountID, id); err != nil {
			return 0, err
		}
	}

	res, err := d.sql.ExecContext(ctx, `DELETE FROM messages WHERE folder_id = ?`, folderID)
	if err != nil {
		return 0, fmt.Errorf("storage: purge messages for folder %d: %w", folderID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: purge rows affected: %w", err)
	}
	return int(n), nil
}
