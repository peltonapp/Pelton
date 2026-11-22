package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrAddressBookNotFound is returned when an address book id has no row.
var ErrAddressBookNotFound = errors.New("storage: address book not found")

// ErrContactNotFound is returned when a contact id has no row.
var ErrContactNotFound = errors.New("storage: contact not found")

// AddressBook is one CardDAV collection the user configured (#168). The
// password is not here: it lives in the os keyring like every other secret.
type AddressBook struct {
	ID int64
	// AccountID is the mail account this book was discovered from, 0 when the
	// user added it by hand.
	AccountID int64
	Name      string
	URL       string
	// CollectionPath is the address book's path on the server, which is what
	// the sync report and every write are addressed to.
	CollectionPath string
	Username       string
	SyncToken      string
	// ReadOnly is set when the server refuses writes, so the ui can say so
	// before the user types an edit.
	ReadOnly   bool
	LastSyncAt string
	LastError  string
	Position   int
	CreatedAt  string
}

// Contact is one address book entry as stored. Card is the vCard the server
// holds, kept whole so an edit here never drops a property Pelton has no field
// for.
type Contact struct {
	ID           int64
	BookID       int64
	Path         string
	ETag         string
	UID          string
	FullName     string
	Organization string
	Title        string
	Note         string
	Card         string
	UpdatedAt    string
	Emails       []ContactValue
	Phones       []ContactValue
}

// ContactValue is one address or number with its vCard label.
type ContactValue struct {
	Value string
	Label string
}

// Local reports whether the book lives only in this database: it has no
// server, so it is never synced and edits are stored here alone. Imported
// address books are local.
func (b AddressBook) Local() bool { return b.URL == "" }

const addressBookColumns =`id, coalesce(account_id, 0), name, url, collection_path,
       username, sync_token, read_only, last_sync_at, last_error, position, created_at`

// CreateAddressBook inserts an address book and returns its new id.
func (d *DB) CreateAddressBook(ctx context.Context, b *AddressBook) (int64, error) {
	const query = `
INSERT INTO address_books (account_id, name, url, collection_path, username, read_only, position, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := d.sql.ExecContext(ctx, query,
		nullableAccount(b.AccountID), b.Name, b.URL, b.CollectionPath, b.Username,
		boolToInt(b.ReadOnly), b.Position, nowText())
	if err != nil {
		return 0, fmt.Errorf("storage: insert address book %q: %w", b.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: address book insert id: %w", err)
	}
	b.ID = id
	return id, nil
}

// ListAddressBooks returns every configured address book, in the order the user
// arranged them and then by id.
func (d *DB) ListAddressBooks(ctx context.Context) ([]AddressBook, error) {
	const query = `SELECT ` + addressBookColumns + `
FROM address_books ORDER BY position = 0, position, id`
	rows, err := d.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("storage: list address books: %w", err)
	}
	defer rows.Close()

	var books []AddressBook
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan address book: %w", err)
		}
		books = append(books, *b)
	}
	return books, rows.Err()
}

// GetAddressBook returns one address book, or ErrAddressBookNotFound.
func (d *DB) GetAddressBook(ctx context.Context, id int64) (*AddressBook, error) {
	const query = `SELECT ` + addressBookColumns + ` FROM address_books WHERE id = ?`
	b, err := scanBook(d.sql.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAddressBookNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get address book %d: %w", id, err)
	}
	return b, nil
}

// UpdateAddressBook saves the editable fields of a book: its name, where it
// points and who it logs in as. The sync token and the last outcome are written
// by the sync itself.
func (d *DB) UpdateAddressBook(ctx context.Context, b AddressBook) error {
	const query = `
UPDATE address_books
SET name = ?, url = ?, collection_path = ?, username = ?, read_only = ?
WHERE id = ?`
	res, err := d.sql.ExecContext(ctx, query,
		b.Name, b.URL, b.CollectionPath, b.Username, boolToInt(b.ReadOnly), b.ID)
	if err != nil {
		return fmt.Errorf("storage: update address book %d: %w", b.ID, err)
	}
	return requireOneRow(res, ErrAddressBookNotFound)
}

// DeleteAddressBook removes a book and the contacts it held. Nothing is deleted
// on the server: this only stops Pelton reading it.
func (d *DB) DeleteAddressBook(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM address_books WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete address book %d: %w", id, err)
	}
	return requireOneRow(res, ErrAddressBookNotFound)
}

// RecordAddressBookSync stores the outcome of a sync run: the new token and
// when it happened on success, or the error on failure with the token left
// alone so the next attempt resumes from the same place.
func (d *DB) RecordAddressBookSync(ctx context.Context, id int64, token, failure string) error {
	if failure != "" {
		_, err := d.sql.ExecContext(ctx,
			`UPDATE address_books SET last_error = ? WHERE id = ?`, failure, id)
		if err != nil {
			return fmt.Errorf("storage: record address book %d failure: %w", id, err)
		}
		return nil
	}
	_, err := d.sql.ExecContext(ctx,
		`UPDATE address_books SET sync_token = ?, last_sync_at = ?, last_error = '' WHERE id = ?`,
		token, nowText(), id)
	if err != nil {
		return fmt.Errorf("storage: record address book %d sync: %w", id, err)
	}
	return nil
}

// SaveContact inserts or updates a contact by its server path, replacing its
// addresses and numbers. It is the one write the sync uses, so a card that came
// back changed lands as one row rather than a delete and an insert.
func (d *DB) SaveContact(ctx context.Context, c *Contact) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin save contact: %w", err)
	}
	defer tx.Rollback()

	const query = `
INSERT INTO contacts (book_id, path, etag, uid, full_name, organization, title, note, card, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(book_id, path) DO UPDATE SET
    etag = excluded.etag, uid = excluded.uid, full_name = excluded.full_name,
    organization = excluded.organization, title = excluded.title, note = excluded.note,
    card = excluded.card, updated_at = excluded.updated_at`
	if _, err := tx.ExecContext(ctx, query,
		c.BookID, c.Path, c.ETag, c.UID, c.FullName, c.Organization, c.Title, c.Note,
		c.Card, nowText()); err != nil {
		return fmt.Errorf("storage: save contact %q: %w", c.Path, err)
	}

	var id int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM contacts WHERE book_id = ? AND path = ?`, c.BookID, c.Path).Scan(&id); err != nil {
		return fmt.Errorf("storage: contact id for %q: %w", c.Path, err)
	}
	c.ID = id

	if err := replaceValues(ctx, tx, `contact_emails`, `email`, id, c.Emails); err != nil {
		return err
	}
	if err := replaceValues(ctx, tx, `contact_phones`, `phone`, id, c.Phones); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit save contact: %w", err)
	}
	return nil
}

