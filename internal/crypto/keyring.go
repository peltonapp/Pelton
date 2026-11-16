package crypto

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Permissions on the key material. The directory is owner-only because a
// private keyring in a world-readable directory is a real compromise, not an
// untidiness; the files match.
const (
	keyDirPerm  os.FileMode = 0o700
	keyFilePerm os.FileMode = 0o600
)

// armor block types for whole keys, as gpg writes them.
const (
	publicKeyBlock  = "PGP PUBLIC KEY BLOCK"
	privateKeyBlock = "PGP PRIVATE KEY BLOCK"
)

// maxKeyFileSize caps an imported file. Real keys are kilobytes; this only
// exists so pointing the importer at a disk image fails fast instead of
// reading it into memory.
const maxKeyFileSize = 1 << 20

// ErrKeyNotFound means no key in either ring carries the given fingerprint.
var ErrKeyNotFound = fmt.Errorf("crypto: key not found")

// ErrNoKeysInFile means an imported file parsed but held no OpenPGP keys.
var ErrNoKeysInFile = fmt.Errorf("crypto: no OpenPGP keys in that file")

// KeyInfo describes one key for display. It deliberately carries no key
// material: the UI never needs it and it should not cross the bind layer.
type KeyInfo struct {
	// Fingerprint is the primary key fingerprint, uppercase hex, no spaces.
	Fingerprint string
	// Name and Email come from the first user id; Emails lists every address
	// the key claims, which is what lookups match against.
	Name   string
	Email  string
	Emails []string
	// Created is when the primary key was made; Expires is zero when it never
	// expires.
	Created time.Time
	Expires time.Time
	// HasPrivate is true for a key this user can sign or decrypt with.
	HasPrivate bool
	// Locked is true when the private material is passphrase protected.
	Locked bool
	// Algorithm and Bits describe the primary key, for the fingerprint row.
	Algorithm string
	Bits      int
}

// Fingerprint renders an entity's primary fingerprint the way this package
// keys everything else by: uppercase hex, unseparated.
func Fingerprint(ent *openpgp.Entity) string {
	if ent == nil || ent.PrimaryKey == nil {
		return ""
	}
	return strings.ToUpper(fmt.Sprintf("%x", ent.PrimaryKey.Fingerprint))
}

// describe builds the display record for one entity.
func describe(ent *openpgp.Entity) KeyInfo {
	info := KeyInfo{
		Fingerprint: Fingerprint(ent),
		HasPrivate:  ent.PrivateKey != nil,
		Locked:      entityLocked(ent),
	}
	if ent.PrimaryKey != nil {
		info.Created = ent.PrimaryKey.CreationTime
		info.Algorithm = algorithmName(ent.PrimaryKey.PubKeyAlgo)
		if bits, err := ent.PrimaryKey.BitLength(); err == nil {
			info.Bits = int(bits)
		}
	}
	for _, id := range ent.Identities {
		if info.Name == "" {
			info.Name = id.UserId.Name
			info.Email = id.UserId.Email
		}
		if id.UserId.Email != "" {
			info.Emails = append(info.Emails, id.UserId.Email)
		}
		// the self-signature carries the expiry, and identities can disagree;
		// the earliest one is the honest answer.
		if id.SelfSignature != nil && id.SelfSignature.KeyLifetimeSecs != nil && ent.PrimaryKey != nil {
			exp := ent.PrimaryKey.CreationTime.Add(time.Duration(*id.SelfSignature.KeyLifetimeSecs) * time.Second)
			if info.Expires.IsZero() || exp.Before(info.Expires) {
				info.Expires = exp
			}
		}
	}
	return info
}

// algorithmName renders a public key algorithm for display.
func algorithmName(algo packet.PublicKeyAlgorithm) string {
	switch algo {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSAEncryptOnly, packet.PubKeyAlgoRSASignOnly:
		return "RSA"
	case packet.PubKeyAlgoElGamal:
		return "ElGamal"
	case packet.PubKeyAlgoDSA:
		return "DSA"
	case packet.PubKeyAlgoECDH:
		return "ECDH"
	case packet.PubKeyAlgoECDSA:
		return "ECDSA"
	case packet.PubKeyAlgoEdDSA:
		return "EdDSA"
	case packet.PubKeyAlgoEd25519:
		return "Ed25519"
	case packet.PubKeyAlgoEd448:
		return "Ed448"
	case packet.PubKeyAlgoX25519:
		return "X25519"
	case packet.PubKeyAlgoX448:
		return "X448"
	default:
		return "unknown"
	}
}

