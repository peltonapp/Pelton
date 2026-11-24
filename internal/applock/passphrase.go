package applock

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// scrypt cost parameters for new hashes. Stored hashes carry their own, so
// raising these later still verifies passphrases set before.
const (
	scryptN      = 1 << 15
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 32
	saltLen      = 16
)

// hashScheme names the format at the start of an encoded hash.
const hashScheme = "scrypt"

// ErrBadHash is returned for a stored hash that is not in the expected form.
var ErrBadHash = errors.New("applock: malformed passphrase hash")

// HashPassphrase returns a salted scrypt hash of passphrase, encoded as
// scrypt$N$r$p$salt$key with the salt and key in unpadded base64. The
// passphrase itself is never part of the result.
func HashPassphrase(passphrase string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%d$%d$%s$%s", hashScheme, scryptN, scryptR, scryptP, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassphrase reports whether passphrase matches an encoded hash from
// HashPassphrase. A malformed hash returns ErrBadHash.
func VerifyPassphrase(passphrase, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != hashScheme {
		return false, ErrBadHash
	}
	n, errN := strconv.Atoi(parts[1])
	r, errR := strconv.Atoi(parts[2])
	p, errP := strconv.Atoi(parts[3])
	if errN != nil || errR != nil || errP != nil || n <= 1 || r <= 0 || p <= 0 {
		return false, ErrBadHash
	}
	enc := base64.RawStdEncoding
	salt, errS := enc.DecodeString(parts[4])
	want, errK := enc.DecodeString(parts[5])
	if errS != nil || errK != nil || len(salt) == 0 || len(want) == 0 {
		return false, ErrBadHash
	}
	got, err := scrypt.Key([]byte(passphrase), salt, n, r, p, len(want))
	if err != nil {
		return false, ErrBadHash
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
