package storage

import (
	"context"
	"fmt"
	"unicode/utf8"
)

// MangledMessage identifies one cached message whose stored text is not valid
// utf-8 and so has to come from the server again.
type MangledMessage struct {
	ID  int64
	UID uint32
}

// markBatch is how many ids one repair-marking statement carries. The scan runs
// once over the whole cache, so it holds a batch rather than every id in a
// mailbox that can be tens of thousands of messages long.
const markBatch = 500

// MarkMangledMessages flags every cached message whose subject or body is not
// valid utf-8 and returns how many it found. Those messages were stored before
// charset detection existed; the raw source is not kept, so a sync of their
// folder fetches them again.
//
// It walks the whole messages table, which is why it is called once, in the
// background, rather than on every start.
func (d *DB) MarkMangledMessages(ctx context.Context) (int, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, subject, body_plain, body_html FROM messages WHERE needs_refetch = 0`)
	if err != nil {
		return 0, fmt.Errorf("storage: scan messages for broken text: %w", err)
	}
	defer rows.Close()

	var (
		found int
		batch []int64
	)
	for rows.Next() {
		var (
			id                       int64
			subject, plain, htmlBody string
		)
		if err := rows.Scan(&id, &subject, &plain, &htmlBody); err != nil {
			return found, fmt.Errorf("storage: scan message text: %w", err)
		}
		if utf8.ValidString(subject) && utf8.ValidString(plain) && utf8.ValidString(htmlBody) {
			continue
		}
		batch = append(batch, id)
		found++
		if len(batch) < markBatch {
			continue
		}
		if err := d.markRefetch(ctx, batch); err != nil {
			return found, err
		}
		batch = batch[:0]
	}
	if err := rows.Err(); err != nil {
		return found, fmt.Errorf("storage: iterate messages for broken text: %w", err)
	}
	if err := d.markRefetch(ctx, batch); err != nil {
		return found, err
	}
	return found, nil
}

func (d *DB) markRefetch(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	marks, args := inClause(ids)
	query := `UPDATE messages SET needs_refetch = 1 WHERE id IN (` + marks + `)`
	if _, err := d.sql.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("storage: mark messages for refetch: %w", err)
	}
	return nil
}

// MessagesNeedingRefetch returns up to limit messages in a folder that are
// marked for refetch, oldest uid first.
func (d *DB) MessagesNeedingRefetch(ctx context.Context, folderID int64, limit int) ([]MangledMessage, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, uid FROM messages WHERE folder_id = ? AND needs_refetch = 1 ORDER BY uid LIMIT ?`,
		folderID, normalizeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("storage: list messages needing refetch in folder %d: %w", folderID, err)
	}
	defer rows.Close()

	var out []MangledMessage
	for rows.Next() {
		var m MangledMessage
		if err := rows.Scan(&m.ID, &m.UID); err != nil {
			return nil, fmt.Errorf("storage: scan message needing refetch: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate messages needing refetch: %w", err)
	}
	return out, nil
}

// RepairMessageText replaces a message's subject and bodies with freshly
// decoded ones and clears the refetch mark. charsetGuess names what the text
// was read as, or is empty when the message turned out to declare a charset
// that exists after all.
func (d *DB) RepairMessageText(ctx context.Context, id int64, subject, plain, htmlBody, charsetGuess string) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE messages
   SET subject = ?, body_plain = ?, body_html = ?, charset_guess = ?, needs_refetch = 0, `+textRewritten+`
 WHERE id = ?`, subject, plain, htmlBody, charsetGuess, id)
	if err != nil {
		return fmt.Errorf("storage: repair text of message %d: %w", id, err)
	}
	return requireOneRow(res, ErrMessageNotFound)
}

// ClearRefetchMark drops the mark without changing the text, for a message the
// server no longer has. Without it every sync would keep trying.
func (d *DB) ClearRefetchMark(ctx context.Context, id int64) error {
	if _, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET needs_refetch = 0 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: clear refetch mark on message %d: %w", id, err)
	}
	return nil
}
