package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ErrMessageNotFound is returned when a message id has no row.
var ErrMessageNotFound = errors.New("storage: message not found")

// ErrMessageBodyComplete is returned when InsertMessageWithAttachments finds
// an existing row whose body is already complete. The row, its flags and its
// attachment files are left unchanged.
var ErrMessageBodyComplete = errors.New("storage: message body already complete")

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
	// within its folder for a given UIDVALIDITY. RemoteID is the opaque server
	// id (imap stores the decimal uid). Rows from an adapter without uids keep
	// UID at 0 even when RemoteID looks numeric.
	UID         uint32
	RemoteID    string
	MessageID   string // rfc Message-ID header, for threading later
	Subject     string
	FromAddress string
	FromName    string
	ToAddresses string
	CcAddresses string
	Date        time.Time
	Flags       Flag
	BodyPlain   string
	BodyHTML    string
	// BodyComplete is false for a list stub, including when BodyPlain holds only
	// a preview. A full insert or body fill sets it true. Rows that existed
	// before the column were backfilled true, because they were stored by a
	// full fetch.
	BodyComplete   bool
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

// SMIMESignature is a stored verification verdict. Status is ” for unsigned
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

// InsertMessage inserts a single message row and returns its new id. This is a
// full write, so the row is marked body-complete.
func (d *DB) InsertMessage(ctx context.Context, m *Message) (int64, error) {
	m.BodyComplete = true
	return insertMessage(ctx, d.sql, m)
}

