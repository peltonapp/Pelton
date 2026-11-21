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

// MessageExtra is a message's locally-only state (color label, offline pin,
// snooze), keyed by the sending account's email and the message's RFC822
// Message-ID rather than a local row id, so it survives export to another
// device where the same message has a different local id.
type MessageExtra struct {
	AccountEmail string
	MessageID    string
	FlagColor    int
	Offline      bool
	SnoozeUntil  string
	SnoozeHidden bool
}

// ListMessageExtras returns every message with any non-default local state
// (a color label, an offline pin, or a snooze), for exporting a metadata
// snapshot. Messages without a Message-ID header are skipped since they have
// no portable key.
func (d *DB) ListMessageExtras(ctx context.Context) ([]MessageExtra, error) {
	const query = `
SELECT a.email, m.message_id, m.flag_color, m.offline, m.snooze_until, m.snooze_hidden
FROM messages m
JOIN accounts a ON a.id = m.account_id
WHERE m.message_id != '' AND (m.flag_color != 0 OR m.offline = 1 OR m.snooze_until != '')`
	rows, err := d.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("storage: list message extras: %w", err)
	}
	defer rows.Close()

	var out []MessageExtra
	for rows.Next() {
		var e MessageExtra
		var offline, hidden int
		if err := rows.Scan(&e.AccountEmail, &e.MessageID, &e.FlagColor, &offline, &e.SnoozeUntil, &hidden); err != nil {
			return nil, fmt.Errorf("storage: scan message extra: %w", err)
		}
		e.Offline = offline != 0
		e.SnoozeHidden = hidden != 0
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate message extras: %w", err)
	}
	return out, nil
}

// ApplyMessageExtra writes a MessageExtra onto whichever local message
// matches its account email and Message-ID, across every folder in that
// account. It is a silent no-op when no local message matches yet (the
// message has not been synced down from the server on this device), since
// the extra simply has nothing to attach to.
func (d *DB) ApplyMessageExtra(ctx context.Context, e MessageExtra) error {
	const query = `
UPDATE messages
SET flag_color = ?, offline = ?, snooze_until = ?, snooze_hidden = ?
WHERE message_id = ?
  AND account_id = (SELECT id FROM accounts WHERE email = ? LIMIT 1)`
	_, err := d.sql.ExecContext(ctx, query,
		e.FlagColor, boolToInt(e.Offline), e.SnoozeUntil, boolToInt(e.SnoozeHidden),
		e.MessageID, e.AccountEmail)
	if err != nil {
		return fmt.Errorf("storage: apply message extra: %w", err)
	}
	return nil
}
