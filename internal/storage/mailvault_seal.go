package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/peltonapp/Pelton/internal/mailvault"
)

// ErrSealedTampered is returned for a sealed message whose ciphertext does
// not authenticate: it was changed, truncated or moved from another row.
var ErrSealedTampered = errors.New("storage: sealed message does not authenticate")

// ErrVaultLocked is returned when sealed content is needed and its vault is
// locked or does not admit the caller.
var ErrVaultLocked = errors.New("storage: the mail vault is locked")

// sealedEnvelope is everything about a message that is sealed apart from its
// bodies: the headers the row would otherwise carry and the attachment
// metadata, in attachment row order.
type sealedEnvelope struct {
	MessageID           string             `json:"messageId"`
	Subject             string             `json:"subject"`
	FromAddress         string             `json:"from"`
	FromName            string             `json:"fromName"`
	ToAddresses         string             `json:"to"`
	CcAddresses         string             `json:"cc"`
	ReplyTo             string             `json:"replyTo"`
	ListUnsubscribe     string             `json:"listUnsubscribe"`
	ListUnsubscribePost bool               `json:"listUnsubscribePost"`
	ListID              string             `json:"listId"`
	SMIME               SMIMESignature     `json:"smime"`
	Auth                MessageAuth        `json:"auth"`
	CharsetGuess        string             `json:"charsetGuess"`
	InReplyTo           []string           `json:"inReplyTo"`
	References          []string           `json:"references"`
	MailKind            MailKind           `json:"mailKind"`
	Attachments         []sealedAttachment `json:"attachments"`
}

type sealedAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	ContentID   string `json:"contentId"`
	Size        int64  `json:"size"`
}

type sealedBody struct {
	Plain string `json:"plain"`
	HTML  string `json:"html"`
}

// vaultTarget is what sealing needs of a vault, none of it secret.
type vaultTarget struct {
	id   int64
	uid  []byte
	pub  []byte
	link []byte
}

func vaultTargetFor(ctx context.Context, ex execer, folderID int64) (*vaultTarget, error) {
	var t vaultTarget
	err := ex.QueryRowContext(ctx, `
SELECT v.id, v.uid, v.public_key, v.link_key
FROM mail_vault_folders f JOIN mail_vaults v ON v.id = f.vault_id
WHERE f.folder_id = ?`, folderID).Scan(&t.id, &t.uid, &t.pub, &t.link)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: vault of folder %d: %w", folderID, err)
	}
	return &t, nil
}

func vaultTargetByID(ctx context.Context, ex execer, vaultID int64) (*vaultTarget, error) {
	t := vaultTarget{id: vaultID}
	err := ex.QueryRowContext(ctx, `SELECT uid, public_key, link_key FROM mail_vaults WHERE id = ?`, vaultID).
		Scan(&t.uid, &t.pub, &t.link)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMailVaultNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: vault %d: %w", vaultID, err)
	}
	return &t, nil
}

func rowBinding(id int64, column string) []string {
	return []string{"messages", strconv.FormatInt(id, 10), column}
}

func attachmentBinding(messageID int64, index int) []string {
	return []string{"attachments", strconv.FormatInt(messageID, 10), strconv.Itoa(index)}
}

// sealedFilePrefix names the files that hold sealed attachment bytes. The
// index after it is the attachment's place in its message.
const sealedFilePrefix = "sealed-"

func sealedFileIndex(diskPath string) (int, bool) {
	base := path.Base(diskPath)
	if !strings.HasPrefix(base, sealedFilePrefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(base, sealedFilePrefix))
	return n, err == nil && n >= 0
}

func linkHashes(link []byte, ids []string) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if k := threadKey(id); k != "" {
			out = append(out, mailvault.LinkHash(link, k))
		}
	}
	return out
}

// sealedRowFrom is the row a sealed message keeps in messages: sync and list
// state, with every column that carries content emptied and the Message-IDs
// replaced by keyed hashes, so sealed mail threads only with sealed mail.
func sealedRowFrom(m *Message, t *vaultTarget) Message {
	row := Message{
		ID: m.ID, AccountID: m.AccountID, FolderID: m.FolderID, UID: m.UID,
		Date: m.Date, Flags: m.Flags, HasAttachments: m.HasAttachments, SizeBytes: m.SizeBytes,
		FlagColor: m.FlagColor, SnoozeUntil: m.SnoozeUntil, SnoozeHidden: m.SnoozeHidden,
		Offline: m.Offline, Pinned: m.Pinned, VaultID: t.id,
		InReplyTo:  linkHashes(t.link, m.InReplyTo),
		References: linkHashes(t.link, m.References),
	}
	if k := threadKey(m.MessageID); k != "" {
		row.MessageID = mailvault.LinkHash(t.link, k)
	}
	return row
}

