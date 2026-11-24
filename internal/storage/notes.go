package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// noteKeyExpr is the key a message's note is stored under: its Message-ID,
// or '#' and its row id when it has none.
const noteKeyExpr = `CASE WHEN messages.message_id = '' THEN '#' || messages.id ELSE messages.message_id END`

// Note is a private note on a message or, when ThreadID is set and MessageKey
// is empty, on a conversation.
type Note struct {
	ID         int64
	AccountID  int64
	ThreadID   int64
	MessageKey string
	Body       string
	UpdatedAt  time.Time
}

// noteTarget is where a note for a message goes.
type noteTarget struct {
	accountID int64
	threadID  int64
	key       string
}

// noteTargetFor resolves the note a message or its conversation keeps. A
// conversation note on a message the threading pass has not reached yet is
// ErrNotThreaded.
func (d *DB) noteTargetFor(ctx context.Context, messageID int64, thread bool) (noteTarget, error) {
	var (
		t     noteTarget
		msgID string
	)
	err := d.sql.QueryRowContext(ctx,
		`SELECT account_id, thread_id, message_id FROM messages WHERE id = ?`, messageID).Scan(&t.accountID, &t.threadID, &msgID)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrMessageNotFound
	}
	if err != nil {
		return t, fmt.Errorf("storage: note target %d: %w", messageID, err)
	}
	if thread {
		if t.threadID <= 0 {
			return t, ErrNotThreaded
		}
		return t, nil
	}
	t.threadID = 0
	t.key = msgID
	if t.key == "" {
		t.key = "#" + strconv.FormatInt(messageID, 10)
	}
	return t, nil
}

func (t noteTarget) where() (string, []any) {
	if t.key == "" {
		return `thread_id = ? AND message_key = ''`, []any{t.threadID}
	}
	return `account_id = ? AND message_key = ?`, []any{t.accountID, t.key}
}

// GetNote returns the note on a message, or on its conversation when thread is
// true. It returns nil when there is none.
func (d *DB) GetNote(ctx context.Context, messageID int64, thread bool) (*Note, error) {
	t, err := d.noteTargetFor(ctx, messageID, thread)
	if err != nil {
		return nil, err
	}
	where, args := t.where()
	var (
		n       Note
		updated string
	)
	err = d.sql.QueryRowContext(ctx,
		`SELECT id, account_id, thread_id, message_key, body, updated_at FROM notes WHERE `+where, args...).
		Scan(&n.ID, &n.AccountID, &n.ThreadID, &n.MessageKey, &n.Body, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get note: %w", err)
	}
	if n.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &n, nil
}

// SaveNote writes the note on a message, or on its conversation when thread
// is true. A blank body deletes the note, and the result is then nil.
func (d *DB) SaveNote(ctx context.Context, messageID int64, thread bool, body string) (*Note, error) {
	if strings.TrimSpace(body) == "" {
		return nil, d.DeleteNote(ctx, messageID, thread)
	}
	t, err := d.noteTargetFor(ctx, messageID, thread)
	if err != nil {
		return nil, err
	}
	now := nowText()
	conflict := `(account_id, message_key) WHERE message_key != ''`
	if t.key == "" {
		conflict = `(thread_id) WHERE message_key = ''`
	}
	_, err = d.sql.ExecContext(ctx, `
INSERT INTO notes (account_id, thread_id, message_key, body, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT `+conflict+` DO UPDATE SET body = excluded.body, updated_at = excluded.updated_at`,
		t.accountID, t.threadID, t.key, body, now, now)
	if err != nil {
		return nil, fmt.Errorf("storage: save note: %w", err)
	}
	return d.GetNote(ctx, messageID, thread)
}

// DeleteNote removes the note on a message, or on its conversation when
// thread is true. Deleting a note that does not exist is not an error.
func (d *DB) DeleteNote(ctx context.Context, messageID int64, thread bool) error {
	t, err := d.noteTargetFor(ctx, messageID, thread)
	if err != nil {
		return err
	}
	where, args := t.where()
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM notes WHERE `+where, args...); err != nil {
		return fmt.Errorf("storage: delete note: %w", err)
	}
	return nil
}

// maxNoteMatches caps how many messages a note search resolves to.
const maxNoteMatches = 5000

// NoteMessageIDs returns the ids of the messages whose own note or
// conversation note contains text, newest first. An empty text matches every
// message with a note.
func (d *DB) NoteMessageIDs(ctx context.Context, text string) ([]int64, error) {
	cond := "1"
	var args []any
	if text = strings.TrimSpace(text); text != "" {
		cond = `n.body LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(text)+"%")
	}
	// read from the notes outward: there are few of them, and each reaches its
	// messages through the Message-ID, row id or thread index.
	query := `
SELECT id FROM messages WHERE pending_delete = 0 AND ` + notVaulted + ` AND id IN (
    SELECT m.id FROM notes n JOIN messages m
      ON m.account_id = n.account_id AND m.message_id = n.message_key
    WHERE n.message_key != '' AND n.message_key NOT LIKE '#%' AND ` + cond + `
    UNION
    SELECT CAST(SUBSTR(n.message_key, 2) AS INTEGER) FROM notes n
    WHERE n.message_key LIKE '#%' AND ` + cond + `
    UNION
    SELECT m.id FROM notes n JOIN messages m ON m.thread_id = n.thread_id
    WHERE n.message_key = '' AND n.thread_id != 0 AND ` + cond + `)
ORDER BY date DESC, id DESC LIMIT ?`
	args = append(append(append(args, args...), args...), maxNoteMatches)
	ids, err := queryInt64s(ctx, d.sql, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: note search: %w", err)
	}
	return ids, nil
}