// List returns every key in both rings, private keys first, then by primary
// user id. A missing ring contributes nothing rather than failing.
func (s *PGPKeyStore) List() ([]KeyInfo, error) {
	priv, err := s.load(secringFile)
	if err != nil {
		return nil, err
	}
	pub, err := s.load(pubringFile)
	if err != nil {
		return nil, err
	}
	out := make([]KeyInfo, 0, len(priv)+len(pub))
	seen := make(map[string]bool, len(priv))
	for _, ent := range priv {
		info := describe(ent)
		seen[info.Fingerprint] = true
		out = append(out, info)
	}
	for _, ent := range pub {
		// a private key's public half is normally in both rings; show it once,
		// as the private one, which is the more capable of the two.
		if info := describe(ent); !seen[info.Fingerprint] {
			out = append(out, info)
		}
	}
	return out, nil
}

// Import parses armored or binary key data and files each key into the ring it
// belongs in. A private key is also written to the public ring so it can be
// used as a recipient. Keys already present are replaced, so re-importing an
// updated key refreshes it instead of duplicating it.
func (s *PGPKeyStore) Import(data []byte) ([]KeyInfo, error) {
	list, err := readKeys(data)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNoKeysInFile
	}

	var private, public openpgp.EntityList
	for _, ent := range list {
		if ent.PrivateKey != nil {
			private = append(private, ent)
		}
		public = append(public, ent)
	}

	if len(private) > 0 {
		if err := s.merge(secringFile, private); err != nil {
			return nil, err
		}
	}
	if err := s.merge(pubringFile, public); err != nil {
		return nil, err
	}

	out := make([]KeyInfo, 0, len(list))
	for _, ent := range list {
		out = append(out, describe(ent))
	}
	return out, nil
}

// ImportFile reads a key file from disk and imports it.
func (s *PGPKeyStore) ImportFile(path string) ([]KeyInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("crypto: read key file: %w", err)
	}
	if info.Size() > maxKeyFileSize {
		return nil, fmt.Errorf("crypto: %q is too large to be a key file", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("crypto: read key file: %w", err)
	}
	return s.Import(data)
}

// Delete removes the key with the given fingerprint from both rings. Removing
// a key that is not there is not an error, so a retry after a partial failure
// converges instead of complaining.
func (s *PGPKeyStore) Delete(fingerprint string) error {
	want := NormalizeFingerprint(fingerprint)
	found := false
	for _, file := range []string{secringFile, pubringFile} {
		list, err := s.load(file)
		if err != nil {
			return err
		}
		kept := make(openpgp.EntityList, 0, len(list))
		for _, ent := range list {
			if NormalizeFingerprint(Fingerprint(ent)) == want {
				found = true
				continue
			}
			kept = append(kept, ent)
		}
		if len(kept) == len(list) {
			continue
		}
		if err := s.write(file, kept); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrKeyNotFound, fingerprint)
	}
	return nil
}

