package storage

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The mailbox statistics behind the insights page. Each one is a single
// aggregate in sql over an index holding every column it touches, so a
// statistic never opens a message row and its bodies. Rows awaiting expunge are
// left out everywhere. Each read builds its statement in a *SQL function so the
// tests can check its query plan.

// StatsScope names the mail a volume, people or reply statistic reads.
type StatsScope struct {
	// Counted are the folders holding real mail once: everything but drafts,
	// trash, junk and server side collections such as Gmail's All Mail.
	Counted []int64
	// Sent are the counted folders holding mail the user sent.
	Sent []int64
	// Own are the user's own addresses, lower case.
	Own []string
	// Since starts the date range. The zero time reads all mail.
	Since time.Time
	// Offset moves stored UTC dates into local time for day, weekday and hour
	// buckets.
	Offset time.Duration
}

// received is Counted without Sent.
func (s StatsScope) received() []int64 {
	out := make([]int64, 0, len(s.Counted))
	for _, id := range s.Counted {
		if !slices.Contains(s.Sent, id) {
			out = append(out, id)
		}
	}
	return out
}

func (s StatsScope) since() string {
	return formatTime(s.Since)
}

// senderEmailSQL cuts the bare lower-case address out of from_address. It is
// the leading expression of idx_messages_newsletters and idx_messages_people
// and has to stay identical to the one in migration 0055.
const senderEmailSQL = `lower(trim(CASE WHEN instr(from_address, '<') > 0 AND instr(from_address, '>') > instr(from_address, '<') THEN substr(from_address, instr(from_address, '<') + 1, instr(from_address, '>') - instr(from_address, '<') - 1) ELSE from_address END))`

// The two partial sender indexes split the mail between them: list mail, which
// offers List-Unsubscribe, and the rest.
const (
	fromListMail  = `FROM messages INDEXED BY idx_messages_newsletters WHERE list_unsubscribe != '' AND ` + notVaulted
	fromOtherMail = `FROM messages INDEXED BY idx_messages_people WHERE list_unsubscribe = '' AND ` + notVaulted
)

// offsetModifier is the sqlite date modifier that moves a UTC date by offset.
func offsetModifier(offset time.Duration) string {
	return fmt.Sprintf("%+d minutes", int(offset.Minutes()))
}

// textList renders placeholders and args for a list of strings.
func textList(values []string) (string, []any) {
	marks := make([]string, len(values))
	args := make([]any, len(values))
	for i, v := range values {
		marks[i] = "?"
		args[i] = v
	}
	return strings.Join(marks, ", "), args
}

// queryRows runs query and hands every row to scan.
func (d *DB) queryRows(ctx context.Context, what, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("storage: %s: %w", what, err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("storage: scan %s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: iterate %s: %w", what, err)
	}
	return nil
}

// FolderUsage is how many messages a folder holds and their size.
type FolderUsage struct {
	FolderID int64
	Count    int
	Bytes    int64
}

func folderUsageSQL(folderIDs []int64) (string, []any) {
	folders, args := inClause(folderIDs)
	return `
SELECT folder_id, COUNT(*), COALESCE(SUM(size_bytes), 0)
FROM messages INDEXED BY idx_messages_date
WHERE pending_delete = 0 AND folder_id IN (` + folders + `) AND ` + notVaulted + `
GROUP BY folder_id`, args
}

// FolderUsage sums count and size per folder over folderIDs. Folders without
// mail are absent from the result.
func (d *DB) FolderUsage(ctx context.Context, folderIDs []int64) ([]FolderUsage, error) {
	if len(folderIDs) == 0 {
		return nil, nil
	}
	query, args := folderUsageSQL(folderIDs)
	var out []FolderUsage
	err := d.queryRows(ctx, "folder usage", query, args, func(rows *sql.Rows) error {
		var u FolderUsage
		if err := rows.Scan(&u.FolderID, &u.Count, &u.Bytes); err != nil {
			return err
		}
		out = append(out, u)
		return nil
	})
	return out, err
}

// SenderUsage is one sender with how much mail and storage they account for.
// FromAddress and FromName come from the sender's newest message.
type SenderUsage struct {
	Email       string
	FromAddress string
	FromName    string
	Count       int
	Bytes       int64
}

