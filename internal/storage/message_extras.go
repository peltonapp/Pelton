package storage

import (
	"context"
	"fmt"
)

// SetFlagColor sets a message's color label (0 clears it, 1..8 pick a palette
// color). The color is stored locally; syncing it to an imap keyword is handled
// by the sync layer when the user opts in.
func (d *DB) SetFlagColor(ctx context.Context, id int64, color int) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET flag_color = ? WHERE id = ?`, color, id)
	if err != nil {
		return fmt.Errorf("storage: set flag color on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// FolderFlagColors returns the current color label of every message in a folder,
// keyed by uid, so the sync layer can adopt server-side color changes without a
// write when nothing differs.
func (d *DB) FolderFlagColors(ctx context.Context, folderID int64) (map[uint32]int, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT uid, flag_color FROM messages WHERE folder_id = ?`, folderID)
	if err != nil {
		return nil, fmt.Errorf("storage: folder flag colors %d: %w", folderID, err)
	}
	defer rows.Close()

	out := make(map[uint32]int)
	for rows.Next() {
		var uid uint32
		var color int
		if err := rows.Scan(&uid, &color); err != nil {
			return nil, fmt.Errorf("storage: scan flag color: %w", err)
		}
		out[uid] = color
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate flag colors: %w", err)
	}
	return out, nil
}

// SetOffline pins or unpins a message for offline availability, which drives the
// downloaded indicator.
func (d *DB) SetOffline(ctx context.Context, id int64, offline bool) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET offline = ? WHERE id = ?`, boolToInt(offline), id)
	if err != nil {
		return fmt.Errorf("storage: set offline on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// SetSnooze schedules a message to resurface at until (a stored timestamp). When
// hidden is true the row is also hidden from the list until then; otherwise it
// stays visible and is only marked unread when the timer fires.
func (d *DB) SetSnooze(ctx context.Context, id int64, until string, hidden bool) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET snooze_until = ?, snooze_hidden = ? WHERE id = ?`,
		until, boolToInt(hidden), id)
	if err != nil {
		return fmt.Errorf("storage: set snooze on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// ClearSnooze removes a snooze without reviving the message, used when the user
// cancels a snooze manually.
func (d *DB) ClearSnooze(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET snooze_until = '', snooze_hidden = 0 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: clear snooze on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// DueSnoozes returns the ids of messages whose snooze time has passed (compared
// against now, a stored-format timestamp), so the snooze poller can revive them.
func (d *DB) DueSnoozes(ctx context.Context, now string) ([]int64, error) {
	const query = `SELECT id FROM messages WHERE snooze_until != '' AND snooze_until <= ?`
	rows, err := d.sql.QueryContext(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("storage: query due snoozes: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan due snooze: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate due snoozes: %w", err)
	}
	return ids, nil
}

// ReviveSnoozed clears a message's snooze, unhides it, and marks it unread (by
// clearing the \Seen bit) so it resurfaces as fresh. The unread change is marked
// pending so the next sync pushes it to the server, matching a manual toggle.
func (d *DB) ReviveSnoozed(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages
		 SET snooze_until = '', snooze_hidden = 0,
		     flags = flags & ~?, pending_flag_mask = pending_flag_mask | ?, pending_flags = 1
		 WHERE id = ?`, uint8(FlagSeen), uint8(FlagSeen), id)
	if err != nil {
		return fmt.Errorf("storage: revive snoozed message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}
