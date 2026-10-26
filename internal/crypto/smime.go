package crypto

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/smallstep/pkcs7"
)

// ErrSMIMENotSupported is returned when an S/MIME operation that is not
// implemented is attempted. Verification of received mail is implemented;
// producing signed or encrypted mail is not, so every send attempt fails
// cleanly instead of emitting a broken or plaintext message.
var ErrSMIMENotSupported = errors.New("crypto: sending s/mime is not supported yet")

// SMIME is the S/MIME (RFC 8551) engine. It verifies signatures on received
// mail and refuses to produce them.
//
// The asymmetry is deliberate. Verification needs only to read a CMS SignedData
// structure and check it against the platform's certificate roots, and it makes
// corporate mail legible in a client that would otherwise show an unexplained
// smime.p7s attachment. Signing needs a certificate obtained from a CA, private
// key storage with its own passphrase handling, and per-account identity
// selection, none of which exists yet; encryption needs recipient certificate
// discovery on top. PGP/MIME remains the supported path for sending protected
// mail.
type SMIME struct{}

// NewSMIME returns the S/MIME engine.
func NewSMIME() *SMIME {
	return &SMIME{}
}

// Wrap always fails with ErrSMIMENotSupported. Because it returns a nil Part,
// the smtp layer cannot transmit anything for an S/MIME-protected message, which
// keeps the safety rule intact: a message meant to be protected is never sent in
// the clear.
func (*SMIME) Wrap(entity []byte, mode Mode, opts Options) (*Part, error) {
	return nil, ErrSMIMENotSupported
}

// SignatureStatus is the outcome of verifying a received message's signature.
type SignatureStatus string

const (
	// SigNone means the message carries no S/MIME signature.
	SigNone SignatureStatus = ""
	// SigValid means the signature is intact, the signing certificate chains to
	// a root the platform trusts, and it belongs to the sender.
	SigValid SignatureStatus = "valid"
	// SigUntrusted means the signature is intact but the certificate cannot be
	// vouched for: an unknown or self-signed issuer, an expired certificate, or
	// one issued to somebody other than the sender. The bytes were not tampered
	// with, but nothing establishes who signed them.
	SigUntrusted SignatureStatus = "untrusted"
	// SigInvalid means the signature does not verify against the content, so the
	// message was altered in transit or is malformed.
	SigInvalid SignatureStatus = "invalid"
)

// Signature describes a verified message signature for display. Every field is
// safe to show: it is read out of the certificate, never invented.
type Signature struct {
	Status SignatureStatus
	// SignerName is the display name the signature vouches for and SignerEmail
	// the address. Issuer is the CA's common name for S/MIME and empty for PGP,
	// which has no issuing authority; Fingerprint is the signing key's
	// fingerprint for PGP and empty for S/MIME.
	SignerName  string
	SignerEmail string
	Issuer      string
	Fingerprint string
	// Expires is when the signing certificate stops being valid.
	Expires time.Time
	// Detail explains a non-valid status in one sentence, for a tooltip. Empty
	// when the status is valid.
	Detail string
	// Certs is the signing certificate followed by its issuer, in DER. The
	// message itself is not kept after a sync, so these are what a later
	// revocation check has to work from; asking the authority needs both, the
	// issuer to check the reply is genuine. Empty for PGP and for any signature
	// whose chain could not be built.
	Certs [][]byte
}

// systemRoots is the platform trust store. It is a variable so tests can supply
// their own root, having no way to install a certificate authority on the
// machine running them.
var systemRoots = x509.SystemCertPool

// maxSignatureSize bounds the DER blob taken from a message. A signature is a
// few kilobytes; anything approaching this is not one, and refusing early keeps
// a malformed message from turning into a large allocation.
const maxSignatureSize = 1 << 20

