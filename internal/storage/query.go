package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// MessageQuery selects a page of messages. It supports one or more folders (the
// unified views pass several folder ids at once) and an optional flag filter so
// the unified Flagged view can ask for only flagged rows. A zero RequireFlags
// means no flag filter. Limit and Offset drive list pagination; a non positive
// Limit means no cap.
type MessageQuery struct {
	FolderIDs    []int64
	RequireFlags Flag
	Limit        int
	Offset       int
	// OneCopy lists a message once when its Message-ID sits in several of the
	// folders of one account, which is how gmail labels look over imap: a
	// starred inbox message is in INBOX, All Mail, Starred and Important. The
	// first stored copy is the one listed.
	OneCopy bool
}

// QueryMessages returns the page of messages matching q, newest first. It is the
// single read path the ui list uses for both per folder and unified views.
func (d *DB) QueryMessages(ctx context.Context, q MessageQuery) ([]Message, error) {
	if len(q.FolderIDs) == 0 {
		return nil, nil
	}

	query, args := pageQuery(q)
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: query messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan message: %w", err)
		}
		messages = append(messages, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate messages: %w", err)
	}
	return messages, nil
}

// pageQuery builds the read for one page of the list.
//
// One folder is a straight ordered read: the list index carries folder, date and
// uid in the order the page wants them, so sqlite walks it and stops at the
// limit.
//
// Several folders cannot work that way. An index is sorted per folder, so an
// `IN (...)` over five of them has no single ordered path through and sqlite
// falls back to collecting every matching row and sorting the lot, which at 14k
// messages is the slowest screen in the app. Instead each folder is asked for
// its own newest rows through the index and the small results are merged. Only
// the first offset+limit of any one folder can appear in the page, so that is
// all each subquery has to return: five reads of a few hundred rows rather than
// one sort of everything.
func pageQuery(q MessageQuery) (string, []any) {
	limit := normalizeLimit(q.Limit)
	if len(q.FolderIDs) == 1 {
		where, args := messageWhere(q)
		query := selectMessageColumns + `
FROM messages
WHERE ` + where + `
ORDER BY date DESC, uid DESC
LIMIT ? OFFSET ?`
		return query, append(args, limit, q.Offset)
	}

	// how deep into one folder the page could possibly reach. A no-cap limit
	// leaves the subqueries uncapped too, which is the export path rather than
	// the list.
	perFolder := limit
	if perFolder > 0 {
		perFolder += q.Offset
	}

	parts := make([]string, 0, len(q.FolderIDs))
	args := make([]any, 0, len(q.FolderIDs)*3+2)
	for _, id := range q.FolderIDs {
		where, folderArgs := folderWhere(q, []int64{id})
		parts = append(parts, `SELECT * FROM (`+selectMessageColumns+`
FROM messages WHERE `+where+`
ORDER BY date DESC, uid DESC LIMIT ?)`)
		args = append(args, folderArgs...)
		args = append(args, perFolder)
	}
	query := strings.Join(parts, "\nUNION ALL\n") + `
ORDER BY date DESC, uid DESC
LIMIT ? OFFSET ?`
	return query, append(args, limit, q.Offset)
}

// QueryMessageIDs returns the ids of every message a query matches, newest
// first, ignoring Limit and Offset. Select-all is what wants this: the list
// holds only the pages that were scrolled to, and selecting a whole mailbox
// means naming every row in it, which is one id column rather than the bodies
// that make a page read expensive.
func (d *DB) QueryMessageIDs(ctx context.Context, q MessageQuery) ([]int64, error) {
	if len(q.FolderIDs) == 0 {
		return nil, nil
	}
	where, args := messageWhere(q)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id FROM messages WHERE `+where+` ORDER BY date DESC, uid DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: query message ids: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, 256)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan message id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate message ids: %w", err)
	}
	return ids, nil
}

