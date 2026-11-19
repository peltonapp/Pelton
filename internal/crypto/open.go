package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
)

// Opening a received message is the mirror of Wrap, and the safety rule runs the
// other way: a message that cannot be decrypted or whose signature does not
// check out must never be presented as though it were fine. Every failure is
// reported, and the caller shows the protected form rather than pretending.

var (
	// ErrNotProtected means the message carries no OpenPGP content to open.
	ErrNotProtected = errors.New("crypto: message is not pgp protected")
	// ErrNoDecryptionKey means none of the stored private keys can open the
	// message: it was encrypted to somebody else, or to a key not imported yet.
	ErrNoDecryptionKey = errors.New("crypto: no private key can decrypt this message")
	// ErrMalformedArmor means the pgp block could not be read at all.
	ErrMalformedArmor = errors.New("crypto: the pgp data is malformed")
	// ErrWrongPassphrase means a private key was found but the passphrase given
	// does not unlock it.
	ErrWrongPassphrase = errors.New("crypto: the passphrase does not unlock the key")
)

// Opened is a received message after decryption and signature checking.
type Opened struct {
	// Body is the decrypted MIME entity for encrypted mail, the signed content
	// for signed mail, or the plain text of a clearsigned block. It is never
	// written to disk by this package.
	Body []byte
	// Encrypted records that Body had to be decrypted to be read, which decides
	// whether the caller may cache it.
	Encrypted bool
	// Signature is the verdict on any signature found. A message may be
	// encrypted without being signed, in which case Status is SigNone.
	Signature Signature
	// Extended is the verdict when the extra keys given to OpenWith are also
	// consulted. It equals Signature when none were given or none applied.
	Extended Signature
}

// IsProtected reports whether a raw message carries OpenPGP content worth
// keeping the source for. It is a cheap structural check run over every synced
// message, so it looks at the content type and the armor headers rather than
// attempting to parse anything.
func IsProtected(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	headers, body, found := splitHeaders(raw)
	if !found {
		return false
	}
	mediaType, params := parseContentType(headers)
	switch {
	case mediaType == "multipart/encrypted" &&
		strings.EqualFold(params["protocol"], "application/pgp-encrypted"):
		return true
	case mediaType == "multipart/signed" &&
		strings.EqualFold(params["protocol"], "application/pgp-signature"):
		return true
	}
	return bytes.Contains(body, []byte("-----BEGIN PGP MESSAGE-----")) ||
		bytes.Contains(body, []byte("-----BEGIN PGP SIGNED MESSAGE-----"))
}

// Open decrypts and verifies a received message.
//
// raw is the complete RFC 822 message. passphrase unlocks a private key when
// one is needed and may be nil for mail that is only signed. The four shapes
// real mail uses are all handled: PGP/MIME encrypted and signed (RFC 3156), and
// the inline armored and clearsigned forms that predate it and are still
// common.
func (p *PGP) Open(raw []byte, passphrase []byte) (*Opened, error) {
	return p.OpenWith(raw, passphrase, nil)
}

// OpenWith is Open with additional public keys to check signatures against,
// binary or armored, which never take part in decryption. Opened.Signature
// stays the verdict the keyring alone gives; Opened.Extended is the verdict
// with the extra keys as well.
func (p *PGP) OpenWith(raw []byte, passphrase []byte, extraKeys [][]byte) (*Opened, error) {
	var extra openpgp.EntityList
	for _, k := range extraKeys {
		if list, err := readKeys(k); err == nil {
			extra = append(extra, list...)
		}
	}
	headers, body, found := splitHeaders(raw)
	if !found {
		return nil, ErrNotProtected
	}
	mediaType, params := parseContentType(headers)

	switch {
	case mediaType == "multipart/encrypted" &&
		strings.EqualFold(params["protocol"], "application/pgp-encrypted"):
		return p.openMIMEEncrypted(body, params["boundary"], passphrase, extra)
	case mediaType == "multipart/signed" &&
		strings.EqualFold(params["protocol"], "application/pgp-signature"):
		return p.openMIMESigned(body, params["boundary"], extra)
	}
	return p.openInline(headers, body, passphrase, extra)
}

// openMIMEEncrypted reads an RFC 3156 multipart/encrypted message. The first
// part is the version marker, which carries nothing worth checking beyond its
// presence; the second holds the armored ciphertext.
func (p *PGP) openMIMEEncrypted(body []byte, boundary string, passphrase []byte, extra openpgp.EntityList) (*Opened, error) {
	if boundary == "" {
		return nil, ErrMalformedArmor
	}
	sections := splitOnBoundary(body, []byte("--"+boundary))
	if len(sections) < 2 {
		return nil, ErrMalformedArmor
	}
	_, ciphertext, ok := splitHeaders(sections[1])
	if !ok {
		return nil, ErrMalformedArmor
	}
	return p.decrypt(ciphertext, passphrase, extra)
}

