package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// addressBookCap bounds the harvested address book. Once exceeded, the least
// used and then oldest-touched entries are evicted so the book stays a useful
// set of the addresses the user actually corresponds with.
const addressBookCap = 1000

// AddressBookEntry is one harvested contact used for compose autocomplete.
type AddressBookEntry struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	UseCount  int    `json:"useCount"`
	LastUsed  string `json:"lastUsed"`
	CreatedAt string `json:"createdAt"`
}

// RecordAddress harvests an address seen in mail. It upserts the entry, bumping
// use_count and last_used, filling in a name only when one is known, then prunes
// the book back under its cap. A blank email is ignored.
func (d *DB) RecordAddress(ctx context.Context, email, name string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil
	}
	name = strings.TrimSpace(name)
	now := nowText()
	const query = `
INSERT INTO address_book (email, name, use_count, last_used, created_at)
VALUES (?, ?, 1, ?, ?)
ON CONFLICT(email) DO UPDATE SET
    use_count = use_count + 1,
    last_used = excluded.last_used,
    -- keep an existing name unless we now have one and had none before.
    name = CASE WHEN address_book.name = '' THEN excluded.name ELSE address_book.name END`
	if _, err := d.sql.ExecContext(ctx, query, email, name, now, now); err != nil {
		return fmt.Errorf("storage: record address %q: %w", email, err)
	}
	return d.pruneAddressBook(ctx)
}

// HarvestSenders seeds the address book from every sender already cached in the
// messages table. It only inserts addresses not present yet (INSERT OR IGNORE),
// so re-running is idempotent and never clobbers counts built up from sends.
func (d *DB) HarvestSenders(ctx context.Context) error {
	const query = `
INSERT OR IGNORE INTO address_book (email, name, use_count, last_used, created_at)
SELECT lower(from_address), MAX(from_name), COUNT(*), MAX(date), ?
FROM messages
WHERE from_address LIKE '%@%' AND ` + notVaulted + `
GROUP BY lower(from_address)`
	if _, err := d.sql.ExecContext(ctx, query, nowText()); err != nil {
		return fmt.Errorf("storage: harvest senders: %w", err)
	}
	return d.pruneAddressBook(ctx)
}

// pruneAddressBook evicts the lowest-ranked entries beyond the cap, ranking by
// use_count then last_used so the least used and oldest go first.
func (d *DB) pruneAddressBook(ctx context.Context) error {
	const query = `
DELETE FROM address_book WHERE email IN (
    SELECT email FROM address_book
    ORDER BY use_count DESC, last_used DESC
    LIMIT -1 OFFSET ?
)`
	if _, err := d.sql.ExecContext(ctx, query, addressBookCap); err != nil {
		return fmt.Errorf("storage: prune address book: %w", err)
	}
	return nil
}

// SearchAddresses returns autocomplete candidates matching q (against email or
// name), ranked by use then recency. An empty q returns the top entries.
func (d *DB) SearchAddresses(ctx context.Context, q string, limit int) ([]AddressBookEntry, error) {
	q = strings.TrimSpace(strings.ToLower(q))
	like := "%" + escapeLike(q) + "%"
	const query = `
SELECT email, name, use_count, last_used, created_at
FROM address_book
WHERE ? = '' OR lower(email) LIKE ? ESCAPE '\' OR lower(name) LIKE ? ESCAPE '\'
ORDER BY use_count DESC, last_used DESC
LIMIT ?`
	rows, err := d.sql.QueryContext(ctx, query, q, like, like, normalizeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("storage: search addresses: %w", err)
	}
	defer rows.Close()
	return scanAddressBook(rows)
}

// ListAddresses returns the whole book for the settings manager, ranked.
func (d *DB) ListAddresses(ctx context.Context) ([]AddressBookEntry, error) {
	const query = `
SELECT email, name, use_count, last_used, created_at
FROM address_book ORDER BY use_count DESC, last_used DESC`
	rows, err := d.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("storage: list addresses: %w", err)
	}
	defer rows.Close()
	return scanAddressBook(rows)
}

// DeleteAddress removes one harvested contact.
func (d *DB) DeleteAddress(ctx context.Context, email string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM address_book WHERE email = ?`, email); err != nil {
		return fmt.Errorf("storage: delete address %q: %w", email, err)
	}
	return nil
}

func scanAddressBook(rows *sql.Rows) ([]AddressBookEntry, error) {
	var out []AddressBookEntry
	for rows.Next() {
		var e AddressBookEntry
		if err := rows.Scan(&e.Email, &e.Name, &e.UseCount, &e.LastUsed, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("storage: scan address: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate addresses: %w", err)
	}
	return out, nil
}

// escapeLike escapes the LIKE wildcards in user input so a literal % or _ typed
// in the search box does not act as a wildcard.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