func envelopeFrom(m *Message) sealedEnvelope {
	return sealedEnvelope{
		MessageID: m.MessageID, Subject: m.Subject, FromAddress: m.FromAddress, FromName: m.FromName,
		ToAddresses: m.ToAddresses, CcAddresses: m.CcAddresses, ReplyTo: m.ReplyTo,
		ListUnsubscribe: m.ListUnsubscribe, ListUnsubscribePost: m.ListUnsubscribePost, ListID: m.ListID,
		SMIME: m.SMIME, Auth: m.Auth, CharsetGuess: m.CharsetGuess,
		InReplyTo: m.InReplyTo, References: m.References, MailKind: mailKindFor(m),
	}
}

func (e sealedEnvelope) applyTo(m *Message) {
	m.MessageID, m.Subject, m.FromAddress, m.FromName = e.MessageID, e.Subject, e.FromAddress, e.FromName
	m.ToAddresses, m.CcAddresses, m.ReplyTo = e.ToAddresses, e.CcAddresses, e.ReplyTo
	m.ListUnsubscribe, m.ListUnsubscribePost, m.ListID = e.ListUnsubscribe, e.ListUnsubscribePost, e.ListID
	m.SMIME, m.Auth, m.CharsetGuess = e.SMIME, e.Auth, e.CharsetGuess
	m.InReplyTo, m.References, m.MailKind = e.InReplyTo, e.References, e.MailKind
}

func sealJSON(mk *mailvault.MessageKey, v any, binding []string) ([]byte, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("storage: encode sealed part: %w", err)
	}
	defer mailvault.Wipe(plain)
	return mk.Seal(plain, binding...)
}

func openJSON(mk *mailvault.MessageKey, box []byte, binding []string, v any) error {
	plain, err := mk.Open(box, binding...)
	if err != nil {
		return ErrSealedTampered
	}
	defer mailvault.Wipe(plain)
	if err := json.Unmarshal(plain, v); err != nil {
		return ErrSealedTampered
	}
	return nil
}

// writeSealedFile seals data for attachment index of a message and writes
// it, replacing a file a failed earlier attempt left behind.
func (d *DB) writeSealedFile(accountID, messageID int64, index int, mk *mailvault.MessageKey, data []byte) (string, error) {
	box, err := mk.Seal(data, attachmentBinding(messageID, index)...)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d.attachmentsDir, accountSegment(accountID), messageSegment(messageID))
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return "", fmt.Errorf("storage: create attachment dir: %w", err)
	}
	name := sealedFilePrefix + strconv.Itoa(index)
	if err := os.WriteFile(filepath.Join(dir, name), box, 0o600); err != nil {
		return "", fmt.Errorf("storage: write sealed attachment: %w", err)
	}
	return filepath.ToSlash(filepath.Join(accountSegment(accountID), messageSegment(messageID), name)), nil
}

// insertSealed stores a new message of a vaulted folder. Nothing of its
// content reaches the database or the disk in clear: the row is written
// emptied, the attachments are sealed before they are written, and the
// content goes into mail_vault_sealed. It needs only the vault's public key.
func (d *DB) insertSealed(ctx context.Context, tx *sql.Tx, m *Message, atts []IncomingAttachment, t *vaultTarget) (int64, []string, error) {
	mk, err := mailvault.NewMessageKey(t.pub, t.uid)
	if err != nil {
		return 0, nil, err
	}
	defer mk.Wipe()

	m.HasAttachments = len(atts) > 0
	row := sealedRowFrom(m, t)
	id, err := insertMessage(ctx, tx, &row)
	if err != nil {
		return 0, nil, err
	}
	env := envelopeFrom(m)
	var written []string
	for i, in := range atts {
		meta := sealedAttachment{Filename: sanitizeFilename(in.Filename), ContentType: in.ContentType,
			ContentID: in.ContentID, Size: in.Size}
		att := &Attachment{MessageID: id}
		if in.Content != nil {
			data, err := io.ReadAll(in.Content)
			if err != nil {
				return 0, written, fmt.Errorf("storage: read attachment: %w", err)
			}
			meta.Size = int64(len(data))
			rel, err := d.writeSealedFile(m.AccountID, id, i, mk, data)
			mailvault.Wipe(data)
			if err != nil {
				return 0, written, err
			}
			written = append(written, rel)
			att.DiskPath = rel
		}
		if err := insertAttachment(ctx, tx, att); err != nil {
			return 0, written, err
		}
		env.Attachments = append(env.Attachments, meta)
	}
	if err := insertSealedRow(ctx, tx, id, t.id, mk, env, sealedBody{Plain: m.BodyPlain, HTML: m.BodyHTML}, m.PGPSource); err != nil {
		return 0, written, err
	}
	m.ID, m.VaultID, m.ThreadID = id, t.id, row.ThreadID
	return id, written, nil
}

