package storage

import (
	"context"
	"errors"
	"fmt"
)

// SenderColors values: how html mail from a sender is coloured in a dark
// theme. The empty value follows the global setting.
const (
	SenderColorsDefault  = ""
	SenderColorsAdapt    = "adapt"
	SenderColorsOriginal = "original"
)

// Reader text sizes a sender can be remembered at, in px. Zero follows the
// global size.
const (
	SenderFontSizeMin = 10
	SenderFontSizeMax = 32
)

// ErrSenderReading reports a sender reading choice the store does not take:
// an empty address, an unknown colour mode or a text size out of range.
var ErrSenderReading = errors.New("storage: invalid sender reading choice")

// SenderReading is how the reader shows one sender's mail.
type SenderReading struct {
	// Address is the bare address, lower case.
	Address string
	// Colors is SenderColorsAdapt, SenderColorsOriginal or empty.
	Colors string
	// FontSize is the reader text size in px, or 0.
	FontSize int
}

// ListSenderReading returns every sender with a remembered reading choice,
// ordered by address.
func (d *DB) ListSenderReading(ctx context.Context) ([]SenderReading, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT address, colors, font_size FROM sender_reading ORDER BY address`)
	if err != nil {
		return nil, fmt.Errorf("storage: list sender reading: %w", err)
	}
	defer rows.Close()
	var out []SenderReading
	for rows.Next() {
		var s SenderReading
		if err := rows.Scan(&s.Address, &s.Colors, &s.FontSize); err != nil {
			return nil, fmt.Errorf("storage: scan sender reading: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetSenderColors remembers how html mail from address is coloured; the empty
// mode forgets the choice. A sender left with no choice at all is removed.
func (d *DB) SetSenderColors(ctx context.Context, address, mode string) error {
	if mode != SenderColorsDefault && mode != SenderColorsAdapt && mode != SenderColorsOriginal {
		return ErrSenderReading
	}
	return d.setSenderReading(ctx, address, "colors", mode)
}

// SetSenderFontSize remembers the reader text size for address; 0 forgets it.
// A sender left with no choice at all is removed.
func (d *DB) SetSenderFontSize(ctx context.Context, address string, size int) error {
	if size != 0 && (size < SenderFontSizeMin || size > SenderFontSizeMax) {
		return ErrSenderReading
	}
	return d.setSenderReading(ctx, address, "font_size", size)
}

// setSenderReading writes one column of a sender's row, creating the row when
// needed, and drops the row once both choices are back to their defaults.
// column is one of the two fixed names above, never user input.
func (d *DB) setSenderReading(ctx context.Context, address, column string, value any) error {
	addr := normalizeAddress(address)
	if addr == "" {
		return ErrSenderReading
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin sender reading: %w", err)
	}
	defer tx.Rollback()
	upsert := `INSERT INTO sender_reading (address, ` + column + `, updated_at) VALUES (?, ?, ?)
ON CONFLICT(address) DO UPDATE SET ` + column + ` = excluded.` + column + `, updated_at = excluded.updated_at`
	if _, err := tx.ExecContext(ctx, upsert, addr, value, nowText()); err != nil {
		return fmt.Errorf("storage: set sender reading: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sender_reading WHERE address = ? AND colors = '' AND font_size = 0`, addr); err != nil {
		return fmt.Errorf("storage: prune sender reading: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit sender reading: %w", err)
	}
	return nil
}

// ResetSenderReading forgets every remembered sender reading choice and
// returns how many senders there were.
func (d *DB) ResetSenderReading(ctx context.Context) (int, error) {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM sender_reading`)
	if err != nil {
		return 0, fmt.Errorf("storage: reset sender reading: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("storage: count reset sender reading: %w", err)
	}
	return int(n), nil
}