// openMIMESigned reads an RFC 3156 multipart/signed message. The signed part is
// taken verbatim: the signature covers the bytes as transmitted, so parsing and
// rebuilding the part would invalidate a signature that is perfectly good.
func (p *PGP) openMIMESigned(body []byte, boundary string, extra openpgp.EntityList) (*Opened, error) {
	if boundary == "" {
		return nil, ErrMalformedArmor
	}
	sections := splitOnBoundary(body, []byte("--"+boundary))
	if len(sections) < 2 {
		return nil, ErrMalformedArmor
	}
	signed := sections[0]
	_, sigArmor, ok := splitHeaders(sections[1])
	if !ok {
		return nil, ErrMalformedArmor
	}
	keyring, err := p.keys.PublicKeys()
	if err != nil {
		keyring = nil
	}
	opened := &Opened{Body: signed, Signature: checkDetached(keyring, signed, sigArmor)}
	opened.Extended = opened.Signature
	if opened.Signature.Status == SigUntrusted && len(extra) > 0 {
		opened.Extended = checkDetached(append(append(openpgp.EntityList{}, keyring...), extra...), signed, sigArmor)
	}
	return opened, nil
}

// openInline handles the pre-MIME forms: an armored message or a clearsigned
// block sitting in an ordinary text body.
func (p *PGP) openInline(headers, body []byte, passphrase []byte, extra openpgp.EntityList) (*Opened, error) {
	text, err := decodeBody(headers, body)
	if err != nil {
		text = body
	}
	switch {
	case bytes.Contains(text, []byte("-----BEGIN PGP MESSAGE-----")):
		return p.decrypt(text, passphrase, extra)
	case bytes.Contains(text, []byte("-----BEGIN PGP SIGNED MESSAGE-----")):
		return p.openClearsigned(text, extra)
	}
	return nil, ErrNotProtected
}

// openClearsigned verifies an inline clearsigned block and returns the text it
// covers, with the armor removed.
func (p *PGP) openClearsigned(text []byte, extra openpgp.EntityList) (*Opened, error) {
	block, _ := clearsign.Decode(text)
	if block == nil {
		return nil, ErrMalformedArmor
	}
	keyring, err := p.keys.PublicKeys()
	if err != nil {
		return nil, err
	}
	check := func(ring openpgp.EntityList) Signature {
		if len(ring) == 0 {
			return unknownSigner()
		}
		signer, err := openpgp.CheckDetachedSignature(
			ring, bytes.NewReader(block.Bytes), block.ArmoredSignature.Body, cryptoConfig())
		return signatureVerdict(signer, err)
	}
	opened := &Opened{Body: block.Bytes, Signature: check(keyring)}
	opened.Extended = opened.Signature
	if opened.Signature.Status == SigUntrusted && len(extra) > 0 {
		opened.Extended = check(append(append(openpgp.EntityList{}, keyring...), extra...))
	}
	return opened, nil
}

// decrypt opens an armored OpenPGP message with whichever stored private key it
// was encrypted to, reporting any signature carried inside it. extra keys are
// consulted for the signature only.
func (p *PGP) decrypt(ciphertext, passphrase []byte, extra openpgp.EntityList) (*Opened, error) {
	private, err := p.keys.PrivateKeys()
	if err != nil {
		return nil, err
	}
	if len(private) == 0 {
		return nil, ErrNoDecryptionKey
	}
	// a signature inside an encrypted message is checked against the public
	// keyring, so both rings go to the reader.
	public, err := p.keys.PublicKeys()
	if err != nil {
		return nil, err
	}
	keyring := append(openpgp.EntityList{}, private...)
	keyring = append(keyring, public...)
	base := keyring
	keyring = append(append(openpgp.EntityList{}, keyring...), extra...)

	block, err := armor.Decode(bytes.NewReader(ciphertext))
	if err != nil {
		return nil, ErrMalformedArmor
	}

	// the keys are unlocked here rather than through go-crypto's prompt
	// callback. That callback is invoked once per candidate key, and the primary
	// key is tried before the encryption subkey the message actually names, so a
	// passphrase handed to the first call never reached the key that needed it
	// and a locked key could not open anything even with the right passphrase.
	if err := unlockRing(private, passphrase); err != nil {
		return nil, err
	}

	// nothing is left to ask for: any key that could be unlocked has been. A
	// prompt at this point would be go-crypto looping over keys the passphrase
	// does not fit.
	prompt := func(keys []openpgp.Key, symmetric bool) ([]byte, error) {
		return nil, ErrPassphraseRequired
	}

	md, err := openpgp.ReadMessage(block.Body, keyring, prompt, cryptoConfig())
	if err != nil {
		return nil, decryptError(err)
	}
	plaintext, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		return nil, decryptError(err)
	}
	// the signature state is only final once the body has been read to the end,
	// which is why this is checked here rather than off md alone.
	opened := &Opened{Body: plaintext, Encrypted: true}
	switch {
	case !md.IsSigned:
		// encrypted but unsigned: confidential, with nothing establishing who
		// wrote it.
	case md.SignedBy == nil:
		opened.Signature = unknownSigner()
	default:
		opened.Signature = signatureVerdict(md.SignedBy.Entity, md.SignatureError)
	}
	opened.Extended = opened.Signature
	// a signature only the extra keys could check reads, without them, as one
	// from an unknown key.
	if md.IsSigned && md.SignedBy != nil && len(base.KeysById(md.SignedByKeyId)) == 0 {
		opened.Signature = unknownSigner()
	}
	return opened, nil
}

