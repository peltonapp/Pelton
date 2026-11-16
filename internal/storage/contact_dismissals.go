package storage

import (
	"context"
	"fmt"
)

// ContactKey is the identity a dismissal remembers a contact by: its vCard UID
// together with its book, or its server path when the card carries no UID. It
// survives the full resync that gives every contact a new row id.
func ContactKey(c Contact) string {
	if c.UID != "" {
		return fmt.Sprintf("uid:%d:%s", c.BookID, c.UID)
	}
	return fmt.Sprintf("path:%d:%s", c.BookID, c.Path)
}

// ContactPair is two contact keys in sorted order.
type ContactPair [2]string

// NewContactPair orders two keys, so a pair is the same whichever way round it
// was built.
func NewContactPair(a, b string) ContactPair {
	if b < a {
		a, b = b, a
	}
	return ContactPair{a, b}
}

// DismissContactPairs records pairs of contacts as not duplicates. A pair
// already recorded is left as it is.
func (d *DB) DismissContactPairs(ctx context.Context, pairs []ContactPair) error {
	if len(pairs) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin dismiss contact pairs: %w", err)
	}
	defer tx.Rollback()
	const insert = `INSERT OR IGNORE INTO contact_not_duplicates (key_a, key_b, created_at) VALUES (?, ?, ?)`
	now := nowText()
	for _, p := range pairs {
		if _, err := tx.ExecContext(ctx, insert, p[0], p[1], now); err != nil {
			return fmt.Errorf("storage: dismiss contact pair: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit dismiss contact pairs: %w", err)
	}
	return nil
}

// DismissedContactPairs returns every pair marked as not duplicates.
func (d *DB) DismissedContactPairs(ctx context.Context) (map[ContactPair]bool, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT key_a, key_b FROM contact_not_duplicates`)
	if err != nil {
		return nil, fmt.Errorf("storage: list dismissed contact pairs: %w", err)
	}
	defer rows.Close()
	out := make(map[ContactPair]bool)
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return nil, fmt.Errorf("storage: scan dismissed contact pair: %w", err)
		}
		out[NewContactPair(a, b)] = true
	}
	return out, rows.Err()
}

// ResetDismissedContactPairs forgets every dismissal and returns how many
// pairs there were.
func (d *DB) ResetDismissedContactPairs(ctx context.Context) (int, error) {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM contact_not_duplicates`)
	if err != nil {
		return 0, fmt.Errorf("storage: reset dismissed contact pairs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: reset dismissed contact pairs count: %w", err)
	}
	return int(n), nil
}
