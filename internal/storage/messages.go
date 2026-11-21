package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrMessageNotFound is returned when a message id has no row.
var ErrMessageNotFound = errors.New("storage: message not found")

// Message flags are a bitmask stored in a single integer column. One column
// stays compact and maps directly onto the imap flag set, instead of adding a
// new boolean column every time another flag needs caching.
type Flag uint8

const (
	FlagSeen Flag = 1 << iota
	FlagFlagged
	FlagDeleted
)

// Has reports whether mask contains flag.
func (mask Flag) Has(flag Flag) bool { return mask&flag != 0 }

// Message is cached envelope metadata plus bodies for one mail.
type Message struct {
	ID        int64
	AccountID int64
	FolderID  int64
	// UID is the stable imap identifier, never a sequence number, and is unique
	// within its folder for a given UIDVALIDITY.
	UID            uint32
	MessageID      string // rfc Message-ID header, for threading later
	Subject        string
	FromAddress    string
	FromName       string
	ToAddresses    string
	CcAddresses    string
	Date           time.Time
	Flags          Flag
	BodyPlain      string
	BodyHTML       string
	HasAttachments bool
	SizeBytes      int64
	// FlagColor is 0 (none) or 1..8, a small palette enum kept separate from the
	// Flags bitmask. SnoozeUntil (empty when not snoozed) and SnoozeHidden drive
	// the local snooze. Offline is 1 when the user pinned the message offline.
	FlagColor    int
	SnoozeUntil  string
	SnoozeHidden bool
	Offline      bool
	// ListUnsubscribe is the raw List-Unsubscribe header value ('' when the
	// message advertised none, or was cached before the column existed);
	// ListUnsubscribePost marks RFC 8058 one-click support.
	ListUnsubscribe     string
	ListUnsubscribePost bool
	// SMIME describes the message's s/mime signature as verified when it was
	// synced. The zero value means no signature, which is most mail.
	SMIME SMIMESignature
	// ReplyTo is the Reply-To header ('' when the message has none), and Auth
	// is what the receiving server reported about spf, dkim and dmarc. Both are
	// read at fetch time, since the raw headers are not kept afterwards. An
	// empty Auth means the server said nothing, or the message predates the
	// columns; neither is a failure.
	ReplyTo string
	Auth    MessageAuth
	// CharsetGuess names what the text was read as when the message declared no
	// charset or one nothing knows, and is 'detected' when the guess was made
	// where the name does not travel back. Empty for mail that was right about
	// itself, which is nearly all of it.
	CharsetGuess string
	// InReplyTo and References preserve parsed bare IDs, including order and
	// duplicates. Nil means unknown metadata; a non-nil empty slice means the
	// headers were fetched and contained no IDs.
	InReplyTo  []string
	References []string
}

// MessageAuth is the stored Authentication-Results verdict for one message.
// The domains are what the passing method vouched for, which is what alignment
// against the visible From is judged on.
type MessageAuth struct {
	SPF        string
	DKIM       string
	DMARC      string
	SPFDomain  string
	DKIMDomain string
}

// SMIMESignature is a stored verification verdict. Status is '' for unsigned
// mail, otherwise valid, untrusted or invalid; Detail explains anything that is
// not valid, in a sentence fit to show the reader.
type SMIMESignature struct {
	Status string
	Signer string
	Email  string
	Issuer string
	Detail string
	// Certs is the signing certificate and its issuer in DER, length-prefixed
	// so the pair survives one column. The raw message is not kept, so this is
	// what a revocation check works from. It is written on insert and read back
	// only by that check, never by a list, since a list has no use for a few
	// kilobytes of certificate per row. Fingerprint is sha-256 of the signing
	// certificate and keys the revocation cache.
	Certs       []byte
	Fingerprint string
}

// IncomingAttachment is attachment metadata together with its content, handed
// to InsertMessageWithAttachments. Content is read once and written to disk.
type IncomingAttachment struct {
	Filename    string
	ContentType string
	ContentID   string
	Content     io.Reader
}

