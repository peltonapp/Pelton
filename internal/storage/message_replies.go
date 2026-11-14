package storage

import (
	"context"
	"fmt"
	"time"
)

// Reply kinds recorded for a message sent from Pelton.
const (
	ReplyKindReply   = "reply"
	ReplyKindForward = "forward"
)

// RecordOutgoingReply notes that the outgoing message replyID answers or
// forwards originalID (kind is ReplyKindReply or ReplyKindForward). It is
// written before the message is queued and takes effect through
// MarkReplySent once the server accepted it. Either id may carry angle
// brackets; an empty one records nothing.
func (d *DB) RecordOutgoingReply(ctx context.Context, accountID int64, replyID, originalID, kind string) error {
	replyID, originalID = threadKey(replyID), threadKey(originalID)
	if replyID == "" || originalID == "" {
		return nil
	}
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO message_replies (account_id, reply_message_id, original_message_id, kind, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (account_id, reply_message_id, kind) DO UPDATE SET original_message_id = excluded.original_message_id`,
		accountID, replyID, originalID, kind, nowText())
	if err != nil {
		return fmt.Errorf("storage: record outgoing %s: %w", kind, err)
	}
	return nil
}

// ReplyStateChange is one original message whose answered or forwarded state
// MarkReplySent changed.
type ReplyStateChange struct {
	ID       int64
	FolderID int64
}

// MarkReplySent marks the records of the sent message replyID as sent and sets
// \Answered or $Forwarded on every copy of the message it answered or
// forwarded, queued for the next sync to push like a flag the user set. It
// returns the rows it changed; copies already carrying the flag are left alone.
func (d *DB) MarkReplySent(ctx context.Context, accountID int64, replyID string) ([]ReplyStateChange, error) {
	replyID = threadKey(replyID)
	if replyID == "" {
		return nil, nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: begin reply sent: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
SELECT original_message_id, kind FROM message_replies
WHERE account_id = ? AND reply_message_id = ?`, accountID, replyID)
	if err != nil {
		return nil, fmt.Errorf("storage: read reply records: %w", err)
	}
	type record struct{ original, kind string }
	var records []record
	for rows.Next() {
		var r record
		if err := rows.Scan(&r.original, &r.kind); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan reply record: %w", err)
		}
		records = append(records, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate reply records: %w", err)
	}

	var changed []ReplyStateChange
	for _, r := range records {
		flag := FlagAnswered
		if r.kind == ReplyKindForward {
			flag = FlagForwarded
		}
		matched, err := copiesWithout(ctx, tx, accountID, r.original, flag)
		if err != nil {
			return nil, err
		}
		for _, c := range matched {
			if _, err := tx.ExecContext(ctx, `
UPDATE messages SET flags = flags | ?, pending_flag_mask = pending_flag_mask | ?, pending_flags = 1
WHERE id = ?`, uint8(flag), uint8(flag), c.ID); err != nil {
				return nil, fmt.Errorf("storage: mark message %d %s: %w", c.ID, r.kind, err)
			}
		}
		changed = append(changed, matched...)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE message_replies SET sent = 1 WHERE account_id = ? AND reply_message_id = ?`,
		accountID, replyID); err != nil {
		return nil, fmt.Errorf("storage: mark reply records sent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("storage: commit reply sent: %w", err)
	}
	return changed, nil
}

// copiesWithout lists the rows of a Message-ID in one account that do not
// have flag set yet.
func copiesWithout(ctx context.Context, ex execer, accountID int64, messageID string, flag Flag) ([]ReplyStateChange, error) {
	rows, err := ex.QueryContext(ctx, `
SELECT id, folder_id FROM messages
WHERE account_id = ? AND message_id IN (?, ?) AND pending_delete = 0 AND flags & ? = 0`,
		accountID, messageID, "<"+messageID+">", uint8(flag))
	if err != nil {
		return nil, fmt.Errorf("storage: copies of %q: %w", messageID, err)
	}
	defer rows.Close()
	var out []ReplyStateChange
	for rows.Next() {
		var c ReplyStateChange
		if err := rows.Scan(&c.ID, &c.FolderID); err != nil {
			return nil, fmt.Errorf("storage: scan copy: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SentReplies returns the stored messages sent from Pelton that answered
// (replies) or forwarded (forwards) the message with Message-ID originalID,
// oldest first. A sent message counts once it reached the local cache, which
// is when the Sent folder next syncs; one copy of each is returned.
func (d *DB) SentReplies(ctx context.Context, accountID int64, originalID string) (replies, forwards []int64, err error) {
	originalID = threadKey(originalID)
	if originalID == "" {
		return nil, nil, nil
	}
	rows, err := d.sql.QueryContext(ctx, `
SELECT r.kind, MIN(m.id) FROM message_replies r
JOIN messages m ON m.account_id = r.account_id
  AND m.message_id IN (r.reply_message_id, '<' || r.reply_message_id || '>')
  AND m.pending_delete = 0
WHERE r.account_id = ? AND r.original_message_id = ? AND r.sent = 1
GROUP BY r.kind, r.reply_message_id
ORDER BY MIN(m.date), MIN(m.id)`, accountID, originalID)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: sent replies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			kind string
			id   int64
		)
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, nil, fmt.Errorf("storage: scan sent reply: %w", err)
		}
		if kind == ReplyKindForward {
			forwards = append(forwards, id)
		} else {
			replies = append(replies, id)
		}
	}
	return replies, forwards, rows.Err()
}

// PruneUnsentReplies drops records of messages that never went out (the send
// was cancelled or discarded) once they are older than maxAge, and returns
// how many it removed.
func (d *DB) PruneUnsentReplies(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-maxAge).UTC().Format(timestampLayout)
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM message_replies WHERE sent = 0 AND created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("storage: prune unsent replies: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: rows affected: %w", err)
	}
	return int(n), nil
}