func insertSealedRow(ctx context.Context, ex execer, id, vaultID int64, mk *mailvault.MessageKey, env sealedEnvelope, body sealedBody, source []byte) error {
	envBox, err := sealJSON(mk, env, rowBinding(id, "envelope"))
	if err != nil {
		return err
	}
	bodyBox, err := sealJSON(mk, body, rowBinding(id, "body"))
	if err != nil {
		return err
	}
	var srcBox []byte
	if len(source) > 0 {
		if srcBox, err = mk.Seal(source, rowBinding(id, "source")...); err != nil {
			return err
		}
	}
	if _, err := ex.ExecContext(ctx, `
INSERT INTO mail_vault_sealed (message_id, vault_id, epk, envelope, body, source) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (message_id) DO UPDATE SET vault_id = excluded.vault_id, epk = excluded.epk,
    envelope = excluded.envelope, body = excluded.body, source = excluded.source`,
		id, vaultID, mk.EPK, envBox, bodyBox, srcBox); err != nil {
		return fmt.Errorf("storage: store sealed message %d: %w", id, err)
	}
	return nil
}

// openSealed fills sealed rows with their content. It returns the ids of the
// rows that did not open.
func (d *DB) openSealed(ctx context.Context, gate VaultGate, msgs []*Message) (map[int64]bool, error) {
	bad := map[int64]bool{}
	byID := make(map[int64]*Message, len(msgs))
	ids := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
		ids = append(ids, m.ID)
		bad[m.ID] = true
	}
	if gate == nil {
		return bad, nil
	}
	marks, args := inClause(ids)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT message_id, vault_id, epk, envelope, body FROM mail_vault_sealed WHERE message_id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: read sealed messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, vaultID    int64
			epk, env, body []byte
		)
		if err := rows.Scan(&id, &vaultID, &epk, &env, &body); err != nil {
			return nil, fmt.Errorf("storage: scan sealed message: %w", err)
		}
		m := byID[id]
		if m == nil || vaultID != m.VaultID {
			continue
		}
		mk, err := gate.MessageKey(vaultID, epk)
		if err != nil {
			continue
		}
		var e sealedEnvelope
		var b sealedBody
		if openJSON(mk, env, rowBinding(id, "envelope"), &e) != nil || openJSON(mk, body, rowBinding(id, "body"), &b) != nil {
			continue
		}
		e.applyTo(m)
		m.BodyPlain, m.BodyHTML = b.Plain, b.HTML
		delete(bad, id)
	}
	return bad, rows.Err()
}

// restoreSealedAttachment is RestoreAttachmentFile for a sealed message: the
// bytes fetched again are sealed before they are written.
func (d *DB) restoreSealedAttachment(ctx context.Context, accountID int64, att Attachment, content io.Reader, mk *mailvault.MessageKey) (*Attachment, error) {
	rows, err := attachmentRows(ctx, d.sql, att.MessageID)
	if err != nil {
		return nil, err
	}
	index := -1
	for i, r := range rows {
		if r.ID == att.ID {
			index = i
		}
	}
	if index < 0 {
		return nil, ErrMessageNotFound
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return nil, fmt.Errorf("storage: read attachment: %w", err)
	}
	defer mailvault.Wipe(data)
	rel, err := d.writeSealedFile(accountID, att.MessageID, index, mk, data)
	if err != nil {
		return nil, err
	}
	if _, err := d.sql.ExecContext(ctx, `UPDATE attachments SET disk_path = ? WHERE id = ?`, rel, att.ID); err != nil {
		d.removeAttachmentFiles([]string{rel})
		return nil, fmt.Errorf("storage: restore attachment %d: %w", att.ID, err)
	}
	out := att
	out.DiskPath = rel
	return &out, nil
}