// InsertMessage inserts a single message row and returns its new id.
func (d *DB) InsertMessage(ctx context.Context, m *Message) (int64, error) {
	return insertMessage(ctx, d.sql, m)
}

// InsertMessageWithAttachments inserts a message and its attachments
// atomically. The message row, every attachment row and the has_attachments
// flag are committed together, and the attachment files are written to disk as
// part of the same unit: if anything fails the transaction rolls back and the
// already written files are removed, so the cache never ends up half written.
func (d *DB) InsertMessageWithAttachments(ctx context.Context, m *Message, atts []IncomingAttachment) (int64, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storage: begin message insert: %w", err)
	}
	defer tx.Rollback()

	m.HasAttachments = len(atts) > 0
	id, err := insertMessage(ctx, tx, m)
	if err != nil {
		return 0, err
	}

	var writtenPaths []string
	for _, in := range atts {
		saved, err := d.writeAttachmentFile(m.AccountID, id, in.Filename, in.Content)
		if err != nil {
			d.removeAttachmentFiles(writtenPaths)
			return 0, err
		}
		writtenPaths = append(writtenPaths, saved.DiskPath)
		saved.MessageID = id
		saved.ContentType = in.ContentType
		saved.ContentID = in.ContentID
		if err := insertAttachment(ctx, tx, saved); err != nil {
			d.removeAttachmentFiles(writtenPaths)
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		d.removeAttachmentFiles(writtenPaths)
		return 0, fmt.Errorf("storage: commit message insert: %w", err)
	}
	m.ID = id
	return id, nil
}

