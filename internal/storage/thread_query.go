package storage

import (
	"context"
	"fmt"
	"time"
)

// ThreadQuery selects a page of conversations. Query picks where a thread must
// have a message to be listed and filters those messages the way the flat list
// does; its OneCopy is ignored, since a conversation already lists a message
// once. MemberFolderIDs are the folders whose messages count as members of a
// listed thread (the account's other folders, so a reply in Sent belongs to an
// inbox conversation); the listed folders always count.
type ThreadQuery struct {
	Query           MessageQuery
	MemberFolderIDs []int64
}

// ThreadMember is the light view of one message in a conversation.
type ThreadMember struct {
	ID             int64
	AccountID      int64
	FolderID       int64
	MessageID      string
	FromName       string
	FromAddress    string
	Date           time.Time
	Flags          Flag
	HasAttachments bool
}

// ThreadPage is one conversation in a listing. Head is the newest message of
// the thread that matched the query, which is also what the page is ordered
// by. Members are in date order, oldest first, each message listed once even
// when copies of it sit in several folders. ThreadID is 0 for a message the
// thread backfill has not reached yet; it is then the only member.
type ThreadPage struct {
	ThreadID int64
	Head     Message
	Members  []ThreadMember
}

// threadKeyExpr groups unthreaded rows on their own, so the list is complete
// while the backfill is still running.
const threadKeyExpr = `CASE WHEN thread_id = 0 THEN -id ELSE thread_id END`

// QueryThreads returns a page of conversations matching q, newest head first.
// The order is the flat list's (date, then uid) with the row id as the final
// tie break, so paging never repeats or skips a thread while nothing changes.
func (d *DB) QueryThreads(ctx context.Context, q ThreadQuery) ([]ThreadPage, error) {
	if len(q.Query.FolderIDs) == 0 {
		return nil, nil
	}
	where, args := threadWhere(q.Query)
	query := `
SELECT id FROM (
  SELECT id, date, uid, ROW_NUMBER() OVER (
    PARTITION BY ` + threadKeyExpr + ` ORDER BY date DESC, uid DESC, id DESC) AS rn
  FROM messages WHERE ` + where + `
) WHERE rn = 1
ORDER BY date DESC, uid DESC, id DESC
LIMIT ? OFFSET ?`
	args = append(args, normalizeLimit(q.Query.Limit), q.Query.Offset)
	headIDs, err := queryInt64s(ctx, d.sql, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: query threads: %w", err)
	}
	if len(headIDs) == 0 {
		return nil, nil
	}

	heads, err := d.messagesByID(ctx, headIDs)
	if err != nil {
		return nil, err
	}
	var threadIDs []int64
	for _, id := range headIDs {
		if h, ok := heads[id]; ok && h.ThreadID != 0 {
			threadIDs = append(threadIDs, h.ThreadID)
		}
	}
	members, err := d.threadMembers(ctx, threadIDs, append(append([]int64(nil), q.MemberFolderIDs...), q.Query.FolderIDs...))
	if err != nil {
		return nil, err
	}

	listed := make(map[int64]bool, len(q.Query.FolderIDs))
	for _, id := range q.Query.FolderIDs {
		listed[id] = true
	}
	pages := make([]ThreadPage, 0, len(headIDs))
	for _, id := range headIDs {
		head, ok := heads[id]
		if !ok {
			continue
		}
		page := ThreadPage{ThreadID: head.ThreadID, Head: head}
		if head.ThreadID == 0 {
			page.Members = []ThreadMember{memberOf(head)}
		} else {
			page.Members = dedupeMembers(members[head.ThreadID], head.ID, listed)
		}
		pages = append(pages, page)
	}
	return pages, nil
}

// CountThreads returns how many conversations q lists, ignoring its paging.
func (d *DB) CountThreads(ctx context.Context, q ThreadQuery) (int, error) {
	if len(q.Query.FolderIDs) == 0 {
		return 0, nil
	}
	where, args := threadWhere(q.Query)
	var n int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT `+threadKeyExpr+`) FROM messages WHERE `+where, args...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("storage: count threads: %w", err)
	}
	return n, nil
}

// ThreadMessages returns the messages of one conversation in date order,
// oldest first, each listed once. A non-empty folderIDs keeps only members in
// those folders. Hidden rows (awaiting expunge, snoozed away) are left out.
func (d *DB) ThreadMessages(ctx context.Context, threadID int64, folderIDs []int64) ([]Message, error) {
	if threadID <= 0 {
		return nil, nil
	}
	query := selectMessageColumns + `