// replaceValues rewrites one contact's addresses or numbers.
func replaceValues(ctx context.Context, tx *sql.Tx, table, column string, contactID int64, values []ContactValue) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE contact_id = ?`, contactID); err != nil {
		return fmt.Errorf("storage: clear %s for contact %d: %w", table, contactID, err)
	}
	insert := `INSERT INTO ` + table + ` (contact_id, ` + column + `, label, position) VALUES (?, ?, ?, ?)`
	for i, v := range values {
		value := strings.TrimSpace(v.Value)
		if value == "" {
			continue
		}
		if column == "email" {
			value = strings.ToLower(value)
		}
		if _, err := tx.ExecContext(ctx, insert, contactID, value, v.Label, i+1); err != nil {
			return fmt.Errorf("storage: insert %s for contact %d: %w", table, contactID, err)
		}
	}
	return nil
}

// DeleteContactByPath removes the local copy of a contact the server no longer
// has. A path that is already gone is not an error: the sync says what is gone,
// not what was there.
func (d *DB) DeleteContactByPath(ctx context.Context, bookID int64, path string) error {
	if _, err := d.sql.ExecContext(ctx,
		`DELETE FROM contacts WHERE book_id = ? AND path = ?`, bookID, path); err != nil {
		return fmt.Errorf("storage: delete contact %q: %w", path, err)
	}
	return nil
}

// DeleteContact removes one contact by id.
func (d *DB) DeleteContact(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM contacts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete contact %d: %w", id, err)
	}
	return requireOneRow(res, ErrContactNotFound)
}

// ClearBookContacts empties a book, which is what a server rejecting its own
// sync token forces: the next run is a full read and this is what it replaces.
func (d *DB) ClearBookContacts(ctx context.Context, bookID int64) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM contacts WHERE book_id = ?`, bookID); err != nil {
		return fmt.Errorf("storage: clear contacts for book %d: %w", bookID, err)
	}
	return nil
}

const contactColumns = `id, book_id, path, etag, uid, full_name, organization, title, note, card, updated_at`

// ListContacts returns every contact, by name then address, with their
// addresses and numbers filled in. bookID 0 means all books.
func (d *DB) ListContacts(ctx context.Context, bookID int64) ([]Contact, error) {
	query := `SELECT ` + contactColumns + ` FROM contacts`
	var args []any
	if bookID != 0 {
		query += ` WHERE book_id = ?`
		args = append(args, bookID)
	}
	query += ` ORDER BY full_name = '', lower(full_name), id`

	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan contact: %w", err)
		}
		contacts = append(contacts, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate contacts: %w", err)
	}
	return d.fillContactValues(ctx, contacts)
}

// GetContact returns one contact with its addresses and numbers.
func (d *DB) GetContact(ctx context.Context, id int64) (*Contact, error) {
	const query = `SELECT ` + contactColumns + ` FROM contacts WHERE id = ?`
	c, err := scanContact(d.sql.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrContactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get contact %d: %w", id, err)
	}
	filled, err := d.fillContactValues(ctx, []Contact{*c})
	if err != nil {
		return nil, err
	}
	return &filled[0], nil
}

