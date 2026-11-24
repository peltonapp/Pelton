package crypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Moving an own key to another device: a passphrase protected backup file, and
// the Autocrypt Setup Message (Autocrypt Level 1, section 4.4), which carries
// the key encrypted with a 36 digit setup code the user types on the other
// device.

// ErrWrongSetupCode means a setup message did not open with the code given.
var ErrWrongSetupCode = errors.New("crypto: the setup code does not open this message")

// setupCodeGroups and setupCodeGroupLen shape the code: nine groups of four.
const (
	setupCodeGroups   = 9
	setupCodeGroupLen = 4
	preferHeader      = "Autocrypt-Prefer-Encrypt"
)

// NewSetupCode returns a fresh setup code, nine dash separated groups of four
// random digits.
func NewSetupCode() (string, error) {
	digits := make([]byte, 0, setupCodeGroups*setupCodeGroupLen)
	ten := big.NewInt(10)
	for range setupCodeGroups * setupCodeGroupLen {
		n, err := rand.Int(rand.Reader, ten)
		if err != nil {
			return "", fmt.Errorf("crypto: setup code: %w", err)
		}
		digits = append(digits, byte('0'+n.Int64()))
	}
	code, _ := NormalizeSetupCode(string(digits))
	return code, nil
}

// NormalizeSetupCode reads a code the way a user may type it, with or without
// dashes and spaces, and returns it in its dashed form. It reports false when
// it is not 36 digits.
func NormalizeSetupCode(s string) (string, bool) {
	var digits []byte
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits = append(digits, byte(r))
		case r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r':
		default:
			return "", false
		}
	}
	if len(digits) != setupCodeGroups*setupCodeGroupLen {
		return "", false
	}
	groups := make([]string, 0, setupCodeGroups)
	for i := 0; i < len(digits); i += setupCodeGroupLen {
		groups = append(groups, string(digits[i:i+setupCodeGroupLen]))
	}
	return strings.Join(groups, "-"), true
}

// unlockedOwnKey loads an own key and unlocks it with its stored passphrase.
func (s *PGPKeyStore) unlockedOwnKey(fingerprint string, current []byte) (*openpgp.Entity, error) {
	ent, err := s.byFingerprint(fingerprint, true)
	if err != nil {
		return nil, err
	}
	if err := unlock(ent, current); err != nil {
		return nil, err
	}
	return ent, nil
}

// ExportProtected returns an own key, armored, with its private half protected
// by newPass instead of the passphrase it is kept with, so a backup file opens
// with something the user chose and knows.
func (s *PGPKeyStore) ExportProtected(fingerprint string, current, newPass []byte) ([]byte, error) {
	if len(newPass) == 0 {
		return nil, fmt.Errorf("crypto: a backup needs a passphrase")
	}
	ent, err := s.unlockedOwnKey(fingerprint, current)
	if err != nil {
		return nil, err
	}
	if err := ent.EncryptPrivateKeys(newPass, cryptoConfig()); err != nil {
		return nil, fmt.Errorf("crypto: protect backup: %w", err)
	}
	return armorPrivate(ent, nil)
}

func armorPrivate(ent *openpgp.Entity, headers map[string]string) ([]byte, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, privateKeyBlock, headers)
	if err != nil {
		return nil, fmt.Errorf("crypto: armor key: %w", err)
	}
	if err := ent.SerializePrivateWithoutSigning(w, nil); err != nil {
		w.Close()
		return nil, fmt.Errorf("crypto: serialize key: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("crypto: finish armor: %w", err)
	}
	return buf.Bytes(), nil
}

// SetupPayload builds the encrypted payload of an Autocrypt Setup Message: the
// own key, unprotected inside, encrypted with code.
func (s *PGPKeyStore) SetupPayload(fingerprint string, current []byte, code, preferEncrypt string) ([]byte, error) {
	normal, ok := NormalizeSetupCode(code)
	if !ok {
		return nil, fmt.Errorf("crypto: malformed setup code")
	}
	ent, err := s.unlockedOwnKey(fingerprint, current)
	if err != nil {
		return nil, err
	}
	inner, err := armorPrivate(ent, map[string]string{preferHeader: preferEncrypt})
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	w, err := armor.Encode(&out, pgpMessageBlock, map[string]string{
		"Passphrase-Format": "numeric9x4",
		"Passphrase-Begin":  normal[:2],
	})
	if err != nil {
		return nil, fmt.Errorf("crypto: armor setup message: %w", err)
	}
	cfg := &packet.Config{DefaultCipher: packet.CipherAES128, DefaultHash: cryptoConfig().DefaultHash}
	plain, err := openpgp.SymmetricallyEncrypt(w, []byte(normal), nil, cfg)
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("crypto: encrypt setup message: %w", err)
	}
	if _, err := plain.Write(inner); err != nil {
		plain.Close()
		w.Close()
		return nil, fmt.Errorf("crypto: encrypt setup message: %w", err)
	}
	if err := plain.Close(); err != nil {
		w.Close()
		return nil, fmt.Errorf("crypto: encrypt setup message: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("crypto: finish armor: %w", err)
	}
	return out.Bytes(), nil
}

// ImportSetupPayload opens a setup message payload with code, protects the key
// it carries with newPass and files it in both rings. data may be the html
// file the payload travels in. It returns the key and the prefer-encrypt
// setting the other device sent along.
func (s *PGPKeyStore) ImportSetupPayload(data []byte, code string, newPass []byte) (KeyInfo, string, error) {
	normal, ok := NormalizeSetupCode(code)
	if !ok {
		return KeyInfo{}, "", ErrWrongSetupCode
	}
	start := bytes.Index(data, []byte("-----BEGIN PGP MESSAGE-----"))
	if start < 0 {
		return KeyInfo{}, "", ErrMalformedArmor
	}
	block, err := armor.Decode(bytes.NewReader(data[start:]))
	if err != nil {
		return KeyInfo{}, "", ErrMalformedArmor
	}
	tried := false
	prompt := func(_ []openpgp.Key, symmetric bool) ([]byte, error) {
		if !symmetric || tried {
			return nil, ErrWrongSetupCode
		}
		tried = true
		return []byte(normal), nil
	}
	md, err := openpgp.ReadMessage(block.Body, openpgp.EntityList{}, prompt, nil)
	if err != nil {
		return KeyInfo{}, "", ErrWrongSetupCode
	}
	inner, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		return KeyInfo{}, "", ErrWrongSetupCode
	}

	keyBlock, err := armor.Decode(bytes.NewReader(inner))
	if err != nil {
		return KeyInfo{}, "", ErrMalformedArmor
	}
	prefer := keyBlock.Header[preferHeader]
	list, err := openpgp.ReadKeyRing(keyBlock.Body)
	if err != nil || len(list) == 0 || list[0].PrivateKey == nil {
		return KeyInfo{}, "", ErrNoKeysInFile
	}
	ent := list[0]
	if entityLocked(ent) {
		return KeyInfo{}, "", fmt.Errorf("crypto: the key in the setup message is locked")
	}
	info := describe(ent)
	if err := ent.EncryptPrivateKeys(newPass, cryptoConfig()); err != nil {
		return KeyInfo{}, "", fmt.Errorf("crypto: protect imported key: %w", err)
	}
	if err := s.merge(secringFile, openpgp.EntityList{ent}); err != nil {
		return KeyInfo{}, "", err
	}
	if err := s.merge(pubringFile, openpgp.EntityList{ent}); err != nil {
		return KeyInfo{}, "", err
	}
	info.Locked = true
	return info, prefer, nil
}
