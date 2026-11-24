package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ErrFollowUpNotFound is returned when a follow-up id has no row.
var ErrFollowUpNotFound = errors.New("storage: follow-up not found")

// FollowUp is a "remind me if nobody replies" reminder on a sent message.
// MessageID is the sent message's Message-ID without angle brackets. OutboxID
// is the queued send it belongs to, 0 once that no longer matters. FiredAt is
// zero until the reminder came due with no reply.
type FollowUp struct {
	ID         int64
	AccountID  int64
	MessageID  string
	OutboxID   int64
	Subject    string
	Recipients string
	SentAt     time.Time
	DueAt      time.Time
	FiredAt    time.Time
}

// AddFollowUp stores a reminder and returns its id. The Message-ID is stored
// without angle brackets; an empty one is refused, since nothing could ever
// be recognised as a reply to it.
func (d *DB) AddFollowUp(ctx context.Context, f *FollowUp) (int64, error) {
	key := threadKey(f.MessageID)
	if key == "" {
		return 0, fmt.Errorf("storage: follow-up needs a message id")
	}
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO follow_ups (account_id, message_id, outbox_id, subject, recipients, sent_at, due_at, fired_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)`,
		f.AccountID, key, f.OutboxID, f.Subject, f.Recipients, formatTime(f.SentAt), formatTime(f.DueAt), nowText())
	if err != nil {
		return 0, fmt.Errorf("storage: insert follow-up: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: follow-up insert id: %w", err)
	}
	f.ID = id
	f.MessageID = key
	return id, nil
}

// ListFollowUps returns every reminder, the soonest due first.
func (d *DB) ListFollowUps(ctx context.Context) ([]FollowUp, error) {
	rows, err := d.sql.QueryContext(ctx, `
SELECT id, account_id, message_id, outbox_id, subject, recipients, sent_at, due_at, fired_at
FROM follow_ups ORDER BY due_at, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list follow-ups: %w", err)
	}
	defer rows.Close()
	var out []FollowUp
	for rows.Next() {
		var (
			f                FollowUp
			sent, due, fired string
		)
		if err := rows.Scan(&f.ID, &f.AccountID, &f.MessageID, &f.OutboxID, &f.Subject, &f.Recipients, &sent, &due, &fired); err != nil {
			return nil, fmt.Errorf("storage: scan follow-up: %w", err)
		}
		if f.SentAt, err = parseTime(sent); err != nil {
			return nil, err
		}
		if f.DueAt, err = parseTime(due); err != nil {
			return nil, err
		}
		if f.FiredAt, err = parseTime(fired); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate follow-ups: %w", err)
	}
	return out, nil
}

// DeleteFollowUp removes one reminder, or returns ErrFollowUpNotFound.
func (d *DB) DeleteFollowUp(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM follow_ups WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete follow-up %d: %w", id, err)
	}
	return requireOneRow(res, ErrFollowUpNotFound)
}

// DeleteFollowUpsForOutbox removes the reminders of a queued send that was
// taken back, and reports whether there were any.
func (d *DB) DeleteFollowUpsForOutbox(ctx context.Context, outboxID int64) (bool, error) {
	if outboxID == 0 {
		return false, nil
	}
	res, err := d.sql.ExecContext(ctx, `DELETE FROM follow_ups WHERE outbox_id = ?`, outboxID)
	if err != nil {
		return false, fmt.Errorf("storage: delete follow-ups of outbox %d: %w", outboxID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: follow-ups deleted: %w", err)
	}
	return n > 0, nil
}

// MarkFollowUpFired records that a reminder came due with no reply.
func (d *DB) MarkFollowUpFired(ctx context.Context, id int64, at time.Time) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE follow_ups SET fired_at = ? WHERE id = ?`, formatTime(at), id)
	if err != nil {
		return fmt.Errorf("storage: fire follow-up %d: %w", id, err)
	}
	return requireOneRow(res, ErrFollowUpNotFound)
}

// FollowUpReplied reports whether somebody answered the reminder's message: a
// message in the same account, not sent from the account's own address, whose
// In-Reply-To or References names it. The conversation the message's id was
// filed under narrows the search, so only that thread's messages are read;
// the headers then decide, since a conversation can hold mail joined to it by
// subject alone.
func (d *DB) FollowUpReplied(ctx context.Context, f FollowUp) (bool, error) {
	key := threadKey(f.MessageID)
	if key == "" {
		return false, nil
	}
	var thread int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT thread_id FROM thread_keys WHERE account_id = ? AND key = ?`, f.AccountID, key).Scan(&thread)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: thread of follow-up %d: %w", f.ID, err)
	}
	rows, err := d.sql.QueryContext(ctx, `
SELECT m.in_reply_to, m.[references], m.from_address, a.email
FROM messages m JOIN accounts a ON a.id = m.account_id
WHERE m.account_id = ? AND m.thread_id = ? AND m.pending_delete = 0 AND `+notVaultedM, f.AccountID, thread)
	if err != nil {
		return false, fmt.Errorf("storage: replies of follow-up %d: %w", f.ID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			inReplyTo, references sql.NullString
			from, own             string
		)
		if err := rows.Scan(&inReplyTo, &references, &from, &own); err != nil {
			return false, fmt.Errorf("storage: scan reply of follow-up %d: %w", f.ID, err)
		}
		if strings.EqualFold(strings.TrimSpace(from), strings.TrimSpace(own)) {
			continue
		}
		if namesKey(decodeThreadingIDs(inReplyTo), key) || namesKey(decodeThreadingIDs(references), key) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("storage: iterate replies of follow-up %d: %w", f.ID, err)
	}
	return false, nil
}

// namesKey reports whether any of ids is key once normalized.
func namesKey(ids []string, key string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return threadKey(id) == key })
}

// MessageCopy is one cached copy of a message, by row, folder and thread.
type MessageCopy struct {
	ID       int64
	FolderID int64
	ThreadID int64
}

// CopiesOf returns every cached copy in an account of the message with
// Message-ID messageID, oldest row first, leaving out copies awaiting expunge.
func (d *DB) CopiesOf(ctx context.Context, accountID int64, messageID string) ([]MessageCopy, error) {
	key := threadKey(messageID)
	if key == "" {
		return nil, nil
	}
	rows, err := d.sql.QueryContext(ctx, `
SELECT id, folder_id, thread_id FROM messages
WHERE account_id = ? AND message_id IN (?, ?) AND pending_delete = 0 AND `+notVaulted+`
ORDER BY id`, accountID, key, "<"+key+">")
	if err != nil {
		return nil, fmt.Errorf("storage: copies of %q: %w", key, err)
	}
	defer rows.Close()
	var out []MessageCopy
	for rows.Next() {
		var c MessageCopy
		if err := rows.Scan(&c.ID, &c.FolderID, &c.ThreadID); err != nil {
			return nil, fmt.Errorf("storage: scan copy of %q: %w", key, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate copies of %q: %w", key, err)
	}
	return out, nil
}
