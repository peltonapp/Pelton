package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// subjectWindow bounds how far apart in time a headerless reply and the message
// it is matched to by subject may be, so a monthly "Re: Invoice" does not chain
// a year of unrelated mail into one conversation.
const subjectWindow = 90 * 24 * time.Hour

// clockSkew tolerates a reply dated slightly before its parent, which happens
// whenever the two senders' clocks disagree.
const clockSkew = 24 * time.Hour

// subjectCandidates caps how many same-subject rows the fallback inspects.
const subjectCandidates = 50

// threadInput is what thread assignment reads from a message.
type threadInput struct {
	// id is the row id, 0 for a row that is not inserted yet.
	id         int64
	accountID  int64
	messageID  string
	inReplyTo  []string
	references []string
	subject    string
	from       string
	to         string
	cc         string
	date       time.Time
}

// threadAssignment is the outcome of resolveThread, written onto the row.
type threadAssignment struct {
	threadID int64
	subject  string
	loose    bool
}

func threadInputFor(m *Message) threadInput {
	return threadInput{
		id:         m.ID,
		accountID:  m.AccountID,
		messageID:  m.MessageID,
		inReplyTo:  m.InReplyTo,
		references: m.References,
		subject:    m.Subject,
		from:       m.FromAddress,
		to:         m.ToAddresses,
		cc:         m.CcAddresses,
		date:       m.Date,
	}
}

// resolveThread finds the conversation a message belongs to and records its
// ids under that conversation. Messages are joined when they share any id in
// their Message-ID, In-Reply-To and References, which merges two threads when a
// message links them. A reply that carried no threading headers falls back to
// the subject and the people involved. Everything stays within one account.
// ex must be a transaction.
func resolveThread(ctx context.Context, ex execer, in threadInput) (threadAssignment, error) {
	keys := threadKeys(in)
	subject, reply := normalizeSubject(in.subject)
	loose := reply && len(in.inReplyTo) == 0 && len(in.references) == 0

	found, err := threadsForKeys(ctx, ex, in.accountID, keys)
	if err != nil {
		return threadAssignment{}, err
	}
	if subject != "" && !in.date.IsZero() {
		matched, err := subjectThreads(ctx, ex, in, subject, loose)
		if err != nil {
			return threadAssignment{}, err
		}
		found = append(found, matched...)
	}

	target, err := settleThread(ctx, ex, in.accountID, found)
	if err != nil {
		return threadAssignment{}, err
	}
	for _, key := range keys {
		if _, err := ex.ExecContext(ctx, `
INSERT INTO thread_keys (account_id, key, thread_id) VALUES (?, ?, ?)
ON CONFLICT (account_id, key) DO UPDATE SET thread_id = excluded.thread_id`,
			in.accountID, key, target); err != nil {
			return threadAssignment{}, fmt.Errorf("storage: record thread key: %w", err)
		}
	}
	return threadAssignment{threadID: target, subject: subject, loose: loose}, nil
}

// rethread assigns the stored row m.ID its conversation again from m, as a
// body fill learns the headers a list stub lacked.
func rethread(ctx context.Context, ex execer, m *Message) error {
	in := threadInputFor(m)
	if err := ex.QueryRowContext(ctx, `SELECT account_id FROM messages WHERE id = ?`, m.ID).Scan(&in.accountID); err != nil {
		return fmt.Errorf("storage: account of message %d: %w", m.ID, err)
	}
	assigned, err := resolveThread(ctx, ex, in)
	if err != nil {
		return err
	}
	if _, err := ex.ExecContext(ctx, `UPDATE messages SET thread_id = ?, thread_subject = ?, thread_loose = ? WHERE id = ?`,
		assigned.threadID, assigned.subject, boolToInt(assigned.loose), m.ID); err != nil {
		return fmt.Errorf("storage: rethread message %d: %w", m.ID, err)
	}
	return nil
}

