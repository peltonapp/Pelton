package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// MessageTrust is a message's authentication record and the sender headers
// kept with it (see migration 0111). The zero AuthState means the message has
// no record, which is all mail synced before the record existed.
type MessageTrust struct {
	MessageID int64
	// AuthState is "trusted", "untrusted" or "none".
	AuthState   string
	ServID      string
	SPF         string
	SPFDomain   string
	DKIM        string
	DKIMDomain  string
	DKIMDomains []string
	DMARC       string
	DMARCPolicy string
	HeaderFrom  string
	// ReturnPath, Sender, Signers, ReceivedSPF and ARC are what the message
	// itself claims; nothing here vouches for them.
	ReturnPath  string
	Sender      string
	Signers     []string
	ReceivedSPF string
	ARC         string
	// UnsubSigners are the signers whose signature covers the unsubscribe
	// headers; a one-click unsubscribe is trusted only when one of them also
	// passed.
	UnsubSigners []string
	// ReadReceiptTo is where a read receipt was asked to go, empty when none
	// was asked for.
	ReadReceiptTo string
}

// SetMessageTrust stores the record for one message, replacing any earlier one.
// A message in a mail vault gets none: the record names its sender in clear.
func (d *DB) SetMessageTrust(ctx context.Context, t MessageTrust) error {
	const query = `
INSERT INTO message_trust (message_id, auth_state, servid, spf, spf_domain, dkim, dkim_domain, dkim_domains,
    dmarc, dmarc_policy, header_from, return_path, sender, signers, unsub_signers, received_spf, arc, read_receipt_to)
SELECT m.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
FROM messages m WHERE m.id = ? AND m.vault_id = 0 AND ` + notVaultedM + `
ON CONFLICT(message_id) DO UPDATE SET
    auth_state = excluded.auth_state, servid = excluded.servid, spf = excluded.spf,
    spf_domain = excluded.spf_domain, dkim = excluded.dkim, dkim_domain = excluded.dkim_domain,
    dkim_domains = excluded.dkim_domains, dmarc = excluded.dmarc, dmarc_policy = excluded.dmarc_policy,
    header_from = excluded.header_from, return_path = excluded.return_path, sender = excluded.sender,
    signers = excluded.signers, unsub_signers = excluded.unsub_signers, received_spf = excluded.received_spf,
    arc = excluded.arc, read_receipt_to = excluded.read_receipt_to`
	_, err := d.sql.ExecContext(ctx, query, t.AuthState, t.ServID, t.SPF, t.SPFDomain, t.DKIM,
		t.DKIMDomain, strings.Join(t.DKIMDomains, " "), t.DMARC, t.DMARCPolicy, t.HeaderFrom, t.ReturnPath,
		t.Sender, strings.Join(t.Signers, " "), strings.Join(t.UnsubSigners, " "), t.ReceivedSPF, t.ARC, t.ReadReceiptTo,
		t.MessageID)
	if err != nil {
		return fmt.Errorf("storage: set message trust %d: %w", t.MessageID, err)
	}
	return nil
}

const trustColumns = `message_id, auth_state, servid, spf, spf_domain, dkim, dkim_domain, dkim_domains,
    dmarc, dmarc_policy, header_from, return_path, sender, signers, unsub_signers, received_spf, arc, read_receipt_to`

func scanTrust(row rowScanner) (MessageTrust, error) {
	var t MessageTrust
	var domains, signers, unsub string
	err := row.Scan(&t.MessageID, &t.AuthState, &t.ServID, &t.SPF, &t.SPFDomain, &t.DKIM, &t.DKIMDomain,
		&domains, &t.DMARC, &t.DMARCPolicy, &t.HeaderFrom, &t.ReturnPath, &t.Sender, &signers, &unsub,
		&t.ReceivedSPF, &t.ARC, &t.ReadReceiptTo)
	t.DKIMDomains = strings.Fields(domains)
	t.Signers = strings.Fields(signers)
	t.UnsubSigners = strings.Fields(unsub)
	return t, err
}

