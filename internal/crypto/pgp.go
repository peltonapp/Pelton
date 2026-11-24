package crypto

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"fmt"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

const (
	// PGP/MIME content types and parameters from RFC 3156.
	pgpSignatureType     = "application/pgp-signature"
	pgpEncryptedType     = "application/pgp-encrypted"
	pgpOctetStreamType   = "application/octet-stream"
	pgpSignatureProtocol = "application/pgp-signature"
	pgpEncryptedProtocol = "application/pgp-encrypted"
	// micalg names the message integrity check algorithm; it must match the hash
	// the signature actually uses, which cryptoConfig pins to sha256.
	pgpMicalg = "pgp-sha256"

	// armor block types.
	pgpMessageBlock = "PGP MESSAGE"

	// the version body required in the application/pgp-encrypted control part.
	pgpVersionBody = "Version: 1"

	signatureFilename = "signature.asc"
	encryptedFilename = "encrypted.asc"

	boundaryBytes = 24
)

// PGP implements Engine using OpenPGP (ProtonMail/go-crypto fork). It produces
// proper PGP/MIME structures, never inline-armored bodies.
//
// Combined sign+encrypt order: the message is signed and then encrypted, with
// the signature packet placed inside the encrypted OpenPGP message (the standard
// openpgp.Encrypt with a signer does exactly this). That hides who signed from
// anyone but the recipients and is the widely interoperable choice. The
// alternative, a multipart/signed nested inside multipart/encrypted, is heavier
// and not needed here.
type PGP struct {
	keys *PGPKeyStore
}

// NewPGP returns a PGP engine backed by the given key store.
func NewPGP(keys *PGPKeyStore) *PGP {
	return &PGP{keys: keys}
}

// Wrap signs and/or encrypts the MIME entity and returns the outer PGP/MIME
// part. On any failure it returns a nil Part and an error, so plaintext can
// never escape, per the package safety rule.
func (p *PGP) Wrap(entity []byte, mode Mode, opts Options) (*Part, error) {
	switch mode {
	case ModeSign:
		return p.sign(entity, opts)
	case ModeEncrypt:
		return p.encrypt(entity, opts, false)
	case ModeSignEncrypt:
		return p.encrypt(entity, opts, true)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedMode, mode)
	}
}

// sign builds a multipart/signed body: the canonical MIME entity followed by an
// armored detached signature over it.
func (p *PGP) sign(entity []byte, opts Options) (*Part, error) {
	signer, err := p.signer(opts)
	if err != nil {
		return nil, err
	}

	// rfc 3156 requires the signature be computed over the canonical crlf form of
	// the entity, exactly as it appears as the first part of the multipart/signed.
	canonical := toCRLF(entity)

	var sig bytes.Buffer
	if err := openpgp.ArmoredDetachSignText(&sig, signer, bytes.NewReader(canonical), cryptoConfig()); err != nil {
		return nil, fmt.Errorf("crypto: pgp detached sign: %w", err)
	}

	boundary, err := newBoundary()
	if err != nil {
		return nil, err
	}

	body := assembleSigned(canonical, sig.Bytes(), boundary)
	contentType := fmt.Sprintf("multipart/signed; protocol=%q; micalg=%q; boundary=%q",
		pgpSignatureProtocol, pgpMicalg, boundary)
	return &Part{ContentType: contentType, Body: body}, nil
}

// encrypt builds a multipart/encrypted body. When sign is true the message is
// also signed, with the signature inside the encrypted payload.
func (p *PGP) encrypt(entity []byte, opts Options, sign bool) (*Part, error) {
	if len(opts.RecipientKeys)+len(opts.HiddenKeys) > 0 {
		return p.encryptExplicit(entity, opts, sign)
	}
	recipients, err := p.recipients(opts)
	if err != nil {
		return nil, err
	}

	var signer *openpgp.Entity
	if sign {
		if signer, err = p.signer(opts); err != nil {
			return nil, err
		}
	}

	canonical := toCRLF(entity)

	var armored bytes.Buffer
	armorWriter, err := armor.Encode(&armored, pgpMessageBlock, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: open armor writer: %w", err)
	}

	plaintext, err := openpgp.Encrypt(armorWriter, recipients, signer, nil, cryptoConfig())
	if err != nil {
		// best effort close, the result is discarded either way.
		_ = armorWriter.Close()
		return nil, fmt.Errorf("crypto: pgp encrypt: %w", err)
	}
	if _, err := plaintext.Write(canonical); err != nil {
		_ = plaintext.Close()
		_ = armorWriter.Close()
		return nil, fmt.Errorf("crypto: write plaintext to encryptor: %w", err)
	}
	// closing the encryptor flushes the ciphertext; an error here means the
	// ciphertext is incomplete, so we must fail rather than emit anything.
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

	body := assembleEncrypted(armored.Bytes(), boundary)
	contentType := fmt.Sprintf("multipart/encrypted; protocol=%q; boundary=%q",
		pgpEncryptedProtocol, boundary)
	return &Part{ContentType: contentType, Body: body}, nil
}

