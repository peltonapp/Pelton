package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AccountSieve is how an account reaches its sieve service and what the app
// learned about it. The zero value means the defaults: the imap host, port
// 4190, STARTTLS, nothing learned yet.
type AccountSieve struct {
	AccountID int64
	// Host, Port and TLS are set by hand; empty and 0 mean the defaults. TLS
	// is "" for STARTTLS or "ssl" for implicit TLS.
	Host string
	Port int
	TLS  string
	// Unsupported records that the server has no sieve service.
	Unsupported bool
	CheckedAt   time.Time
	// PreviousActive is the script that was active before Pelton's last save,
	// "" when none was. HasPrevious is false until a save has happened.
	PreviousActive string
	HasPrevious    bool
}

// GetAccountSieve returns an account's sieve settings, the zero value with
// AccountID set when it has none.
func (d *DB) GetAccountSieve(ctx context.Context, accountID int64) (AccountSieve, error) {
	const query = `
SELECT host, port, tls, unsupported, checked_at, previous_active, has_previous
FROM account_sieve WHERE account_id = ?`
	s := AccountSieve{AccountID: accountID}
	var checked string
	err := d.sql.QueryRowContext(ctx, query, accountID).Scan(
		&s.Host, &s.Port, &s.TLS, &s.Unsupported, &checked, &s.PreviousActive, &s.HasPrevious)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("storage: get account %d sieve: %w", accountID, err)
	}
	if s.CheckedAt, err = parseTime(checked); err != nil {
		return s, err
	}
	return s, nil
}

// SetAccountSieve stores an account's sieve settings, replacing any before.
func (d *DB) SetAccountSieve(ctx context.Context, s AccountSieve) error {
	const query = `
INSERT INTO account_sieve (account_id, host, port, tls, unsupported, checked_at, previous_active, has_previous)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET host = excluded.host, port = excluded.port, tls = excluded.tls,
    unsupported = excluded.unsupported, checked_at = excluded.checked_at,
    previous_active = excluded.previous_active, has_previous = excluded.has_previous`
	checked := ""
	if !s.CheckedAt.IsZero() {
		checked = formatTime(s.CheckedAt.UTC())
	}
	if _, err := d.sql.ExecContext(ctx, query, s.AccountID, s.Host, s.Port, s.TLS,
		s.Unsupported, checked, s.PreviousActive, s.HasPrevious); err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY") {
			return ErrAccountNotFound
		}
		return fmt.Errorf("storage: set account %d sieve: %w", s.AccountID, err)
	}
	return nil
}
