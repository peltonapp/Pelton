package crypto

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
)

const (
	// pubringFile holds recipients' armored public keys, concatenated. secringFile
	// holds the user's armored private keys. This is the gpg-like keyring model:
	// one file each, looked up by the email address in a key's user id.
	pubringFile = "pubring.asc"
	secringFile = "secring.asc"
)

// PGPKeyStore is a file-backed lookup for OpenPGP keys. Recipient public keys
// and the user's private keys live in two armored keyring files under dir.
// Private key material is read here but only unlocked transiently during a
// signing or encryption call; passphrases are never held by the store.
//
// Keys found automatically (Autocrypt, WKD, key servers) are not written here;
// they are kept apart so the keyring stays what the user put in it.
type PGPKeyStore struct {
	dir string
}

// NewPGPKeyStore returns a store reading keyring files from dir.
func NewPGPKeyStore(dir string) *PGPKeyStore {
	return &PGPKeyStore{dir: dir}
}

// RecipientKey returns the public key whose user id matches email, for
// encrypting to that recipient. It returns ErrRecipientKeyNotFound if absent.
func (s *PGPKeyStore) RecipientKey(email string) (*openpgp.Entity, error) {
	list, err := s.load(pubringFile)
	if err != nil {
		return nil, err
	}
	ent := entityForEmail(list, email)
	if ent == nil {
		return nil, fmt.Errorf("%w: %s", ErrRecipientKeyNotFound, email)
	}
	return ent, nil
}

// SenderKey returns the private key whose user id matches email, for signing or
// decryption. The key may still be locked; callers unlock it with a passphrase.
// It returns ErrSenderKeyNotFound if absent.
func (s *PGPKeyStore) SenderKey(email string) (*openpgp.Entity, error) {
	list, err := s.load(secringFile)
	if err != nil {
		return nil, err
	}
	ent := entityForEmail(list, email)
	if ent == nil {
		return nil, fmt.Errorf("%w: %s", ErrSenderKeyNotFound, email)
	}
	if ent.PrivateKey == nil {
		return nil, fmt.Errorf("%w: %s has no private key material", ErrSenderKeyNotFound, email)
	}
	return ent, nil
}

// PrivateKeys returns every private key in the store. Decryption needs all of
// them rather than one looked up by address: a message names the key it was
// encrypted to by key id, and that key may belong to any of the user's
// identities, including an address the message was never sent to directly.
func (s *PGPKeyStore) PrivateKeys() (openpgp.EntityList, error) {
	return s.load(secringFile)
}

// PublicKeys returns every key that can check a signature: the public keyring,
// plus the public halves of the user's own private keys so mail they signed
// themselves still verifies.
func (s *PGPKeyStore) PublicKeys() (openpgp.EntityList, error) {
	pub, err := s.load(pubringFile)
	if err != nil {
		return nil, err
	}
	sec, err := s.load(secringFile)
	if err != nil {
		return nil, err
	}
	return append(pub, sec...), nil
}

// load reads and parses one armored keyring file. A missing file is reported as
// an empty keyring so lookups fall through to the not-found errors with a clear
// message rather than a confusing open error.
func (s *PGPKeyStore) load(file string) (openpgp.EntityList, error) {
	path := filepath.Join(s.dir, file)
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("crypto: open keyring %q: %w", path, err)
	}
	defer f.Close()

	// a ring emptied of its last key is left as an empty file by some editors
	// and by older versions of this code; treat it as no keys rather than
	// failing every lookup with an armor error.
	if info, err := f.Stat(); err == nil && info.Size() == 0 {
		return nil, nil
	}

	list, err := openpgp.ReadArmoredKeyRing(f)
	if err != nil {
		return nil, fmt.Errorf("crypto: read keyring %q: %w", path, err)
	}
	return list, nil
}

// entityForEmail finds the first entity carrying an identity with the given
// email, matched case-insensitively. Returns nil when none match.
func entityForEmail(list openpgp.EntityList, email string) *openpgp.Entity {
	target := strings.ToLower(strings.TrimSpace(email))
	for _, ent := range list {
		for _, id := range ent.Identities {
			if strings.ToLower(id.UserId.Email) == target {
				return ent
			}
		}
	}
	return nil
}