// fillContactValues loads the addresses and numbers for a set of contacts in
// two queries rather than two per contact.
func (d *DB) fillContactValues(ctx context.Context, contacts []Contact) ([]Contact, error) {
	if len(contacts) == 0 {
		return contacts, nil
	}
	index := make(map[int64]int, len(contacts))
	ids := make([]any, 0, len(contacts))
	for i, c := range contacts {
		index[c.ID] = i
		ids = append(ids, c.ID)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")

	for _, table := range []struct{ name, column string }{{"contact_emails", "email"}, {"contact_phones", "phone"}} {
		query := `SELECT contact_id, ` + table.column + `, label FROM ` + table.name +
			` WHERE contact_id IN (` + placeholders + `) ORDER BY contact_id, position, rowid`
		rows, err := d.sql.QueryContext(ctx, query, ids...)
		if err != nil {
			return nil, fmt.Errorf("storage: list %s: %w", table.name, err)
		}
		for rows.Next() {
			var (
				contactID int64
				value     ContactValue
			)
			if err := rows.Scan(&contactID, &value.Value, &value.Label); err != nil {
				rows.Close()
				return nil, fmt.Errorf("storage: scan %s: %w", table.name, err)
			}
			at, ok := index[contactID]
			if !ok {
				continue
			}
			if table.name == "contact_emails" {
				contacts[at].Emails = append(contacts[at].Emails, value)
			} else {
				contacts[at].Phones = append(contacts[at].Phones, value)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("storage: iterate %s: %w", table.name, err)
		}
	}
	return contacts, nil
}

// ContactMatch is one autocomplete candidate from the real address book.
type ContactMatch struct {
	Email string
	Name  string
}

// SearchContacts returns contacts whose name or address matches, for compose
// autocomplete. Matching on either is the point: people search for a person by
// name and for a mailing list by address.
func (d *DB) SearchContacts(ctx context.Context, query string, limit int) ([]ContactMatch, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	if limit <= 0 {
		limit = 8
	}
	const stmt = `
SELECT e.email, c.full_name
FROM contact_emails e
JOIN contacts c ON c.id = e.contact_id
WHERE e.email LIKE ? ESCAPE '\' OR lower(c.full_name) LIKE ? ESCAPE '\'
ORDER BY lower(c.full_name) = '', lower(c.full_name), e.position, e.email
LIMIT ?`
	pattern := "%" + escapeLike(query) + "%"
	rows, err := d.sql.QueryContext(ctx, stmt, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: search contacts: %w", err)
	}
	defer rows.Close()

	var out []ContactMatch
	for rows.Next() {
		var m ContactMatch
		if err := rows.Scan(&m.Email, &m.Name); err != nil {
			return nil, fmt.Errorf("storage: scan contact match: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ContactNames returns the name to show for each of the given addresses that
// the real address book knows, so a synced contact's name wins over whatever a
// sender once put in a From header.
func (d *DB) ContactNames(ctx context.Context, emails []string) (map[string]string, error) {
	if len(emails) == 0 {
		return map[string]string{}, nil
	}
	args := make([]any, 0, len(emails))
	for _, e := range emails {
		args = append(args, strings.ToLower(strings.TrimSpace(e)))
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	query := `
SELECT e.email, c.full_name
FROM contact_emails e
JOIN contacts c ON c.id = e.contact_id
WHERE e.email IN (` + placeholders + `) AND c.full_name <> ''`
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: contact names: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, len(emails))
	for rows.Next() {
		var email, name string
		if err := rows.Scan(&email, &name); err != nil {
			return nil, fmt.Errorf("storage: scan contact name: %w", err)
		}
		// several contacts can carry the same address; first one wins, which
		// the ordering by contact id makes stable.
		if _, seen := out[email]; !seen {
			out[email] = name
		}
	}
	return out, rows.Err()
}

func scanBook(row rowScanner) (*AddressBook, error) {
	var (
		b        AddressBook
		readOnly int
	)
	if err := row.Scan(&b.ID, &b.AccountID, &b.Name, &b.URL, &b.CollectionPath,
		&b.Username, &b.SyncToken, &readOnly, &b.LastSyncAt, &b.LastError,
		&b.Position, &b.CreatedAt); err != nil {
		return nil, err
	}
	b.ReadOnly = readOnly != 0
	return &b, nil
}

func scanContact(row rowScanner) (*Contact, error) {
	var c Contact
	if err := row.Scan(&c.ID, &c.BookID, &c.Path, &c.ETag, &c.UID, &c.FullName,
		&c.Organization, &c.Title, &c.Note, &c.Card, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

// nullableAccount stores 0 as NULL, so a hand-added book has no account rather
// than one that does not exist.
func nullableAccount(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
