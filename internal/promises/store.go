package promises

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Direction says who owes a stored promise, seen from the user.
type Direction string

// The directions.
const (
	// Mine is a promise the user owes.
	Mine Direction = "mine"
	// Theirs is a promise owed to the user.
	Theirs Direction = "theirs"
)

// State is where a stored promise is.
type State string

// The states.
const (
	StateSuggested State = "suggested"
	StateAccepted  State = "accepted"
	StateDismissed State = "dismissed"
	// StateClosed is an accepted promise owed to the user that a reply from
	// that person closed.
	StateClosed State = "closed"
)

// ErrNotFound is returned for a promise id with no row.
var ErrNotFound = errors.New("promises: not found")

// Promise is one stored promise.
type Promise struct {
	ID        int64
	AccountID int64
	// MailID is the Message-ID of the mail it was found in, without angle
	// brackets.
	MailID      string
	ThreadID    int64
	Direction   Direction
	PersonName  string
	PersonEmail string
	Title       string
	Quote       string
	// Due is a date (2006-01-02), rfc3339, or empty.
	Due        string
	Confidence float64
	Source     string
	State      State
	TaskUID    string
	// ReplyID is the Message-ID of the reply that closed it or offers to,
	// and ReplyAt when that reply was written.
	ReplyID  string
	ReplyAt  time.Time
	KeepOpen []string
	MailDate time.Time
	Created  time.Time
}

// Store keeps promises in the app's database.
type Store struct {
	db *sql.DB
}

// NewStore returns a store on db, which must have migration 0107 applied.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

const timeLayout = time.RFC3339

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(timeLayout, s)
	return t
}

// QuoteKey is the form of a sentence two copies of a mail agree on.
func QuoteKey(quote string) string {
	return strings.ToLower(strings.Join(strings.Fields(quote), " "))
}

// Scanned reports whether a mail was already read for promises.
func (s *Store) Scanned(ctx context.Context, accountID int64, mailID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promise_scans WHERE account_id = ? AND mail_id = ?`, accountID, mailID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("promises: read scans: %w", err)
	}
	return n > 0, nil
}

// MarkScanned records that a mail was read, by "rules" or "ai".
func (s *Store) MarkScanned(ctx context.Context, accountID int64, mailID, source string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO promise_scans (account_id, mail_id, source, scanned_at) VALUES (?, ?, ?, ?)
ON CONFLICT (account_id, mail_id) DO UPDATE SET source = excluded.source, scanned_at = excluded.scanned_at`,
		accountID, mailID, source, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("promises: record scan: %w", err)
	}
	return nil
}

