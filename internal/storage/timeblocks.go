package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/mailvault"
)

// ErrTimeBlockNotFound is returned when a time block id has no row.
var ErrTimeBlockNotFound = errors.New("storage: time block not found")

// The states of a block's link to its calendar event.
const (
	BlockLinkOK        = ""
	BlockLinkOutOfStep = "out_of_step"
	BlockLinkEventGone = "event_gone"
)

// TimeBlock is time set aside for a task or a mail.
type TimeBlock struct {
	ID int64
	// Title is a title the user typed, kept only with TitleOwn. A block
	// without one shows its task's or its mail's title, read when it is
	// shown, so no copy of a mail's subject is ever stored here.
	Title    string
	TitleOwn bool
	Start    time.Time
	End      time.Time
	// TaskUID is the task the block is for, empty for none.
	TaskUID string
	// MessageID and MessageHeader name the mail the block is for: the stored
	// message and its Message-ID. Both are zero for none.
	MessageID     int64
	MessageHeader string
	Done          bool
	// CalendarID is the calendar the block is written to, 0 for a block kept
	// on this device only. EventUID is that event's UID.
	CalendarID int64
	EventUID   string
	// EventTitle is what the event is called: "", "generic" or "title".
	EventTitle string
	// LinkState is BlockLinkOK, BlockLinkOutOfStep or BlockLinkEventGone.
	LinkState string
	CreatedAt string
	UpdatedAt string
}

const timeBlockColumns = `id, title, title_own, start_at, end_at, task_uid, message_id, message_header, done,
       coalesce(calendar_id, 0), event_uid, event_title, link_state, created_at, updated_at`

// ownTitle drops a title that is not the user's own, so none is stored.
func ownTitle(b *TimeBlock) {
	if !b.TitleOwn || b.Title == "" {
		b.Title, b.TitleOwn = "", false
	}
}

// CreateTimeBlock inserts a block and returns its new id. A title that is not
// marked as the user's own is not stored.
func (d *DB) CreateTimeBlock(ctx context.Context, b *TimeBlock) (int64, error) {
	ownTitle(b)
	now := nowText()
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO time_blocks (title, title_own, start_at, end_at, task_uid, message_id, message_header, done,
                         calendar_id, event_uid, event_title, link_state, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.Title, boolToInt(b.TitleOwn), formatTime(b.Start), formatTime(b.End), b.TaskUID, b.MessageID, b.MessageHeader, boolToInt(b.Done),
		nullableAccount(b.CalendarID), b.EventUID, b.EventTitle, b.LinkState, now, now)
	if err != nil {
		return 0, fmt.Errorf("storage: insert time block: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: time block insert id: %w", err)
	}
	b.ID, b.CreatedAt, b.UpdatedAt = id, now, now
	return id, nil
}

// UpdateTimeBlock saves every field of a block, or returns
// ErrTimeBlockNotFound. A title that is not marked as the user's own is
// cleared.
func (d *DB) UpdateTimeBlock(ctx context.Context, b *TimeBlock) error {
	ownTitle(b)
	now := nowText()
	res, err := d.sql.ExecContext(ctx, `
UPDATE time_blocks SET title = ?, title_own = ?, start_at = ?, end_at = ?, task_uid = ?, message_id = ?, message_header = ?,
       done = ?, calendar_id = ?, event_uid = ?, event_title = ?, link_state = ?, updated_at = ?
WHERE id = ?`,
		b.Title, boolToInt(b.TitleOwn), formatTime(b.Start), formatTime(b.End), b.TaskUID, b.MessageID, b.MessageHeader, boolToInt(b.Done),
		nullableAccount(b.CalendarID), b.EventUID, b.EventTitle, b.LinkState, now, b.ID)
	if err != nil {
		return fmt.Errorf("storage: update time block %d: %w", b.ID, err)
	}
	if err := requireOneRow(res, ErrTimeBlockNotFound); err != nil {
		return err
	}
	b.UpdatedAt = now
	return nil
}

