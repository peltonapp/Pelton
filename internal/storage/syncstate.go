package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
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
	return appendMessageStates(nil, rows)
}

// appendMessageStates scans rows of (id, uid, remote_id, flags,
// pending_flags, pending_delete) onto states and closes rows.
func appendMessageStates(states []MessageState, rows *sql.Rows) ([]MessageState, error) {
	defer rows.Close()
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

// MessageStatesByRemoteID returns the sync state of the folder's cached
// messages among remoteIDs, in no particular order; ids without a row are
// left out. It reads only those rows, so a caller holding a bounded page of
// server ids pays for the page, not the folder.
func (d *DB) MessageStatesByRemoteID(ctx context.Context, folderID int64, remoteIDs []string) ([]MessageState, error) {
	var states []MessageState
	for chunk := range slices.Chunk(remoteIDs, 500) {
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(chunk)), ", ")
		args := make([]any, 0, len(chunk)+1)
		args = append(args, folderID)
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := d.sql.QueryContext(ctx, `
SELECT id, uid, remote_id, flags, pending_flags, pending_delete
FROM messages WHERE folder_id = ? AND remote_id IN (`+marks+`)`, args...)
		if err != nil {
			return nil, fmt.Errorf("storage: message states by remote id for folder %d: %w", folderID, err)
		}
		states, err = appendMessageStates(states, rows)
		if err != nil {
			return nil, err
		}
	}
	return states, nil
}

// FolderHasMessages reports whether the folder has any cached message,
// without reading them.
func (d *DB) FolderHasMessages(ctx context.Context, folderID int64) (bool, error) {
	var has bool
	if err := d.sql.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM messages WHERE folder_id = ?)`, folderID).Scan(&has); err != nil {
		return false, fmt.Errorf("storage: folder %d has messages: %w", folderID, err)
	}
	return has, nil
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
// meaning nothing older is left to fetch. sync_floor_id follows it, since an
// IMAP remote id is the decimal uid and AnyFolderHasOlder reads both: a
// floor uid sets it, and clearing clears an id that mirrored the old uid. An
// opaque id set by SetFolderSyncWindow is left alone when clearing.
func (d *DB) SetFolderSyncFloorUID(ctx context.Context, folderID int64, uid uint32) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE folders SET
  sync_floor_id = CASE
    WHEN ?1 > 0 THEN CAST(?1 AS TEXT)
    WHEN sync_floor_id = CAST(sync_floor_uid AS TEXT) THEN ''
    ELSE sync_floor_id END,
  sync_floor_uid = ?1
WHERE id = ?2`, uid, folderID)
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
	ids := func(tx *sql.Tx) ([]int64, error) {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM messages WHERE folder_id = ?`, folderID)
		if err != nil {
			return nil, fmt.Errorf("storage: list messages to purge for folder %d: %w", folderID, err)
		}
		defer rows.Close()
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, fmt.Errorf("storage: scan message id to purge: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("storage: iterate messages to purge: %w", err)
		}
		return ids, nil
	}

	var n int64
	err := d.withStagedMessageDirsTx(ctx, accountID, ids, func(tx *sql.Tx) error {
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
