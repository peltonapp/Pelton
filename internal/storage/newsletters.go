package storage

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// NewsletterSender is one sender of List-Unsubscribe mail, aggregated over the
// folders a NewsletterSenders call read. The Latest fields describe the
// sender's newest message, which is the one an unsubscribe acts on.
type NewsletterSender struct {
	// Email is the bare lower-case address.
	Email  string
	Count  int
	Unread int
	Bytes  int64
	First  time.Time
	Last   time.Time
	// Recent counts the messages dated on or after the recentSince argument.
	Recent int

	LatestID            int64
	LatestFromAddress   string
	LatestFromName      string
	ListID              string
	ListUnsubscribe     string
	ListUnsubscribePost bool
}

func newsletterSendersSQL(folderIDs []int64, recentSince time.Time) (string, []any) {
	folders, folderArgs := inClause(folderIDs)
	args := []any{uint8(FlagSeen), formatTime(recentSince)}
	args = append(args, folderArgs...)
	args = append(args, folderArgs...)
	return `
WITH agg AS (
    SELECT ` + senderEmailSQL + ` AS email, COUNT(*) AS n,
           SUM(flags & ? = 0) AS unread, SUM(size_bytes) AS bytes,
           MIN(date) AS first, MAX(date) AS last, SUM(date >= ?) AS recent
    ` + fromListMail + ` AND pending_delete = 0 AND folder_id IN (` + folders + `)
    GROUP BY ` + senderEmailSQL + `
)
SELECT agg.email, agg.n, agg.unread, agg.bytes, agg.first, agg.last, agg.recent,
       m.id, m.from_address, m.from_name, m.list_id, m.list_unsubscribe, m.list_unsubscribe_post
FROM agg
JOIN messages m ON m.id = (
    SELECT id ` + fromListMail + ` AND ` + senderEmailSQL + ` = agg.email
      AND pending_delete = 0 AND folder_id IN (` + folders + `)
    ORDER BY date DESC LIMIT 1
)
WHERE agg.email != '' AND ` + notVaultedM, args
}

// NewsletterSenders aggregates every sender whose mail carries List-Unsubscribe
// in folderIDs, leaving out rows awaiting expunge. Each sender is one row,
// ordered by address. The aggregate reads only the sender index, so it stays
// fast on a mailbox of hundreds of thousands of messages; the newest message of
// each sender is then one index seek. No folders means no senders.
func (d *DB) NewsletterSenders(ctx context.Context, folderIDs []int64, recentSince time.Time) ([]NewsletterSender, error) {
	if len(folderIDs) == 0 {
		return nil, nil
	}
	query, args := newsletterSendersSQL(folderIDs, recentSince)
	var out []NewsletterSender
	err := d.queryRows(ctx, "newsletter senders", query, args, func(rows *sql.Rows) error {
		var (
			s           NewsletterSender
			first, last string
			post        int
			err         error
		)
		if err := rows.Scan(&s.Email, &s.Count, &s.Unread, &s.Bytes, &first, &last, &s.Recent,
			&s.LatestID, &s.LatestFromAddress, &s.LatestFromName, &s.ListID, &s.ListUnsubscribe, &post); err != nil {
			return err
		}
		if s.First, err = parseTime(first); err != nil {
			return err
		}
		if s.Last, err = parseTime(last); err != nil {
			return err
		}
		s.ListUnsubscribePost = post != 0
		out = append(out, s)
		return nil
	})
	return out, err
}

// SenderMessagesQuery picks the List-Unsubscribe mail of some senders for a
// bulk action.
type SenderMessagesQuery struct {
	// Emails are bare addresses; case does not matter.
	Emails    []string
	FolderIDs []int64
	// Before keeps only messages dated earlier; the zero time keeps all.
	Before time.Time
	// KeepLatest leaves each sender's newest message out of the result.
	KeepLatest bool
}

func senderMessageIDsSQL(q SenderMessagesQuery) (string, []any) {
	emails := make([]string, len(q.Emails))
	for i, e := range q.Emails {
		emails[i] = strings.ToLower(strings.TrimSpace(e))
	}
	emailMarks, args := textList(emails)
	folders, folderArgs := inClause(q.FolderIDs)
	return `
SELECT id, ` + senderEmailSQL + ` AS email, date ` + fromListMail + ` AND pending_delete = 0
  AND ` + senderEmailSQL + ` IN (` + emailMarks + `)
  AND folder_id IN (` + folders + `)
ORDER BY email, date DESC`, append(args, folderArgs...)
}

// SenderMessageIDs returns the ids of the List-Unsubscribe mail q selects,
// newest first per sender, leaving out rows awaiting expunge. KeepLatest skips
// each sender's newest message before the Before cut applies, so keeping the
// latest never removes it whatever its age. Mail without a date counts as
// older than any cut.
func (d *DB) SenderMessageIDs(ctx context.Context, q SenderMessagesQuery) ([]int64, error) {
	if len(q.Emails) == 0 || len(q.FolderIDs) == 0 {
		return nil, nil
	}
	query, args := senderMessageIDsSQL(q)
	before := formatTime(q.Before)
	var (
		out  []int64
		prev string
	)
	err := d.queryRows(ctx, "sender message ids", query, args, func(rows *sql.Rows) error {
		var (
			id          int64
			email, date string
		)
		if err := rows.Scan(&id, &email, &date); err != nil {
			return err
		}
		first := email != prev
		prev = email
		if q.KeepLatest && first {
			return nil
		}
		if before != "" && date >= before {
			return nil
		}
		out = append(out, id)
		return nil
	})
	return out, err
}