// GetTimeBlock returns one block, or ErrTimeBlockNotFound.
func (d *DB) GetTimeBlock(ctx context.Context, id int64) (*TimeBlock, error) {
	b, err := scanTimeBlock(d.sql.QueryRowContext(ctx, `SELECT `+timeBlockColumns+` FROM time_blocks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTimeBlockNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get time block %d: %w", id, err)
	}
	return b, nil
}

// ListTimeBlocks returns the blocks overlapping [from, to), by start.
func (d *DB) ListTimeBlocks(ctx context.Context, from, to time.Time) ([]TimeBlock, error) {
	return d.queryTimeBlocks(ctx, `SELECT `+timeBlockColumns+` FROM time_blocks
WHERE start_at < ? AND end_at > ? ORDER BY start_at, id`, formatTime(to), formatTime(from))
}

// TaskTimeBlocks returns every block of the tasks with these UIDs, by start.
func (d *DB) TaskTimeBlocks(ctx context.Context, uids []string) ([]TimeBlock, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	args := make([]any, len(uids))
	for i, uid := range uids {
		args[i] = uid
	}
	return d.queryTimeBlocks(ctx, `SELECT `+timeBlockColumns+` FROM time_blocks
WHERE task_uid IN (`+strings.TrimSuffix(strings.Repeat("?,", len(uids)), ",")+`) ORDER BY start_at, id`, args...)
}

// AllTaskTimeBlocks returns every block that is for a task, by start.
func (d *DB) AllTaskTimeBlocks(ctx context.Context) ([]TimeBlock, error) {
	return d.queryTimeBlocks(ctx, `SELECT `+timeBlockColumns+` FROM time_blocks WHERE task_uid != '' ORDER BY start_at, id`)
}

// LinkedTimeBlocks returns the blocks written to a calendar, or whose event
// went with its calendar, by start.
func (d *DB) LinkedTimeBlocks(ctx context.Context) ([]TimeBlock, error) {
	return d.queryTimeBlocks(ctx, `SELECT `+timeBlockColumns+` FROM time_blocks WHERE event_uid != '' ORDER BY start_at, id`)
}

// SourcedTimeBlocks returns the blocks made for a mail or a task, by start.
func (d *DB) SourcedTimeBlocks(ctx context.Context) ([]TimeBlock, error) {
	return d.queryTimeBlocks(ctx, `SELECT `+timeBlockColumns+` FROM time_blocks
WHERE message_id != 0 OR message_header != '' OR task_uid != '' ORDER BY start_at, id`)
}

// HeaderVault returns the vault a message with this Message-ID is in, written
// with or without angle brackets, locked or not; 0 when no stored message
// with it is in a vault.
func (d *DB) HeaderVault(ctx context.Context, headerID string) (int64, error) {
	id := strings.Trim(strings.TrimSpace(headerID), "<>")
	if id == "" {
		return 0, nil
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT folder_id, vault_id FROM messages WHERE message_id IN (?, ?)`, id, "<"+id+">")
	if err != nil {
		return 0, fmt.Errorf("storage: vault of message %q: %w", id, err)
	}
	type place struct{ folder, vault int64 }
	var found []place
	for rows.Next() {
		var p place
		if err := rows.Scan(&p.folder, &p.vault); err != nil {
			rows.Close()
			return 0, fmt.Errorf("storage: scan vault of message: %w", err)
		}
		found = append(found, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, p := range found {
		if p.vault != 0 {
			return p.vault, nil
		}
		if v, err := d.FolderVault(ctx, p.folder); err != nil || v != 0 {
			return v, err
		}
	}
	return d.sealedHeaderVault(ctx, id)
}

// sealedHeaderVault finds a sealed message by its Message-ID, which its row
// keeps only as a hash keyed by its vault's link key.
func (d *DB) sealedHeaderVault(ctx context.Context, headerID string) (int64, error) {
	key := threadKey(headerID)
	if key == "" {
		return 0, nil
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT id, link_key FROM mail_vaults`)
	if err != nil {
		return 0, fmt.Errorf("storage: vault link keys: %w", err)
	}
	type vaultKey struct {
		id   int64
		link []byte
	}
	var vaults []vaultKey
	for rows.Next() {
		var v vaultKey
		if err := rows.Scan(&v.id, &v.link); err != nil {
			rows.Close()
			return 0, fmt.Errorf("storage: scan vault link key: %w", err)
		}
		vaults = append(vaults, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, v := range vaults {
		var n int
		if err := d.sql.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE message_id = ? AND vault_id = ?`,
			mailvault.LinkHash(v.link, key), v.id).Scan(&n); err != nil {
			return 0, fmt.Errorf("storage: sealed message by id: %w", err)
		}
		if n > 0 {
			return v.id, nil
		}
	}
	return 0, nil
}

// SetTaskTimeBlocksDone marks the blocks of a task done or open. Reopening
// only touches the blocks that end after since, so time already spent stays
// done.
func (d *DB) SetTaskTimeBlocksDone(ctx context.Context, uid string, done bool, since time.Time) (int64, error) {
	query := `UPDATE time_blocks SET done = 1, updated_at = ? WHERE task_uid = ? AND done = 0`
	args := []any{nowText(), uid}
	if !done {
		query = `UPDATE time_blocks SET done = 0, updated_at = ? WHERE task_uid = ? AND done = 1 AND end_at > ?`
		args = append(args, formatTime(since))
	}
	res, err := d.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("storage: mark blocks of task %q: %w", uid, err)
	}
	return res.RowsAffected()
}

// UnlinkTaskTimeBlocks keeps a task's blocks but forgets the task. A block
// without a title of its own takes title as its own, so it still says what it
// was for once the task is gone; an empty title leaves it without one.
func (d *DB) UnlinkTaskTimeBlocks(ctx context.Context, uid, title string) error {
	if _, err := d.sql.ExecContext(ctx, `
UPDATE time_blocks SET title = CASE WHEN title_own = 1 THEN title ELSE ? END,
       title_own = CASE WHEN title_own = 1 OR ? != '' THEN 1 ELSE 0 END, task_uid = '', updated_at = ?
WHERE task_uid = ?`, title, title, nowText(), uid); err != nil {
		return fmt.Errorf("storage: unlink blocks of task %q: %w", uid, err)
	}
	return nil
}

// DeleteTimeBlock removes a block, or returns ErrTimeBlockNotFound.
func (d *DB) DeleteTimeBlock(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM time_blocks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete time block %d: %w", id, err)
	}
	return requireOneRow(res, ErrTimeBlockNotFound)
}

func (d *DB) queryTimeBlocks(ctx context.Context, query string, args ...any) ([]TimeBlock, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list time blocks: %w", err)
	}
	defer rows.Close()
	var out []TimeBlock
	for rows.Next() {
		b, err := scanTimeBlock(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan time block: %w", err)
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func scanTimeBlock(row rowScanner) (*TimeBlock, error) {
	var (
		b          TimeBlock
		start, end string
		done, own  int
	)
	if err := row.Scan(&b.ID, &b.Title, &own, &start, &end, &b.TaskUID, &b.MessageID, &b.MessageHeader, &done,
		&b.CalendarID, &b.EventUID, &b.EventTitle, &b.LinkState, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	var err error
	if b.Start, err = parseTime(start); err != nil {
		return nil, err
	}
	if b.End, err = parseTime(end); err != nil {
		return nil, err
	}
	b.Done, b.TitleOwn = done != 0, own != 0
	return &b, nil
}
