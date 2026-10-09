package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// MessageState is the lightweight per-message view the sync engine needs:
// identity, flags and whether a local change is waiting to be pushed. it
// deliberately carries no bodies so a full-folder scan stays cheap.
type MessageState struct {
	ID            int64
	UID           uint32
	RemoteID      string
	Flags         Flag
	PendingFlags  bool
	PendingDelete bool
}

// ListMessageStates returns the sync state of every cached message in a folder,
// ordered by uid.
func (d *DB) ListMessageStates(ctx context.Context, folderID int64) ([]MessageState, error) {
	const query = `
SELECT id, uid, remote_id, flags, pending_flags, pending_delete
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
		if err := rows.Scan(&s.ID, &s.UID, &s.RemoteID, &flags, &pendingFlags, &pendingDelete); err != nil {
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

// MessageBodyState says whether a cached message already has its body, which
// is what the bulk offline download needs to choose between fetching a message
// and only pinning it.
type MessageBodyState struct {
	ID           int64
	UID          uint32
	RemoteID     string
	BodyComplete bool
}

// MessageBodyStates returns the folder's rows dated since the cutoff, leaving
// out ones queued for deletion. A zero since returns every row.
func (d *DB) MessageBodyStates(ctx context.Context, folderID int64, since time.Time) ([]MessageBodyState, error) {
	// date holds formatTime's UTC RFC 3339 text, which sorts in time order, so
	// the cutoff is compared in that same form.
	const query = `
SELECT id, uid, remote_id, body_complete
FROM messages WHERE folder_id = ? AND pending_delete = 0 AND (? = '' OR date >= ?)
ORDER BY id`
	cutoff := formatTime(since)
	states, err := d.queryAll(ctx, func(r *sql.Rows) (MessageBodyState, error) {
		var (
			s        MessageBodyState
			complete int
		)
		if err := r.Scan(&s.ID, &s.UID, &s.RemoteID, &complete); err != nil {
			return MessageBodyState{}, fmt.Errorf("storage: scan body state: %w", err)
		}
		s.BodyComplete = complete != 0
		return s, nil
	}, query, folderID, cutoff, cutoff)
	if err != nil {
		return nil, fmt.Errorf("storage: list body states for folder %d: %w", folderID, err)
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

// ResolvePendingFlags stores final as a message's flags and clears its pending
// marker once a sync has settled the local change with the server. It writes
// only while the row still has the flags snapshot and the marker the sync read:
// a change made since then stays pending for the next sync. It reports whether
// the row was written.
func (d *DB) ResolvePendingFlags(ctx context.Context, id int64, snapshot, final Flag) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET flags = ?, pending_flags = 0 WHERE id = ? AND flags = ? AND pending_flags = 1`,
		uint8(final), id, uint8(snapshot))
	if err != nil {
		return false, fmt.Errorf("storage: resolve pending flags on message %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: resolve pending flags on message %d: %w", id, err)
	}
	return n == 1, nil
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

// folderHasOlderOnServer is true when the folder's newest-first sync window
// still leaves mail on the server. IMAP usually sets sync_floor_uid; an adapter
// without numeric ids uses sync_floor_id and keeps sync_floor_uid at zero.
func folderHasOlderOnServer(floorUID uint32, floorID string) bool {
	return floorUID > 0 || floorID != ""
}

// FolderHasOlderOnServer reports whether one folder still has mail below its
// sync window.
func (d *DB) FolderHasOlderOnServer(ctx context.Context, folderID int64) (bool, error) {
	var floorUID uint32
	var floorID string
	err := d.sql.QueryRowContext(ctx,
		`SELECT sync_floor_uid, sync_floor_id FROM folders WHERE id = ?`, folderID).Scan(&floorUID, &floorID)
	if err != nil {
		return false, fmt.Errorf("storage: sync floor for folder %d: %w", folderID, err)
	}
	return folderHasOlderOnServer(floorUID, floorID), nil
}

// AnyFolderHasOlder reports whether any of the given folders still has messages
// on the server below its sync floor, i.e. whether a backfill would fetch
// anything. An empty list is false. IMAP floors use sync_floor_uid; opaque
// floors use a non-empty sync_floor_id.
func (d *DB) AnyFolderHasOlder(ctx context.Context, folderIDs []int64) (bool, error) {
	if len(folderIDs) == 0 {
		return false, nil
	}
	marks, args := inClause(folderIDs)
	query := `SELECT COUNT(*) FROM folders WHERE (sync_floor_uid > 0 OR sync_floor_id != '') AND id IN (` + marks + `)`
	var n int
	if err := d.sql.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return false, fmt.Errorf("storage: check older messages for folders: %w", err)
	}
	return n > 0, nil
}

// PurgeFolderMessages removes every cached message and its attachment files for
// a folder. Attachment directories are staged first so a transaction failure can
// restore them; after commit the staging tree is removed. Returns the row count.
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

	var n int64
	err = d.withStagedMessageDirs(ctx, accountID, ids, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE folder_id = ?`, folderID)
		if err != nil {
			return fmt.Errorf("storage: purge messages for folder %d: %w", folderID, err)
		}
		n, err = res.RowsAffected()
		if err != nil {
			return fmt.Errorf("storage: purge rows affected: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// DeleteCachedMessage removes one message row and stages its attachment
// directory so a transaction failure can restore the files.
func (d *DB) DeleteCachedMessage(ctx context.Context, accountID, messageID int64) error {
	return d.withStagedMessageDirs(ctx, accountID, []int64{messageID}, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, messageID)
		if err != nil {
			return fmt.Errorf("storage: delete cached message %d: %w", messageID, err)
		}
		return requireOneRow(res, ErrMessageNotFound)
	})
}

// AdoptServerFlags stores the server's flags on a message unless it has a local
// flag change waiting to be pushed, which wins until the push resolves it. A
// sync decides to adopt from a snapshot read earlier, so the row may have
// gained a pending change since. It reports whether the flags were written.
func (d *DB) AdoptServerFlags(ctx context.Context, id int64, flags Flag) (bool, error) {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET flags = ? WHERE id = ? AND pending_flags = 0`, uint8(flags), id)
	if err != nil {
		return false, fmt.Errorf("storage: adopt server flags on message %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: adopt server flags on message %d: %w", id, err)
	}
	return n == 1, nil
}