// sealedState returns a message's folder and vault, for the reads that take
// only an id.
func (d *DB) sealedState(ctx context.Context, messageID int64) (folderID, vaultID int64, err error) {
	err = d.sql.QueryRowContext(ctx, `SELECT folder_id, vault_id FROM messages WHERE id = ?`, messageID).
		Scan(&folderID, &vaultID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrMessageNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("storage: message %d vault: %w", messageID, err)
	}
	return folderID, vaultID, nil
}

// MessageVault returns the vault a message is sealed to or whose folder it
// is in, 0 for none, locked or not.
func (d *DB) MessageVault(ctx context.Context, messageID int64) (int64, error) {
	folderID, vaultID, err := d.sealedState(ctx, messageID)
	if err != nil || vaultID != 0 {
		return vaultID, err
	}
	return d.FolderVault(ctx, folderID)
}

// sealedKey checks that ctx may see a message and returns its vault and, for
// a sealed message, its key and envelope. A message in clear returns a nil
// key.
func (d *DB) sealedKey(ctx context.Context, messageID int64) (*mailvault.MessageKey, *sealedEnvelope, []byte, error) {
	folderID, vaultID, err := d.sealedState(ctx, messageID)
	if err != nil {
		return nil, nil, nil, err
	}
	folders, gate, err := d.vaultFolders(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if folderHidden(folders, gate, vaultAudience(ctx), folderID) {
		return nil, nil, nil, ErrMessageNotFound
	}
	if vaultID == 0 {
		return nil, nil, nil, nil
	}
	if gate == nil {
		return nil, nil, nil, ErrVaultLocked
	}
	var epk, env, source []byte
	if err := d.sql.QueryRowContext(ctx,
		`SELECT epk, envelope, source FROM mail_vault_sealed WHERE message_id = ? AND vault_id = ?`, messageID, vaultID).
		Scan(&epk, &env, &source); err != nil {
		return nil, nil, nil, ErrSealedTampered
	}
	mk, err := gate.MessageKey(vaultID, epk)
	if err != nil {
		return nil, nil, nil, ErrVaultLocked
	}
	var e sealedEnvelope
	if err := openJSON(mk, env, rowBinding(messageID, "envelope"), &e); err != nil {
		return nil, nil, nil, err
	}
	return mk, &e, source, nil
}

// secureConn returns a connection with secure_delete on, so the cleartext a
// sealing step replaces is overwritten in the database file rather than left
// in free space. release turns it off again and returns the connection.
func (d *DB) secureConn(ctx context.Context) (*sql.Conn, func(), error) {
	conn, err := d.sql.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: vault connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA secure_delete = ON`); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("storage: secure delete: %w", err)
	}
	return conn, func() {
		_, _ = conn.ExecContext(context.Background(), `PRAGMA secure_delete = OFF`)
		conn.Close()
	}, nil
}

// SealPending seals up to limit messages of a vault's folders that are still
// stored in clear and returns how many it sealed; 0 means none are left. Each
// message is sealed in its own transaction, so a crash leaves every message
// either sealed or in clear, never neither, and the next call carries on. It
// needs only the vault's public key.
func (d *DB) SealPending(ctx context.Context, vaultID int64, limit int) (int, error) {
	conn, release, err := d.secureConn(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	d.wipePending(ctx, conn)
	t, err := vaultTargetByID(ctx, conn, vaultID)
	if err != nil {
		return 0, err
	}
	ids, err := queryIDs(ctx, conn, `
SELECT m.id FROM messages m JOIN mail_vault_folders f ON f.folder_id = m.folder_id
WHERE f.vault_id = ? AND m.vault_id = 0 ORDER BY m.id LIMIT ?`, vaultID, normalizeLimit(limit))
	if err != nil {
		return 0, err
	}
	done := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		if err := d.sealExisting(ctx, conn, t, id); err != nil {
			return done, err
		}
		done++
	}
	d.wipePending(ctx, conn)
	return done, nil
}

func queryIDs(ctx context.Context, ex execer, query string, args ...any) ([]int64, error) {
	rows, err := ex.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: vault ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan vault id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (d *DB) sealExisting(ctx context.Context, conn *sql.Conn, t *vaultTarget, id int64) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin seal: %w", err)
	}
	defer tx.Rollback()
	m, err := scanMessage(tx.QueryRowContext(ctx, selectMessageByID+` AND vault_id = 0`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: read message %d to seal: %w", id, err)
	}
	var source []byte
	if err := tx.QueryRowContext(ctx, `SELECT raw FROM message_pgp WHERE message_id = ?`, id).Scan(&source); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("storage: read pgp source %d: %w", id, err)
	}
	atts, err := attachmentRows(ctx, tx, id)
	if err != nil {
		return err
	}

	mk, err := mailvault.NewMessageKey(t.pub, t.uid)
	if err != nil {
		return err
	}
	defer mk.Wipe()
	env := envelopeFrom(m)
	for i, a := range atts {
		meta := sealedAttachment{Filename: a.Filename, ContentType: a.ContentType, ContentID: a.ContentID, Size: a.SizeBytes}
		newPath := ""
		if a.DiskPath != "" {
			full := filepath.Join(d.attachmentsDir, filepath.FromSlash(a.DiskPath))
			data, err := os.ReadFile(full)
			switch {
			case err == nil:
				newPath, err = d.writeSealedFile(m.AccountID, id, i, mk, data)
				mailvault.Wipe(data)
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO mail_vault_wipes (path) VALUES (?)`, a.DiskPath); err != nil {
					return fmt.Errorf("storage: queue attachment wipe: %w", err)
				}
			case !errors.Is(err, os.ErrNotExist):
				return fmt.Errorf("storage: read attachment to seal: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE attachments SET filename = '', content_type = '', content_id = '', size_bytes = 0, disk_path = ? WHERE id = ?`,
			newPath, a.ID); err != nil {
			return fmt.Errorf("storage: seal attachment row: %w", err)
		}
		env.Attachments = append(env.Attachments, meta)
	}

	row := sealedRowFrom(m, t)
	assigned, err := resolveThread(ctx, tx, threadInputFor(&row))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE messages SET message_id = ?, subject = '', from_address = '', from_name = '', to_addresses = '',
    cc_addresses = '', body_plain = '', body_html = '', list_unsubscribe = '', list_unsubscribe_post = 0,
    smime_status = '', smime_signer = '', smime_email = '', smime_issuer = '', smime_detail = '',
    smime_certs = X'', smime_fingerprint = '', reply_to = '', auth_spf = '', auth_dkim = '', auth_dmarc = '',
    auth_spf_domain = '', auth_dkim_domain = '', charset_guess = '', in_reply_to = ?, [references] = ?,
    thread_id = ?, thread_subject = '', thread_loose = 0, mail_kind = 0, list_id = '', vault_id = ?