// UpsertMessageListMeta inserts a header-only stub or refreshes list columns for
// an existing (folder_id, remote_id). Preview is written to body_plain only when
// the row has no body yet, so a later Fetch does not lose filled text. It never
// marks the body complete: a new stub stays incomplete, and a refresh leaves
// body_complete as it was. A refresh keeps the stored flags while a local flag
// change is pending, so a server snapshot cannot undo it before it is pushed.
//
// The lookup and the write run in one transaction. Two syncs of the same folder
// can both find a message missing; without it the second insert fails on the
// unique (folder_id, remote_id) index instead of refreshing the first one's row.
func (d *DB) UpsertMessageListMeta(ctx context.Context, m *Message) (int64, error) {
	remoteID := m.RemoteID
	if remoteID == "" && m.UID != 0 {
		remoteID = strconv.FormatUint(uint64(m.UID), 10)
	}
	if remoteID == "" {
		return 0, fmt.Errorf("storage: upsert list meta requires remote_id")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storage: begin list meta upsert: %w", err)
	}
	defer tx.Rollback()

	var (
		id        int64
		bodyPlain string
		bodyHTML  string
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, body_plain, body_html FROM messages WHERE folder_id = ? AND remote_id = ?`,
		m.FolderID, remoteID,
	).Scan(&id, &bodyPlain, &bodyHTML)
	if errors.Is(err, sql.ErrNoRows) {
		stub := *m
		stub.RemoteID = remoteID
		stub.BodyComplete = false
		newID, err := insertMessage(ctx, tx, &stub)
		if err != nil {
			return 0, err
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("storage: commit list meta stub %q: %w", remoteID, err)
		}
		m.ID = newID
		return newID, nil
	}
	if err != nil {
		return 0, fmt.Errorf("storage: lookup message %q: %w", remoteID, err)
	}

	preview := m.BodyPlain
	newPlain := bodyPlain
	if bodyPlain == "" && bodyHTML == "" && preview != "" {
		newPlain = preview
	}
	_, err = tx.ExecContext(ctx, `
UPDATE messages SET
  subject = ?, from_address = ?, from_name = ?, to_addresses = ?,
  date = ?, flags = CASE WHEN pending_flags = 0 THEN ? ELSE flags END,
  size_bytes = ?, has_attachments = ?,
  body_plain = ?, uid = CASE WHEN uid = 0 AND ? != 0 THEN ? ELSE uid END
WHERE id = ?`,
		m.Subject, m.FromAddress, m.FromName, m.ToAddresses,
		formatTime(m.Date), uint8(m.Flags), m.SizeBytes, boolToInt(m.HasAttachments),
		newPlain, m.UID, m.UID, id,
	)
	if err != nil {
		return 0, fmt.Errorf("storage: update list meta %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storage: commit list meta %d: %w", id, err)
	}
	m.ID = id
	m.RemoteID = remoteID
	return id, nil
}

// InsertMessageWithAttachments inserts a message and its attachments
// atomically. When (folder_id, remote_id) already exists as an incomplete
// stub (body_complete = 0), the row is filled in place. A row that is already
// body-complete is left unchanged and ErrMessageBodyComplete is returned, so
// import and download cannot wipe a stored message. A stub fill keeps the
// stored mask when Flags == 0 (the caller never loaded flags) or when the row
// has a local flag change not pushed yet, so neither clears \Seen or
// \Flagged the user set. A zero Date
// (no Date header) keeps the stub's date. Attachment files
// already on disk are staged aside until the fill commits; a rollback puts
// them back. New files written for the fill are removed if the transaction
// does not commit.
func (d *DB) InsertMessageWithAttachments(ctx context.Context, m *Message, atts []IncomingAttachment) (int64, error) {
	remoteID := m.RemoteID
	if remoteID == "" && m.UID != 0 {
		remoteID = strconv.FormatUint(uint64(m.UID), 10)
	}

	var (
		existingID   int64
		bodyComplete int
	)
	if remoteID != "" {
		err := d.sql.QueryRowContext(ctx,
			`SELECT id, body_complete FROM messages WHERE folder_id = ? AND remote_id = ?`,
			m.FolderID, remoteID,
		).Scan(&existingID, &bodyComplete)
		if errors.Is(err, sql.ErrNoRows) {
			existingID = 0
		} else if err != nil {
			return 0, fmt.Errorf("storage: lookup message for body fill: %w", err)
		}
	}

	if existingID != 0 {
		if bodyComplete != 0 {
			return 0, ErrMessageBodyComplete
		}
		return d.replaceMessageBody(ctx, existingID, m, atts)
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storage: begin message insert: %w", err)
	}
	defer tx.Rollback()

	m.HasAttachments = len(atts) > 0
	m.BodyComplete = true
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

func (d *DB) replaceMessageBody(ctx context.Context, id int64, m *Message, atts []IncomingAttachment) (int64, error) {
	staged, stagingRoot, err := d.stageMessageDirs(m.AccountID, []int64{id})
	if err != nil {
		return 0, err
	}

	remoteID := m.RemoteID
	if remoteID == "" && m.UID != 0 {
		remoteID = strconv.FormatUint(uint64(m.UID), 10)
	}

	// flags is the mask written into the row. Zero from the caller means the
	// flags were not loaded, so the stored mask is kept.
	var flags Flag
	err = d.commitStagedReplace(ctx, m.AccountID, id, staged, stagingRoot, func(tx *sql.Tx) ([]string, error) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE message_id = ?`, id); err != nil {
			return nil, fmt.Errorf("storage: clear attachments for %d: %w", id, err)
		}

		// a local flag change not pushed yet outranks the server's flags the
		// fill carries; the push still needs it.
		var stored uint8
		var pending int
		if err := tx.QueryRowContext(ctx, `SELECT flags, pending_flags FROM messages WHERE id = ?`, id).Scan(&stored, &pending); err != nil {
			return nil, fmt.Errorf("storage: read flags for message %d: %w", id, err)
		}
		flags = m.Flags
		if flags == 0 || pending != 0 {
			flags = Flag(stored)
		}

		hasAttachments := len(atts) > 0
		// a From header without a display name keeps the name the list stub
		// already got from the envelope.
		_, err := tx.ExecContext(ctx, `
UPDATE messages SET
  uid = CASE WHEN ? != 0 THEN ? ELSE uid END,
  message_id = ?, subject = ?, from_address = ?, from_name = CASE WHEN ? != '' THEN ? ELSE from_name END,
  to_addresses = ?, cc_addresses = ?, date = CASE WHEN ? != '' THEN ? ELSE date END, flags = ?,
  body_plain = ?, body_html = ?, has_attachments = ?, size_bytes = ?,
  list_unsubscribe = ?, list_unsubscribe_post = ?,
  smime_status = ?, smime_signer = ?, smime_email = ?, smime_issuer = ?, smime_detail = ?,
  smime_certs = ?, smime_fingerprint = ?,
  reply_to = ?, auth_spf = ?, auth_dkim = ?, auth_dmarc = ?, auth_spf_domain = ?, auth_dkim_domain = ?,
  charset_guess = ?, body_complete = 1
WHERE id = ?`,
			m.UID, m.UID,
			m.MessageID, m.Subject, m.FromAddress, m.FromName, m.FromName,
			m.ToAddresses, m.CcAddresses, formatTime(m.Date), formatTime(m.Date), uint8(flags),
			m.BodyPlain, m.BodyHTML, boolToInt(hasAttachments), m.SizeBytes,
			m.ListUnsubscribe, boolToInt(m.ListUnsubscribePost),
			m.SMIME.Status, m.SMIME.Signer, m.SMIME.Email, m.SMIME.Issuer, m.SMIME.Detail,
			orEmptyBlob(m.SMIME.Certs), m.SMIME.Fingerprint,
			m.ReplyTo, m.Auth.SPF, m.Auth.DKIM, m.Auth.DMARC, m.Auth.SPFDomain, m.Auth.DKIMDomain,
			m.CharsetGuess, id,
		)
		if err != nil {
			return nil, fmt.Errorf("storage: update message body %d: %w", id, err)
		}

		written := make([]string, 0, len(atts))
		for _, in := range atts {
			saved, err := d.writeAttachmentFile(m.AccountID, id, in.Filename, in.Content)
			if err != nil {
				return written, err
			}
			written = append(written, saved.DiskPath)
			saved.MessageID = id
			saved.ContentType = in.ContentType
			saved.ContentID = in.ContentID
			if err := insertAttachment(ctx, tx, saved); err != nil {
				return written, err
			}
		}
		return written, nil
	})
	if err != nil {
		return 0, err
	}

	m.HasAttachments = len(atts) > 0
	m.BodyComplete = true
	m.Flags = flags
	m.ID = id
	m.RemoteID = remoteID
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
	remoteID := m.RemoteID
	if remoteID == "" && m.UID != 0 {
		remoteID = strconv.FormatUint(uint64(m.UID), 10)
	}
	const query = `
INSERT INTO messages (
    account_id, folder_id, uid, remote_id, message_id, subject, from_address, from_name,
    to_addresses, cc_addresses, date, flags, body_plain, body_html,
    has_attachments, size_bytes, list_unsubscribe, list_unsubscribe_post,
    smime_status, smime_signer, smime_email, smime_issuer, smime_detail,
    smime_certs, smime_fingerprint,
    reply_to, auth_spf, auth_dkim, auth_dmarc, auth_spf_domain, auth_dkim_domain,
    charset_guess, body_complete
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := ex.ExecContext(ctx, query,
		m.AccountID, m.FolderID, m.UID, remoteID, m.MessageID, m.Subject, m.FromAddress,
		m.FromName, m.ToAddresses, m.CcAddresses, formatTime(m.Date), uint8(m.Flags),
		m.BodyPlain, m.BodyHTML, boolToInt(m.HasAttachments), m.SizeBytes,
		m.ListUnsubscribe, boolToInt(m.ListUnsubscribePost),
		m.SMIME.Status, m.SMIME.Signer, m.SMIME.Email, m.SMIME.Issuer, m.SMIME.Detail,
		orEmptyBlob(m.SMIME.Certs), m.SMIME.Fingerprint,
		m.ReplyTo, m.Auth.SPF, m.Auth.DKIM, m.Auth.DMARC, m.Auth.SPFDomain, m.Auth.DKIMDomain,
		m.CharsetGuess, boolToInt(m.BodyComplete))
	if err != nil {
		return 0, fmt.Errorf("storage: insert message uid %d: %w", m.UID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: message insert id: %w", err)
	}
	m.ID = id
	m.RemoteID = remoteID
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

// CountBodyComplete returns how many of a folder's stored rows have a body.
func (d *DB) CountBodyComplete(ctx context.Context, folderID int64) (int, error) {
	var n int
	if err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE folder_id = ? AND body_complete != 0`, folderID).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count bodies for folder %d: %w", folderID, err)
	}
	return n, nil
}

