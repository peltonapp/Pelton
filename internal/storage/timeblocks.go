package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
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
	// Title is empty while the title comes from the linked mail.
	Title string
	Start time.Time
	End   time.Time
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

const timeBlockColumns = `id, title, start_at, end_at, task_uid, message_id, message_header, done,
       coalesce(calendar_id, 0), event_uid, event_title, link_state, created_at, updated_at`

// CreateTimeBlock inserts a block and returns its new id.
func (d *DB) CreateTimeBlock(ctx context.Context, b *TimeBlock) (int64, error) {
	now := nowText()
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO time_blocks (title, start_at, end_at, task_uid, message_id, message_header, done,
                         calendar_id, event_uid, event_title, link_state, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.Title, formatTime(b.Start), formatTime(b.End), b.TaskUID, b.MessageID, b.MessageHeader, boolToInt(b.Done),
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
// ErrTimeBlockNotFound.
func (d *DB) UpdateTimeBlock(ctx context.Context, b *TimeBlock) error {
	now := nowText()
	res, err := d.sql.ExecContext(ctx, `
UPDATE time_blocks SET title = ?, start_at = ?, end_at = ?, task_uid = ?, message_id = ?, message_header = ?,
       done = ?, calendar_id = ?, event_uid = ?, event_title = ?, link_state = ?, updated_at = ?
WHERE id = ?`,
		b.Title, formatTime(b.Start), formatTime(b.End), b.TaskUID, b.MessageID, b.MessageHeader, boolToInt(b.Done),
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

// UnlinkTaskTimeBlocks keeps a task's blocks but forgets the task, giving
// each the title it showed so it still says what it was for.
func (d *DB) UnlinkTaskTimeBlocks(ctx context.Context, uid, title string) error {
	if _, err := d.sql.ExecContext(ctx, `
UPDATE time_blocks SET title = CASE WHEN title = '' THEN ? ELSE title END, task_uid = '', updated_at = ?
WHERE task_uid = ?`, title, nowText(), uid); err != nil {
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
		done       int
	)
	if err := row.Scan(&b.ID, &b.Title, &start, &end, &b.TaskUID, &b.MessageID, &b.MessageHeader, &done,
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
	b.Done = done != 0
	return &b, nil
}
