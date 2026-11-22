package desktop

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/scrypt"
)

// scrypt cost parameters for deriving the export encryption key from the
// user's chosen password. N=2^15 is deliberately expensive (a fraction of a
// second on a modern machine) since the only thing protecting the exported
// mailbox passwords is this one password the user typed into a dialog.
const (
	scryptN = 1 << 15
	scryptR = 8
	scryptP = 1
	keyLen  = 32
	saltLen = 16
)

// backupScryptN is the N both directions use: scryptN, which a backup
// written by any build needs. Tests that only round-trip within the process
// lower it, since N=2^15 costs seconds under -race.
var backupScryptN = scryptN

// encryptedBlob is a password-encrypted secret embedded in a backup file. The
// salt travels with the blob (as every KDF salt should) so the same password
// re-derives the exact key on import.
type encryptedBlob struct {
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

// encryptWithPassword derives a key from password via scrypt and seals
// plaintext with AES-256-GCM under a fresh random salt and nonce.
func encryptWithPassword(password string, plaintext []byte) (*encryptedBlob, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("pelton: generate salt: %w", err)
	}
	key, err := scrypt.Key([]byte(password), salt, backupScryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return nil, fmt.Errorf("pelton: derive key: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("pelton: generate nonce: %w", err)
	}
	return &encryptedBlob{
		Salt:       salt,
		Nonce:      nonce,
		Ciphertext: gcm.Seal(nil, nonce, plaintext, nil),
	}, nil
}

// decryptWithPassword reverses encryptWithPassword. An error here almost
// always means the password was wrong: GCM's auth tag check fails on any
// key mismatch, which is indistinguishable from corruption, so the message
// is deliberately generic rather than implying anything more specific.
func decryptWithPassword(password string, blob *encryptedBlob) ([]byte, error) {
	if blob == nil {
		return nil, errors.New("pelton: no encrypted data")
	}
	key, err := scrypt.Key([]byte(password), blob.Salt, backupScryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return nil, fmt.Errorf("pelton: derive key: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(blob.Nonce) != gcm.NonceSize() {
		return nil, errors.New("pelton: corrupt encrypted data")
	}
	plaintext, err := gcm.Open(nil, blob.Nonce, blob.Ciphertext, nil)
	if err != nil {
		return nil, errors.New("pelton: wrong password or corrupt data")
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("pelton: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("pelton: init gcm: %w", err)
	}
	return gcm, nil
}