// VerifySMIME reports whether raw, a complete RFC 822 message, carries a valid
// S/MIME signature from fromAddress.
//
// It never returns an error: a message that is not signed, or whose signature
// cannot be read, is a result rather than a failure, and the caller shows the
// status either way. Verification is a pure function of the bytes and the
// platform trust store, and makes no network calls. Whether the certificate has
// since been withdrawn is a separate question, answered by CheckRevocation only
// when the reader has turned that on.
func VerifySMIME(raw []byte, fromAddress string) Signature {
	der, content, ok := smimeParts(raw)
	if !ok {
		return Signature{Status: SigNone}
	}

	p7, err := pkcs7.Parse(der)
	if err != nil {
		return Signature{Status: SigInvalid, Detail: "the signature could not be read"}
	}
	// A detached signature covers bytes that live outside the CMS structure, so
	// they have to be handed back before it can be checked. An opaque signature
	// already carries its own content.
	if len(content) > 0 {
		p7.Content = content
	}

	signer := p7.GetOnlySigner()
	if signer == nil {
		return Signature{Status: SigInvalid, Detail: "the signature names no certificate"}
	}
	sig := Signature{
		SignerName:  signer.Subject.CommonName,
		SignerEmail: certEmail(signer),
		Issuer:      signer.Issuer.CommonName,
		Expires:     signer.NotAfter,
	}

	if err := verifyContent(p7, content); err != nil {
		// A certificate that was outside its validity window when the message
		// was signed says nothing about whether the bytes were altered, so it
		// must not be reported as tampering.
		var window *pkcs7.SigningTimeNotValidError
		if !errors.As(err, &window) {
			return Signature{Status: SigInvalid, Detail: "the message was altered after it was signed"}
		}
		sig.Status = SigUntrusted
		sig.Detail = signingWindowProblem(window)
		return sig
	}

	chain, detail := trustedChain(signer, p7.Certificates, fromAddress)
	if detail != "" {
		sig.Status = SigUntrusted
		sig.Detail = detail
		return sig
	}
	sig.Status = SigValid
	sig.Certs = chain
	return sig
}

// verifyContent checks the signature. Mail is transmitted with CRLF line
// endings and the signature is computed over them, but a message that has been
// through a store or a gateway that rewrote them would then fail against bytes
// that are otherwise untouched, so a detached signature gets a second attempt
// over the canonical form.
func verifyContent(p7 *pkcs7.PKCS7, content []byte) error {
	err := p7.Verify()
	if err == nil || len(content) == 0 {
		return err
	}
	canonical := toCRLF(content)
	if bytes.Equal(canonical, content) {
		return err
	}
	p7.Content = canonical
	return p7.Verify()
}

// signingWindowProblem describes a signature made outside the certificate's
// validity window.
func signingWindowProblem(e *pkcs7.SigningTimeNotValidError) string {
	if e.SigningTime.After(e.NotAfter) {
		return "the signing certificate had already expired when the message was signed"
	}
	return "the signing certificate was not yet valid when the message was signed"
}

// trustedChain validates the certificate and returns the signer and its issuer
// in DER, or a one-sentence reason it cannot be trusted. The issuer comes from
// the chain the platform actually built rather than from the bundle the message
// carried, so it is the one that was verified against.
func trustedChain(signer *x509.Certificate, bundle []*x509.Certificate, fromAddress string) ([][]byte, string) {
	roots, err := systemRoots()
	if err != nil || roots == nil {
		return nil, "the system certificate store could not be read"
	}
	intermediates := x509.NewCertPool()
	for _, c := range bundle {
		if !c.Equal(signer) {
			intermediates.AddCert(c)
		}
	}
	chains, err := signer.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection},
	})
	if err != nil {
		return nil, chainProblem(err)
	}

	// A certificate that is genuine but issued to somebody else does not
	// establish that this sender wrote the message.
	if detail := addressProblem(signer, fromAddress); detail != "" {
		return nil, detail
	}
	return signerAndIssuer(chains), ""
}

