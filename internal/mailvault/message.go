package mailvault

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
	"sync"
)

// MessageKey seals and opens the fields of one message. A key made by
// NewMessageKey needs only the vault's public key, which is how mail synced
// while the vault is locked is sealed; the same key comes back from
// Keys.MessageKey only once the vault is unlocked.
//
// A MessageKey is safe for concurrent use: Wipe waits for any Seal or Open in
// progress, and after it both return ErrWiped, so a lock that wipes a key
// another goroutine is using can fail that call but never let it seal with
// a half-zeroed key.
type MessageKey struct {
	mu  sync.RWMutex
	key []byte
	uid []byte
	// EPK is the ephemeral public key stored with the message.
	EPK []byte
}

// NewMessageKey makes a fresh key for one message, sealed to the vault's
// public key. Its ephemeral private half is discarded at once.
func NewMessageKey(public, uid []byte) (*MessageKey, error) {
	peer, err := ecdh.X25519().NewPublicKey(public)
	if err != nil {
		return nil, fmt.Errorf("mailvault: vault public key: %w", err)
	}
	eph, err := ecdh.X25519().GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("mailvault: ephemeral key: %w", err)
	}
	shared, err := eph.ECDH(peer)
	if err != nil {
		return nil, fmt.Errorf("mailvault: key exchange: %w", err)
	}
	defer Wipe(shared)
	return deriveMessageKey(shared, eph.PublicKey().Bytes(), public, uid)
}

func deriveMessageKey(shared, epk, public, uid []byte) (*MessageKey, error) {
	salt := make([]byte, 0, len(epk)+len(public))
	salt = append(salt, epk...)
	salt = append(salt, public...)
	key, err := hkdf.Key(sha256.New, shared, salt, string(aad("message", string(uid))), KeySize)
	if err != nil {
		return nil, fmt.Errorf("mailvault: derive message key: %w", err)
	}
	return &MessageKey{key: key, uid: uid, EPK: append([]byte(nil), epk...)}, nil
}

// Seal encrypts plaintext for the place named by binding, typically the
// table, the row id and the column.
func (k *MessageKey) Seal(plaintext []byte, binding ...string) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.key == nil {
		return nil, ErrWiped
	}
	return seal(k.key, plaintext, aad(append([]string{"field", string(k.uid)}, binding...)...))
}

// Open decrypts a box sealed for the same binding. A box from another row or
// column, or one with any byte changed, is ErrOpen.
func (k *MessageKey) Open(box []byte, binding ...string) ([]byte, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.key == nil {
		return nil, ErrWiped
	}
	return open(k.key, box, aad(append([]string{"field", string(k.uid)}, binding...)...))
}

// Wipe zeroes the key.
func (k *MessageKey) Wipe() {
	k.mu.Lock()
	defer k.mu.Unlock()
	Wipe(k.key)
	k.key = nil
}
