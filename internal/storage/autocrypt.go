package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AutocryptPeer is the stored Autocrypt state of one correspondent address.
type AutocryptPeer struct {
	Addr               string
	LastSeen           time.Time
	AutocryptTimestamp time.Time
	PublicKey          []byte
	PreferEncrypt      string
	GossipTimestamp    time.Time
	GossipKey          []byte
}

// CachedKey is what one lookup source answered for an address. Key is nil
// when the source had nothing.
type CachedKey struct {
	Addr      string
	Source    string
	Key       []byte
	FetchedAt time.Time
}

// AcceptedKey is the key accepted for sending to an address, with the user's
// encryption preference for that address.
type AcceptedKey struct {
	Addr        string
	Fingerprint string
	Source      string
	AcceptedAt  time.Time
	Verified    bool
	Preference  string
}

// KeySighting is the first time a key was seen for an address.
type KeySighting struct {
	Fingerprint string
	Source      string
	FirstSeen   time.Time
}

// KeyLookupLogEntry is one request a key lookup made.
type KeyLookupLogEntry struct {
	ID      int64
	At      time.Time
	Source  string
	Addr    string
	Outcome string
}

// keyLookupLogKeep is how many log entries are kept; older ones are dropped
// as new ones arrive.
const keyLookupLogKeep = 1000

func unixSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnix(s int64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(s, 0).UTC()
}

// AutocryptPeerFor returns the stored state for addr, and false when there is
// none.
func (d *DB) AutocryptPeerFor(ctx context.Context, addr string) (AutocryptPeer, bool, error) {
	const query = `
SELECT addr, last_seen, autocrypt_timestamp, public_key, prefer_encrypt, gossip_timestamp, gossip_key
FROM autocrypt_peers WHERE addr = ?`
	p, err := scanPeer(d.sql.QueryRowContext(ctx, query, addr))
	if errors.Is(err, sql.ErrNoRows) {
		return AutocryptPeer{}, false, nil
	}
	if err != nil {
		return AutocryptPeer{}, false, fmt.Errorf("storage: autocrypt peer %s: %w", addr, err)
	}
	return p, true, nil
}

func scanPeer(row rowScanner) (AutocryptPeer, error) {
	var p AutocryptPeer
	var last, ts, gossip int64
	if err := row.Scan(&p.Addr, &last, &ts, &p.PublicKey, &p.PreferEncrypt, &gossip, &p.GossipKey); err != nil {
		return AutocryptPeer{}, err
	}
	p.LastSeen, p.AutocryptTimestamp, p.GossipTimestamp = fromUnix(last), fromUnix(ts), fromUnix(gossip)
	return p, nil
}

// SaveAutocryptPeer writes the state for one address, replacing what was
// there.
func (d *DB) SaveAutocryptPeer(ctx context.Context, p AutocryptPeer) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO autocrypt_peers (addr, last_seen, autocrypt_timestamp, public_key, prefer_encrypt, gossip_timestamp, gossip_key)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(addr) DO UPDATE SET
    last_seen = excluded.last_seen,
    autocrypt_timestamp = excluded.autocrypt_timestamp,
    public_key = excluded.public_key,
    prefer_encrypt = excluded.prefer_encrypt,
    gossip_timestamp = excluded.gossip_timestamp,
    gossip_key = excluded.gossip_key`,
		p.Addr, unixSeconds(p.LastSeen), unixSeconds(p.AutocryptTimestamp), p.PublicKey, p.PreferEncrypt, unixSeconds(p.GossipTimestamp), p.GossipKey)
	if err != nil {
		return fmt.Errorf("storage: save autocrypt peer %s: %w", p.Addr, err)
	}
	return nil
}

// ListAutocryptPeers returns every peer with a key, by address.
func (d *DB) ListAutocryptPeers(ctx context.Context) ([]AutocryptPeer, error) {
	rows, err := d.sql.QueryContext(ctx, `
