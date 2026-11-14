package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// MessageRef says where a stored message lives.
type MessageRef struct {
	ID        int64
	AccountID int64
	FolderID  int64
}

// FindMessageByHeaderID finds a message by its Message-ID header, given
// without angle brackets. The oldest copy wins when several folders hold one.
func (d *DB) FindMessageByHeaderID(ctx context.Context, headerID string) (*MessageRef, error) {
	const query = `
SELECT id, account_id, folder_id FROM messages
WHERE message_id IN (?, ?)
ORDER BY id LIMIT 1`
	var ref MessageRef
	err := d.sql.QueryRowContext(ctx, query, headerID, "<"+headerID+">").Scan(&ref.ID, &ref.AccountID, &ref.FolderID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: find message by header id: %w", err)
	}
	return &ref, nil
}
