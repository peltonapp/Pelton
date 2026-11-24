package storage

import (
	"context"
	"fmt"
)

// SetPinned pins or unpins a message and every copy of it in the account's
// other folders, and marks the change for the next sync to store on the
// server.
func (d *DB) SetPinned(ctx context.Context, id int64, pinned bool) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE messages SET pinned = ?1, pin_pending = 1
WHERE id = ?2 OR (message_id != '' AND (account_id, message_id) =
    (SELECT account_id, message_id FROM messages WHERE id = ?2))`, boolToInt(pinned), id)
	if err != nil {
		return fmt.Errorf("storage: pin message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// SetThreadPinned pins or unpins every message of a conversation.
func (d *DB) SetThreadPinned(ctx context.Context, threadID int64, pinned bool) error {
	if threadID <= 0 {
		return ErrNotThreaded
	}
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET pinned = ?, pin_pending = 1 WHERE thread_id = ? AND pinned != ?`,
		boolToInt(pinned), threadID, boolToInt(pinned))
	if err != nil {
		return fmt.Errorf("storage: pin thread %d: %w", threadID, err)
	}
	return nil
}

// PinState is a message's pin as sync sees it.
type PinState struct {
	ID      int64
	UID     uint32
	Pinned  bool
	Pending bool
}

// FolderPinStates returns the pin state of every message in a folder that is
// pinned or has a pin change waiting, keyed by uid.
func (d *DB) FolderPinStates(ctx context.Context, folderID int64) (map[uint32]PinState, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, uid, pinned, pin_pending FROM messages WHERE folder_id = ? AND (pinned = 1 OR pin_pending = 1)`, folderID)
	if err != nil {
		return nil, fmt.Errorf("storage: folder %d pins: %w", folderID, err)
	}
	defer rows.Close()
	out := make(map[uint32]PinState)
	for rows.Next() {
		var (
			s               PinState
			pinned, pending int
		)
		if err := rows.Scan(&s.ID, &s.UID, &pinned, &pending); err != nil {
			return nil, fmt.Errorf("storage: scan pin: %w", err)
		}
		s.Pinned = pinned != 0
		s.Pending = pending != 0
		out[s.UID] = s
	}
	return out, rows.Err()
}

// AdoptPin records the server's pin for a message and clears any pending
// change, since the two now agree.
func (d *DB) AdoptPin(ctx context.Context, id int64, pinned bool) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET pinned = ?, pin_pending = 0 WHERE id = ?`, boolToInt(pinned), id)
	if err != nil {
		return fmt.Errorf("storage: adopt pin %d: %w", id, err)
	}
	return nil
}

// ClearPinPending marks every pending pin change in a folder as settled, for a
// mailbox that cannot store the keyword: the pin then stays local.
func (d *DB) ClearPinPending(ctx context.Context, folderID int64) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET pin_pending = 0 WHERE folder_id = ? AND pin_pending = 1`, folderID)
	if err != nil {
		return fmt.Errorf("storage: clear pending pins %d: %w", folderID, err)
	}
	return nil
}