// signer loads the sender's private key and unlocks it. A missing key or a
// locked key with no passphrase is a hard failure.
func (p *PGP) signer(opts Options) (*openpgp.Entity, error) {
	if opts.SenderEmail == "" {
		return nil, fmt.Errorf("%w: no sender email given", ErrSenderKeyNotFound)
	}
	ent, err := p.keys.SenderKey(opts.SenderEmail)
	if err != nil {
		return nil, err
	}
	if err := unlock(ent, opts.Passphrase); err != nil {
		return nil, err
	}
	return ent, nil
}

// recipients loads a public key for every recipient. A single missing key fails
// the whole operation so we never encrypt to a subset and silently drop someone.
func (p *PGP) recipients(opts Options) ([]*openpgp.Entity, error) {
	if len(opts.Recipients) == 0 {
		return nil, ErrNoRecipients
	}
	entities := make([]*openpgp.Entity, 0, len(opts.Recipients))
	for _, addr := range opts.Recipients {
		ent, err := p.keys.RecipientKey(addr)
		if err != nil {
			return nil, err
		}
		entities = append(entities, ent)
	}
	return entities, nil
}

// Unlock decrypts an entity's private keys in place if they are locked, and
// reports whether the passphrase fits. Callers outside this package use it to
// check a passphrase before holding on to it, so a typo surfaces at the prompt
// rather than at the moment a message is sent.
func Unlock(ent *openpgp.Entity, passphrase []byte) error {
	return unlock(ent, passphrase)
}

// unlock decrypts an entity's private keys in place if they are locked. With no
// passphrase available for a locked key it returns ErrPassphraseRequired.
func unlock(ent *openpgp.Entity, passphrase []byte) error {
	if !entityLocked(ent) {
		return nil
	}
	if len(passphrase) == 0 {
		return fmt.Errorf("%w: %s", ErrPassphraseRequired, primaryEmail(ent))
	}
	if err := ent.DecryptPrivateKeys(passphrase); err != nil {
		return fmt.Errorf("crypto: decrypt private key for %s: %w", primaryEmail(ent), err)
	}
	return nil
}

// KeyLocked reports whether an entity's private material still needs a
// passphrase. The compose window asks before offering to sign with it, so the
// user is prompted at the point they chose to protect a message rather than at
// the point it fails to go out.
func KeyLocked(ent *openpgp.Entity) bool {
	return entityLocked(ent)
}

// entityLocked reports whether the primary key or any subkey is still encrypted.
func entityLocked(ent *openpgp.Entity) bool {
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

func primaryEmail(ent *openpgp.Entity) string {
	for _, id := range ent.Identities {
		return id.UserId.Email
	}
	return "unknown"
}

// cryptoConfig pins the digest to sha256 so it matches the micalg parameter and
// the cipher to a modern default.
func cryptoConfig() *packet.Config {
	return &packet.Config{
		DefaultHash:   crypto.SHA256,
		DefaultCipher: packet.CipherAES256,
	}
}

// toCRLF normalises line endings to crlf, which mime and the detached signature
// both require. it collapses any existing crlf first so mixed endings do not
// double up.
func toCRLF(b []byte) []byte {
	unix := bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(unix, []byte("\n"), []byte("\r\n"))
}

// newBoundary returns a random multipart boundary unlikely to collide with the
// body content.
func newBoundary() (string, error) {
	buf := make([]byte, boundaryBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto: generate boundary: %w", err)
	}
	return fmt.Sprintf("pelton-%x", buf), nil
}

// assembleSigned writes the multipart/signed body by hand so the first part is
// the signed entity byte for byte, which is what the detached signature covers.
// using multipart.Writer here would risk reflowing those bytes.
func assembleSigned(entity, signature []byte, boundary string) []byte {
	var b bytes.Buffer
	writeBoundary(&b, boundary, false)
	b.Write(entity)
	b.WriteString("\r\n")
	writeBoundary(&b, boundary, false)
	b.WriteString("Content-Type: " + pgpSignatureType + "; name=\"" + signatureFilename + "\"\r\n")
	b.WriteString("Content-Description: OpenPGP digital signature\r\n")
	b.WriteString("\r\n")
	b.Write(signature)
	b.WriteString("\r\n")
	writeBoundary(&b, boundary, true)
	return b.Bytes()
}

// assembleEncrypted writes the multipart/encrypted body: a version control part
// followed by the armored ciphertext part.
func assembleEncrypted(ciphertext []byte, boundary string) []byte {
	var b bytes.Buffer
	writeBoundary(&b, boundary, false)
	b.WriteString("Content-Type: " + pgpEncryptedType + "\r\n")
	b.WriteString("Content-Description: PGP/MIME version identification\r\n")
	b.WriteString("\r\n")
	b.WriteString(pgpVersionBody + "\r\n")
	writeBoundary(&b, boundary, false)
	b.WriteString("Content-Type: " + pgpOctetStreamType + "; name=\"" + encryptedFilename + "\"\r\n")
	b.WriteString("Content-Description: OpenPGP encrypted message\r\n")
	b.WriteString("\r\n")
	b.Write(ciphertext)
	b.WriteString("\r\n")
	writeBoundary(&b, boundary, true)
	return b.Bytes()
}

// writeBoundary writes a multipart delimiter line, the closing one when final.
func writeBoundary(b *bytes.Buffer, boundary string, final bool) {
	b.WriteString("--")
	b.WriteString(boundary)
	if final {
		b.WriteString("--")
	}
	b.WriteString("\r\n")
}
