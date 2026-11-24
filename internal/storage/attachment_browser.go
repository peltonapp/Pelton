package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AttachmentKind is the group the attachments browser files a part under.
type AttachmentKind string

const (
	KindPDF         AttachmentKind = "pdf"
	KindImage       AttachmentKind = "image"
	KindSpreadsheet AttachmentKind = "spreadsheet"
	KindArchive     AttachmentKind = "archive"
	KindMedia       AttachmentKind = "media"
	KindDocument    AttachmentKind = "document"
	KindOther       AttachmentKind = "other"
)

// Images below these sizes are left out of the browser: anything under
// TinyImageBytes is a tracking pixel or a spacer, and an image carrying a
// content id under InlineImageBytes is a logo or icon embedded in a signature
// or a newsletter layout. Larger inline images are photos and stay listed.
const (
	TinyImageBytes   = 2 << 10
	InlineImageBytes = 20 << 10
)

// kindRule classifies by content type (exact types or prefixes ending in /)
// and by filename extension. Rules are tried in order, so text/csv lands in
// spreadsheets before text/ makes it a document.
type kindRule struct {
	kind  AttachmentKind
	types []string
	exts  []string
}

var kindRules = []kindRule{
	{KindPDF, []string{"application/pdf"}, []string{"pdf"}},
	{KindImage, []string{"image/"}, []string{"png", "jpg", "jpeg", "gif", "webp", "heic", "heif", "bmp", "tif", "tiff", "svg"}},
	{KindSpreadsheet, []string{
		"text/csv",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.oasis.opendocument.spreadsheet",
	}, []string{"xls", "xlsx", "xlsm", "ods", "csv", "tsv", "numbers"}},
	{KindArchive, []string{
		"application/zip",
		"application/x-zip-compressed",
		"application/gzip",
		"application/x-tar",
		"application/x-7z-compressed",
		"application/vnd.rar",
		"application/x-rar-compressed",
	}, []string{"zip", "tar", "gz", "tgz", "bz2", "xz", "7z", "rar"}},
	{KindMedia, []string{"audio/", "video/"}, []string{"mp3", "wav", "m4a", "flac", "ogg", "opus", "aac", "mp4", "mov", "m4v", "mkv", "avi", "webm"}},
	{KindDocument, []string{
		"text/",
		"application/msword",
		"application/rtf",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.ms-powerpoint",
		"application/vnd.oasis.opendocument.text",
		"application/vnd.oasis.opendocument.presentation",
	}, []string{"doc", "docx", "odt", "rtf", "txt", "md", "pages", "ppt", "pptx", "odp", "key", "epub"}},
}

// kindExpr is the SQL that classifies the joined attachment row a, matching
// ClassifyAttachment. Every literal comes from kindRules, never from input.
var kindExpr = buildKindExpr()

func buildKindExpr() string {
	var b strings.Builder
	b.WriteString("(CASE")
	for _, rule := range kindRules {
		var conds []string
		for _, ct := range rule.types {
			if strings.HasSuffix(ct, "/") {
				conds = append(conds, "lower(a.content_type) LIKE '"+ct+"%'")
			} else {
				conds = append(conds, "lower(a.content_type) = '"+ct+"'")
			}
		}
		for _, ext := range rule.exts {
			conds = append(conds, "lower(a.filename) LIKE '%."+ext+"'")
		}
		b.WriteString(" WHEN " + strings.Join(conds, " OR ") + " THEN '" + string(rule.kind) + "'")
	}
	b.WriteString(" ELSE '" + string(KindOther) + "' END)")
	return b.String()
}

// ClassifyAttachment returns the kind of a part from its content type and
// filename, the same answer the browser query computes in SQL.
func ClassifyAttachment(contentType, filename string) AttachmentKind {
	ct := strings.ToLower(contentType)
	name := strings.ToLower(filename)
	for _, rule := range kindRules {
		for _, t := range rule.types {
			if (strings.HasSuffix(t, "/") && strings.HasPrefix(ct, t)) || ct == t {
				return rule.kind
			}
		}
		for _, ext := range rule.exts {
			if strings.HasSuffix(name, "."+ext) {
				return rule.kind
			}
		}
	}
	return KindOther
}