// threadKeys is the message's own id followed by every id it refers to, in a
// normalized form and without duplicates.
func threadKeys(in threadInput) []string {
	keys := make([]string, 0, 1+len(in.inReplyTo)+len(in.references))
	add := func(raw string) {
		if key := threadKey(raw); key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	add(in.messageID)
	for _, id := range in.inReplyTo {
		add(id)
	}
	for _, id := range in.references {
		add(id)
	}
	return keys
}

// threadKey strips the angle brackets and whitespace a Message-ID may carry.
// It does not validate: an id that would be refused in an outgoing header can
// still link two received messages.
func threadKey(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return strings.TrimSpace(s)
}

func threadsForKeys(ctx context.Context, ex execer, accountID int64, keys []string) ([]int64, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	marks := strings.Repeat("?, ", len(keys)-1) + "?"
	args := make([]any, 0, len(keys)+1)
	args = append(args, accountID)
	for _, k := range keys {
		args = append(args, k)
	}
	return queryInt64s(ctx, ex,
		`SELECT DISTINCT thread_id FROM thread_keys WHERE account_id = ? AND key IN (`+marks+`)`, args...)
}

// subjectThreads is the fallback for mail without usable headers. A headerless
// reply joins the newest earlier message with the same subject that shares a
// participant with it. Any message also collects headerless replies to it that
// arrived first and found no parent, since sync stores newest mail first. The
// account's own address
// does not count as a shared participant: it is on every message.
func subjectThreads(ctx context.Context, ex execer, in threadInput, subject string, loose bool) ([]int64, error) {
	var own string
	if err := ex.QueryRowContext(ctx, `SELECT email FROM accounts WHERE id = ?`, in.accountID).Scan(&own); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("storage: account for thread fallback: %w", err)
	}
	people := participants(own, in.from, in.to, in.cc)
	if len(people) == 0 {
		return nil, nil
	}

	var out []int64
	if loose {
		rows, err := subjectRows(ctx, ex, in, subject, false,
			in.date.Add(-subjectWindow), in.date.Add(clockSkew), "DESC")
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if overlaps(people, participants(own, r.from, r.to, r.cc)) {
				out = append(out, r.threadID)
				break
			}
		}
	}
	rows, err := subjectRows(ctx, ex, in, subject, true,
		in.date.Add(-clockSkew), in.date.Add(subjectWindow), "ASC")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if overlaps(people, participants(own, r.from, r.to, r.cc)) {
			out = append(out, r.threadID)
		}
	}
	return out, nil
}

type subjectRow struct {
	threadID     int64
	from, to, cc string
}