WHERE id = ? AND vault_id = 0`,
		row.MessageID, encodeThreadingIDs(row.InReplyTo), encodeThreadingIDs(row.References),
		assigned.threadID, t.id, id); err != nil {
		return fmt.Errorf("storage: seal message row %d: %w", id, err)
	}
	if err := purgeDerived(ctx, tx, m); err != nil {
		return err
	}
	if err := insertSealedRow(ctx, tx, id, t.id, mk, env, sealedBody{Plain: m.BodyPlain, HTML: m.BodyHTML}, source); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit seal %d: %w", id, err)
	}
	return nil
}

// purgeDerived drops what other tables copied out of a message that is now
// sealed: its pgp source, the reply records and promises keyed by its
// Message-ID.
func purgeDerived(ctx context.Context, tx *sql.Tx, m *Message) error {
	type stmt struct {
		q    string
		args []any
	}
	stmts := []stmt{
		{`DELETE FROM message_pgp WHERE message_id = ?`, []any{m.ID}},
		{`DELETE FROM message_trust WHERE message_id = ?`, []any{m.ID}},
		{`DELETE FROM privacy_scans WHERE msg_key IN (?, ?)`, []any{PrivacyKey(*m), fmt.Sprintf("row:%d", m.ID)}},
		{`UPDATE unsubscribes SET still_message_id = 0 WHERE still_message_id = ?`, []any{m.ID}},
	}
	if key := threadKey(m.MessageID); key != "" {
		stmts = append(stmts,
			stmt{`DELETE FROM message_replies WHERE account_id = ? AND (original_message_id = ? OR reply_message_id = ?)`, []any{m.AccountID, key, key}},
			stmt{`DELETE FROM promises WHERE account_id = ? AND mail_id = ?`, []any{m.AccountID, key}},
			stmt{`DELETE FROM promise_scans WHERE account_id = ? AND mail_id = ?`, []any{m.AccountID, key}},
		)
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return fmt.Errorf("storage: purge derived data of %d: %w", m.ID, err)
		}
	}
	return nil
}

func attachmentRows(ctx context.Context, ex execer, messageID int64) ([]Attachment, error) {
	rows, err := ex.QueryContext(ctx, `