// checkDetached verifies an armored detached signature over content.
func checkDetached(keyring openpgp.EntityList, content, sigArmor []byte) Signature {
	if len(keyring) == 0 {
		return unknownSigner()
	}
	signer, err := openpgp.CheckArmoredDetachedSignature(
		keyring, bytes.NewReader(content), bytes.NewReader(sigArmor), cryptoConfig())
	if err == nil {
		return signatureVerdict(signer, nil)
	}
	// mail that passed through something that rewrote its line endings would
	// otherwise be reported as altered, so the canonical form gets a second try.
	canonical := toCRLF(content)
	if !bytes.Equal(canonical, content) {
		if signer, retry := openpgp.CheckArmoredDetachedSignature(
			keyring, bytes.NewReader(canonical), bytes.NewReader(sigArmor), cryptoConfig()); retry == nil {
			return signatureVerdict(signer, nil)
		}
	}
	return signatureVerdict(nil, err)
}

// signatureVerdict turns a verification outcome into the same three-state
// verdict S/MIME reports, so the reading pane has one way to describe a
// signature whatever produced it.
func signatureVerdict(signer *openpgp.Entity, err error) Signature {
	if err != nil {
		if isUnknownIssuer(err) {
			return unknownSigner()
		}
		return Signature{Status: SigInvalid, Detail: "the message was altered after it was signed"}
	}
	if signer == nil {
		return unknownSigner()
	}
	name, email := identityOf(signer)
	return Signature{
		Status:      SigValid,
		SignerName:  name,
		SignerEmail: email,
		Fingerprint: Fingerprint(signer),
	}
}

// unknownSigner is the verdict for a signature made by a key that is not in the
// keyring. The content may be untouched, but with no key there is no way to
// establish that, so it is reported as unverified rather than good or bad.
func unknownSigner() Signature {
	return Signature{
		Status: SigUntrusted,
		Detail: "the signing key is not in your keyring, so the signature cannot be checked",
	}
}

// isUnknownIssuer reports whether a verification failure was caused by a
// missing key rather than by the content not matching.
func isUnknownIssuer(err error) bool {
	return errors.Is(err, pgperrors.ErrUnknownIssuer) ||
		strings.Contains(err.Error(), "unknown issuer")
}

// identityOf reads a display name and address out of a key's primary user id.
func identityOf(ent *openpgp.Entity) (name, email string) {
	for _, id := range ent.Identities {
		if id.UserId == nil {
			continue
		}
		if email == "" && id.UserId.Email != "" {
			name, email = id.UserId.Name, id.UserId.Email
		}
	}
	return name, email
}

// decryptError maps a go-crypto failure onto the sentinel a caller can act on.
func decryptError(err error) error {
	switch {
	case errors.Is(err, ErrPassphraseRequired):
		return ErrPassphraseRequired
	case errors.Is(err, pgperrors.ErrKeyIncorrect):
		return ErrWrongPassphrase
	case strings.Contains(err.Error(), "no suitable key"),
		strings.Contains(err.Error(), "cannot decrypt"):
		return ErrNoDecryptionKey
	}
	return fmt.Errorf("crypto: decrypt: %w", err)
}

// unlockRing decrypts every locked private key the passphrase fits, in memory
// only: the store reloads from disk on the next call, so nothing stays unlocked
// beyond this operation.
//
// It reports a problem only when no usable key is left. A ring holding one
// unlocked key and one belonging to another passphrase is fine, since the
// message names the key it wants and go-crypto will find it.
func unlockRing(private openpgp.EntityList, passphrase []byte) error {
	var usable, locked int
	for _, ent := range private {
		if !entityLocked(ent) {
			usable++
			continue
		}
		locked++
		if len(passphrase) == 0 {
			continue
		}
		if err := ent.DecryptPrivateKeys(passphrase); err == nil {
			usable++
		}
	}
	switch {
	case usable > 0:
		return nil
	case locked > 0 && len(passphrase) == 0:
		return ErrPassphraseRequired
	case locked > 0:
		return ErrWrongPassphrase
	default:
		return ErrNoDecryptionKey
	}
}