func subjectRows(ctx context.Context, ex execer, in threadInput, subject string, looseOnly bool, from, to time.Time, order string) ([]subjectRow, error) {
	query := `
SELECT thread_id, from_address, to_addresses, cc_addresses FROM messages
WHERE account_id = ? AND thread_subject = ? AND thread_id != 0 AND id != ?
  AND date >= ? AND date <= ? AND ` + notVaulted
	if looseOnly {
		// a headerless reply that already found its parent stays where it is.
		query += ` AND thread_loose = 1 AND NOT EXISTS (
  SELECT 1 FROM messages p WHERE p.thread_id = messages.thread_id
  AND p.thread_loose = 0 AND p.date < messages.date)`
	}
	query += ` ORDER BY date ` + order + ` LIMIT ?`
	rows, err := ex.QueryContext(ctx, query, in.accountID, subject, in.id,
		formatTime(from), formatTime(to), subjectCandidates)
	if err != nil {
		return nil, fmt.Errorf("storage: thread subject candidates: %w", err)
	}
	defer rows.Close()
	var out []subjectRow
	for rows.Next() {
		var r subjectRow
		if err := rows.Scan(&r.threadID, &r.from, &r.to, &r.cc); err != nil {
			return nil, fmt.Errorf("storage: scan thread subject candidate: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// settleThread picks the conversation for a message: a new one when nothing
// matched, otherwise the oldest matched one, into which every other match is
// merged.
func settleThread(ctx context.Context, ex execer, accountID int64, found []int64) (int64, error) {
	slices.Sort(found)
	found = slices.Compact(found)
	if len(found) == 0 {
		res, err := ex.ExecContext(ctx, `INSERT INTO threads (account_id) VALUES (?)`, accountID)
		if err != nil {
			return 0, fmt.Errorf("storage: create thread: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("storage: thread id: %w", err)
		}
		return id, nil
	}
	target := found[0]
	for _, other := range found[1:] {
		if err := mergeThread(ctx, ex, other, target); err != nil {
			return 0, err
		}
	}
	return target, nil
}

// mergeThread folds one conversation into another. A mute on either side
// carries over, and so does a note; two notes are joined into one. Project
// links and dismissed project suggestions follow the conversation.
func mergeThread(ctx context.Context, ex execer, from, into int64) error {
	for _, stmt := range []string{
		`UPDATE messages SET thread_id = ? WHERE thread_id = ?`,
		`UPDATE thread_keys SET thread_id = ? WHERE thread_id = ?`,
		`UPDATE threads SET muted = 1 WHERE id = ?1 AND EXISTS (SELECT 1 FROM threads WHERE id = ?2 AND muted = 1)`,
		`UPDATE notes SET body = body || char(10) || char(10) || (SELECT o.body FROM notes o WHERE o.thread_id = ?2 AND o.message_key = '')
		 WHERE thread_id = ?1 AND message_key = '' AND EXISTS (SELECT 1 FROM notes o WHERE o.thread_id = ?2 AND o.message_key = '')`,
		`DELETE FROM notes WHERE thread_id = ?2 AND message_key = '' AND EXISTS (SELECT 1 FROM notes o WHERE o.thread_id = ?1 AND o.message_key = '')`,
		`UPDATE notes SET thread_id = ?1 WHERE thread_id = ?2 AND message_key = ''`,
		`UPDATE project_links SET thread_id = ?1 WHERE thread_id = ?2`,
		`INSERT OR IGNORE INTO project_dismissals (thread_id, created_at)
		 SELECT ?1, created_at FROM project_dismissals WHERE thread_id = ?2`,
		`DELETE FROM project_dismissals WHERE thread_id = ?2`,
	} {
		if _, err := ex.ExecContext(ctx, stmt, into, from); err != nil {
			return fmt.Errorf("storage: merge thread %d into %d: %w", from, into, err)
		}
	}
	if _, err := ex.ExecContext(ctx, `DELETE FROM threads WHERE id = ?`, from); err != nil {
		return fmt.Errorf("storage: drop merged thread %d: %w", from, err)
	}
	return nil
}

// BackfillThreads assigns a conversation to up to batch messages that have
// none yet, in one transaction, and returns how many it assigned. Rows are
// picked by thread_id 0, so the work resumes wherever an interrupted run
// stopped and a finished one does nothing; 0 means every message is threaded.
func (d *DB) BackfillThreads(ctx context.Context, batch int) (int, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storage: begin thread backfill: %w", err)
	}
	defer tx.Rollback()

	inputs, err := unthreadedRows(ctx, tx, batch)
	if err != nil {
		return 0, err
	}
	for _, in := range inputs {
		assigned, err := resolveThread(ctx, tx, in)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET thread_id = ?, thread_subject = ?, thread_loose = ? WHERE id = ?`,
			assigned.threadID, assigned.subject, boolToInt(assigned.loose), in.id); err != nil {
			return 0, fmt.Errorf("storage: set thread on message %d: %w", in.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storage: commit thread backfill: %w", err)
	}
	return len(inputs), nil
}

func unthreadedRows(ctx context.Context, ex execer, batch int) ([]threadInput, error) {
	rows, err := ex.QueryContext(ctx, `
SELECT id, account_id, message_id, subject, from_address, to_addresses, cc_addresses,
       date, in_reply_to, [references]
FROM messages WHERE thread_id = 0 ORDER BY id LIMIT ?`, normalizeLimit(batch))
	if err != nil {
		return nil, fmt.Errorf("storage: list unthreaded messages: %w", err)
	}
	defer rows.Close()
	var out []threadInput
	for rows.Next() {
		var (
			in         threadInput
			date       string
			inReplyTo  sql.NullString
			references sql.NullString
		)
		if err := rows.Scan(&in.id, &in.accountID, &in.messageID, &in.subject, &in.from,
			&in.to, &in.cc, &date, &inReplyTo, &references); err != nil {
			return nil, fmt.Errorf("storage: scan unthreaded message: %w", err)
		}
		if in.date, err = parseTime(date); err != nil {
			return nil, err
		}
		in.inReplyTo = decodeThreadingIDs(inReplyTo)
		in.references = decodeThreadingIDs(references)
		out = append(out, in)
	}
	return out, rows.Err()
}

func queryInt64s(ctx context.Context, ex execer, query string, args ...any) ([]int64, error) {
	rows, err := ex.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: query ids: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// replyPrefixes mark a subject as a reply, in the languages mail clients
// commonly write them in. forwardPrefixes are stripped for matching but do not
// make a message a reply.
var (
	replyPrefixes   = []string{"re", "aw", "sv", "vs", "antw", "odp", "rif", "ynt", "ref", "res"}
	forwardPrefixes = []string{"fw", "fwd", "wg", "tr", "rv", "enc", "doorst"}
)

// normalizeSubject strips list tags and reply or forward prefixes, then folds
// case and whitespace. reply reports whether a reply prefix was among them.
func normalizeSubject(subject string) (normalized string, reply bool) {
	s := strings.TrimSpace(subject)
	for {
		if strings.HasPrefix(s, "[") {
			if end := strings.IndexByte(s, ']'); end > 0 && strings.TrimSpace(s[end+1:]) != "" {
				s = strings.TrimSpace(s[end+1:])
				continue
			}
		}
		word, rest, ok := cutPrefixWord(s)
		if !ok {
			break
		}
		lower := strings.ToLower(word)
		switch {
		case slices.Contains(replyPrefixes, lower):
			reply = true
		case slices.Contains(forwardPrefixes, lower):
		default:
			return foldSubject(s), reply
		}
		s = rest
	}
	return foldSubject(s), reply
}

// cutPrefixWord splits "Re: x", "Re[2]: x", "RE^2: x" and "Re（2）：x" style
// prefixes off a subject.
func cutPrefixWord(s string) (word, rest string, ok bool) {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLetter(r) {
			break
		}
		i += size
	}
	if i == 0 || i > 8 {
		return "", "", false
	}
	word = s[:i]
	tail := s[i:]
	if strings.HasPrefix(tail, "[") || strings.HasPrefix(tail, "(") {
		end := strings.IndexAny(tail, "])")
		if end < 0 {
			return "", "", false
		}
		tail = tail[end+1:]
	} else if strings.HasPrefix(tail, "^") {
		j := 1
		for j < len(tail) && tail[j] >= '0' && tail[j] <= '9' {
			j++
		}
		tail = tail[j:]
	}
	tail = strings.TrimLeft(tail, " ")
	switch {
	case strings.HasPrefix(tail, ":"):
		tail = tail[1:]
	case strings.HasPrefix(tail, "："):
		tail = tail[len("："):]
	default:
		return "", "", false
	}
	return word, strings.TrimSpace(tail), true
}

func foldSubject(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

var addressPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-!#$&'*/=?^{|}~]+@[A-Za-z0-9.\-]+`)

// participants collects the lower-cased addresses in the given header values,
// leaving out own.
func participants(own string, values ...string) []string {
	own = strings.ToLower(strings.TrimSpace(own))
	var out []string
	for _, v := range values {
		for _, addr := range addressPattern.FindAllString(v, -1) {
			addr = strings.ToLower(addr)
			if addr != own && !slices.Contains(out, addr) {
				out = append(out, addr)
			}
		}
	}
	return out
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}
