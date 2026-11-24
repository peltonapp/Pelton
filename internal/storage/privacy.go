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

// PrivacyScan is what one message tried to learn about its reader, as the
// privacy report counts it (see migration 0112).
type PrivacyScan struct {
	// Key identifies the message across resyncs: see PrivacyKey.
	Key        string
	Sender     string
	SenderName string
	ListID     string
	Date       time.Time
	Pixels     int
	Links      int
	Receipt    bool
	Companies  []string
}

// PrivacyKey is the counter key of a message: its account and Message-ID, or
// its row id when it has no Message-ID.
func PrivacyKey(m Message) string {
	id := strings.Trim(strings.TrimSpace(m.MessageID), "<>")
	if id == "" {
		return fmt.Sprintf("row:%d", m.ID)
	}
	return fmt.Sprintf("%d:%s", m.AccountID, strings.ToLower(id))
}

// RecordPrivacyScan stores a message's counts once; a message already counted
// keeps its first row.
func (d *DB) RecordPrivacyScan(ctx context.Context, s PrivacyScan) error {
	receipt := 0
	if s.Receipt {
		receipt = 1
	}
	_, err := d.sql.ExecContext(ctx, `INSERT OR IGNORE INTO privacy_scans
(msg_key, sender, sender_name, list_id, date, pixels, links, receipt, companies)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Key, normalizeAddress(s.Sender), s.SenderName, s.ListID, formatTime(s.Date),
		s.Pixels, s.Links, receipt, strings.Join(s.Companies, "|"))
	if err != nil {
		return fmt.Errorf("storage: record privacy scan: %w", err)
	}
	return nil
}

// PrivacyScanned reports which of the given keys are counted already.
func (d *DB) PrivacyScanned(ctx context.Context, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(keys) == 0 {
		return out, nil
	}
	marks, args := textList(keys)
	rows, err := d.sql.QueryContext(ctx, `SELECT msg_key FROM privacy_scans WHERE msg_key IN (`+marks+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: privacy scanned: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

// PrivacySender is one sender's line of the privacy report.
type PrivacySender struct {
	Sender    string
	Name      string
	Mails     int
	Pixels    int
	Links     int
	Receipts  int
	First     time.Time
	Last      time.Time
	Companies []string
}

// PrivacySenders sums the counters per sender for mail dated since since; the
// zero time means all of it.
func (d *DB) PrivacySenders(ctx context.Context, since time.Time) ([]PrivacySender, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT sender, MAX(sender_name), COUNT(*), SUM(pixels),
    SUM(links), SUM(receipt), MIN(date), MAX(date), GROUP_CONCAT(companies, '|')
FROM privacy_scans WHERE date >= ? GROUP BY sender`, formatTime(since))
	if err != nil {
		return nil, fmt.Errorf("storage: privacy senders: %w", err)
	}
	defer rows.Close()
	var out []PrivacySender
	for rows.Next() {
		var s PrivacySender
		var first, last string
		var companies sql.NullString
		if err := rows.Scan(&s.Sender, &s.Name, &s.Mails, &s.Pixels, &s.Links, &s.Receipts,
			&first, &last, &companies); err != nil {
			return nil, fmt.Errorf("storage: scan privacy sender: %w", err)
		}
		if s.First, err = parseTime(first); err != nil {
			return nil, err
		}
		if s.Last, err = parseTime(last); err != nil {
			return nil, err
		}
		for _, c := range strings.Split(companies.String, "|") {
			if c != "" && !slices.Contains(s.Companies, c) {
				s.Companies = append(s.Companies, c)
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MessageListID returns a message's List-Id, which a message read leaves out.
func (d *DB) MessageListID(ctx context.Context, id int64) (string, error) {
	var listID string
	err := d.sql.QueryRowContext(ctx, `SELECT list_id FROM messages WHERE id = ? AND `+notVaulted, id).Scan(&listID)
	if err != nil {
		return "", fmt.Errorf("storage: message list id %d: %w", id, err)
	}
	return listID, nil
}

// ClearPrivacyScans forgets every counter.
func (d *DB) ClearPrivacyScans(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM privacy_scans`); err != nil {
		return fmt.Errorf("storage: clear privacy scans: %w", err)
	}
	return nil
}

// Unsubscribe is an unsubscribe Pelton carried out for one sender.
type Unsubscribe struct {
	Sender    string
	ListID    string
	AccountID int64
	// Method is "oneclick", "mailto" or "link".
	Method string
	At     time.Time
	// StillCount counts the mails that arrived after the grace period;
	// StillAt and StillMessageID are the newest of them.
	StillCount     int
	StillAt        time.Time
	StillMessageID int64
	DismissedAt    time.Time
	// Blocked is "junk" or "trash" when new mail is moved there.
	Blocked string
}

// RecordUnsubscribe remembers an unsubscribe, starting the still-sending
// watch afresh: a retry forgets what was seen after the earlier attempt.
func (d *DB) RecordUnsubscribe(ctx context.Context, u Unsubscribe) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO unsubscribes (sender, list_id, account_id, method, unsubscribed_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(sender) DO UPDATE SET list_id = excluded.list_id, account_id = excluded.account_id,
    method = excluded.method, unsubscribed_at = excluded.unsubscribed_at,
    still_count = 0, still_at = '', still_message_id = 0, dismissed_at = ''`,
		normalizeAddress(u.Sender), u.ListID, u.AccountID, u.Method, formatTime(u.At))
	if err != nil {
		return fmt.Errorf("storage: record unsubscribe: %w", err)
	}
	return nil
}

const unsubscribeColumns = `sender, list_id, account_id, method, unsubscribed_at, still_count, still_at,
    still_message_id, dismissed_at, blocked`

func scanUnsubscribe(row rowScanner) (Unsubscribe, error) {
	var u Unsubscribe
	var at, stillAt, dismissed string
	if err := row.Scan(&u.Sender, &u.ListID, &u.AccountID, &u.Method, &at, &u.StillCount, &stillAt,
		&u.StillMessageID, &dismissed, &u.Blocked); err != nil {
		return u, err
	}
	var err error
	if u.At, err = parseTime(at); err != nil {
		return u, err
	}
	if u.StillAt, err = parseTime(stillAt); err != nil {
		return u, err
	}
	u.DismissedAt, err = parseTime(dismissed)
	return u, err
}

// ListUnsubscribes returns every recorded unsubscribe, newest first.
func (d *DB) ListUnsubscribes(ctx context.Context) ([]Unsubscribe, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT `+unsubscribeColumns+` FROM unsubscribes ORDER BY unsubscribed_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("storage: list unsubscribes: %w", err)
	}
	defer rows.Close()
	var out []Unsubscribe
	for rows.Next() {
		u, err := scanUnsubscribe(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan unsubscribe: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UnsubscribeFor returns the unsubscribe a message from sender on list belongs
// to: the sender's own, or else one for the same List-Id. ok is false when
// neither was unsubscribed from.
func (d *DB) UnsubscribeFor(ctx context.Context, sender, listID string) (Unsubscribe, bool, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT `+unsubscribeColumns+` FROM unsubscribes
WHERE sender = ? OR (list_id <> '' AND list_id = ?)
ORDER BY sender = ? DESC LIMIT 1`, normalizeAddress(sender), listID, normalizeAddress(sender))
	u, err := scanUnsubscribe(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Unsubscribe{}, false, nil
	}
	if err != nil {
		return Unsubscribe{}, false, fmt.Errorf("storage: unsubscribe for: %w", err)
	}
	return u, true, nil
}

// NoteStillSending counts a mail that arrived after the grace period, once:
// mail no newer than the last one counted is ignored.
func (d *DB) NoteStillSending(ctx context.Context, sender string, at time.Time, messageID int64) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE unsubscribes SET still_count = still_count + 1, still_at = ?, still_message_id = ?
WHERE sender = ? AND still_at < ?`, formatTime(at), messageID, normalizeAddress(sender), formatTime(at))
	if err != nil {
		return fmt.Errorf("storage: note still sending: %w", err)
	}
	return nil
}

// DismissStillSending stops the still-sending notice for a sender until more
// mail arrives after now.
func (d *DB) DismissStillSending(ctx context.Context, sender string) error {
	_, err := d.sql.ExecContext(ctx, `UPDATE unsubscribes SET dismissed_at = ? WHERE sender = ?`, nowText(), normalizeAddress(sender))
	if err != nil {
		return fmt.Errorf("storage: dismiss still sending: %w", err)
	}
	return nil
}

// SetUnsubscribeBlock sets where new mail from an unsubscribed sender is moved:
// "junk", "trash" or "" for nowhere.
func (d *DB) SetUnsubscribeBlock(ctx context.Context, sender, where string) error {
	if where != "" && where != "junk" && where != "trash" {
		return fmt.Errorf("storage: unknown block target %q", where)
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE unsubscribes SET blocked = ? WHERE sender = ?`, where, normalizeAddress(sender))
	if err != nil {
		return fmt.Errorf("storage: set unsubscribe block: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("storage: %s was never unsubscribed from", sender)
	}
	return nil
}