// addressProblem reports that the certificate names somebody other than the
// sender, or empty when it names them.
func addressProblem(signer *x509.Certificate, fromAddress string) string {
	want := bareAddress(fromAddress)
	if want == "" {
		return ""
	}
	for _, e := range signer.EmailAddresses {
		if strings.EqualFold(strings.TrimSpace(e), want) {
			return ""
		}
	}
	if got := certEmail(signer); got != "" {
		return fmt.Sprintf("the certificate was issued to %s, not %s", got, want)
	}
	return fmt.Sprintf("the certificate does not name %s", want)
}

// signerAndIssuer takes the two certificates a revocation check needs out of a
// verified chain. A self-signed certificate cannot be reached here, since it
// would not have verified, so a chain always has at least two entries.
func signerAndIssuer(chains [][]*x509.Certificate) [][]byte {
	for _, chain := range chains {
		if len(chain) >= 2 {
			return [][]byte{chain[0].Raw, chain[1].Raw}
		}
	}
	return nil
}

// chainProblem turns an x509 verification failure into a sentence a reader can
// act on, rather than the library's own wording.
func chainProblem(err error) string {
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
		return "the signing certificate has expired"
	}
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); ok {
		return "the signing certificate was issued by an authority this computer does not trust"
	}
	return "the signing certificate could not be validated"
}