FROM messages WHERE thread_id = ? AND pending_delete = 0 AND snooze_hidden = 0`
	args := []any{threadID}
	if len(folderIDs) > 0 {
		marks, folderArgs := inClause(folderIDs)
		query += ` AND folder_id IN (` + marks + `)`
		args = append(args, folderArgs...)
	}
	query += ` ORDER BY date ASC, uid ASC, id ASC`
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: thread %d messages: %w", threadID, err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan thread message: %w", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// threadWhere is the flat list's filter without the one-copy rule.
func threadWhere(q MessageQuery) (string, []any) {
	q.OneCopy = false
	return messageWhere(q)
}

func (d *DB) messagesByID(ctx context.Context, ids []int64) (map[int64]Message, error) {
	marks, args := inClause(ids)
	rows, err := d.sql.QueryContext(ctx, selectMessageColumns+`
FROM messages WHERE id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: messages by id: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]Message, len(ids))
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan message: %w", err)
		}
		out[m.ID] = *m
	}
	return out, rows.Err()
}

// threadMembers reads the light member rows of several threads at once,
// grouped by thread in date order.
func (d *DB) threadMembers(ctx context.Context, threadIDs, folderIDs []int64) (map[int64][]ThreadMember, error) {
	out := make(map[int64][]ThreadMember, len(threadIDs))
	if len(threadIDs) == 0 {
		return out, nil
	}
	threadMarks, args := inClause(threadIDs)
	folderMarks, folderArgs := inClause(folderIDs)
	args = append(args, folderArgs...)
	rows, err := d.sql.QueryContext(ctx, `
SELECT id, account_id, folder_id, thread_id, message_id, from_name, from_address, date, flags, has_attachments
FROM messages
WHERE thread_id IN (`+threadMarks+`) AND folder_id IN (`+folderMarks+`)
  AND pending_delete = 0 AND snooze_hidden = 0
ORDER BY date ASC, uid ASC, id ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: thread members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			m        ThreadMember
			threadID int64
			date     string
			flags    uint8
			hasAtt   int
		)
		if err := rows.Scan(&m.ID, &m.AccountID, &m.FolderID, &threadID, &m.MessageID,
			&m.FromName, &m.FromAddress, &date, &flags, &hasAtt); err != nil {
			return nil, fmt.Errorf("storage: scan thread member: %w", err)
		}
		if m.Date, err = parseTime(date); err != nil {
			return nil, err
		}
		m.Flags = Flag(flags)
		m.HasAttachments = hasAtt != 0
		out[threadID] = append(out[threadID], m)
	}
	return out, rows.Err()
}

func memberOf(m Message) ThreadMember {
	return ThreadMember{
		ID:             m.ID,
		AccountID:      m.AccountID,
		FolderID:       m.FolderID,
		MessageID:      m.MessageID,
		FromName:       m.FromName,
		FromAddress:    m.FromAddress,
		Date:           m.Date,
		Flags:          m.Flags,
		HasAttachments: m.HasAttachments,
	}
}

// dedupeMembers keeps one row per Message-ID. The head wins, then a copy in a
// listed folder, then the first one stored, so the ids handed out are the ones
// the listing itself shows.
func dedupeMembers(members []ThreadMember, headID int64, listed map[int64]bool) []ThreadMember {
	rank := func(m ThreadMember) int {
		switch {
		case m.ID == headID:
			return 2
		case listed[m.FolderID]:
			return 1
		}
		return 0
	}
	out := make([]ThreadMember, 0, len(members))
	seen := make(map[string]int, len(members))
	for _, m := range members {
		if m.MessageID == "" {
			out = append(out, m)
			continue
		}
		if i, ok := seen[m.MessageID]; ok {
			if rank(m) > rank(out[i]) {
				out[i] = m
			}
			continue
		}
		seen[m.MessageID] = len(out)
		out = append(out, m)
	}
	return out
}
