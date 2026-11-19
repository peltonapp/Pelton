package storage

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// RevocationRecord is a cached answer from a certificate's issuing authority,
// together with when it stops being current.
type RevocationRecord struct {
	Fingerprint string
	Status      string
	Detail      string
	RevokedAt   time.Time
	CheckedAt   time.Time
	NextUpdate  time.Time
}

// Fresh reports whether the record can still be used at now. An authority that
// named no next update gets the caller's fallback lifetime, so a responder that
// says nothing does not mean asking it again on every open.
func (r RevocationRecord) Fresh(now time.Time, fallback time.Duration) bool {
	if !r.NextUpdate.IsZero() {
		return now.Before(r.NextUpdate)
	}
	return now.Before(r.CheckedAt.Add(fallback))
}

// ErrRevocationNotCached means the certificate has not been asked about yet.
var ErrRevocationNotCached = errors.New("storage: revocation not cached")

// RevocationFor returns the cached answer for a certificate, or
// ErrRevocationNotCached.
func (d *DB) RevocationFor(ctx context.Context, fingerprint string) (*RevocationRecord, error) {
	const query = `
SELECT fingerprint, status, detail, revoked_at, checked_at, next_update
FROM smime_revocation WHERE fingerprint = ?`
	var (
		r                      RevocationRecord
		revoked, checked, next string
	)
	err := d.sql.QueryRowContext(ctx, query, fingerprint).
		Scan(&r.Fingerprint, &r.Status, &r.Detail, &revoked, &checked, &next)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRevocationNotCached
	}
	if err != nil {
		return nil, fmt.Errorf("storage: revocation for %s: %w", fingerprint, err)
	}
	// a timestamp that will not parse is treated as absent, which only makes the
	// answer look older than it is and provokes a fresh check.
	r.RevokedAt, _ = parseTime(revoked)
	r.CheckedAt, _ = parseTime(checked)
	r.NextUpdate, _ = parseTime(next)
	return &r, nil
}

// SaveRevocation records an answer, replacing any earlier one for the same
// certificate.
func (d *DB) SaveRevocation(ctx context.Context, r RevocationRecord) error {
	const query = `
INSERT INTO smime_revocation (fingerprint, status, detail, revoked_at, checked_at, next_update)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(fingerprint) DO UPDATE SET
    status = excluded.status, detail = excluded.detail,
    revoked_at = excluded.revoked_at, checked_at = excluded.checked_at,
    next_update = excluded.next_update`
	_, err := d.sql.ExecContext(ctx, query, r.Fingerprint, r.Status, r.Detail,
		formatTime(r.RevokedAt), formatTime(r.CheckedAt), formatTime(r.NextUpdate))
	if err != nil {
		return fmt.Errorf("storage: save revocation for %s: %w", r.Fingerprint, err)
	}
	return nil
}

// ClearRevocations empties the cache. Turning the setting off leaves nothing
// behind, and turning it back on asks again rather than showing an answer from
// weeks ago as if it were current.
func (d *DB) ClearRevocations(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM smime_revocation`); err != nil {
		return fmt.Errorf("storage: clear revocations: %w", err)
	}
	return nil
}

// MessageSMIMECerts returns the signing certificate and its issuer for a
// message, in DER. It is a query of its own because the blob has no business in
// a message list.
func (d *DB) MessageSMIMECerts(ctx context.Context, messageID int64) ([][]byte, error) {
	var blob []byte
	err := d.sql.QueryRowContext(ctx,
		`SELECT smime_certs FROM messages WHERE id = ? AND `+notVaulted, messageID).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: smime certs for message %d: %w", messageID, err)
	}
	return DecodeCerts(blob), nil
}

// EncodeCerts packs certificates into one blob, each behind its length. A pair
// of certificates in one column beats a table whose only purpose is to hold the
// second one.
func EncodeCerts(certs [][]byte) []byte {
	if len(certs) == 0 {
		return nil
	}
	size := 0
	for _, c := range certs {
		size += 4 + len(c)
	}
	out := make([]byte, 0, size)
	for _, c := range certs {
		out = binary.BigEndian.AppendUint32(out, uint32(len(c)))
		out = append(out, c...)
	}
	return out
}

// DecodeCerts unpacks what EncodeCerts wrote. A blob that does not decode
// cleanly yields nothing rather than a partial chain: half a chain cannot be
// checked, and guessing at the rest is worse than saying so.
func DecodeCerts(blob []byte) [][]byte {
	var out [][]byte
	for len(blob) >= 4 {
		n := int(binary.BigEndian.Uint32(blob[:4]))
		blob = blob[4:]
		if n < 0 || n > len(blob) {
			return nil
		}
		out = append(out, blob[:n])
		blob = blob[n:]
	}
	if len(blob) != 0 {
		return nil
	}
	return out
}
