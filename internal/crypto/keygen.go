package crypto

import (
	"bytes"
	"crypto"
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Keys Pelton makes for itself use the curve every current OpenPGP program
// reads: an Ed25519 signing primary key with a Curve25519 encryption subkey,
// in the long established v4 format rather than the newer RFC 9580 one that
// older programs still reject.
const (
	// AutoKeyLifetime is how long a generated or extended key stays valid.
	AutoKeyLifetime = 2 * 365 * 24 * time.Hour
	// AutoKeyRenewWithin is how close to its expiry a key gets before Extend
	// pushes the expiry out again.
	AutoKeyRenewWithin = 180 * 24 * time.Hour
)

func autoKeyConfig(now time.Time, lifetime time.Duration) *packet.Config {
	return &packet.Config{
		Algorithm:       packet.PubKeyAlgoEdDSA,
		Curve:           packet.Curve25519,
		DefaultHash:     crypto.SHA256,
		DefaultCipher:   packet.CipherAES256,
		KeyLifetimeSecs: uint32(lifetime / time.Second),
		Time:            func() time.Time { return now },
	}
}

// Generate makes a new key pair for one address, protects its private half
// with passphrase and files it in both rings, the same way an imported key is
// kept. The name may be empty; characters a user id cannot hold are dropped.
func (s *PGPKeyStore) Generate(name, email string, passphrase []byte, now time.Time) (KeyInfo, error) {
	if strings.TrimSpace(email) == "" {
		return KeyInfo{}, fmt.Errorf("crypto: generate key: no address")
	}
	if len(passphrase) == 0 {
		return KeyInfo{}, fmt.Errorf("crypto: generate key: no passphrase")
	}
	cfg := autoKeyConfig(now, AutoKeyLifetime)
	ent, err := openpgp.NewEntity(cleanUserIDPart(name), "", cleanUserIDPart(email), cfg)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("crypto: generate key: %w", err)
	}
	info := describe(ent)
	if err := ent.EncryptPrivateKeys(passphrase, cfg); err != nil {
		return KeyInfo{}, fmt.Errorf("crypto: protect generated key: %w", err)
	}
	if err := s.merge(secringFile, openpgp.EntityList{ent}); err != nil {
		return KeyInfo{}, err
	}
	if err := s.merge(pubringFile, openpgp.EntityList{ent}); err != nil {
		return KeyInfo{}, err
	}
	info.Locked = true
	return info, nil
}

// cleanUserIDPart drops the characters go-crypto refuses in a user id.
func cleanUserIDPart(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '(', ')', '<', '>', 0:
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}

// Demote moves a key behind every other key in both rings, so a lookup by
// address finds another key for the same address first. The key itself stays,
// to read mail that was encrypted to it.
func (s *PGPKeyStore) Demote(fingerprint string) error {
	want := NormalizeFingerprint(fingerprint)
	for _, file := range []string{secringFile, pubringFile} {
		list, err := s.load(file)
		if err != nil {
			return err
		}
		var kept, moved openpgp.EntityList
		for _, ent := range list {
			if NormalizeFingerprint(Fingerprint(ent)) == want {
				moved = append(moved, ent)
			} else {
				kept = append(kept, ent)
			}
		}
		if len(moved) == 0 {
			continue
		}
		if err := s.write(file, append(kept, moved...)); err != nil {
			return err
		}
	}
	return nil
}

// Extend renews an own key that expires within AutoKeyRenewWithin, so it is
// valid for AutoKeyLifetime from now. The user id self-signatures are replaced
// by newer ones carrying the new expiry, which every program that refreshes
// the key adopts. It reports false and changes nothing for a key that does not
// expire or has time left.
func (s *PGPKeyStore) Extend(fingerprint string, passphrase []byte, now time.Time) (KeyInfo, bool, error) {
	ent, err := s.byFingerprint(fingerprint, true)
	if err != nil {
		return KeyInfo{}, false, err
	}
	info := describe(ent)
	if info.Expires.IsZero() || info.Expires.Sub(now) > AutoKeyRenewWithin {
		return info, false, nil
	}
	if err := unlock(ent, passphrase); err != nil {
		return KeyInfo{}, false, err
	}
	cfg := autoKeyConfig(now, AutoKeyLifetime)
	lifetime := uint32(now.Add(AutoKeyLifetime).Sub(ent.PrimaryKey.CreationTime) / time.Second)

	for id, ident := range ent.Identities {
		if ident.SelfSignature == nil {
			continue
		}
		renewed := *ident.SelfSignature
		renewed.CreationTime = now
		renewed.KeyLifetimeSecs = &lifetime
		renewed.Notations = nil
		if err := renewed.SignUserId(id, ent.PrimaryKey, ent.PrivateKey, cfg); err != nil {
			return KeyInfo{}, false, fmt.Errorf("crypto: renew user id: %w", err)
		}
		kept := []*packet.Signature{&renewed}
		for _, sig := range ident.Signatures {
			if !sig.CheckKeyIdOrFingerprint(ent.PrimaryKey) {
				kept = append(kept, sig)
			}
		}
		ident.SelfSignature = &renewed
		ident.Signatures = kept
	}

	// serialising and reading back gives an entity whose packets match what is
	// written, before its secrets are locked again.
	var buf bytes.Buffer
	if err := ent.SerializePrivateWithoutSigning(&buf, cfg); err != nil {
		return KeyInfo{}, false, fmt.Errorf("crypto: serialize renewed key: %w", err)
	}
	list, err := openpgp.ReadKeyRing(&buf)
	if err != nil || len(list) != 1 {
		return KeyInfo{}, false, fmt.Errorf("crypto: read renewed key: %v", err)
	}
	renewed := list[0]
	if err := renewed.EncryptPrivateKeys(passphrase, cfg); err != nil {
		return KeyInfo{}, false, fmt.Errorf("crypto: protect renewed key: %w", err)
	}
	if err := s.merge(secringFile, openpgp.EntityList{renewed}); err != nil {
		return KeyInfo{}, false, err
	}
	if err := s.merge(pubringFile, openpgp.EntityList{renewed}); err != nil {
		return KeyInfo{}, false, err
	}
	out := describe(renewed)
	out.Locked = true
	return out, true, nil
}
