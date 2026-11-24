package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNoPGPSource is returned when a message has no stored pgp source, either
// because it is not protected or because it was cached before the source was
// kept.
var ErrNoPGPSource = errors.New("storage: no pgp source for message")

// SetMessagePGPSource stores the raw rfc 822 source of a pgp protected message
// so it can be decrypted and verified on demand. The bytes are the sender's
// ciphertext; decrypted plaintext is never stored.
func (d *DB) SetMessagePGPSource(ctx context.Context, messageID int64, raw []byte) error {
	mk, _, _, err := d.sealedKey(ctx, messageID)
	if err != nil {
		return err
	}
	if mk != nil {
		box, err := mk.Seal(raw, rowBinding(messageID, "source")...)
		if err != nil {
			return err
		}
		if _, err := d.sql.ExecContext(ctx, `UPDATE mail_vault_sealed SET source = ? WHERE message_id = ?`, box, messageID); err != nil {
			return fmt.Errorf("storage: store sealed pgp source for message %d: %w", messageID, err)
		}
		return nil
	}
	_, err = d.sql.ExecContext(ctx,
		`INSERT INTO message_pgp (message_id, raw) VALUES (?, ?)
         ON CONFLICT(message_id) DO UPDATE SET raw = excluded.raw`, messageID, raw)
	if err != nil {
		return fmt.Errorf("storage: store pgp source for message %d: %w", messageID, err)
	}
	return nil
}

// MessagePGPSource returns a message's stored pgp source, or ErrNoPGPSource.
func (d *DB) MessagePGPSource(ctx context.Context, messageID int64) ([]byte, error) {
	mk, _, source, err := d.sealedKey(ctx, messageID)
	if errors.Is(err, ErrMessageNotFound) {
		return nil, ErrNoPGPSource
	}
	if err != nil {
		return nil, err
	}
	if mk != nil {
		if len(source) == 0 {
			return nil, ErrNoPGPSource
		}
		raw, err := mk.Open(source, rowBinding(messageID, "source")...)
		if err != nil {
			return nil, ErrSealedTampered
		}
		return raw, nil
	}
	var raw []byte
	err = d.sql.QueryRowContext(ctx,
		`SELECT raw FROM message_pgp WHERE message_id = ?`, messageID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoPGPSource
	}
	if err != nil {
		return nil, fmt.Errorf("storage: read pgp source for message %d: %w", messageID, err)
	}
	return raw, nil
}