// senderExpr is the bare lowercased address of m.from_address. It must match
// the expression of idx_messages_sender (migration 0045) character for
// character, so the index applies. The column is left unqualified: the
// attachments table has none of that name, so it always means messages.
const senderExpr = `lower(trim(CASE WHEN instr(from_address, '<') > 0 AND instr(from_address, '>') > instr(from_address, '<')
        THEN substr(from_address, instr(from_address, '<') + 1, instr(from_address, '>') - instr(from_address, '<') - 1)
        ELSE from_address END))`

// AttachmentQuery selects a page of the attachments browser. FolderIDs is the
// mail the caller lets the browser see; an empty set matches nothing. Every
// other field narrows the result and is ignored when zero.
type AttachmentQuery struct {
	FolderIDs []int64
	Kinds     []AttachmentKind
	AccountID int64
	// Sender matches part of the sender's address or name; SenderAddress is
	// one exact bare address and uses the sender index.
	Sender        string
	SenderAddress string
	// Since and Until bound the message date, Until exclusive.
	Since time.Time
	Until time.Time
	// Name matches part of the filename.
	Name   string
	Limit  int
	Offset int
}

// AttachmentEntry is one attachment with the message it came with.
type AttachmentEntry struct {
	Attachment
	Kind        AttachmentKind
	AccountID   int64
	FolderID    int64
	ThreadID    int64
	Subject     string
	FromAddress string
	FromName    string
	Date        time.Time
}

// QueryAttachments returns a page of attachments newest first. A message the
// account holds in several folders (gmail labels) lists its files once, and
// tiny images are left out as described at TinyImageBytes.
func (d *DB) QueryAttachments(ctx context.Context, q AttachmentQuery) ([]AttachmentEntry, error) {
	if len(q.FolderIDs) == 0 {
		return nil, nil
	}
	where, args := attachmentWhere(q)
	query := `
SELECT a.id, a.message_id, a.filename, a.content_type, a.size_bytes, a.content_id, a.disk_path,
       ` + kindExpr + `, m.account_id, m.folder_id, m.thread_id, m.subject, m.from_address, m.from_name, m.date
FROM ` + attachmentSource(q) + `
JOIN attachments a ON a.message_id = m.id
WHERE ` + where + `
ORDER BY m.date DESC, m.id DESC, a.id
LIMIT ? OFFSET ?`
	args = append(args, normalizeLimit(q.Limit), q.Offset)
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: query attachments: %w", err)
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
		return nil, fmt.Errorf("storage: iterate attachment entries: %w", err)
	}
	return out, nil
}

// CountAttachments returns how many attachments q matches, ignoring its
// limit and offset.
func (d *DB) CountAttachments(ctx context.Context, q AttachmentQuery) (int, error) {
	if len(q.FolderIDs) == 0 {
		return 0, nil
	}
	where, args := attachmentWhere(q)
	var n int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+attachmentSource(q)+` JOIN attachments a ON a.message_id = m.id WHERE `+where, args...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("storage: count attachments: %w", err)
	}
	return n, nil
}

// attachmentSource names the index the messages side is read through. The
// store keeps no planner statistics, and without them sqlite reaches for the
// folder index and sorts everything after; walking the partial date index
// lets a page stop at its limit, and one exact sender is a handful of rows in
// the sender index.
func attachmentSource(q AttachmentQuery) string {
	if q.SenderAddress != "" {
		return "messages m INDEXED BY idx_messages_sender"
	}
	return "messages m INDEXED BY idx_messages_with_attachments"
}