// MessageIDByRemoteID returns the local id of the message stored under remoteID
// in a folder, or ErrMessageNotFound.
func (d *DB) MessageIDByRemoteID(ctx context.Context, folderID int64, remoteID string) (int64, error) {
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT id FROM messages WHERE folder_id = ? AND remote_id = ?`, folderID, remoteID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrMessageNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("storage: lookup message %q: %w", remoteID, err)
	}
	return id, nil
}

// RemoteIDsNeedingBodyNewest returns the remote ids of a folder's stored rows
// that still lack a body (body_complete = 0), newest first. limit > 0 first
// takes the folder's limit newest rows and then keeps the incomplete ones, so
// bodies stay within the X newest messages and do not creep older on each
// call. limit <= 0 returns every incomplete row. Once a folder's stubs exist,
// this is the source of its body-fetch targets.
func (d *DB) RemoteIDsNeedingBodyNewest(ctx context.Context, folderID int64, limit int) ([]string, error) {
	query, args := remoteIDsNeedingBodyNewestQuery(folderID, limit)
	out, err := d.queryAll(ctx, func(r *sql.Rows) (string, error) {
		var id string
		if err := r.Scan(&id); err != nil {
			return "", fmt.Errorf("storage: scan id needing body: %w", err)
		}
		return id, nil
	}, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: ids needing body for folder %d: %w", folderID, err)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

func remoteIDsNeedingBodyNewestQuery(folderID int64, limit int) (string, []any) {
	const order = ` ORDER BY date DESC, uid DESC`
	if limit > 0 {
		return `SELECT remote_id FROM (SELECT remote_id, body_complete, date, uid FROM messages WHERE folder_id = ?` +
			order + ` LIMIT ?) WHERE body_complete = 0` + order, []any{folderID, limit}
	}
	return `SELECT remote_id FROM messages WHERE folder_id = ? AND body_complete = 0` + order, []any{folderID}
}

// RemoteIDsNeedingBody returns remoteIDs that still need a body, in the same
// order. A missing row needs a body. A row with body_complete set does not.
// That is the same predicate as MessageNeedsBody. An empty remoteIDs returns
// nil. When every id is already complete the result is a non-nil empty slice.
func (d *DB) RemoteIDsNeedingBody(ctx context.Context, folderID int64, remoteIDs []string) ([]string, error) {
	if len(remoteIDs) == 0 {
		return nil, nil
	}
	complete := make(map[string]struct{})
	const chunkSize = 80
	for i := 0; i < len(remoteIDs); i += chunkSize {
		j := min(i+chunkSize, len(remoteIDs))
		chunk := remoteIDs[i:j]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, folderID)
		placeholders := make([]string, len(chunk))
		for k, id := range chunk {
			placeholders[k] = "?"
			args = append(args, id)
		}
		query := `SELECT remote_id FROM messages WHERE folder_id = ? AND body_complete != 0 AND remote_id IN (` + strings.Join(placeholders, ",") + `)`
		completeIDs, err := d.queryAll(ctx, func(r *sql.Rows) (string, error) {
			var id string
			if err := r.Scan(&id); err != nil {
				return "", fmt.Errorf("storage: scan body-complete id: %w", err)
			}
			return id, nil
		}, query, args...)
		if err != nil {
			return nil, fmt.Errorf("storage: body-complete ids for folder %d: %w", folderID, err)
		}
		for _, id := range completeIDs {
			complete[id] = struct{}{}
		}
	}
	need := make([]string, 0, len(remoteIDs))
	for _, id := range remoteIDs {
		if _, ok := complete[id]; ok {
			continue
		}
		need = append(need, id)
	}
	return need, nil
}

// MessageDatesByRemoteID returns the stored date of each listed remote id in a
// folder. Ids with no row are left out. Only the date column is read, so it
// stays cheap for a folder with tens of thousands of messages.
func (d *DB) MessageDatesByRemoteID(ctx context.Context, folderID int64, remoteIDs []string) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(remoteIDs))
	const chunkSize = 80
	for i := 0; i < len(remoteIDs); i += chunkSize {
		chunk := remoteIDs[i:min(i+chunkSize, len(remoteIDs))]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, folderID)
		placeholders := make([]string, len(chunk))
		for k, id := range chunk {
			placeholders[k] = "?"
			args = append(args, id)
		}
		rows, err := d.sql.QueryContext(ctx,
			`SELECT remote_id, date FROM messages WHERE folder_id = ? AND remote_id IN (`+strings.Join(placeholders, ",")+`)`,
			args...)
		if err != nil {
			return nil, fmt.Errorf("storage: message dates for folder %d: %w", folderID, err)
		}
		for rows.Next() {
			var id, raw string
			if err := rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return nil, fmt.Errorf("storage: scan message date: %w", err)
			}
			at, err := parseTime(raw)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = at
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: iterate message dates: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

// MessageNeedsBody reports whether id still needs a full body fetch. It is
// true when body_complete is 0. A missing id returns ErrMessageNotFound.
func (d *DB) MessageNeedsBody(ctx context.Context, id int64) (bool, error) {
	var complete int
	err := d.sql.QueryRowContext(ctx, `SELECT body_complete FROM messages WHERE id = ?`, id).Scan(&complete)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrMessageNotFound
	}
	if err != nil {
		return false, fmt.Errorf("storage: body complete for message %d: %w", id, err)
	}
	return complete == 0, nil
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
SELECT id, account_id, folder_id, uid, remote_id, message_id, subject, from_address,
       from_name, to_addresses, cc_addresses, date, flags, body_plain,
       body_html, has_attachments, size_bytes, flag_color, snooze_until,
       snooze_hidden, offline, list_unsubscribe, list_unsubscribe_post,
       smime_status, smime_signer, smime_email, smime_issuer, smime_detail,
       smime_fingerprint, reply_to, auth_spf, auth_dkim, auth_dmarc, auth_spf_domain,
       auth_dkim_domain, charset_guess, body_complete`

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
		bodyComplete int
	)
	if err := row.Scan(&m.ID, &m.AccountID, &m.FolderID, &m.UID, &m.RemoteID, &m.MessageID,
		&m.Subject, &m.FromAddress, &m.FromName, &m.ToAddresses, &m.CcAddresses,
		&date, &flags, &m.BodyPlain, &m.BodyHTML, &hasAtt, &m.SizeBytes,
		&m.FlagColor, &m.SnoozeUntil, &snoozeHidden, &offline,
		&m.ListUnsubscribe, &unsubPost,
		&m.SMIME.Status, &m.SMIME.Signer, &m.SMIME.Email, &m.SMIME.Issuer,
		&m.SMIME.Detail, &m.SMIME.Fingerprint, &m.ReplyTo, &m.Auth.SPF, &m.Auth.DKIM, &m.Auth.DMARC,
		&m.Auth.SPFDomain, &m.Auth.DKIMDomain, &m.CharsetGuess, &bodyComplete); err != nil {
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
	m.BodyComplete = bodyComplete != 0
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
