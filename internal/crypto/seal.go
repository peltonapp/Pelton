package crypto

import (
	"bytes"
	"fmt"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// Sealing is encryption to the user's own key, for data Pelton keeps on the
// user's own machine rather than sends: a draft of a message they have chosen
// to encrypt has no business sitting in the database as plaintext just because
// it has not been sent yet.
//
// It is deliberately not the PGP/MIME path. There is no recipient, no
// signature and no MIME structure, only an armored OpenPGP message the same key
// can open again.

// Seal encrypts plaintext to the key belonging to email and returns the armored
// message. It fails rather than returning anything when no key is found, so a
// caller cannot store plaintext by accident.
func (p *PGP) Seal(email string, plaintext []byte) ([]byte, error) {
	if email == "" {
		return nil, fmt.Errorf("%w: no address to seal to", ErrRecipientKeyNotFound)
	}
	ent, err := p.keys.RecipientKey(email)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	armorWriter, err := armor.Encode(&out, pgpMessageBlock, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: open armor writer: %w", err)
	}
	w, err := openpgp.Encrypt(armorWriter, []*openpgp.Entity{ent}, nil, nil, cryptoConfig())
	if err != nil {
		_ = armorWriter.Close()
		return nil, fmt.Errorf("crypto: seal: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		_ = w.Close()
		_ = armorWriter.Close()
		return nil, fmt.Errorf("crypto: write sealed data: %w", err)
	}
	if err := w.Close(); err != nil {
		_ = armorWriter.Close()
		return nil, fmt.Errorf("crypto: finalize sealed data: %w", err)
	}
	if err := armorWriter.Close(); err != nil {
		return nil, fmt.Errorf("crypto: finalize armor: %w", err)
	}
	return out.Bytes(), nil
}

// Unseal decrypts what Seal produced. A locked key with no passphrase gives
// ErrPassphraseRequired, which the caller turns into a prompt rather than a
// failure.
func (p *PGP) Unseal(sealed, passphrase []byte) ([]byte, error) {
	opened, err := p.decrypt(sealed, passphrase, nil)
	if err != nil {
		return nil, err
	}
	return opened.Body, nil
}