// Export returns one key as an armored block. includePrivate exports the
// private half, which is the only backup path for it: key material is
// deliberately kept out of the settings backup archive.
func (s *PGPKeyStore) Export(fingerprint string, includePrivate bool) ([]byte, error) {
	ent, err := s.byFingerprint(fingerprint, includePrivate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	block := publicKeyBlock
	if includePrivate {
		block = privateKeyBlock
	}
	w, err := armor.Encode(&buf, block, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: armor key: %w", err)
	}
	if includePrivate {
		// without signing, so a locked key exports without its passphrase.
		err = ent.SerializePrivateWithoutSigning(w, nil)
	} else {
		err = ent.Serialize(w)
	}
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("crypto: serialize key: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("crypto: finish armor: %w", err)
	}
	return buf.Bytes(), nil
}

// SenderKeyByFingerprint returns a private key by fingerprint rather than by
// address, for accounts that pinned a specific signing key.
func (s *PGPKeyStore) SenderKeyByFingerprint(fingerprint string) (*openpgp.Entity, error) {
	ent, err := s.byFingerprint(fingerprint, true)
	if err != nil {
		return nil, err
	}
	return ent, nil
}

// byFingerprint finds one key, in the private ring when private is required.
func (s *PGPKeyStore) byFingerprint(fingerprint string, private bool) (*openpgp.Entity, error) {
	file := pubringFile
	if private {
		file = secringFile
	}
	list, err := s.load(file)
	if err != nil {
		return nil, err
	}
	want := NormalizeFingerprint(fingerprint)
	for _, ent := range list {
		if NormalizeFingerprint(Fingerprint(ent)) == want {
			if private && ent.PrivateKey == nil {
				return nil, fmt.Errorf("%w: %s has no private key material", ErrSenderKeyNotFound, fingerprint)
			}
			return ent, nil
		}
	}
	if private {
		return nil, fmt.Errorf("%w: %s", ErrSenderKeyNotFound, fingerprint)
	}
	return nil, fmt.Errorf("%w: %s", ErrKeyNotFound, fingerprint)
}

// merge adds entities to a ring, replacing any existing key with the same
// fingerprint.
func (s *PGPKeyStore) merge(file string, incoming openpgp.EntityList) error {
	existing, err := s.load(file)
	if err != nil {
		return err
	}
	replacing := make(map[string]bool, len(incoming))
	for _, ent := range incoming {
		replacing[NormalizeFingerprint(Fingerprint(ent))] = true
	}
	merged := make(openpgp.EntityList, 0, len(existing)+len(incoming))
	for _, ent := range existing {
		if !replacing[NormalizeFingerprint(Fingerprint(ent))] {
			merged = append(merged, ent)
		}
	}
	merged = append(merged, incoming...)
	return s.write(file, merged)
}

// write replaces a keyring file atomically, so an interrupted write cannot
// leave a truncated ring behind and lose every key in it.
//
// Every entity goes inside one armor block, the way gpg writes a keyring:
// ReadArmoredKeyRing only reads the first block in a file, so one block per
// key would make every key after the first invisible.
func (s *PGPKeyStore) write(file string, list openpgp.EntityList) error {
	if err := os.MkdirAll(s.dir, keyDirPerm); err != nil {
		return fmt.Errorf("crypto: create key directory: %w", err)
	}
	path := filepath.Join(s.dir, file)

	// an empty ring is the absence of a file, not a file with no keys in it:
	// an empty armor block is not valid armor and would fail to parse.
	if len(list) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("crypto: remove empty keyring: %w", err)
		}
		return nil
	}

	private := file == secringFile
	block := publicKeyBlock
	if private {
		block = privateKeyBlock
	}

	var buf bytes.Buffer
	w, err := armor.Encode(&buf, block, nil)
	if err != nil {
		return fmt.Errorf("crypto: armor keyring: %w", err)
	}
	for _, ent := range list {
		if private {
			// without signing, so a locked key round-trips without its passphrase.
			err = ent.SerializePrivateWithoutSigning(w, nil)
		} else {
			err = ent.Serialize(w)
		}
		if err != nil {
			w.Close()
			return fmt.Errorf("crypto: serialize keyring: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("crypto: finish armor: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), keyFilePerm); err != nil {
		return fmt.Errorf("crypto: write keyring: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("crypto: replace keyring: %w", err)
	}
	// rename keeps the temp file's mode, but an existing target that predates
	// this code could still be too open.
	if err := os.Chmod(path, keyFilePerm); err != nil {
		return fmt.Errorf("crypto: set keyring permissions: %w", err)
	}
	return nil
}

// readKeys parses a key file that may be armored or binary. gpg exports either
// depending on the flag the user happened to use.
func readKeys(data []byte) (openpgp.EntityList, error) {
	if list, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(data)); err == nil {
		return list, nil
	}
	list, err := openpgp.ReadKeyRing(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoKeysInFile, err)
	}
	return list, nil
}

// NormalizeFingerprint strips the spacing gpg prints fingerprints with, so a
// value pasted from `gpg --list-keys` matches one from Fingerprint.
func NormalizeFingerprint(fp string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(fp), " ", ""))
}

// EnsureKeyDir creates the key directory with owner-only permissions, and
// tightens it if it already exists with looser ones.
func EnsureKeyDir(dir string) error {
	if err := os.MkdirAll(dir, keyDirPerm); err != nil {
		return fmt.Errorf("crypto: create key directory: %w", err)
	}
	if err := os.Chmod(dir, keyDirPerm); err != nil {
		return fmt.Errorf("crypto: set key directory permissions: %w", err)
	}
	return nil
}