// certEmail reads the address a certificate was issued to, preferring the
// subject alternative name that S/MIME actually uses.
func certEmail(c *x509.Certificate) string {
	if len(c.EmailAddresses) > 0 {
		return strings.TrimSpace(c.EmailAddresses[0])
	}
	for _, name := range c.Subject.Names {
		// the legacy emailAddress attribute, oid 1.2.840.113549.1.9.1.
		if name.Type.String() == "1.2.840.113549.1.9.1" {
			if s, ok := name.Value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// bareAddress reduces a From header to the address inside it, lowercased.
func bareAddress(header string) string {
	if addr, err := mail.ParseAddress(strings.TrimSpace(header)); err == nil {
		return strings.ToLower(strings.TrimSpace(addr.Address))
	}
	// a header the parser rejects may still be a plain address.
	s := strings.ToLower(strings.TrimSpace(header))
	if strings.Contains(s, "@") && !strings.ContainsAny(s, "<> ") {
		return s
	}
	return ""
}

// smimeParts locates the signature in a raw message and returns the DER bytes
// along with the content they cover. content is empty for an opaque signature,
// which carries its own. ok is false when the message is not S/MIME signed.
func smimeParts(raw []byte) (der, content []byte, ok bool) {
	headers, body, found := splitHeaders(raw)
	if !found {
		return nil, nil, false
	}
	mediaType, params := parseContentType(headers)

	switch {
	case mediaType == "multipart/signed" &&
		strings.EqualFold(params["protocol"], "application/pkcs7-signature"):
		return detachedParts(body, params["boundary"])
	case mediaType == "application/pkcs7-mime" &&
		strings.EqualFold(params["smime-type"], "signed-data"):
		blob, decodeErr := decodeBody(headers, body)
		if decodeErr != nil {
			return nil, nil, false
		}
		return blob, nil, true
	}
	return nil, nil, false
}

// detachedParts splits a multipart/signed body into the exact bytes that were
// signed and the signature that covers them.
//
// The signed part is taken verbatim, headers included, because the signature is
// computed over the part as it was transmitted. Re-encoding it through a MIME
// writer would change the bytes and invalidate a signature that is perfectly
// good, so nothing here parses and rebuilds it.
func detachedParts(body []byte, boundary string) (der, content []byte, ok bool) {
	if boundary == "" {
		return nil, nil, false
	}
	delimiter := []byte("--" + boundary)

	sections := splitOnBoundary(body, delimiter)
	if len(sections) < 2 {
		return nil, nil, false
	}
	content = sections[0]

	// the signature is whichever remaining part declares itself as one; a
	// message may legitimately carry more than two sections.
	for _, section := range sections[1:] {
		sigHeaders, sigBody, found := splitHeaders(section)
		if !found {
			continue
		}
		mediaType, _ := parseContentType(sigHeaders)
		if mediaType != "application/pkcs7-signature" && mediaType != "application/x-pkcs7-signature" {
			continue
		}
		blob, err := decodeBody(sigHeaders, sigBody)
		if err != nil {
			return nil, nil, false
		}
		return blob, content, true
	}
	return nil, nil, false
}

// splitOnBoundary returns the body sections between MIME boundary delimiters,
// each with the CRLF that precedes the following delimiter removed, as the MIME
// rules require.
func splitOnBoundary(body, delimiter []byte) [][]byte {
	var sections [][]byte
	rest := body
	// skip the preamble ahead of the first delimiter.
	idx := bytes.Index(rest, delimiter)
	if idx < 0 {
		return nil
	}
	rest = rest[idx+len(delimiter):]

	for {
		rest = trimLineEnding(rest)
		next := bytes.Index(rest, delimiter)
		if next < 0 {
			return sections
		}
		section := rest[:next]
		// the line break immediately before a delimiter belongs to the
		// delimiter, not to the part it terminates.
		section = trimTrailingLineEnding(section)
		sections = append(sections, section)

		rest = rest[next+len(delimiter):]
		if strings.HasPrefix(string(rest), "--") {
			return sections
		}
	}
}

// splitHeaders divides an RFC 822 entity into its header block and body at the
// first blank line, tolerating bare LF as well as CRLF.
func splitHeaders(entity []byte) (headers, body []byte, ok bool) {
	if before, after, ok := bytes.Cut(entity, []byte("\r\n\r\n")); ok {
		return before, after, true
	}
	if before, after, ok := bytes.Cut(entity, []byte("\n\n")); ok {
		return before, after, true
	}
	// an entity with headers and no body is still well formed.
	return entity, nil, len(entity) > 0
}

// parseContentType reads the Content-Type of a header block.
func parseContentType(headers []byte) (mediaType string, params map[string]string) {
	value := headerValue(headers, "content-type")
	if value == "" {
		return "", map[string]string{}
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil {
		return "", map[string]string{}
	}
	return strings.ToLower(mediaType), params
}

// headerValue returns a header's value, unfolding continuation lines.
func headerValue(headers []byte, name string) string {
	lines := strings.Split(strings.ReplaceAll(string(headers), "\r\n", "\n"), "\n")
	for i, line := range lines {
		key, value, found := strings.Cut(line, ":")
		if !found || !strings.EqualFold(strings.TrimSpace(key), name) {
			continue
		}
		var b strings.Builder
		b.WriteString(strings.TrimSpace(value))
		for _, cont := range lines[i+1:] {
			if cont == "" || (!strings.HasPrefix(cont, " ") && !strings.HasPrefix(cont, "\t")) {
				break
			}
			b.WriteString(" ")
			b.WriteString(strings.TrimSpace(cont))
		}
		return b.String()
	}
	return ""
}

// decodeBody returns a part's bytes, undoing base64 when the part says it is
// encoded that way. Signatures are always base64 in practice; binary is
// accepted because the transfer encoding, not the convention, decides.
func decodeBody(headers, body []byte) ([]byte, error) {
	if len(body) > maxSignatureSize {
		return nil, errors.New("crypto: signature part is too large")
	}
	encoding := strings.ToLower(strings.TrimSpace(headerValue(headers, "content-transfer-encoding")))
	if encoding != "base64" {
		return body, nil
	}
	// base64 in mail is wrapped across lines, which the decoder does not accept.
	packed := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, string(body))
	out, err := base64.StdEncoding.DecodeString(packed)
	if err != nil {
		return nil, fmt.Errorf("crypto: decode signature: %w", err)
	}
	return out, nil
}

// trimLineEnding removes one leading line break.
func trimLineEnding(b []byte) []byte {
	b = bytes.TrimPrefix(b, []byte("\r\n"))
	return bytes.TrimPrefix(b, []byte("\n"))
}

// trimTrailingLineEnding removes one trailing line break.
func trimTrailingLineEnding(b []byte) []byte {
	if bytes.HasSuffix(b, []byte("\r\n")) {
		return b[:len(b)-2]
	}
	return bytes.TrimSuffix(b, []byte("\n"))
}
