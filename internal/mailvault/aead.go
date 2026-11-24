// Package mailvault is the cryptography behind the mail vault: folders and
// accounts whose cached mail is encrypted on disk with a passphrase of its
// own.
//
// Each vault has a random 32-byte data key. The data key is stored twice,
// wrapped under a key derived from the passphrase and under a key derived
// from a recovery code, both with Argon2id and stored parameters and salt.
// Changing the passphrase rewraps the data key and touches no mail.
//
// Each vault also has an X25519 key pair. The public half is stored in clear
// so mail synced while the vault is locked can still be sealed; the private
// half is encrypted under the data key. Every message gets its own key from
// an ephemeral-static X25519 exchange with the vault's public key, run
// through HKDF-SHA256. Fields are sealed with XChaCha20-Poly1305 under random
// 192-bit nonces, with associated data naming the vault, the row and the
// column, so a ciphertext moved to another row or column fails to open.
//
// Keys live in byte slices that Wipe zeroes. Go cannot promise that no copy
// survives elsewhere in memory: the runtime may have moved a slice, and the
// standard library's X25519 private key keeps its own copy that can only be
// dropped, not zeroed.
package mailvault

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// KeySize is the size of every symmetric key in a vault.
const KeySize = 32

// boxVersion is the first byte of every ciphertext this package writes.
const boxVersion byte = 1

// maxSealPlaintext is the largest plaintext seal accepts.
const maxSealPlaintext = 1 << 30

// aadLabel opens every associated data string, so a ciphertext from another
// purpose or format version never authenticates here.
const aadLabel = "pelton-mailvault/v1"

// ErrOpen is returned when a ciphertext does not authenticate: a wrong key,
// a changed byte, a truncated box, or a box moved to another row or column.
var ErrOpen = errors.New("mailvault: ciphertext does not authenticate")

// aad joins its parts with a length prefix each, so no two different lists of
// parts can produce the same bytes.
func aad(parts ...string) []byte {
	out := make([]byte, 0, 64)
	out = binary.AppendUvarint(out, uint64(len(aadLabel)))
	out = append(out, aadLabel...)
	for _, p := range parts {
		out = binary.AppendUvarint(out, uint64(len(p)))
		out = append(out, p...)
	}
	return out
}

// seal encrypts plaintext under key with a fresh random nonce. The result is
// the version byte, the nonce and the ciphertext with its tag.
func seal(key, plaintext, ad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("mailvault: cipher: %w", err)
	}
	if len(plaintext) > maxSealPlaintext {
		return nil, errors.New("mailvault: plaintext too large to seal")
	}
	out := make([]byte, 1+aead.NonceSize(), 1+aead.NonceSize()+len(plaintext)+aead.Overhead())
	out[0] = boxVersion
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, fmt.Errorf("mailvault: nonce: %w", err)
	}
	return aead.Seal(out, out[1:], plaintext, ad), nil
}

// open reverses seal. Any failure, including a box too short to hold a nonce
// and a tag, is ErrOpen.
func open(key, box, ad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("mailvault: cipher: %w", err)
	}
	if len(box) < 1+aead.NonceSize()+aead.Overhead() || box[0] != boxVersion {
		return nil, ErrOpen
	}
	nonce := box[1 : 1+aead.NonceSize()]
	plain, err := aead.Open(nil, nonce, box[1+aead.NonceSize():], ad)
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}

// randomBytes returns n bytes from the system's secure random source.
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("mailvault: random: %w", err)
	}
	return b, nil
}

// Wipe zeroes b.
func Wipe(b []byte) {
	clear(b)
}
