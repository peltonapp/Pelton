package crypto

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	v2 "github.com/ProtonMail/go-crypto/openpgp/v2"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// encryptExplicit encrypts to keys the caller found itself rather than to
// keyring entries looked up by address. Hidden keys are encrypted to without
// their key id on the message, which is how a Bcc recipient reads the message
// without the other recipients learning who they are. go-crypto offers hidden
// recipients only in its v2 API, so this path reads the keys with it.
func (p *PGP) encryptExplicit(entity []byte, opts Options, sign bool) (*Part, error) {
	if len(opts.RecipientKeys)+len(opts.HiddenKeys) == 0 {
		return nil, ErrNoRecipients
	}
	to, err := parseV2Keys(opts.RecipientKeys)
	if err != nil {
		return nil, err
	}
	hidden, err := parseV2Keys(opts.HiddenKeys)
	if err != nil {
		return nil, err
	}

	var signers []*v2.Entity
	if sign {
		signer, err := p.v2Signer(opts)
		if err != nil {
			return nil, err
		}
		signers = []*v2.Entity{signer}
	}

	var armored bytes.Buffer
	armorWriter, err := armor.Encode(&armored, pgpMessageBlock, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: open armor writer: %w", err)
	}
	plaintext, err := v2.EncryptWithParams(armorWriter, to, hidden, &v2.EncryptParams{Signers: signers, Config: cryptoConfig()})
	if err != nil {
		_ = armorWriter.Close()
		return nil, fmt.Errorf("crypto: pgp encrypt: %w", err)
	}
	if _, err := plaintext.Write(toCRLF(entity)); err != nil {
		_ = plaintext.Close()
		_ = armorWriter.Close()
		return nil, fmt.Errorf("crypto: write plaintext to encryptor: %w", err)
	}
	if err := plaintext.Close(); err != nil {
		_ = armorWriter.Close()
		return nil, fmt.Errorf("crypto: finalize encryption: %w", err)
	}
	if err := armorWriter.Close(); err != nil {
		return nil, fmt.Errorf("crypto: finalize armor: %w", err)
	}

	boundary, err := newBoundary()
	if err != nil {
		return nil, err
	}
	return &Part{
		ContentType: fmt.Sprintf("multipart/encrypted; protocol=%q; boundary=%q", pgpEncryptedProtocol, boundary),
		Body:        assembleEncrypted(armored.Bytes(), boundary),
	}, nil
}

// parseV2Keys reads binary or armored public keys, one key per entry.
func parseV2Keys(keys [][]byte) ([]*v2.Entity, error) {
	out := make([]*v2.Entity, 0, len(keys))
	for _, k := range keys {
		list, err := v2.ReadKeyRing(bytes.NewReader(k))
		if err != nil || len(list) == 0 {
			list, err = v2.ReadArmoredKeyRing(bytes.NewReader(k))
		}
		if err != nil || len(list) == 0 {
			return nil, fmt.Errorf("%w: unreadable key: %v", ErrRecipientKeyNotFound, err)
		}
		out = append(out, list[0])
	}
	return out, nil
}

// v2Signer finds the sender's private key in the secret ring through the v2
// API and unlocks it.
func (p *PGP) v2Signer(opts Options) (*v2.Entity, error) {
	if opts.SenderEmail == "" {
		return nil, fmt.Errorf("%w: no sender email given", ErrSenderKeyNotFound)
	}
	v1, err := p.keys.SenderKey(opts.SenderEmail)
	if err != nil {
		return nil, err
	}
	want := v1.PrimaryKey.Fingerprint
	f, err := os.Open(filepath.Join(p.keys.dir, secringFile))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSenderKeyNotFound, err)
	}
	defer f.Close()
	list, err := v2.ReadArmoredKeyRing(f)
	if err != nil {
		return nil, fmt.Errorf("crypto: read keyring: %w", err)
	}
	for _, ent := range list {
		if !bytes.Equal(ent.PrimaryKey.Fingerprint, want) {
			continue
		}
		if v2Locked(ent) {
			if len(opts.Passphrase) == 0 {
				return nil, fmt.Errorf("%w: %s", ErrPassphraseRequired, opts.SenderEmail)
			}
			if err := ent.DecryptPrivateKeys(opts.Passphrase); err != nil {
				return nil, fmt.Errorf("crypto: decrypt private key for %s: %w", opts.SenderEmail, err)
			}
		}
		return ent, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrSenderKeyNotFound, opts.SenderEmail)
}

func v2Locked(ent *v2.Entity) bool {
	if ent.PrivateKey != nil && ent.PrivateKey.Encrypted {
		return true
	}
	for _, sk := range ent.Subkeys {
		if sk.PrivateKey != nil && sk.PrivateKey.Encrypted {
			return true
		}
	}
	return false
}