SELECT addr, last_seen, autocrypt_timestamp, public_key, prefer_encrypt, gossip_timestamp, gossip_key
FROM autocrypt_peers WHERE public_key IS NOT NULL OR gossip_key IS NOT NULL ORDER BY addr`)
	if err != nil {
		return nil, fmt.Errorf("storage: list autocrypt peers: %w", err)
	}
	defer rows.Close()
	var out []AutocryptPeer
	for rows.Next() {
		p, err := scanPeer(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: list autocrypt peers: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CachedKeyFor returns what source last answered for addr, and false when it
// was never asked.
func (d *DB) CachedKeyFor(ctx context.Context, addr, source string) (CachedKey, bool, error) {
	var c CachedKey
	var at int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT addr, source, public_key, fetched_at FROM key_lookup_cache WHERE addr = ? AND source = ?`,
		addr, source).Scan(&c.Addr, &c.Source, &c.Key, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return CachedKey{}, false, nil
	}
	if err != nil {
		return CachedKey{}, false, fmt.Errorf("storage: cached key for %s: %w", addr, err)
	}
	c.FetchedAt = fromUnix(at)
	return c, true, nil
}

// CachedKeysFor returns every cached answer for addr.
func (d *DB) CachedKeysFor(ctx context.Context, addr string) ([]CachedKey, error) {
	return d.cachedKeys(ctx, `SELECT addr, source, public_key, fetched_at FROM key_lookup_cache WHERE addr = ? ORDER BY source`, addr)
}

// ListFoundKeys returns every cached answer that holds a key.
func (d *DB) ListFoundKeys(ctx context.Context) ([]CachedKey, error) {
	return d.cachedKeys(ctx, `SELECT addr, source, public_key, fetched_at FROM key_lookup_cache WHERE public_key IS NOT NULL ORDER BY addr, source`)
}

func (d *DB) cachedKeys(ctx context.Context, query string, args ...any) ([]CachedKey, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: cached keys: %w", err)
	}
	defer rows.Close()
	var out []CachedKey
	for rows.Next() {
		var c CachedKey
		var at int64
		if err := rows.Scan(&c.Addr, &c.Source, &c.Key, &at); err != nil {
			return nil, fmt.Errorf("storage: cached keys: %w", err)
		}
		c.FetchedAt = fromUnix(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveCachedKey records what source answered for addr at a time; a nil key
// records that it had none.
func (d *DB) SaveCachedKey(ctx context.Context, c CachedKey) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO key_lookup_cache (addr, source, public_key, fetched_at) VALUES (?, ?, ?, ?)
ON CONFLICT(addr, source) DO UPDATE SET public_key = excluded.public_key, fetched_at = excluded.fetched_at`,
		c.Addr, c.Source, c.Key, unixSeconds(c.FetchedAt))
	if err != nil {
		return fmt.Errorf("storage: cache key for %s: %w", c.Addr, err)
	}
	return nil
}

// RecordKeySeen notes that a key was seen for addr at a time, keeping the
// earliest time and the source it was first seen from.
func (d *DB) RecordKeySeen(ctx context.Context, addr, fingerprint, source string, at time.Time) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO contact_key_history (addr, fingerprint, source, first_seen) VALUES (?, ?, ?, ?)
ON CONFLICT(addr, fingerprint) DO UPDATE SET
    source = CASE WHEN excluded.first_seen < first_seen THEN excluded.source ELSE source END,
    first_seen = MIN(first_seen, excluded.first_seen)`,
		addr, fingerprint, source, unixSeconds(at))
	if err != nil {
		return fmt.Errorf("storage: record key for %s: %w", addr, err)
	}
	return nil
}

// KeySightingFor returns when a key was first seen for addr, and false when it
// never was.
func (d *DB) KeySightingFor(ctx context.Context, addr, fingerprint string) (KeySighting, bool, error) {
	var s KeySighting
	var at int64
	err := d.sql.QueryRowContext(ctx,
		`SELECT fingerprint, source, first_seen FROM contact_key_history WHERE addr = ? AND fingerprint = ?`,
		addr, fingerprint).Scan(&s.Fingerprint, &s.Source, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return KeySighting{}, false, nil
	}
	if err != nil {
		return KeySighting{}, false, fmt.Errorf("storage: key sighting for %s: %w", addr, err)
	}
	s.FirstSeen = fromUnix(at)
	return s, true, nil
}