// orEmptyBlob keeps a nil slice from binding as NULL, which a NOT NULL column
// refuses. Unsigned mail carries no certificates and must still insert.
func orEmptyBlob(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

func insertMessage(ctx context.Context, ex execer, m *Message) (int64, error) {
	const query = `
INSERT INTO messages (
    account_id, folder_id, uid, message_id, subject, from_address, from_name,
    to_addresses, cc_addresses, date, flags, body_plain, body_html,
    has_attachments, size_bytes, list_unsubscribe, list_unsubscribe_post,
    smime_status, smime_signer, smime_email, smime_issuer, smime_detail,
    smime_certs, smime_fingerprint,
    reply_to, auth_spf, auth_dkim, auth_dmarc, auth_spf_domain, auth_dkim_domain,
    charset_guess, in_reply_to, [references]
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := ex.ExecContext(ctx, query,
		m.AccountID, m.FolderID, m.UID, m.MessageID, m.Subject, m.FromAddress,
		m.FromName, m.ToAddresses, m.CcAddresses, formatTime(m.Date), uint8(m.Flags),
		m.BodyPlain, m.BodyHTML, boolToInt(m.HasAttachments), m.SizeBytes,
		m.ListUnsubscribe, boolToInt(m.ListUnsubscribePost),
		m.SMIME.Status, m.SMIME.Signer, m.SMIME.Email, m.SMIME.Issuer, m.SMIME.Detail,
		orEmptyBlob(m.SMIME.Certs), m.SMIME.Fingerprint,
		m.ReplyTo, m.Auth.SPF, m.Auth.DKIM, m.Auth.DMARC, m.Auth.SPFDomain, m.Auth.DKIMDomain,
		m.CharsetGuess, encodeThreadingIDs(m.InReplyTo), encodeThreadingIDs(m.References))
	if err != nil {
		return 0, fmt.Errorf("storage: insert message uid %d: %w", m.UID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: message insert id: %w", err)
	}
	m.ID = id
	return id, nil
}

// GetMessage returns one message by id, or ErrMessageNotFound.
func (d *DB) GetMessage(ctx context.Context, id int64) (*Message, error) {
	m, err := scanMessage(d.sql.QueryRowContext(ctx, selectMessageByID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get message %d: %w", id, err)
	}
	return m, nil
}

// ListMessages returns messages in a folder, newest first, capped at limit
// (limit <= 0 means no cap).
func (d *DB) ListMessages(ctx context.Context, folderID int64, limit int) ([]Message, error) {
	const query = selectMessageColumns + `
FROM messages WHERE folder_id = ? ORDER BY date DESC, uid DESC LIMIT ?`
	rows, err := d.sql.QueryContext(ctx, query, folderID, normalizeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("storage: list messages for folder %d: %w", folderID, err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan message: %w", err)
		}
		messages = append(messages, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate messages: %w", err)
	}
	return messages, nil
}

// ListMessagesForIndex returns messages with id greater than afterID, ordered by
// id ascending, up to limit. The search layer uses it to backfill and to index
// newly synced mail incrementally by walking the id watermark forward.
func (d *DB) ListMessagesForIndex(ctx context.Context, afterID int64, limit int) ([]Message, error) {
	const query = selectMessageColumns + `
FROM messages WHERE id > ? ORDER BY id ASC LIMIT ?`
	rows, err := d.sql.QueryContext(ctx, query, afterID, normalizeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("storage: list messages for index after %d: %w", afterID, err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan message for index: %w", err)
		}
		messages = append(messages, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate index messages: %w", err)
	}
	return messages, nil
}

// SetMessageFlags replaces a message's flag bitmask.
func (d *DB) SetMessageFlags(ctx context.Context, id int64, flags Flag) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET flags = ? WHERE id = ?`, uint8(flags), id)
	if err != nil {
		return fmt.Errorf("storage: set flags on message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// DeleteMessage removes a message; its attachment rows cascade. Attachment
// files on disk are removed separately via DeleteAttachmentFilesForMessage.
func (d *DB) DeleteMessage(ctx context.Context, id int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("storage: delete message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

const selectMessageColumns = `
SELECT id, account_id, folder_id, uid, message_id, subject, from_address,
       from_name, to_addresses, cc_addresses, date, flags, body_plain,
       body_html, has_attachments, size_bytes, flag_color, snooze_until,
       snooze_hidden, offline, list_unsubscribe, list_unsubscribe_post,
       smime_status, smime_signer, smime_email, smime_issuer, smime_detail,
       smime_fingerprint, reply_to, auth_spf, auth_dkim, auth_dmarc, auth_spf_domain,
       auth_dkim_domain, charset_guess, in_reply_to, [references]`

const selectMessageByID = selectMessageColumns + `
FROM messages WHERE id = ?`

func scanMessage(row rowScanner) (*Message, error) {
	var (
		m            Message
		date         string
		flags        uint8
		hasAtt       int
		snoozeHidden int
		offline      int
		unsubPost    int
		inReplyTo    sql.NullString
		references   sql.NullString
	)
	if err := row.Scan(&m.ID, &m.AccountID, &m.FolderID, &m.UID, &m.MessageID,
		&m.Subject, &m.FromAddress, &m.FromName, &m.ToAddresses, &m.CcAddresses,
		&date, &flags, &m.BodyPlain, &m.BodyHTML, &hasAtt, &m.SizeBytes,
		&m.FlagColor, &m.SnoozeUntil, &snoozeHidden, &offline,
		&m.ListUnsubscribe, &unsubPost,
		&m.SMIME.Status, &m.SMIME.Signer, &m.SMIME.Email, &m.SMIME.Issuer,
		&m.SMIME.Detail, &m.SMIME.Fingerprint, &m.ReplyTo, &m.Auth.SPF, &m.Auth.DKIM, &m.Auth.DMARC,
		&m.Auth.SPFDomain, &m.Auth.DKIMDomain, &m.CharsetGuess, &inReplyTo, &references); err != nil {
		return nil, err
	}
	t, err := parseTime(date)
	if err != nil {
		return nil, err
	}
	m.Date = t
	m.Flags = Flag(flags)
	m.HasAttachments = hasAtt != 0
	m.SnoozeHidden = snoozeHidden != 0
	m.Offline = offline != 0
	m.ListUnsubscribePost = unsubPost != 0
	m.InReplyTo = decodeThreadingIDs(inReplyTo)
	m.References = decodeThreadingIDs(references)
	return &m, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// normalizeLimit turns a non positive limit into a no cap sentinel for LIMIT.
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return -1
	}
	return limit
}