SELECT id, message_id, filename, content_type, size_bytes, content_id, disk_path
FROM attachments WHERE message_id = ? ORDER BY id`, messageID)
	if err != nil {
		return nil, fmt.Errorf("storage: list attachments for message %d: %w", messageID, err)
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType, &a.SizeBytes, &a.ContentID, &a.DiskPath); err != nil {
			return nil, fmt.Errorf("storage: scan attachment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// wipePending overwrites and removes the attachment files a committed
// sealing step left in clear. It is best effort: a file it cannot reach stays
// queued for the next call.
func (d *DB) wipePending(ctx context.Context, ex execer) {
	paths, err := queryStrings(ctx, ex, `SELECT path FROM mail_vault_wipes`)
	if err != nil {
		return
	}
	for _, rel := range paths {
		full := filepath.Join(d.attachmentsDir, filepath.FromSlash(rel))
		if err := WipeFile(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		_, _ = ex.ExecContext(ctx, `DELETE FROM mail_vault_wipes WHERE path = ?`, rel)
	}
}

func queryStrings(ctx context.Context, ex execer, query string, args ...any) ([]string, error) {
	rows, err := ex.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// WipeFile overwrites a file with zeros, flushes it and removes it. On an
// SSD or a copy-on-write or snapshotting filesystem the old blocks may
// survive regardless; this is the most a program can do from above the
// filesystem.
func WipeFile(full string) error {
	f, err := os.OpenFile(full, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil {
		zero := make([]byte, 64*1024)
		for left := info.Size(); left > 0 && err == nil; {
			n := min(left, int64(len(zero)))
			_, err = f.Write(zero[:n])
			left -= n
		}
		if err == nil {
			err = f.Sync()
		}
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Remove(full)
}

// UnsealPending turns up to limit sealed messages of a vault back into
// ordinary rows and returns how many it unsealed. The vault must be unlocked.
// Like SealPending, each message moves in one transaction.
func (d *DB) UnsealPending(ctx context.Context, vaultID int64, limit int) (int, error) {
	d.vault.mu.RLock()
	gate := d.vault.gate
	d.vault.mu.RUnlock()
	if gate == nil || !gate.Visible(vaultID, VaultAudienceUser) {
		return 0, ErrVaultLocked
	}
	ids, err := queryIDs(ctx, d.sql, `SELECT id FROM messages WHERE vault_id = ? ORDER BY id LIMIT ?`, vaultID, normalizeLimit(limit))
	if err != nil {
		return 0, err
	}
	done := 0
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		if err := d.unsealOne(ctx, gate, vaultID, id); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

func (d *DB) unsealOne(ctx context.Context, gate VaultGate, vaultID, id int64) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin unseal: %w", err)
	}
	defer tx.Rollback()
	m, err := scanMessage(tx.QueryRowContext(ctx, selectMessageByID+` AND vault_id = ?`, id, vaultID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: read message %d to unseal: %w", id, err)
	}
	var epk, envBox, bodyBox, srcBox []byte
	if err := tx.QueryRowContext(ctx, `SELECT epk, envelope, body, source FROM mail_vault_sealed WHERE message_id = ?`, id).
		Scan(&epk, &envBox, &bodyBox, &srcBox); err != nil {
		return fmt.Errorf("storage: read sealed %d: %w", id, ErrSealedTampered)
	}
	mk, err := gate.MessageKey(vaultID, epk)
	if err != nil {
		return ErrVaultLocked
	}
	var env sealedEnvelope
	var body sealedBody
	if err := openJSON(mk, envBox, rowBinding(id, "envelope"), &env); err != nil {
		return fmt.Errorf("storage: message %d: %w", id, err)
	}
	if err := openJSON(mk, bodyBox, rowBinding(id, "body"), &body); err != nil {
		return fmt.Errorf("storage: message %d: %w", id, err)
	}
	var source []byte
	if len(srcBox) > 0 {
		if source, err = mk.Open(srcBox, rowBinding(id, "source")...); err != nil {
			return fmt.Errorf("storage: message %d source: %w", id, ErrSealedTampered)
		}
	}
	env.applyTo(m)
	m.BodyPlain, m.BodyHTML = body.Plain, body.HTML

	atts, err := attachmentRows(ctx, tx, id)
	if err != nil {
		return err
	}
	var written, sealedFiles []string
	for i, a := range atts {
		if i >= len(env.Attachments) {
			return fmt.Errorf("storage: message %d attachments: %w", id, ErrSealedTampered)
		}
		meta := env.Attachments[i]
		restored := Attachment{Filename: meta.Filename, SizeBytes: meta.Size}
		if idx, ok := sealedFileIndex(a.DiskPath); ok && a.DiskPath != "" {
			box, err := os.ReadFile(filepath.Join(d.attachmentsDir, filepath.FromSlash(a.DiskPath)))
			if err == nil {
				plain, err := mk.Open(box, attachmentBinding(id, idx)...)
				if err != nil {
					d.removeAttachmentFiles(written)
					return fmt.Errorf("storage: attachment of %d: %w", id, ErrSealedTampered)
				}
				saved, err := d.writeAttachmentFile(m.AccountID, id, meta.Filename, bytes.NewReader(plain))
				if err != nil {
					d.removeAttachmentFiles(written)
					return err
				}
				written = append(written, saved.DiskPath)
				sealedFiles = append(sealedFiles, a.DiskPath)
				restored = *saved
			}
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE attachments SET filename = ?, content_type = ?, content_id = ?, size_bytes = ?, disk_path = ? WHERE id = ?`,
			restored.Filename, meta.ContentType, meta.ContentID, restored.SizeBytes, restored.DiskPath, a.ID); err != nil {
			d.removeAttachmentFiles(written)
			return fmt.Errorf("storage: unseal attachment row: %w", err)
		}
	}

	m.VaultID = 0
	assigned, err := resolveThread(ctx, tx, threadInputFor(m))
	if err != nil {
		d.removeAttachmentFiles(written)
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE messages SET message_id = ?, subject = ?, from_address = ?, from_name = ?, to_addresses = ?,
    cc_addresses = ?, body_plain = ?, body_html = ?, list_unsubscribe = ?, list_unsubscribe_post = ?,
    smime_status = ?, smime_signer = ?, smime_email = ?, smime_issuer = ?, smime_detail = ?,
    smime_certs = ?, smime_fingerprint = ?, reply_to = ?, auth_spf = ?, auth_dkim = ?, auth_dmarc = ?,
    auth_spf_domain = ?, auth_dkim_domain = ?, charset_guess = ?, in_reply_to = ?, [references] = ?,
    thread_id = ?, thread_subject = ?, thread_loose = ?, mail_kind = ?, list_id = ?, vault_id = 0
WHERE id = ? AND vault_id = ?`,
		m.MessageID, m.Subject, m.FromAddress, m.FromName, m.ToAddresses,
		m.CcAddresses, m.BodyPlain, m.BodyHTML, m.ListUnsubscribe, boolToInt(m.ListUnsubscribePost),
		m.SMIME.Status, m.SMIME.Signer, m.SMIME.Email, m.SMIME.Issuer, m.SMIME.Detail,
		orEmptyBlob(m.SMIME.Certs), m.SMIME.Fingerprint, m.ReplyTo, m.Auth.SPF, m.Auth.DKIM, m.Auth.DMARC,
		m.Auth.SPFDomain, m.Auth.DKIMDomain, m.CharsetGuess,
		encodeThreadingIDs(m.InReplyTo), encodeThreadingIDs(m.References),
		assigned.threadID, assigned.subject, boolToInt(assigned.loose), int(m.MailKind), m.ListID,
		id, vaultID); err != nil {
		d.removeAttachmentFiles(written)
		return fmt.Errorf("storage: unseal message row %d: %w", id, err)
	}
	if len(source) > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO message_pgp (message_id, raw) VALUES (?, ?)
ON CONFLICT (message_id) DO UPDATE SET raw = excluded.raw`, id, source); err != nil {
			d.removeAttachmentFiles(written)
			return fmt.Errorf("storage: restore pgp source %d: %w", id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mail_vault_sealed WHERE message_id = ?`, id); err != nil {
		d.removeAttachmentFiles(written)
		return fmt.Errorf("storage: drop sealed %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		d.removeAttachmentFiles(written)
		return fmt.Errorf("storage: commit unseal %d: %w", id, err)
	}
	d.removeAttachmentFiles(sealedFiles)
	return nil
}

// CountVaultWork returns how many messages a vault still has to seal and how
// many it holds sealed.
func (d *DB) CountVaultWork(ctx context.Context, vaultID int64) (pending, sealed int, err error) {
	err = d.sql.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM messages m JOIN mail_vault_folders f ON f.folder_id = m.folder_id
        WHERE f.vault_id = ?1 AND m.vault_id = 0),
       (SELECT COUNT(*) FROM messages WHERE vault_id = ?1)`, vaultID).Scan(&pending, &sealed)
	if err != nil {
		return 0, 0, fmt.Errorf("storage: vault %d work: %w", vaultID, err)
	}
	return pending, sealed, nil
}

