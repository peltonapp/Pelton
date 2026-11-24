package storage

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// maxPrepAddresses caps how many addresses one MessagesInvolving call matches
// against, since each adds two LIKE tests to a scan.
const maxPrepAddresses = 24

// MessagesInvolving returns the newest messages since the given time in which
// any of the bare addresses is the sender or a recipient, capped at limit. The
// folders are the mail the caller lets it see; trash and junk are the caller's
// to leave out. Rows the list hides are left out.
//
// The recipient columns are text, so the query narrows with LIKE and the
// result is then checked against the parsed address lists: an address that
// only appears inside a longer one is not a match.
func (d *DB) MessagesInvolving(ctx context.Context, addresses []string, folderIDs []int64, since time.Time, limit int) ([]Message, error) {
	wanted := make(map[string]bool, len(addresses))
	var list []string
	for _, a := range addresses {
		if key := normalizeAddress(a); key != "" && !wanted[key] {
			wanted[key] = true
			list = append(list, key)
		}
	}
	if len(list) == 0 || len(folderIDs) == 0 {
		return nil, nil
	}
	if len(list) > maxPrepAddresses {
		list = list[:maxPrepAddresses]
		wanted = map[string]bool{}
		for _, a := range list {
			wanted[a] = true
		}
	}
	folders, args := inClause(folderIDs)
	conds := make([]string, 0, 3*len(list))
	for range list {
		conds = append(conds, senderExpr+" = ?", `lower(to_addresses) LIKE ? ESCAPE '\'`, `lower(cc_addresses) LIKE ? ESCAPE '\'`)
	}
	where := make([]any, 0, 3*len(list))
	for _, a := range list {
		where = append(where, a, likePattern(a), likePattern(a))
	}
	args = append(args, formatTime(since))
	args = append(args, where...)

	query := selectMessageColumns + `
FROM messages
WHERE pending_delete = 0 AND snooze_hidden = 0 AND folder_id IN (` + folders + `) AND date >= ? AND ` + notVaulted + `
AND (` + strings.Join(conds, " OR ") + `)
ORDER BY date DESC, id DESC`
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: messages involving addresses: %w", err)
	}
	defer rows.Close()

	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan message: %w", err)
		}
		if !involves(*m, wanted) {
			continue
		}
		out = append(out, *m)
		if len(out) >= normalizeLimit(limit) {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate messages involving addresses: %w", err)
	}
	return out, nil
}

// involves says whether a message's sender or a recipient is one of the
// addresses.
func involves(m Message, wanted map[string]bool) bool {
	if wanted[bareAddr(m.FromAddress)] {
		return true
	}
	for _, list := range []string{m.ToAddresses, m.CcAddresses} {
		for _, a := range AddressList(list) {
			if wanted[a] {
				return true
			}
		}
	}
	return false
}

// bareAddr is the lowercase bare address of a stored address, which may carry a
// display name in angle brackets.
func bareAddr(s string) string {
	if a, err := mail.ParseAddress(s); err == nil {
		return normalizeAddress(a.Address)
	}
	if lt := strings.LastIndex(s, "<"); lt >= 0 {
		if gt := strings.Index(s[lt:], ">"); gt >= 0 {
			return normalizeAddress(s[lt+1 : lt+gt])
		}
		return normalizeAddress(s[lt+1:])
	}
	return normalizeAddress(s)
}

// AddressList returns the bare lowercase addresses of a stored recipient list
// such as "Ada <ada@x.example>, bob@x.example". A part that holds no address
// is skipped.
func AddressList(list string) []string {
	if strings.TrimSpace(list) == "" {
		return nil
	}
	var out []string
	if parsed, err := mail.ParseAddressList(list); err == nil {
		for _, a := range parsed {
			out = append(out, normalizeAddress(a.Address))
		}
		return out
	}
	for _, part := range strings.Split(list, ",") {
		if a := bareAddr(part); strings.Contains(a, "@") {
			out = append(out, a)
		}
	}
	return out
}

// MessagesWithSubject returns the newest messages since the given time whose
// subject contains the text, capped at limit. An empty text matches nothing.
func (d *DB) MessagesWithSubject(ctx context.Context, text string, folderIDs []int64, since time.Time, limit int) ([]Message, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(folderIDs) == 0 {
		return nil, nil
	}
	folders, args := inClause(folderIDs)
	args = append(args, formatTime(since), likePattern(strings.ToLower(text)), normalizeLimit(limit))
	query := selectMessageColumns + `
FROM messages
WHERE pending_delete = 0 AND snooze_hidden = 0 AND folder_id IN (` + folders + `) AND date >= ? AND ` + notVaulted + `
AND lower(subject) LIKE ? ESCAPE '\'
ORDER BY date DESC, id DESC
LIMIT ?`
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: messages with subject: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan message: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate messages with subject: %w", err)
	}
	return out, nil
}

// AttachmentsOfMessages returns the attachments of the given messages, newest
// message first, in the shape the attachments browser lists them: tiny images
// and small inline images are left out as it leaves them out.
func (d *DB) AttachmentsOfMessages(ctx context.Context, messageIDs []int64) ([]AttachmentEntry, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	ids, args := inClause(messageIDs)
	query := `
SELECT a.id, a.message_id, a.filename, a.content_type, a.size_bytes, a.content_id, a.disk_path,
       ` + kindExpr + `, m.account_id, m.folder_id, m.thread_id, m.subject, m.from_address, m.from_name, m.date
FROM messages m
JOIN attachments a ON a.message_id = m.id
WHERE m.id IN (` + ids + `) AND ` + notVaultedM + `
AND NOT (` + kindExpr + fmt.Sprintf(` = 'image' AND (a.size_bytes < %d OR (a.content_id <> '' AND a.size_bytes < %d)))`, TinyImageBytes, InlineImageBytes) + `
ORDER BY m.date DESC, m.id DESC, a.id`
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: attachments of messages: %w", err)
	}
	defer rows.Close()
	var out []AttachmentEntry
	for rows.Next() {
		var (
			e    AttachmentEntry
			kind string
			date string
		)
		if err := rows.Scan(&e.ID, &e.MessageID, &e.Filename, &e.ContentType, &e.SizeBytes,
			&e.ContentID, &e.DiskPath, &kind, &e.AccountID, &e.FolderID, &e.ThreadID,
			&e.Subject, &e.FromAddress, &e.FromName, &date); err != nil {
			return nil, fmt.Errorf("storage: scan attachment entry: %w", err)
		}
		e.Kind = AttachmentKind(kind)
		if e.Date, err = parseTime(date); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate attachments of messages: %w", err)
	}
	return out, nil
}