func biggestSendersSQL(folderIDs []int64, limit int) (string, []any) {
	folders, folderArgs := inClause(folderIDs)
	part := `SELECT ` + senderEmailSQL + ` AS email, size_bytes `
	filter := ` AND pending_delete = 0 AND folder_id IN (` + folders + `) AND ` + notVaulted
	args := append(append([]any{}, folderArgs...), folderArgs...)
	return `
SELECT email, COUNT(*), COALESCE(SUM(size_bytes), 0) AS bytes FROM (
    ` + part + fromListMail + filter + `
    UNION ALL
    ` + part + fromOtherMail + filter + `
)
WHERE email != ''
GROUP BY email
ORDER BY bytes DESC, email
LIMIT ?`, append(args, normalizeLimit(limit))
}

// BiggestSenders returns the senders whose mail takes the most space in
// folderIDs, largest first, at most limit of them.
func (d *DB) BiggestSenders(ctx context.Context, folderIDs []int64, limit int) ([]SenderUsage, error) {
	if len(folderIDs) == 0 {
		return nil, nil
	}
	query, args := biggestSendersSQL(folderIDs, limit)
	return d.senderUsage(ctx, "biggest senders", query, args)
}

func topSendersSQL(s StatsScope, limit int) (string, []any) {
	folders, args := inClause(s.received())
	own := ""
	if len(s.Own) > 0 {
		marks, ownArgs := textList(s.Own)
		own = " AND " + senderEmailSQL + " NOT IN (" + marks + ")"
		args = append(args, ownArgs...)
	}
	return `
SELECT ` + senderEmailSQL + ` AS email, COUNT(*) AS n, COALESCE(SUM(size_bytes), 0)
` + fromOtherMail + ` AND pending_delete = 0
  AND folder_id IN (` + folders + `) AND ` + notVaulted + own + ` AND date >= ?
GROUP BY ` + senderEmailSQL + `
HAVING email != ''
ORDER BY n DESC, email
LIMIT ?`, append(args, s.since(), normalizeLimit(limit))
}

// TopSenders returns who writes to the user most in the received part of s:
// people rather than lists, so mail offering List-Unsubscribe and mail from the
// user's own addresses are left out. Most messages first, at most limit.
func (d *DB) TopSenders(ctx context.Context, s StatsScope, limit int) ([]SenderUsage, error) {
	if len(s.received()) == 0 {
		return nil, nil
	}
	query, args := topSendersSQL(s, limit)
	return d.senderUsage(ctx, "top senders", query, args)
}

