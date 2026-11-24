package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Why a found key cannot be encrypted to.
const (
	KeyProblemExpired      = "expired"
	KeyProblemRevoked      = "revoked"
	KeyProblemNoEncryption = "noencryption"
	KeyProblemNoAddress    = "noaddress"
)

// ErrUnusableKey means key data parsed but holds no key that can be encrypted
// to now.
var ErrUnusableKey = errors.New("crypto: no usable key")

// PublicKeyInfo describes one correspondent's key found outside the keyring,
// in an Autocrypt header or a lookup. It carries no key material.
type PublicKeyInfo struct {
	Fingerprint string
	Emails      []string
	Created     time.Time
	Expires     time.Time
	// Usable is true when the key can be encrypted to now; Problem says why
	// not otherwise.
	Usable  bool
	Problem string
}

// InspectPublicKey reads one key, armored or binary, and says whether it can
// be encrypted to at now. Data holding several keys is described by its first.
func InspectPublicKey(data []byte, now time.Time) (PublicKeyInfo, error) {
	list, err := readKeys(data)
	if err != nil {
		return PublicKeyInfo{}, err
	}
	if len(list) == 0 {
		return PublicKeyInfo{}, ErrNoKeysInFile
	}
	return inspect(list[0], now), nil
}

// KeyUsable reports whether data holds a key that can be encrypted to at now.
func KeyUsable(data []byte, now time.Time) bool {
	info, err := InspectPublicKey(data, now)
	return err == nil && info.Usable
}

func inspect(ent *openpgp.Entity, now time.Time) PublicKeyInfo {
	d := describe(ent)
	info := PublicKeyInfo{Fingerprint: d.Fingerprint, Emails: d.Emails, Created: d.Created, Expires: d.Expires}
	_, canEncrypt := ent.EncryptionKey(now)
	switch {
	case ent.Revoked(now):
		info.Problem = KeyProblemRevoked
	case !d.Expires.IsZero() && !d.Expires.After(now):
		info.Problem = KeyProblemExpired
	case !canEncrypt:
		info.Problem = KeyProblemNoEncryption
	default:
		info.Usable = true
	}
	return info
}

// SelectKeyFor picks, from key data that may hold several keys, the first one
// that carries a user id for email and can be encrypted to at now, and returns
// it as binary key data. A lookup result is only trusted for the address it
// was asked about, so a key without that address is never chosen.
func SelectKeyFor(data []byte, email string, now time.Time) ([]byte, PublicKeyInfo, error) {
	list, err := readKeys(data)
	if err != nil {
		return nil, PublicKeyInfo{}, err
	}
	target := strings.ToLower(strings.TrimSpace(email))
	var last PublicKeyInfo
	for _, ent := range list {
		info := inspect(ent, now)
		if !hasEmail(ent, target) {
			if last.Problem == "" {
				last = info
				last.Problem = KeyProblemNoAddress
			}
			continue
		}
		if !info.Usable {
			last = info
			continue
		}
		var buf bytes.Buffer
		if err := ent.Serialize(&buf); err != nil {
			return nil, PublicKeyInfo{}, fmt.Errorf("crypto: serialize key: %w", err)
		}
		return buf.Bytes(), info, nil
	}
	return nil, last, ErrUnusableKey
}

func hasEmail(ent *openpgp.Entity, target string) bool {
	for _, id := range ent.Identities {
		if id.UserId != nil && strings.ToLower(id.UserId.Email) == target {
			return true
		}
	}
	return false
}

// AutocryptKeyData returns the smallest form of an own key that still works,
// for the Autocrypt header: the primary key, the user id for email with its
// self-signature, and the current encryption subkey with its binding. A key
// with no user id for email keeps its primary user id instead.
func (s *PGPKeyStore) AutocryptKeyData(fingerprint, email string, now time.Time) ([]byte, error) {
	ent, err := s.byFingerprint(fingerprint, false)
	if err != nil {
		return nil, err
	}
	return minimalKey(ent, email, now)
}

func minimalKey(ent *openpgp.Entity, email string, now time.Time) ([]byte, error) {
	enc, ok := ent.EncryptionKey(now)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnusableKey, Fingerprint(ent))
	}
	small := &openpgp.Entity{
		PrimaryKey: ent.PrimaryKey,
		Identities: map[string]*openpgp.Identity{},
		Signatures: ent.Signatures,
	}
	only := func(id *openpgp.Identity) *openpgp.Identity {
		return &openpgp.Identity{Name: id.Name, UserId: id.UserId, SelfSignature: id.SelfSignature, Signatures: []*packet.Signature{id.SelfSignature}}
	}
	target := strings.ToLower(strings.TrimSpace(email))
	for name, id := range ent.Identities {
		if id.SelfSignature != nil && strings.ToLower(id.UserId.Email) == target {
			small.Identities[name] = only(id)
			break
		}
	}
	if len(small.Identities) == 0 {
		if _, primary := ent.PrimarySelfSignature(); primary != nil {
			small.Identities[primary.Name] = only(primary)
		}
	}
	for _, sk := range ent.Subkeys {
		if sk.PublicKey.KeyId == enc.PublicKey.KeyId {
			small.Subkeys = append(small.Subkeys, openpgp.Subkey{PublicKey: sk.PublicKey, Sig: sk.Sig})
		}
	}
	var buf bytes.Buffer
	if err := small.Serialize(&buf); err != nil {
		return nil, fmt.Errorf("crypto: serialize key: %w", err)
	}
	return buf.Bytes(), nil
}

// RecipientKeyData returns the public key the keyring holds for email, as
// binary key data, when it can be encrypted to at now.
func (s *PGPKeyStore) RecipientKeyData(email string, now time.Time) ([]byte, PublicKeyInfo, error) {
	ent, err := s.RecipientKey(email)
	if err != nil {
		return nil, PublicKeyInfo{}, err
	}
	info := inspect(ent, now)
	if !info.Usable {
		return nil, info, ErrUnusableKey
	}
	var buf bytes.Buffer
	if err := ent.Serialize(&buf); err != nil {
		return nil, PublicKeyInfo{}, fmt.Errorf("crypto: serialize key: %w", err)
	}
	return buf.Bytes(), info, nil
}