// attachmentWhere builds the clause QueryAttachments and CountAttachments
// share, so the page and the total never disagree.
func attachmentWhere(q AttachmentQuery) (string, []any) {
	folders, args := inClause(q.FolderIDs)
	conds := []string{
		"m.has_attachments = 1",
		"m.pending_delete = 0",
		"m.folder_id IN (" + folders + ")",
		notVaultedM,
		fmt.Sprintf("NOT (%s = 'image' AND (a.size_bytes < %d OR (a.content_id <> '' AND a.size_bytes < %d)))",
			kindExpr, TinyImageBytes, InlineImageBytes),
	}
	// the first stored copy of a message stands for the rest, the way the
	// flagged and unread views list it.
	others, otherArgs := inClause(q.FolderIDs)
	conds = append(conds, `(m.message_id = '' OR NOT EXISTS (
SELECT 1 FROM messages o WHERE o.account_id = m.account_id AND o.message_id = m.message_id
AND o.id < m.id AND o.has_attachments = 1 AND o.pending_delete = 0 AND o.folder_id IN (`+others+`)))`)
	args = append(args, otherArgs...)

	if len(q.Kinds) > 0 {
		marks := make([]string, len(q.Kinds))
		for i, k := range q.Kinds {
			marks[i] = "?"
			args = append(args, string(k))
		}
		conds = append(conds, kindExpr+" IN ("+strings.Join(marks, ", ")+")")
	}
	if q.AccountID != 0 {
		conds = append(conds, "m.account_id = ?")
		args = append(args, q.AccountID)
	}
	if q.SenderAddress != "" {
		conds = append(conds, senderMatch)
		args = append(args, normalizeAddress(q.SenderAddress))
	}
	if s := strings.TrimSpace(q.Sender); s != "" {
		pattern := likePattern(s)
		conds = append(conds, `(m.from_address LIKE ? ESCAPE '\' OR m.from_name LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if !q.Since.IsZero() {
		conds = append(conds, "m.date >= ?")
		args = append(args, formatTime(q.Since))
	}
	if !q.Until.IsZero() {
		conds = append(conds, "m.date < ?")
		args = append(args, formatTime(q.Until))
	}
	if n := strings.TrimSpace(q.Name); n != "" {
		conds = append(conds, `a.filename LIKE ? ESCAPE '\'`)
		args = append(args, likePattern(n))
	}
	return strings.Join(conds, "\nAND "), args
}

// likePattern wraps s for a substring LIKE, escaping its own wildcards.
func likePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// GetAttachmentEntry returns one attachment with its message, or
// ErrMessageNotFound when either is gone.
func (d *DB) GetAttachmentEntry(ctx context.Context, attachmentID int64) (*AttachmentEntry, error) {
	const query = `
SELECT a.id, a.message_id, a.filename, a.content_type, a.size_bytes, a.content_id, a.disk_path,
       m.account_id, m.folder_id, m.thread_id, m.subject, m.from_address, m.from_name, m.date
FROM attachments a JOIN messages m ON m.id = a.message_id
WHERE a.id = ? AND ` + notVaultedM
	var (
		e    AttachmentEntry
		date string
	)
	err := d.sql.QueryRowContext(ctx, query, attachmentID).Scan(&e.ID, &e.MessageID, &e.Filename,
		&e.ContentType, &e.SizeBytes, &e.ContentID, &e.DiskPath, &e.AccountID, &e.FolderID,
		&e.ThreadID, &e.Subject, &e.FromAddress, &e.FromName, &date)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get attachment %d: %w", attachmentID, err)
	}
	e.Kind = ClassifyAttachment(e.ContentType, e.Filename)
	if e.Date, err = parseTime(date); err != nil {
		return nil, err
	}
	return &e, nil
}

// AttachmentOnDisk reports whether an attachment's bytes are in the local
// store. A row with no file is one whose message was cached without its
// attachments, or whose file has gone missing since.
func (d *DB) AttachmentOnDisk(diskPath string) bool {
	if diskPath == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(d.attachmentsDir, filepath.FromSlash(diskPath)))
	return err == nil && !info.IsDir()
}

// RestoreAttachmentFile writes the bytes of an attachment whose file is
// missing and points its row at the new file.
func (d *DB) RestoreAttachmentFile(ctx context.Context, accountID int64, att Attachment, content io.Reader) (*Attachment, error) {
	mk, _, _, err := d.sealedKey(ctx, att.MessageID)
	if err != nil {
		return nil, err
	}
	if mk != nil {
		return d.restoreSealedAttachment(ctx, accountID, att, content, mk)
	}
	saved, err := d.writeAttachmentFile(accountID, att.MessageID, att.Filename, content)
	if err != nil {
		return nil, err
	}
	const query = `UPDATE attachments SET disk_path = ?, size_bytes = ? WHERE id = ?`
	if _, err := d.sql.ExecContext(ctx, query, saved.DiskPath, saved.SizeBytes, att.ID); err != nil {
		d.removeAttachmentFiles([]string{saved.DiskPath})
		return nil, fmt.Errorf("storage: restore attachment %d: %w", att.ID, err)
	}
	out := att
	out.DiskPath = saved.DiskPath
	out.SizeBytes = saved.SizeBytes
	return &out, nil
}
