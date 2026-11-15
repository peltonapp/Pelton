package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// senderMatch is the WHERE condition for one bare sender address, written so
// idx_messages_sender applies. The reads name that index, since the store
// keeps no planner statistics and sqlite would otherwise pick the folder one.
var senderMatch = senderExpr + " = ?"

// SenderMessages returns the newest messages from one bare address in the
// given folders, capped at limit. Rows the list hides are left out.
func (d *DB) SenderMessages(ctx context.Context, address string, folderIDs []int64, limit int) ([]Message, error) {
	if len(folderIDs) == 0 || strings.TrimSpace(address) == "" {
		return nil, nil
	}
	folders, args := inClause(folderIDs)
	args = append([]any{normalizeAddress(address)}, args...)
	args = append(args, normalizeLimit(limit))
	query := selectMessageColumns + `
FROM messages INDEXED BY idx_messages_sender
WHERE ` + senderMatch + `
AND pending_delete = 0 AND snooze_hidden = 0 AND folder_id IN (` + folders + `)
ORDER BY date DESC, id DESC
LIMIT ?`
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: sender messages: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan sender message: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate sender messages: %w", err)
	}
	return out, nil
}

// SenderStats is how much mail one address has sent that is cached here.
type SenderStats struct {
	Count int
	First time.Time
	Last  time.Time
}

// SenderStatsFor counts the messages from one bare address in the given
// folders and the dates of the first and the latest.
func (d *DB) SenderStatsFor(ctx context.Context, address string, folderIDs []int64) (SenderStats, error) {
	if len(folderIDs) == 0 || strings.TrimSpace(address) == "" {
		return SenderStats{}, nil
	}
	folders, args := inClause(folderIDs)
	args = append([]any{normalizeAddress(address)}, args...)
	query := `SELECT COUNT(*), COALESCE(MIN(date), ''), COALESCE(MAX(date), '') FROM messages INDEXED BY idx_messages_sender
WHERE ` + senderMatch + ` AND pending_delete = 0 AND folder_id IN (` + folders + `)`
	var (
		s           SenderStats
		first, last string
	)
	if err := d.sql.QueryRowContext(ctx, query, args...).Scan(&s.Count, &first, &last); err != nil {
		return SenderStats{}, fmt.Errorf("storage: sender stats: %w", err)
	}
	var err error
	if s.First, err = parseTime(first); err != nil {
		return SenderStats{}, err
	}
	if s.Last, err = parseTime(last); err != nil {
		return SenderStats{}, err
	}
	return s, nil
}

func normalizeAddress(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}
