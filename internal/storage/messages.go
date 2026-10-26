package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrMessageNotFound is returned when a message id has no row.
var ErrMessageNotFound = errors.New("storage: message not found")

// ErrMessageBodyComplete is returned when a body fill finds an existing row
// whose body is already complete. The row, its flags and its
// attachment files are left unchanged.
var ErrMessageBodyComplete = errors.New("storage: message body already complete")

// ErrBodyTargetChanged is returned when a fetched body no longer belongs
// where it was fetched for: the folder's generation (UIDVALIDITY) is not the
// one the body came from, or its remote id now names a different row. Nothing
// is written. It is a dropped body, not a store failure.
var ErrBodyTargetChanged = errors.New("storage: body target changed while it was fetched")

// BodyTarget pins a body write to what the fetch was started for. Both are
// checked in the transaction that writes the body.
type BodyTarget struct {
	// UIDValidity is the folder generation the body was fetched under. When
	// it is not zero the write is refused unless the folder still stores it.
	UIDValidity uint32
	// MessageID is the row the body was fetched for. When it is not zero the
	// body fills only that row: a different row holding the remote id is
	// refused, and with no row nothing is inserted.
	MessageID int64
}

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
	BodyComplete bool
	// SearchState is what the search index held for the row when it was read.
	// Reads fill it; inserts and updates ignore it (see RecordSearchState).
	SearchState SearchState
	// TextRev is the row's indexed-text revision when it was read. Every
	// write that changes indexed text bumps it, so an index pass records its
	// result only for rows whose text it actually indexed (see IndexRead).
	TextRev        int64
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

// SMIMESignature is a stored verification verdict. Status is empty for
// unsigned mail, otherwise valid, untrusted or invalid; Detail explains
// anything that is not valid, in a sentence fit to show the reader.
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
	if err := checkListMeta(m); err != nil {
		return 0, err
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storage: begin list meta upsert: %w", err)
	}
	defer tx.Rollback()
	id, err := upsertListMeta(ctx, tx, m)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storage: commit list meta %q: %w", m.RemoteID, err)
	}
	return id, nil
}

// ListMetaBatch is what one UpsertMessageListMetaBatch transaction did.
type ListMetaBatch struct {
	// Done is how many of the messages, from the first, the transaction got
	// to: all of them, or fewer when the budget ran out or one failed. The
	// rest were not looked at.
	Done int
	// IDs holds the row id of every message done. When Err is set the last
	// one failed, its id is 0, and Err says why.
	IDs []int64
	Err error
	// Held is how long the transaction held the write lock, from BEGIN
	// returning to COMMIT returning.
	Held time.Duration
}

// UpsertMessageListMetaBatch is UpsertMessageListMeta for many messages in
// one transaction, in order, each under its own savepoint. A message that
// fails is undone alone and ends the transaction there: the ones before it
// commit, and the caller decides whether to send the rest. It also stops
// taking messages once budget has passed (after at least one), so a slow
// disk cannot stretch the write lock past it. Everything the rows need must
// be ready before the call: the transaction does no other work. An error
// means the transaction itself failed and nothing in it was stored.
func (d *DB) UpsertMessageListMetaBatch(ctx context.Context, ms []Message, budget time.Duration) (ListMetaBatch, error) {
	var out ListMetaBatch
	if len(ms) == 0 {
		return out, nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("storage: begin list meta batch: %w", err)
	}
	defer tx.Rollback()
	began := time.Now()
	out.IDs = make([]int64, 0, len(ms))
	for i := range ms {
		if i > 0 && time.Since(began) >= budget {
			break
		}
		id, err := upsertListMetaSavepoint(ctx, tx, &ms[i])
		out.IDs = append(out.IDs, id)
		if err != nil {
			out.Err = err
			break
		}
	}
	if err := tx.Commit(); err != nil {
		return ListMetaBatch{}, fmt.Errorf("storage: commit list meta batch: %w", err)
	}
	out.Held = time.Since(began)
	out.Done = len(out.IDs)
	return out, nil
}

