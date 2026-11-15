package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrSnippetNotFound is returned when a snippet id has no row.
var ErrSnippetNotFound = errors.New("storage: snippet not found")

// Snippet is a reusable text block for the composer. Body is plain text with
// {placeholders}. Subject, To and Cc are optional and fill only empty fields.
// Trigger is the word that, typed with its prefix, inserts the snippet; empty
// means none. Position orders the list.
type Snippet struct {
	ID        int64
	Name      string
	Body      string
	Subject   string
	To        string
	Cc        string
	Trigger   string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

const snippetColumns = `id, name, body, subject, to_addresses, cc_addresses, trigger_word, position, created_at, updated_at`

// ListSnippets returns every snippet in the user's order.
func (d *DB) ListSnippets(ctx context.Context) ([]Snippet, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT `+snippetColumns+` FROM snippets ORDER BY position, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list snippets: %w", err)
	}
	defer rows.Close()
	var out []Snippet
	for rows.Next() {
		s, err := scanSnippet(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan snippet: %w", err)
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate snippets: %w", err)
	}
	return out, nil
}

// CreateSnippet inserts a snippet at the end of the list and returns its id.
func (d *DB) CreateSnippet(ctx context.Context, s *Snippet) (int64, error) {
	now := time.Now().UTC()
	res, err := d.sql.ExecContext(ctx, `
INSERT INTO snippets (name, body, subject, to_addresses, cc_addresses, trigger_word, position, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM snippets), ?, ?)`,
		s.Name, s.Body, s.Subject, s.To, s.Cc, s.Trigger, formatTime(now), formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("storage: insert snippet: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: snippet insert id: %w", err)
	}
	if err := d.sql.QueryRowContext(ctx, `SELECT position FROM snippets WHERE id = ?`, id).Scan(&s.Position); err != nil {
		return 0, fmt.Errorf("storage: snippet position: %w", err)
	}
	s.ID, s.CreatedAt, s.UpdatedAt = id, now, now
	return id, nil
}

// UpdateSnippet saves the editable fields of a snippet; its position stays.
func (d *DB) UpdateSnippet(ctx context.Context, s *Snippet) error {
	now := time.Now().UTC()
	res, err := d.sql.ExecContext(ctx, `
UPDATE snippets SET name = ?, body = ?, subject = ?, to_addresses = ?, cc_addresses = ?, trigger_word = ?, updated_at = ?
WHERE id = ?`, s.Name, s.Body, s.Subject, s.To, s.Cc, s.Trigger, formatTime(now), s.ID)
	if err != nil {
		return fmt.Errorf("storage: update snippet %d: %w", s.ID, err)
	}
	s.UpdatedAt = now
	return requireOneRow(res, ErrSnippetNotFound)
}

// DeleteSnippet removes a snippet, or returns ErrSnippetNotFound.
func (d *DB) DeleteSnippet(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM snippets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete snippet %d: %w", id, err)
	}
	return requireOneRow(res, ErrSnippetNotFound)
}

// ReorderSnippets puts the snippets in the order of ids, in one transaction.
// Snippets left out keep their place after the ones named.
func (d *DB) ReorderSnippets(ctx context.Context, ids []int64) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin reorder snippets: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE snippets SET position = position + ?`, len(ids)); err != nil {
		return fmt.Errorf("storage: shift snippets: %w", err)
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE snippets SET position = ? WHERE id = ?`, i, id); err != nil {
			return fmt.Errorf("storage: place snippet %d: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit reorder snippets: %w", err)
	}
	return nil
}

func scanSnippet(row rowScanner) (*Snippet, error) {
	var (
		s                Snippet
		created, updated string
	)
	if err := row.Scan(&s.ID, &s.Name, &s.Body, &s.Subject, &s.To, &s.Cc, &s.Trigger, &s.Position, &created, &updated); err != nil {
		return nil, err
	}
	var err error
	if s.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if s.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &s, nil
}