// Add stores a found promise unless the same sentence of the same mail is
// stored already, in whatever state. It reports whether it added one, and
// fills p.ID either way.
func (s *Store) Add(ctx context.Context, p *Promise) (bool, error) {
	now := formatTime(time.Now())
	if p.State == "" {
		p.State = StateSuggested
	}
	res, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO promises (account_id, mail_id, thread_id, direction, person_name, person_email, title, quote,
    quote_key, due, confidence, source, state, task_uid, mail_date, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.AccountID, p.MailID, p.ThreadID, string(p.Direction), p.PersonName, p.PersonEmail, p.Title, p.Quote,
		QuoteKey(p.Quote), p.Due, p.Confidence, p.Source, string(p.State), p.TaskUID, formatTime(p.MailDate), now, now)
	if err != nil {
		return false, fmt.Errorf("promises: add: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM promises WHERE account_id = ? AND mail_id = ? AND quote_key = ?`,
		p.AccountID, p.MailID, QuoteKey(p.Quote)).Scan(&p.ID); err != nil {
		return false, fmt.Errorf("promises: id of added: %w", err)
	}
	return n > 0, nil
}

const columns = `id, account_id, mail_id, thread_id, direction, person_name, person_email, title, quote, due,
    confidence, source, state, task_uid, reply_id, reply_at, keep_open, mail_date, created_at`

func scanPromise(row interface{ Scan(...any) error }) (Promise, error) {
	var (
		p                                   Promise
		dir, state, replyAt, keep, mail, at string
	)
	err := row.Scan(&p.ID, &p.AccountID, &p.MailID, &p.ThreadID, &dir, &p.PersonName, &p.PersonEmail, &p.Title,
		&p.Quote, &p.Due, &p.Confidence, &p.Source, &state, &p.TaskUID, &p.ReplyID, &replyAt, &keep, &mail, &at)
	if err != nil {
		return Promise{}, err
	}
	p.Direction, p.State = Direction(dir), State(state)
	p.ReplyAt, p.MailDate, p.Created = parseTime(replyAt), parseTime(mail), parseTime(at)
	if keep != "" {
		p.KeepOpen = strings.Split(keep, "\n")
	}
	return p, nil
}

func (s *Store) query(ctx context.Context, where string, args ...any) ([]Promise, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM promises WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("promises: list: %w", err)
	}
	defer rows.Close()
	var out []Promise
	for rows.Next() {
		p, err := scanPromise(rows)
		if err != nil {
			return nil, fmt.Errorf("promises: scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get returns one promise, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id int64) (Promise, error) {
	p, err := scanPromise(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM promises WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Promise{}, ErrNotFound
	}
	if err != nil {
		return Promise{}, fmt.Errorf("promises: get %d: %w", id, err)
	}
	return p, nil
}

// ForMail returns the promises of one mail, in every state.
func (s *Store) ForMail(ctx context.Context, accountID int64, mailID string) ([]Promise, error) {
	return s.query(ctx, `account_id = ? AND mail_id = ?`, accountID, mailID)
}

// List returns the promises in any of the states, oldest first.
func (s *Store) List(ctx context.Context, states ...State) ([]Promise, error) {
	if len(states) == 0 {
		return s.query(ctx, `1 = 1`)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(states)), ",")
	args := make([]any, len(states))
	for i, st := range states {
		args[i] = string(st)
	}
	return s.query(ctx, `state IN (`+marks+`)`, args...)
}

// Save writes what can change about a promise: its text, owner, person, due
// date, state, task and the reply that closed it.
func (s *Store) Save(ctx context.Context, p Promise) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE promises SET thread_id = ?, direction = ?, person_name = ?, person_email = ?, title = ?, due = ?, state = ?,
    task_uid = ?, reply_id = ?, reply_at = ?, keep_open = ?, updated_at = ?
WHERE id = ?`,
		p.ThreadID, string(p.Direction), p.PersonName, p.PersonEmail, p.Title, p.Due, string(p.State),
		p.TaskUID, p.ReplyID, formatTime(p.ReplyAt), strings.Join(p.KeepOpen, "\n"), formatTime(time.Now()), p.ID)
	if err != nil {
		return fmt.Errorf("promises: save %d: %w", p.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Quiet stops reading mail from a sender for promises, and dismisses the
// suggestions about that person already waiting. It returns the ids it
// dismissed.
func (s *Store) Quiet(ctx context.Context, email string) ([]int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, nil
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO promise_quiet_senders (email, created_at) VALUES (?, ?)`,
		email, formatTime(time.Now())); err != nil {
		return nil, fmt.Errorf("promises: quiet sender: %w", err)
	}
	list, err := s.query(ctx, `state = ? AND LOWER(person_email) = ?`, string(StateSuggested), email)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, p := range list {
		p.State = StateDismissed
		if err := s.Save(ctx, p); err != nil {
			return ids, err
		}
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// Unquiet reads a sender's mail for promises again, and brings back the
// suggestions ids names.
func (s *Store) Unquiet(ctx context.Context, email string, ids []int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM promise_quiet_senders WHERE email = ?`, strings.ToLower(strings.TrimSpace(email))); err != nil {
		return fmt.Errorf("promises: unquiet sender: %w", err)
	}
	for _, id := range ids {
		p, err := s.Get(ctx, id)
		if err != nil {
			continue
		}
		if p.State == StateDismissed {
			p.State = StateSuggested
			if err := s.Save(ctx, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// IsQuiet reports whether mail from a sender is left unread.
func (s *Store) IsQuiet(ctx context.Context, email string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM promise_quiet_senders WHERE email = ?`, strings.ToLower(strings.TrimSpace(email))).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("promises: read quiet senders: %w", err)
	}
	return n > 0, nil
}