// upsertListMetaSavepoint runs upsertListMeta under a savepoint, so a failed
// message undoes only its own writes and the transaction carries on.
func upsertListMetaSavepoint(ctx context.Context, tx *sql.Tx, m *Message) (int64, error) {
	if err := checkListMeta(m); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT list_meta`); err != nil {
		return 0, fmt.Errorf("storage: savepoint list meta %q: %w", m.RemoteID, err)
	}
	id, err := upsertListMeta(ctx, tx, m)
	if err != nil {
		if _, rerr := tx.ExecContext(ctx, `ROLLBACK TO list_meta`); rerr != nil {
			return 0, errors.Join(err, fmt.Errorf("storage: roll back list meta %q: %w", m.RemoteID, rerr))
		}
	}
	if _, rerr := tx.ExecContext(ctx, `RELEASE list_meta`); rerr != nil {
		return 0, errors.Join(err, fmt.Errorf("storage: release list meta %q: %w", m.RemoteID, rerr))
	}
	return id, err
}

// checkListMeta settles m's remote id, which a list row is keyed by: a uid
// stands in for a missing one.
func checkListMeta(m *Message) error {
	if m.RemoteID == "" && m.UID != 0 {
		m.RemoteID = strconv.FormatUint(uint64(m.UID), 10)
	}
	if m.RemoteID == "" {
		return fmt.Errorf("storage: upsert list meta requires remote_id")
	}
	return nil
}

// upsertListMeta is the lookup and write of UpsertMessageListMeta inside tx.
// m.RemoteID must be set (checkListMeta).
func upsertListMeta(ctx context.Context, tx *sql.Tx, m *Message) (int64, error) {
	remoteID := m.RemoteID
	var (
		id        int64
		bodyPlain string
		bodyHTML  string
		old       Message
		oldDate   string
	)
	err := tx.QueryRowContext(ctx,
		`SELECT id, body_plain, body_html, subject, from_address, from_name, to_addresses, date
		   FROM messages WHERE folder_id = ? AND remote_id = ?`,
		m.FolderID, remoteID,
	).Scan(&id, &bodyPlain, &bodyHTML, &old.Subject, &old.FromAddress, &old.FromName, &old.ToAddresses, &oldDate)
	if errors.Is(err, sql.ErrNoRows) {
		stub := *m
		stub.RemoteID = remoteID
		stub.BodyComplete = false
		newID, err := insertMessage(ctx, tx, &stub)
		if err != nil {
			return 0, err
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
	date := formatTime(m.Date)
	// A refresh that changes indexed text bumps text_rev, so an index pass
	// holding the old text does not record it as current, and marks the
	// row's index entry stale (listMetaStale), so the next pass indexes it
	// again: a complete row drops to headers, a stub to not indexed. Only
	// the text the index holds counts, compared as it is indexed, so a
	// refresh that changes nothing searchable leaves the index alone.
	textChanged := m.Subject != old.Subject ||
		indexedFrom(m.FromName, m.FromAddress) != indexedFrom(old.FromName, old.FromAddress) ||
		m.ToAddresses != old.ToAddresses ||
		!sameStoredDate(date, oldDate) || newPlain != bodyPlain
	_, err = tx.ExecContext(ctx, `
UPDATE messages SET
  subject = ?, from_address = ?, from_name = ?, to_addresses = ?,
  date = ?, flags = CASE WHEN pending_flags = 0 THEN ? ELSE flags END,
  size_bytes = ?, has_attachments = ?,
  body_plain = ?, uid = CASE WHEN uid = 0 AND ? != 0 THEN ? ELSE uid END,
  text_rev = text_rev + ?,
  search_state = CASE WHEN ? = 0 THEN search_state ELSE `+listMetaStale+` END
WHERE id = ?`,
		m.Subject, m.FromAddress, m.FromName, m.ToAddresses,
		date, uint8(m.Flags), m.SizeBytes, boolToInt(m.HasAttachments),
		newPlain, m.UID, m.UID, boolToInt(textChanged), boolToInt(textChanged), id,
	)
	if err != nil {
		return 0, fmt.Errorf("storage: update list meta %d: %w", id, err)
	}
	m.ID = id
	return id, nil
}

// InsertMessageWithAttachments inserts a message and its attachments
// atomically. When (folder_id, remote_id) already exists as an incomplete
// stub (body_complete = 0), the row is filled in place as FillMessageBody
// does. A row that is already body-complete is left unchanged and
// ErrMessageBodyComplete is returned, so import and download cannot wipe a
// stored message. A stub deleted while it was being filled returns
// ErrMessageNotFound rather than coming back as a new row. A stub fill keeps
// the stored mask when Flags == 0 (the caller never loaded flags) or when the
// row has a local flag change not pushed yet, so neither clears \Seen or
// \Flagged the user set. A zero Date
// (no Date header) keeps the stub's date. Attachment files
// already on disk are staged aside until the fill commits; a rollback puts
// them back. New files written for the fill are removed if the transaction
// does not commit.
// It checks no generation: use InsertFetchedMessage for mail fetched from a
// server folder.
func (d *DB) InsertMessageWithAttachments(ctx context.Context, m *Message, atts []IncomingAttachment) (int64, error) {
	return d.InsertFetchedMessage(ctx, BodyTarget{}, m, atts)
}

// InsertFetchedMessage is InsertMessageWithAttachments for a message fetched
// under want: a fill or insert into a folder whose generation is no longer
// want.UIDValidity, or a fill of a row other than want.MessageID, returns
// ErrBodyTargetChanged and writes nothing. With want.MessageID set it never
// inserts; a missing row is ErrMessageNotFound.
func (d *DB) InsertFetchedMessage(ctx context.Context, want BodyTarget, m *Message, atts []IncomingAttachment) (int64, error) {
	existingID, complete, err := d.bodyFillTarget(ctx, m)
	if err != nil && !errors.Is(err, ErrMessageNotFound) {
		return 0, err
	}
	if existingID != 0 {
		if err := want.checkRow(existingID); err != nil {
			return 0, err
		}
		if complete {
			return 0, ErrMessageBodyComplete
		}
		return d.replaceMessageBody(ctx, want, existingID, m, atts)
	}
	if want.MessageID != 0 {
		return 0, ErrMessageNotFound
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storage: begin message insert: %w", err)
	}
	defer tx.Rollback()
	if err := want.checkGeneration(ctx, tx, m.FolderID); err != nil {
		return 0, err
	}

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

// FillMessageBody writes a body fetched under want into the existing
// incomplete row for (folder_id, remote_id) and never inserts one. A missing
// row, including one deleted while the body was in flight (the message was
// moved or archived), returns ErrMessageNotFound; a row whose body is already
// complete returns ErrMessageBodyComplete; a folder reset to another
// generation, or a row other than want.MessageID, returns
// ErrBodyTargetChanged. All three leave the database unchanged. Flags, date,
// sender name and attachment files are handled as in
// InsertMessageWithAttachments.
func (d *DB) FillMessageBody(ctx context.Context, want BodyTarget, m *Message, atts []IncomingAttachment) (int64, error) {
	id, complete, err := d.bodyFillTarget(ctx, m)
	if err != nil {
		return 0, err
	}
	if err := want.checkRow(id); err != nil {
		return 0, err
	}
	if complete {
		return 0, ErrMessageBodyComplete
	}
	return d.replaceMessageBody(ctx, want, id, m, atts)
}

// checkRow refuses a row other than the one the body was fetched for.
func (want BodyTarget) checkRow(id int64) error {
	if want.MessageID != 0 && id != want.MessageID {
		return fmt.Errorf("%w: remote id now names row %d, the body is for row %d", ErrBodyTargetChanged, id, want.MessageID)
	}
	return nil
}

// checkGeneration refuses a folder that no longer stores the generation the
// body was fetched under. It runs in the write transaction, so a reset
// cannot land between the check and the write.
func (want BodyTarget) checkGeneration(ctx context.Context, tx *sql.Tx, folderID int64) error {
	if want.UIDValidity == 0 {
		return nil
	}
	var got uint32
	err := tx.QueryRowContext(ctx, `SELECT uid_validity FROM folders WHERE id = ?`, folderID).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: folder %d is gone", ErrBodyTargetChanged, folderID)
	}
	if err != nil {
		return fmt.Errorf("storage: read generation of folder %d: %w", folderID, err)
	}
	if got != want.UIDValidity {
		return fmt.Errorf("%w: folder %d is at generation %d, the body is from %d", ErrBodyTargetChanged, folderID, got, want.UIDValidity)
	}
	return nil
}

// bodyFillTarget looks up the row a body fill for m would write, or returns
// ErrMessageNotFound. replaceMessageBody checks the row again inside its
// transaction.
func (d *DB) bodyFillTarget(ctx context.Context, m *Message) (id int64, complete bool, err error) {
	remoteID := m.RemoteID
	if remoteID == "" && m.UID != 0 {
		remoteID = strconv.FormatUint(uint64(m.UID), 10)
	}
	if remoteID == "" {
		return 0, false, ErrMessageNotFound
	}
	var bodyComplete int
	err = d.sql.QueryRowContext(ctx,
		`SELECT id, body_complete FROM messages WHERE folder_id = ? AND remote_id = ?`,
		m.FolderID, remoteID,
	).Scan(&id, &bodyComplete)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, ErrMessageNotFound
	}
	if err != nil {
		return 0, false, fmt.Errorf("storage: lookup message for body fill: %w", err)
	}
	if d.bodyFillLookupHook != nil {
		d.bodyFillLookupHook()
	}
	return id, bodyComplete != 0, nil
}

func (d *DB) replaceMessageBody(ctx context.Context, want BodyTarget, id int64, m *Message, atts []IncomingAttachment) (int64, error) {
	remoteID := m.RemoteID
	if remoteID == "" && m.UID != 0 {
		remoteID = strconv.FormatUint(uint64(m.UID), 10)
	}

	// flags is the mask written into the row. Zero from the caller means the
	// flags were not loaded, so the stored mask is kept.
	var flags Flag
	var stored uint8
	var pending int
	check := func(tx *sql.Tx) error {
		// The row may have been deleted, replaced or filled, or the folder
		// reset to another generation, since the caller looked it up. None
		// may be overwritten, and no file is touched until this holds under
		// the write lock.
		if err := want.checkGeneration(ctx, tx, m.FolderID); err != nil {
			return err
		}
		var complete int
		err := tx.QueryRowContext(ctx, `SELECT flags, pending_flags, body_complete FROM messages WHERE id = ? AND folder_id = ? AND remote_id = ?`,
			id, m.FolderID, remoteID).Scan(&stored, &pending, &complete)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMessageNotFound
		}
		if err != nil {
			return fmt.Errorf("storage: read flags for message %d: %w", id, err)
		}
		if complete != 0 {
			return ErrMessageBodyComplete
		}
		return nil
	}
	err := d.commitStagedReplace(ctx, m.AccountID, id, check, func(tx *sql.Tx) ([]string, error) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM attachments WHERE message_id = ?`, id); err != nil {
			return nil, fmt.Errorf("storage: clear attachments for %d: %w", id, err)
		}

		// a local flag change not pushed yet outranks the server's flags the
		// fill carries; the push still needs it.
		flags = m.Flags
		if flags == 0 || pending != 0 {
			flags = Flag(stored)
		}

		hasAttachments := len(atts) > 0
		// a From header without a display name keeps the name the list stub
		// already got from the envelope.
		res, err := tx.ExecContext(ctx, `
UPDATE messages SET
  uid = CASE WHEN ? != 0 THEN ? ELSE uid END,
  message_id = ?, subject = ?, from_address = ?, from_name = CASE WHEN ? != '' THEN ? ELSE from_name END,
  to_addresses = ?, cc_addresses = ?, date = CASE WHEN ? != '' THEN ? ELSE date END, flags = ?,
  body_plain = ?, body_html = ?, has_attachments = ?, size_bytes = ?,
  list_unsubscribe = ?, list_unsubscribe_post = ?,
  smime_status = ?, smime_signer = ?, smime_email = ?, smime_issuer = ?, smime_detail = ?,
  smime_certs = ?, smime_fingerprint = ?,
  reply_to = ?, auth_spf = ?, auth_dkim = ?, auth_dmarc = ?, auth_spf_domain = ?, auth_dkim_domain = ?,
  charset_guess = ?, body_complete = 1, `+textRewritten+`
WHERE id = ? AND body_complete = 0`,
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
		if n, err := res.RowsAffected(); err != nil {
			return nil, fmt.Errorf("storage: update message body %d: %w", id, err)
		} else if n != 1 {
			return nil, ErrMessageBodyComplete
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
// that still lack a body (body_complete = 0), newest first, at most limit of
// them (limit <= 0: all). Rows the user deleted locally are left out, and so
// are rows below floorUID, a numeric sync floor (0: none); rows without a uid
// always count. These are the stubs whose body fetch failed or was cut short.
func (d *DB) RemoteIDsNeedingBodyNewest(ctx context.Context, folderID int64, floorUID uint32, limit int) ([]string, error) {
	query, args := remoteIDsNeedingBodyNewestQuery(folderID, floorUID, limit)
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

func remoteIDsNeedingBodyNewestQuery(folderID int64, floorUID uint32, limit int) (string, []any) {
	return `SELECT remote_id FROM messages
WHERE folder_id = ? AND body_complete = 0 AND pending_delete = 0 AND (uid = 0 OR uid >= ?)
ORDER BY date DESC, uid DESC LIMIT ?`, []any{folderID, floorUID, normalizeLimit(limit)}
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

// MessageIDsByRemoteID returns the row id of each listed remote id in a
// folder. Ids with no row are left out.
func (d *DB) MessageIDsByRemoteID(ctx context.Context, folderID int64, remoteIDs []string) (map[string]int64, error) {
	type row struct {
		remote string
		id     int64
	}
	out := make(map[string]int64, len(remoteIDs))
	const chunkSize = 80
	for i := 0; i < len(remoteIDs); i += chunkSize {
		chunk := remoteIDs[i:min(i+chunkSize, len(remoteIDs))]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, folderID)
		for _, id := range chunk {
			args = append(args, id)
		}
		query := `SELECT remote_id, id FROM messages WHERE folder_id = ? AND remote_id IN (` +
			strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",") + `)`
		rows, err := d.queryAll(ctx, func(r *sql.Rows) (row, error) {
			var x row
			if err := r.Scan(&x.remote, &x.id); err != nil {
				return row{}, fmt.Errorf("storage: scan message id: %w", err)
			}
			return x, nil
		}, query, args...)
		if err != nil {
			return nil, fmt.Errorf("storage: message ids for folder %d: %w", folderID, err)
		}
		for _, x := range rows {
			out[x.remote] = x.id
		}
	}
	return out, nil
}

// MessageSizesByRemoteID returns the stored size_bytes (RFC822.SIZE for a
// stub) of each listed remote id in a folder. Ids with no row are left out.
func (d *DB) MessageSizesByRemoteID(ctx context.Context, folderID int64, remoteIDs []string) (map[string]int64, error) {
	type sized struct {
		id   string
		size int64
	}
	out := make(map[string]int64, len(remoteIDs))
	const chunkSize = 80
	for i := 0; i < len(remoteIDs); i += chunkSize {
		chunk := remoteIDs[i:min(i+chunkSize, len(remoteIDs))]
		args := make([]any, 0, len(chunk)+1)
		args = append(args, folderID)
		for _, id := range chunk {
			args = append(args, id)
		}
		query := `SELECT remote_id, size_bytes FROM messages WHERE folder_id = ? AND remote_id IN (` +
			strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",") + `)`
		rows, err := d.queryAll(ctx, func(r *sql.Rows) (sized, error) {
			var s sized
			if err := r.Scan(&s.id, &s.size); err != nil {
				return sized{}, fmt.Errorf("storage: scan message size: %w", err)
			}
			return s, nil
		}, query, args...)
		if err != nil {
			return nil, fmt.Errorf("storage: message sizes for folder %d: %w", folderID, err)
		}
		for _, s := range rows {
			out[s.id] = s.size
		}
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

// SearchState is what the search index holds for a message.
type SearchState int

const (
	// SearchNotIndexed: the row is not in the index.
	SearchNotIndexed SearchState = 0
	// SearchHeaders: indexed as a stub, by its headers and preview only.
	SearchHeaders SearchState = 1
	// SearchFull: indexed with its full body.
	SearchFull SearchState = 2
)

// textRewritten is the SET clause of an update that rewrites a row's indexed
// text (a body fill, a charset repair). It drops a full index state back to
// headers, so the body pass indexes the row again, and bumps text_rev, so a
// pass that read the old text cannot record its result over the new one.
// The state alone cannot tell: a row read at 0 or 1 keeps that value.
const textRewritten = `search_state = min(search_state, 1), text_rev = text_rev + 1`

// listMetaStale is the search_state of a row whose list metadata (subject,
// sender, recipients, date, preview) changed: a complete row drops to
// headers, so the body pass indexes it again; a stub drops to not indexed,
// so the stale-stub pass (ListMessagesForReindex) indexes its new headers,
// even behind the id watermark.
const listMetaStale = `CASE WHEN body_complete = 1 THEN min(search_state, 1) ELSE 0 END`

// indexedFrom is the sender text the search index holds for a row: name and
// address in one field.
func indexedFrom(name, address string) string {
	return strings.TrimSpace(name + " " + address)
}

// sameStoredDate reports whether two stored dates are the same instant, so a
// row stored in another form (an older build's offset) is not taken for a
// changed date. Text that does not parse is compared as is.
func sameStoredDate(a, b string) bool {
	if a == b {
		return true
	}
	ta, errA := parseTime(a)
	tb, errB := parseTime(b)
	if errA != nil || errB != nil {
		return false
	}
	return ta.Equal(tb)
}

// reindexIDs selects the ids ListMessagesForReindex returns: one batch, in
// id order. Each arm walks its partial index (idx_messages_search_pending,
// idx_messages_search_stale_stub) in id order and stops at the limit, so a
// batch reads at most two limits of index entries, whatever the backlog
// between afterID and the watermark; the merge keeps the first limit ids of
// both. An unbounded IN over the arms would collect the whole backlog for
// every batch.
const reindexIDs = `
  SELECT id FROM (SELECT id FROM messages WHERE body_complete = 1 AND search_state < 2 AND id > ?1 AND id <= ?2 ORDER BY id LIMIT ?3)
  UNION ALL
  SELECT id FROM (SELECT id FROM messages WHERE body_complete = 0 AND search_state = 0 AND id > ?1 AND id <= ?2 ORDER BY id LIMIT ?3)
  ORDER BY id LIMIT ?3`

// listForReindexQuery fetches the rows of one reindex batch.
const listForReindexQuery = selectMessageColumns + `
FROM messages WHERE id IN (` + reindexIDs + `) ORDER BY id ASC`

// ListMessagesForReindex returns rows with id at most upToID whose search
// entry is behind their text: complete rows whose body is not indexed yet
// (bodies that landed after their stub was indexed, a list refresh or repair
// that changed the text, or an index write that never happened), and stubs
// not indexed (a list refresh changed their headers, or a pass that read them
// lost to a rewrite). Rows past the watermark are the new-row pass's.
// Ordered by id, up to limit; pass the last id seen as afterID to page.
func (d *DB) ListMessagesForReindex(ctx context.Context, afterID, upToID int64, limit int) ([]Message, error) {
	rows, err := d.sql.QueryContext(ctx, listForReindexQuery, afterID, upToID, normalizeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("storage: list messages for reindex: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan message for reindex: %w", err)
		}
		messages = append(messages, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate reindex messages: %w", err)
	}
	return messages, nil
}

// IndexRead is what an index pass read of a row: its search state and the
// revision of the text it indexed.
type IndexRead struct {
	ID      int64
	State   SearchState
	TextRev int64
}

// IndexRead returns what an index pass reading m has in hand.
func (m Message) IndexRead() IndexRead {
	return IndexRead{ID: m.ID, State: m.SearchState, TextRev: m.TextRev}
}

// RecordSearchState records what the search index now holds for the rows in
// read, as the index pass read them. Call it after the index write
// succeeded. A row whose text revision or state changed since the read keeps
// its state: a fill or charset repair in between rewrote the text the pass
// indexed, and the next pass indexes it again.
func (d *DB) RecordSearchState(ctx context.Context, read []IndexRead, state SearchState) error {
	type key struct {
		state SearchState
		rev   int64
	}
	byRead := make(map[key][]int64)
	for _, r := range read {
		k := key{r.State, r.TextRev}
		byRead[k] = append(byRead[k], r.ID)
	}
	for k, ids := range byRead {
		for chunk := range slices.Chunk(ids, 500) {
			marks, args := inClause(chunk)
			if _, err := d.sql.ExecContext(ctx,
				`UPDATE messages SET search_state = ? WHERE search_state = ? AND text_rev = ? AND id IN (`+marks+`)`,
				append([]any{int(state), int(k.state), k.rev}, args...)...); err != nil {
				return fmt.Errorf("storage: record search state: %w", err)
			}
		}
	}
	return nil
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
       auth_dkim_domain, charset_guess, body_complete, search_state, text_rev`

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
		&m.Auth.SPFDomain, &m.Auth.DKIMDomain, &m.CharsetGuess, &bodyComplete, &m.SearchState, &m.TextRev); err != nil {
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