// DeleteMailVaultIfEmpty removes a vault that no longer holds a sealed
// message, and with it its folders, so mail synced afterwards is stored in
// clear. It reports whether the vault was removed.
func (d *DB) DeleteMailVaultIfEmpty(ctx context.Context, vaultID int64) (bool, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("storage: begin vault delete: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
DELETE FROM mail_vaults WHERE id = ? AND NOT EXISTS (SELECT 1 FROM messages WHERE vault_id = ?)`, vaultID, vaultID)
	if err != nil {
		return false, fmt.Errorf("storage: delete vault %d: %w", vaultID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("storage: commit vault delete: %w", err)
	}
	d.invalidateVaultFolders()
	return n > 0, nil
}

// ScrubAfterSealing removes what sealing left behind in the database: thread
// keys made from the cleartext Message-IDs of sealed mail, the full-text
// index entries of the old text, and the write-ahead log. It runs with
// secure_delete on, rebuilds the full-text index from the emptied rows and
// truncates the WAL. It reports whether the WAL could be truncated; it cannot
// while another connection is reading, and the next call tries again.
func (d *DB) ScrubAfterSealing(ctx context.Context) (bool, error) {
	conn, release, err := d.secureConn(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	if err := rebuildThreadKeys(ctx, conn); err != nil {
		return false, err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO messages_fts(messages_fts) VALUES ('rebuild')`); err != nil {
		return false, fmt.Errorf("storage: rebuild full-text index: %w", err)
	}
	return d.truncateWAL(ctx, conn)
}