// GetMessageTrust returns a message's record. A message with none, which
// includes every message in a mail vault, returns the zero record and no error.
func (d *DB) GetMessageTrust(ctx context.Context, messageID int64) (MessageTrust, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT `+trustColumns+` FROM message_trust WHERE message_id = ?
AND message_id IN (SELECT m.id FROM messages m WHERE m.id = ? AND m.vault_id = 0 AND `+notVaultedM+`)`, messageID, messageID)
	t, err := scanTrust(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageTrust{}, nil
	}
	if err != nil {
		return MessageTrust{}, fmt.Errorf("storage: get message trust %d: %w", messageID, err)
	}
	return t, nil
}

// Sender marks.
const (
	// SenderKnown is "I know this sender".
	SenderKnown = "known"
	// SenderImpostor is "not who they claim to be".
	SenderImpostor = "impostor"
)

// SenderMark is what the user said about one address and when.
type SenderMark struct {
	Address string
	Mark    string
	At      time.Time
}

// GetSenderMark returns the mark on a bare address, the zero value when it
// has none.
func (d *DB) GetSenderMark(ctx context.Context, address string) (SenderMark, error) {
	addr := normalizeAddress(address)
	var m SenderMark
	var at string
	err := d.sql.QueryRowContext(ctx, `SELECT address, mark, marked_at FROM sender_marks WHERE address = ?`, addr).Scan(&m.Address, &m.Mark, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return SenderMark{}, nil
	}
	if err != nil {
		return SenderMark{}, fmt.Errorf("storage: get sender mark: %w", err)
	}
	m.At, err = parseTime(at)
	return m, err
}

// SetSenderMark marks a bare address SenderKnown or SenderImpostor; the empty
// mark removes it.
func (d *DB) SetSenderMark(ctx context.Context, address, mark string) error {
	addr := normalizeAddress(address)
	if addr == "" {
		return fmt.Errorf("storage: sender mark needs an address")
	}
	var err error
	switch mark {
	case "":
		_, err = d.sql.ExecContext(ctx, `DELETE FROM sender_marks WHERE address = ?`, addr)
	case SenderKnown, SenderImpostor:
		_, err = d.sql.ExecContext(ctx, `INSERT INTO sender_marks (address, mark, marked_at) VALUES (?, ?, ?)
ON CONFLICT(address) DO UPDATE SET mark = excluded.mark, marked_at = excluded.marked_at`, addr, mark, nowText())
	default:
		return fmt.Errorf("storage: unknown sender mark %q", mark)
	}
	if err != nil {
		return fmt.Errorf("storage: set sender mark: %w", err)
	}
	return nil
}

// TrustHistoryEntry is one earlier message from a sender, with what its
// record said about how it was sent.
type TrustHistoryEntry struct {
	ID       int64
	Date     time.Time
	FromName string
	ReplyTo  string
	// Trust is the zero value for mail without a record.
	Trust MessageTrust
}

// SenderTrustHistory returns the newest messages from one bare address in the
// given folders, older than before and other than exclude, capped at limit.
func (d *DB) SenderTrustHistory(ctx context.Context, address string, folderIDs []int64, before time.Time, exclude int64, limit int) ([]TrustHistoryEntry, error) {
	if len(folderIDs) == 0 || strings.TrimSpace(address) == "" {
		return nil, nil
	}
	folders, args := inClause(folderIDs)
	args = append([]any{normalizeAddress(address), formatTime(before), exclude}, args...)
	args = append(args, normalizeLimit(limit))
	query := `SELECT m.id, m.date, m.from_name, m.reply_to, COALESCE(t.auth_state, ''), COALESCE(t.spf, ''),
    COALESCE(t.spf_domain, ''), COALESCE(t.dkim_domains, ''), COALESCE(t.dmarc, ''), COALESCE(t.header_from, '')
FROM messages m INDEXED BY idx_messages_sender
LEFT JOIN message_trust t ON t.message_id = m.id
WHERE ` + strings.ReplaceAll(senderExpr, "from_address", "m.from_address") + ` = ?
AND m.date <= ? AND m.id <> ? AND m.pending_delete = 0 AND m.folder_id IN (` + folders + `) AND ` + notVaultedM + `
ORDER BY m.date DESC, m.id DESC
LIMIT ?`
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: sender trust history: %w", err)
	}
	defer rows.Close()
	var out []TrustHistoryEntry
	for rows.Next() {
		var e TrustHistoryEntry
		var date, domains string
		if err := rows.Scan(&e.ID, &date, &e.FromName, &e.ReplyTo, &e.Trust.AuthState, &e.Trust.SPF,
			&e.Trust.SPFDomain, &domains, &e.Trust.DMARC, &e.Trust.HeaderFrom); err != nil {
			return nil, fmt.Errorf("storage: scan sender trust history: %w", err)
		}
		if e.Date, err = parseTime(date); err != nil {
			return nil, err
		}
		e.Trust.MessageID = e.ID
		e.Trust.DKIMDomains = strings.Fields(domains)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SentLines returns the To and Cc lines of the newest messages in the given
// (sent) folders, capped at limit, with their dates; contains, when set, keeps
// only lines that mention it. The lines are as stored, formatted, and a match
// on contains can be a longer address, so the caller parses the addresses.
func (d *DB) SentLines(ctx context.Context, folderIDs []int64, contains string, limit int) ([]SentLine, error) {
	if len(folderIDs) == 0 {
		return nil, nil
	}
	folders, args := inClause(folderIDs)
	query := `SELECT to_addresses, cc_addresses, date FROM messages
WHERE folder_id IN (` + folders + `) AND pending_delete = 0 AND ` + notVaulted
	if contains != "" {
		query += ` AND lower(to_addresses || ',' || cc_addresses) LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(normalizeAddress(contains))+"%")
	}
	query += ` ORDER BY date DESC LIMIT ?`
	args = append(args, normalizeLimit(limit))
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: sent recipients: %w", err)
	}
	defer rows.Close()
	var out []SentLine
	for rows.Next() {
		var l SentLine
		var date string
		if err := rows.Scan(&l.To, &l.Cc, &date); err != nil {
			return nil, fmt.Errorf("storage: scan sent recipients: %w", err)
		}
		if l.Date, err = parseTime(date); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SentLine is the recipients of one sent message.
type SentLine struct {
	To   string
	Cc   string
	Date time.Time
}

// ContactAddresses returns every address in the real address books with the
// contact's name, lowercased address to name.
func (d *DB) ContactAddresses(ctx context.Context) (map[string]string, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT lower(e.email), c.full_name FROM contact_emails e JOIN contacts c ON c.id = e.contact_id ORDER BY c.id`)
	if err != nil {
		return nil, fmt.Errorf("storage: contact addresses: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var email, name string
		if err := rows.Scan(&email, &name); err != nil {
			return nil, fmt.Errorf("storage: scan contact address: %w", err)
		}
		if _, seen := out[email]; !seen && strings.Contains(email, "@") {
			out[email] = name
		}
	}
	return out, rows.Err()
}
