package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrOutboxNotFound is returned when an outbox id has no row.
var ErrOutboxNotFound = errors.New("storage: outbox message not found")

// OutboxRow is one queued message as stored. State values are owned by the
// internal/outbox package and passed through as opaque strings; this layer only
// persists them. raw_message is the fully built, possibly encrypted, mime.
type OutboxRow struct {
	ID            int64
	AccountID     int64
	EnvelopeFrom  string
	Recipients    string // newline separated, set by the outbox package
	Raw           []byte
	State         string
	Attempts      int
	LastError     string
	NextAttemptAt time.Time
	CreatedAt     time.Time
}

// InsertOutbox inserts a queued message and returns its new id. CreatedAt and
// NextAttemptAt default to now when left zero.
func (d *DB) InsertOutbox(ctx context.Context, row OutboxRow) (int64, error) {
	created := row.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	next := row.NextAttemptAt
	if next.IsZero() {
		next = created
	}

	const query = `
INSERT INTO outbox (account_id, envelope_from, recipients, raw_message, state, attempts, last_error, next_attempt_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := d.sql.ExecContext(ctx, query,
		row.AccountID, row.EnvelopeFrom, row.Recipients, row.Raw, row.State,
		row.Attempts, row.LastError, formatTime(next), formatTime(created))
	if err != nil {
		return 0, fmt.Errorf("storage: insert outbox message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: outbox insert id: %w", err)
	}
	return id, nil
}

// ClaimDueOutbox atomically picks the oldest message in queuedState whose
// next_attempt_at has passed, flips it to sendingState, and returns it. It
// returns (nil, nil) when nothing is due. Messages of the accounts in skip are
// passed over, so one account can send while another is still busy. The select
// and update run in one transaction so two workers cannot claim the same row.
func (d *DB) ClaimDueOutbox(ctx context.Context, queuedState, sendingState string, now time.Time, skip []int64) (*OutboxRow, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: begin claim outbox: %w", err)
	}
	defer tx.Rollback()

	args := []any{queuedState, formatTime(now)}
	exclude := ""
	if len(skip) > 0 {
		exclude = " AND account_id NOT IN (?" + strings.Repeat(", ?", len(skip)-1) + ")"
		for _, id := range skip {
			args = append(args, id)
		}
	}
	selectDue := `
SELECT id, account_id, envelope_from, recipients, raw_message, state, attempts, last_error, next_attempt_at, created_at
FROM outbox
WHERE state = ? AND next_attempt_at <= ?` + exclude + `
ORDER BY id
LIMIT 1`
	row, err := scanOutbox(tx.QueryRowContext(ctx, selectDue, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: select due outbox: %w", err)
	}

	const claim = `UPDATE outbox SET state = ? WHERE id = ?`
	if _, err := tx.ExecContext(ctx, claim, sendingState, row.ID); err != nil {
		return nil, fmt.Errorf("storage: claim outbox %d: %w", row.ID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("storage: commit claim outbox %d: %w", row.ID, err)
	}

	row.State = sendingState
	return row, nil
}

// UpdateOutboxState writes the outcome of a send attempt: the new state, attempt
// count, last error and next retry time.
func (d *DB) UpdateOutboxState(ctx context.Context, id int64, state string, attempts int, lastErr string, nextAttempt time.Time) error {
	const query = `
UPDATE outbox SET state = ?, attempts = ?, last_error = ?, next_attempt_at = ? WHERE id = ?`
	res, err := d.sql.ExecContext(ctx, query, state, attempts, lastErr, formatTime(nextAttempt), id)
	if err != nil {
		return fmt.Errorf("storage: update outbox %d: %w", id, err)
	}
	return requireOneRow(res, ErrOutboxNotFound)
}

// UpdateOutboxStateIf is UpdateOutboxState guarded on the row still being in
// wantState, returning whether it was updated. It backs retrying a failed send:
// the worker must not have picked the row up again between the click and the
// write, which would otherwise reset the attempt count of a live send.
func (d *DB) UpdateOutboxStateIf(ctx context.Context, id int64, wantState, state string, attempts int, lastErr string, nextAttempt time.Time) (bool, error) {
	const query = `
UPDATE outbox SET state = ?, attempts = ?, last_error = ?, next_attempt_at = ? WHERE id = ? AND state = ?`
	res, err := d.sql.ExecContext(ctx, query, state, attempts, lastErr, formatTime(nextAttempt), id, wantState)
	if err != nil {
		return false, fmt.Errorf("storage: update outbox %d if %s: %w", id, wantState, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: rows affected: %w", err)
	}
	return n > 0, nil
}

// RequeueOutboxState flips every row in fromState back to toState, used on
// startup to recover messages left mid-send by a previous crashed run.
func (d *DB) RequeueOutboxState(ctx context.Context, fromState, toState string) (int, error) {
	const query = `UPDATE outbox SET state = ? WHERE state = ?`
	res, err := d.sql.ExecContext(ctx, query, toState, fromState)
	if err != nil {
		return 0, fmt.Errorf("storage: requeue outbox %s->%s: %w", fromState, toState, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: rows affected: %w", err)
	}
	return int(n), nil
}

// DeleteOutboxByState removes every row in the given state and returns how many
// were deleted. Used to prune sent messages after the ui has shown them.
func (d *DB) DeleteOutboxByState(ctx context.Context, state string) (int, error) {
	const query = `DELETE FROM outbox WHERE state = ?`
	res, err := d.sql.ExecContext(ctx, query, state)
	if err != nil {
		return 0, fmt.Errorf("storage: delete outbox in state %s: %w", state, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: rows affected: %w", err)
	}
	return int(n), nil
}

// DeleteOutboxIfState deletes a single row only if it is still in wantState,
// returning whether a row was removed. It is the cancel primitive for undo-send:
// a message can be pulled back only while it is still queued, not once sending.
func (d *DB) DeleteOutboxIfState(ctx context.Context, id int64, wantState string) (bool, error) {
	const query = `DELETE FROM outbox WHERE id = ? AND state = ?`
	res, err := d.sql.ExecContext(ctx, query, id, wantState)
	if err != nil {
		return false, fmt.Errorf("storage: delete outbox %d if %s: %w", id, wantState, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: rows affected: %w", err)
	}
	return n > 0, nil
}

// ListOutbox returns every outbox row ordered by id, for inspection and the cli.
func (d *DB) ListOutbox(ctx context.Context) ([]OutboxRow, error) {
	const query = `
SELECT id, account_id, envelope_from, recipients, raw_message, state, attempts, last_error, next_attempt_at, created_at
FROM outbox ORDER BY id`
	rows, err := d.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("storage: list outbox: %w", err)
	}
	defer rows.Close()

	var out []OutboxRow
	for rows.Next() {
		row, err := scanOutbox(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan outbox: %w", err)
		}
		out = append(out, *row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate outbox: %w", err)
	}
	return out, nil
}

func scanOutbox(row rowScanner) (*OutboxRow, error) {
	var (
		r       OutboxRow
		next    string
		created string
	)
	if err := row.Scan(&r.ID, &r.AccountID, &r.EnvelopeFrom, &r.Recipients, &r.Raw,
		&r.State, &r.Attempts, &r.LastError, &next, &created); err != nil {
		return nil, err
	}
	nextAt, err := parseTime(next)
	if err != nil {
		return nil, err
	}
	createdAt, err := parseTime(created)
	if err != nil {
		return nil, err
	}
	r.NextAttemptAt = nextAt
	r.CreatedAt = createdAt
	return &r, nil
}

// CountOutboxInStates returns how many rows are in any of the given states.
// Background sync uses it to stand aside while something is waiting to be sent:
// a message the user pressed send on should not queue behind a mailbox that is
// downloading fourteen thousand messages (#310).
func (d *DB) CountOutboxInStates(ctx context.Context, states ...string) (int, error) {
	if len(states) == 0 {
		return 0, nil
	}
	marks := make([]string, len(states))
	args := make([]any, len(states))
	for i, state := range states {
		marks[i] = "?"
		args[i] = state
	}
	var n int
	query := `SELECT COUNT(*) FROM outbox WHERE state IN (` + strings.Join(marks, ", ") + `)`
	if err := d.sql.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count outbox rows: %w", err)
	}
	return n, nil
}