func (d *DB) truncateWAL(ctx context.Context, conn *sql.Conn) (bool, error) {
	var busy, logFrames, checkpointed int
	if err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return false, fmt.Errorf("storage: checkpoint: %w", err)
	}
	return busy == 0, nil
}

// rebuildThreadKeys writes thread_keys again from the rows themselves, so a
// key made from a Message-ID that only sealed mail carried in clear is gone.
func rebuildThreadKeys(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin thread keys: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx,
		`SELECT account_id, thread_id, message_id, in_reply_to, [references] FROM messages WHERE thread_id != 0 ORDER BY id`)
	if err != nil {
		return fmt.Errorf("storage: read thread keys: %w", err)
	}
	type keyRow struct {
		account, thread int64
		key             string
	}
	var keys []keyRow
	for rows.Next() {
		var (
			account, thread int64
			mid             string
			irt, refs       sql.NullString
		)
		if err := rows.Scan(&account, &thread, &mid, &irt, &refs); err != nil {
			rows.Close()
			return fmt.Errorf("storage: scan thread keys: %w", err)
		}
		in := threadInput{messageID: mid, inReplyTo: decodeThreadingIDs(irt), references: decodeThreadingIDs(refs)}
		for _, k := range threadKeys(in) {
			keys = append(keys, keyRow{account, thread, k})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_keys`); err != nil {
		return fmt.Errorf("storage: clear thread keys: %w", err)
	}
	for _, k := range keys {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO thread_keys (account_id, key, thread_id) VALUES (?, ?, ?)
ON CONFLICT (account_id, key) DO UPDATE SET thread_id = excluded.thread_id`, k.account, k.key, k.thread); err != nil {
			return fmt.Errorf("storage: write thread key: %w", err)
		}
	}
	return tx.Commit()
}