// AcceptedKeyFor returns the accepted key and preference for addr, and false
// when there is no record.
func (d *DB) AcceptedKeyFor(ctx context.Context, addr string) (AcceptedKey, bool, error) {
	c, err := scanAcceptedKey(d.sql.QueryRowContext(ctx,
		`SELECT addr, fingerprint, source, accepted_at, verified, preference FROM contact_keys WHERE addr = ?`, addr))
	if errors.Is(err, sql.ErrNoRows) {
		return AcceptedKey{}, false, nil
	}
	if err != nil {
		return AcceptedKey{}, false, fmt.Errorf("storage: contact key for %s: %w", addr, err)
	}
	return c, true, nil
}

func scanAcceptedKey(row rowScanner) (AcceptedKey, error) {
	var c AcceptedKey
	var at int64
	var verified int
	if err := row.Scan(&c.Addr, &c.Fingerprint, &c.Source, &at, &verified, &c.Preference); err != nil {
		return AcceptedKey{}, err
	}
	c.AcceptedAt, c.Verified = fromUnix(at), verified != 0
	return c, nil
}

// SaveAcceptedKey writes the record for one address.
func (d *DB) SaveAcceptedKey(ctx context.Context, c AcceptedKey) error {
	verified := 0
	if c.Verified {
		verified = 1
	}
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO contact_keys (addr, fingerprint, source, accepted_at, verified, preference) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(addr) DO UPDATE SET
    fingerprint = excluded.fingerprint, source = excluded.source, accepted_at = excluded.accepted_at,
    verified = excluded.verified, preference = excluded.preference`,
		c.Addr, c.Fingerprint, c.Source, unixSeconds(c.AcceptedAt), verified, c.Preference)
	if err != nil {
		return fmt.Errorf("storage: save contact key for %s: %w", c.Addr, err)
	}
	return nil
}

// ListAcceptedKeys returns every contact record, by address.
func (d *DB) ListAcceptedKeys(ctx context.Context) ([]AcceptedKey, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT addr, fingerprint, source, accepted_at, verified, preference FROM contact_keys ORDER BY addr`)
	if err != nil {
		return nil, fmt.Errorf("storage: list contact keys: %w", err)
	}
	defer rows.Close()
	var out []AcceptedKey
	for rows.Next() {
		c, err := scanAcceptedKey(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: list contact keys: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ForgetContactKeys removes everything learned about addr: its Autocrypt
// state, cached lookups, key history and accepted key.
func (d *DB) ForgetContactKeys(ctx context.Context, addr string) error {
	for _, table := range []string{"autocrypt_peers", "key_lookup_cache", "contact_key_history", "contact_keys"} {
		if _, err := d.sql.ExecContext(ctx, `DELETE FROM `+table+` WHERE addr = ?`, addr); err != nil {
			return fmt.Errorf("storage: forget keys of %s: %w", addr, err)
		}
	}
	return nil
}

// AddKeyLookupLog records one lookup request and drops the oldest entries
// past the kept number.
func (d *DB) AddKeyLookupLog(ctx context.Context, e KeyLookupLogEntry) error {
	if _, err := d.sql.ExecContext(ctx,
		`INSERT INTO key_lookup_log (at, source, addr, outcome) VALUES (?, ?, ?, ?)`,
		unixSeconds(e.At), e.Source, e.Addr, e.Outcome); err != nil {
		return fmt.Errorf("storage: log key lookup: %w", err)
	}
	if _, err := d.sql.ExecContext(ctx,
		`DELETE FROM key_lookup_log WHERE id <= (SELECT MAX(id) FROM key_lookup_log) - ?`, keyLookupLogKeep); err != nil {
		return fmt.Errorf("storage: trim key lookup log: %w", err)
	}
	return nil
}

// ListKeyLookupLog returns the newest entries first, at most limit of them.
func (d *DB) ListKeyLookupLog(ctx context.Context, limit int) ([]KeyLookupLogEntry, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, at, source, addr, outcome FROM key_lookup_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list key lookup log: %w", err)
	}
	defer rows.Close()
	var out []KeyLookupLogEntry
	for rows.Next() {
		var e KeyLookupLogEntry
		var at int64
		if err := rows.Scan(&e.ID, &at, &e.Source, &e.Addr, &e.Outcome); err != nil {
			return nil, fmt.Errorf("storage: list key lookup log: %w", err)
		}
		e.At = fromUnix(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ClearKeyLookupLog removes every log entry.
func (d *DB) ClearKeyLookupLog(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM key_lookup_log`); err != nil {
		return fmt.Errorf("storage: clear key lookup log: %w", err)
	}
	return nil
}