// CountMessages returns how many messages match q ignoring its limit and offset,
// so the ui can show totals and decide whether more pages exist.
func (d *DB) CountMessages(ctx context.Context, q MessageQuery) (int, error) {
	if len(q.FolderIDs) == 0 {
		return 0, nil
	}
	where, args := messageWhere(q)
	var n int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE `+where, args...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("storage: count messages: %w", err)
	}
	return n, nil
}

// FolderCounts returns the total and unread message counts for a single folder.
// Unread means the \Seen flag bit is not set.
func (d *DB) FolderCounts(ctx context.Context, folderID int64) (total, unread int, err error) {
	const query = `
SELECT COUNT(*), COALESCE(SUM(CASE WHEN flags & ? = 0 THEN 1 ELSE 0 END), 0)
FROM messages WHERE folder_id = ?`
	if err := d.sql.QueryRowContext(ctx, query, uint8(FlagSeen), folderID).Scan(&total, &unread); err != nil {
		return 0, 0, fmt.Errorf("storage: folder counts %d: %w", folderID, err)
	}
	return total, unread, nil
}

// UnreadCount returns the number of unread messages across the given folders,
// used for unified view badges.
func (d *DB) UnreadCount(ctx context.Context, folderIDs []int64) (int, error) {
	if len(folderIDs) == 0 {
		return 0, nil
	}
	placeholders, args := inClause(folderIDs)
	args = append([]any{uint8(FlagSeen)}, args...)
	query := `SELECT COUNT(*) FROM messages WHERE flags & ? = 0 AND folder_id IN (` + placeholders + `)`
	var n int
	if err := d.sql.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: unread count: %w", err)
	}
	return n, nil
}

// LatestMessageFrom returns the most recent cached message whose sender matches
// value: an exact from-address when matchDomain is false, or any sender in the
// domain when it is true. Returns nil (no error) when nothing matches, so the
// image allowlist ui can show an example message for a trusted sender/domain.
func (d *DB) LatestMessageFrom(ctx context.Context, value string, matchDomain bool) (*Message, error) {
	cond := "LOWER(from_address) = ?"
	arg := strings.ToLower(value)
	if matchDomain {
		cond = "LOWER(from_address) LIKE ?"
		arg = "%@" + strings.ToLower(value)
	}
	query := selectMessageColumns + `
FROM messages
WHERE ` + cond + `
ORDER BY date DESC, uid DESC
LIMIT 1`
	m, err := scanMessage(d.sql.QueryRowContext(ctx, query, arg))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: latest message from %q: %w", value, err)
	}
	return m, nil
}

// messageWhere builds the shared WHERE clause and its args for QueryMessages and
// CountMessages so the two never drift apart.
func messageWhere(q MessageQuery) (string, []any) {
	return folderWhere(q, q.FolderIDs)
}

// folderWhere is messageWhere narrowed to folderIDs, for the per folder reads of
// a unified page. OneCopy still looks for other copies across all of q's
// folders, so a page picks the same copy whichever folder it reads.
func folderWhere(q MessageQuery, folderIDs []int64) (string, []any) {
	placeholders, args := inClause(folderIDs)
	// pending_delete rows are awaiting server expunge; hide them from the list so
	// a local delete disappears immediately and reappears nowhere. snooze_hidden
	// rows are snoozed-and-hidden; they stay out of the list until the snooze fires
	// (the poller flips the bit back).
	where := "pending_delete = 0 AND snooze_hidden = 0 AND folder_id IN (" + placeholders + ")"
	if q.RequireFlags != 0 {
		// every requested flag bit must be set: (flags & mask) = mask.
		where += " AND (flags & ?) = ?"
		args = append(args, uint8(q.RequireFlags), uint8(q.RequireFlags))
	}
	if q.OneCopy {
		all, allArgs := inClause(q.FolderIDs)
		other := "o.pending_delete = 0 AND o.snooze_hidden = 0 AND o.folder_id IN (" + all + ")"
		if q.RequireFlags != 0 {
			other += " AND (o.flags & ?) = ?"
			allArgs = append(allArgs, uint8(q.RequireFlags), uint8(q.RequireFlags))
		}
		where += ` AND (message_id = '' OR NOT EXISTS (
SELECT 1 FROM messages o WHERE o.account_id = messages.account_id
AND o.message_id = messages.message_id AND o.id < messages.id AND ` + other + `))`
		args = append(args, allArgs...)
	}
	return where, args
}

// inClause renders n "?" placeholders and the matching args slice for an IN list.
func inClause(ids []int64) (string, []any) {
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args[i] = id
	}
	return strings.Join(marks, ", "), args
}