func (d *DB) senderUsage(ctx context.Context, what, query string, args []any) ([]SenderUsage, error) {
	var out []SenderUsage
	err := d.queryRows(ctx, what, query, args, func(rows *sql.Rows) error {
		var u SenderUsage
		if err := rows.Scan(&u.Email, &u.Count, &u.Bytes); err != nil {
			return err
		}
		out = append(out, u)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err := d.senderName(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// senderNameSQL looks up a sender's newest message that carries a display
// name, or the newest one when none does, in each sender index and keeps the
// better of the two.
const senderNameSQL = `
SELECT from_address, from_name FROM (
    SELECT * FROM (SELECT from_address, from_name, date, ` + namedSQL + ` AS named ` + fromListMail + ` AND ` + senderEmailSQL + ` = ?1 ORDER BY named DESC, date DESC LIMIT 1)
    UNION ALL
    SELECT * FROM (SELECT from_address, from_name, date, ` + namedSQL + ` AS named ` + fromOtherMail + ` AND ` + senderEmailSQL + ` = ?1 ORDER BY named DESC, date DESC LIMIT 1)
)
ORDER BY named DESC, date DESC LIMIT 1`

// namedSQL is 1 for a message whose sender carries a display name.
const namedSQL = `(from_name != '' OR instr(from_address, '<') > 1)`

// senderName fills the From fields from the sender's newest message.
func (d *DB) senderName(ctx context.Context, u *SenderUsage) error {
	if err := d.sql.QueryRowContext(ctx, senderNameSQL, u.Email).Scan(&u.FromAddress, &u.FromName); err != nil {
		return fmt.Errorf("storage: sender name %q: %w", u.Email, err)
	}
	return nil
}

// AttachmentUsage is one stored attachment and the message holding it.
type AttachmentUsage struct {
	ID          int64
	MessageID   int64
	Filename    string
	ContentType string
	Bytes       int64
	Subject     string
	FromAddress string
	FromName    string
	Date        time.Time
}

func biggestAttachmentsSQL(folderIDs []int64, limit int) (string, []any) {
	folders, args := inClause(folderIDs)
	return `
SELECT a.id, a.message_id, a.filename, a.content_type, a.size_bytes,
       m.subject, m.from_address, m.from_name, m.date
FROM attachments a INDEXED BY idx_attachments_size
JOIN messages m ON m.id = a.message_id
WHERE m.pending_delete = 0 AND m.folder_id IN (` + folders + `) AND ` + notVaultedM + `
ORDER BY a.size_bytes DESC
LIMIT ?`, append(args, normalizeLimit(limit))
}

// BiggestAttachments returns the largest attachments of messages in
// folderIDs, largest first, at most limit.
func (d *DB) BiggestAttachments(ctx context.Context, folderIDs []int64, limit int) ([]AttachmentUsage, error) {
	if len(folderIDs) == 0 {
		return nil, nil
	}
	query, args := biggestAttachmentsSQL(folderIDs, limit)
	var out []AttachmentUsage
	err := d.queryRows(ctx, "biggest attachments", query, args, func(rows *sql.Rows) error {
		var (
			u    AttachmentUsage
			date string
			err  error
		)
		if err := rows.Scan(&u.ID, &u.MessageID, &u.Filename, &u.ContentType, &u.Bytes,
			&u.Subject, &u.FromAddress, &u.FromName, &date); err != nil {
			return err
		}
		if u.Date, err = parseTime(date); err != nil {
			return err
		}
		out = append(out, u)
		return nil
	})
	return out, err
}

// DayVolume is the mail received and sent on one local day, YYYY-MM-DD.
type DayVolume struct {
	Day      string
	Received int
	Sent     int
}

func dailyVolumeSQL(s StatsScope) (string, []any) {
	counted, countedArgs := inClause(s.Counted)
	sentExpr := "0"
	var sentArgs []any
	if len(s.Sent) > 0 {
		var marks string
		marks, sentArgs = inClause(s.Sent)
		sentExpr = "folder_id IN (" + marks + ")"
	}
	args := []any{offsetModifier(s.Offset)}
	args = append(args, sentArgs...)
	args = append(args, s.since())
	args = append(args, countedArgs...)
	return `
SELECT substr(datetime(date, ?), 1, 10) AS day, ` + sentExpr + ` AS sent, COUNT(*)
FROM messages INDEXED BY idx_messages_date
WHERE date >= ? AND date != '' AND pending_delete = 0 AND folder_id IN (` + counted + `) AND ` + notVaulted + `
GROUP BY day, sent
ORDER BY day`, args
}

// DailyVolume counts the mail of s per local day, oldest first. Days without
// mail are absent; the caller fills the gaps it wants to draw.
func (d *DB) DailyVolume(ctx context.Context, s StatsScope) ([]DayVolume, error) {
	if len(s.Counted) == 0 {
		return nil, nil
	}
	query, args := dailyVolumeSQL(s)
	var out []DayVolume
	err := d.queryRows(ctx, "daily volume", query, args, func(rows *sql.Rows) error {
		var (
			day     string
			sent, n int
		)
		if err := rows.Scan(&day, &sent, &n); err != nil {
			return err
		}
		if len(out) == 0 || out[len(out)-1].Day != day {
			out = append(out, DayVolume{Day: day})
		}
		if sent != 0 {
			out[len(out)-1].Sent += n
		} else {
			out[len(out)-1].Received += n
		}
		return nil
	})
	return out, err
}

// HourCount is how much mail arrived in one local hour of one weekday, summed
// over the range. Weekday is 0 for Sunday, as in time.Weekday.
type HourCount struct {
	Weekday int
	Hour    int
	Count   int
}

func receivedByHourSQL(s StatsScope) (string, []any) {
	folders, folderArgs := inClause(s.received())
	modifier := offsetModifier(s.Offset)
	return `
SELECT CAST(strftime('%w', date, ?) AS INTEGER) AS wd, CAST(strftime('%H', date, ?) AS INTEGER) AS hr, COUNT(*)
FROM messages INDEXED BY idx_messages_date
WHERE date >= ? AND date != '' AND pending_delete = 0 AND folder_id IN (` + folders + `) AND ` + notVaulted + `
GROUP BY wd, hr`, append([]any{modifier, modifier, s.since()}, folderArgs...)
}

// ReceivedByHour counts the received mail of s per local weekday and hour.
// Cells without mail are absent.
func (d *DB) ReceivedByHour(ctx context.Context, s StatsScope) ([]HourCount, error) {
	if len(s.received()) == 0 {
		return nil, nil
	}
	query, args := receivedByHourSQL(s)
	var out []HourCount
	err := d.queryRows(ctx, "received by hour", query, args, func(rows *sql.Rows) error {
		var c HourCount
		if err := rows.Scan(&c.Weekday, &c.Hour, &c.Count); err != nil {
			return err
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

// RecipientGroup is one distinct To and Cc pair of sent mail and how often it
// was used. The caller splits the address lists.
type RecipientGroup struct {
	To    string
	Cc    string
	Count int
}

func sentRecipientsSQL(s StatsScope) (string, []any) {
	folders, args := inClause(s.Sent)
	return `
SELECT to_addresses, cc_addresses, COUNT(*)
FROM messages INDEXED BY idx_messages_list
WHERE pending_delete = 0 AND folder_id IN (` + folders + `) AND ` + notVaulted + ` AND date >= ?
GROUP BY to_addresses, cc_addresses`, append(args, s.since())
}

// SentRecipients groups the sent mail of s by its To and Cc headers. Sent mail
// is a small share of a mailbox, so this one reads the rows through the list
// index rather than carrying the address lists in an index of their own.
func (d *DB) SentRecipients(ctx context.Context, s StatsScope) ([]RecipientGroup, error) {
	if len(s.Sent) == 0 {
		return nil, nil
	}
	query, args := sentRecipientsSQL(s)
	var out []RecipientGroup
	err := d.queryRows(ctx, "sent recipients", query, args, func(rows *sql.Rows) error {
		var g RecipientGroup
		if err := rows.Scan(&g.To, &g.Cc, &g.Count); err != nil {
			return err
		}
		out = append(out, g)
		return nil
	})
	return out, err
}

// ReplyMedian is the median gap of one side's replies and how many replies it
// was taken over. N is 0 when there were none.
type ReplyMedian struct {
	N      int
	Median time.Duration
}

// ReplyTimes is how quickly the user answers and how quickly others answer
// the user.
type ReplyTimes struct {
	Mine   ReplyMedian
	Theirs ReplyMedian
}

func replyTimesSQL(s StatsScope, maxGap time.Duration) (string, []any) {
	counted, countedArgs := inClause(s.Counted)
	mineExpr := "0"
	var args []any
	if len(s.Sent) > 0 {
		marks, sentArgs := inClause(s.Sent)
		mineExpr = "folder_id IN (" + marks + ")"
		args = append(args, sentArgs...)
	}
	if len(s.Own) > 0 {
		marks, ownArgs := textList(s.Own)
		mineExpr = "(" + mineExpr + " OR " + senderEmailSQL + " IN (" + marks + "))"
		args = append(args, ownArgs...)
	}
	args = append(args, countedArgs...)
	args = append(args, s.since(), maxGap.Seconds())
	return `
WITH sided AS (
    SELECT thread_id, date, ` + mineExpr + ` AS mine
    FROM messages INDEXED BY idx_messages_thread_date
    WHERE thread_id > 0 AND date != '' AND pending_delete = 0 AND folder_id IN (` + counted + `) AND ` + notVaulted + `
), ordered AS (
    SELECT date, mine, LAG(date) OVER w AS prev_date, LAG(mine) OVER w AS prev_mine
    FROM sided
    WINDOW w AS (PARTITION BY thread_id ORDER BY date)
), gaps AS (
    SELECT mine, (julianday(date) - julianday(prev_date)) * 86400.0 AS secs
    FROM ordered
    WHERE prev_date IS NOT NULL AND mine != prev_mine AND date >= ?
), ranked AS (
    SELECT mine, secs,
           ROW_NUMBER() OVER (PARTITION BY mine ORDER BY secs) AS rn,
           COUNT(*) OVER (PARTITION BY mine) AS n
    FROM gaps
    WHERE secs > 0 AND secs <= ?
)
SELECT mine, n, AVG(secs) FROM ranked
WHERE rn IN ((n + 1) / 2, (n + 2) / 2)
GROUP BY mine, n`, args
}

// ReplyTimes measures replies inside conversations. Walking each thread in
// date order, a message from one side that follows a message from the other
// side is a reply, and the gap between the two is its reply time. The user's
// side is mail in a sent folder or from one of the own addresses. Only replies
// dated in the range count, and gaps longer than maxGap are a conversation
// picked up again rather than a reply, so they are left out. The median of an
// even count is the mean of the middle two.
func (d *DB) ReplyTimes(ctx context.Context, s StatsScope, maxGap time.Duration) (ReplyTimes, error) {
	var out ReplyTimes
	if len(s.Counted) == 0 {
		return out, nil
	}
	query, args := replyTimesSQL(s, maxGap)
	err := d.queryRows(ctx, "reply times", query, args, func(rows *sql.Rows) error {
		var (
			mine, n int
			secs    float64
		)
		if err := rows.Scan(&mine, &n, &secs); err != nil {
			return err
		}
		m := ReplyMedian{N: n, Median: time.Duration(secs * float64(time.Second))}
		if mine != 0 {
			out.Mine = m
		} else {
			out.Theirs = m
		}
		return nil
	})
	return out, err
}
