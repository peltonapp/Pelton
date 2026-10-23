// Package certtrust decides whether a mail server's TLS certificate is
// trusted for one mailbox. By default that is the system's normal
// verification. A mailbox can widen it in two explicit ways, never by turning
// verification off: pinning the exact certificates the user reviewed and
// accepted (Proton Mail Bridge, a self-signed home server), and adding a CA
// the user supplied as PEM (an internal company CA) on top of the system
// roots (#446).
package certtrust

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
	"time"
)

// Trust is what one mailbox trusts beyond the system roots.
type Trust struct {
	// Pins are SHA-256 fingerprints of leaf certificates the user accepted,
	// lowercase hex without separators. A pinned certificate is accepted as is:
	// hostname and validity dates are not checked, since the user trusted those
	// exact bytes and self-signed certificates routinely fail both.
	Pins []string
	// CAPEM holds extra root certificates, PEM encoded. Certificates chaining to
	// them are verified normally, hostname and dates included.
	CAPEM string
}

// IsZero reports whether the trust adds nothing to the system roots.
func (t Trust) IsZero() bool {
	return len(t.Pins) == 0 && strings.TrimSpace(t.CAPEM) == ""
}

// ErrInvalidCA means the CA text holds no certificate that could be parsed.
var ErrInvalidCA = errors.New("certtrust: no certificate found in the CA file")

// TLSConfig returns the client tls config for serverName. With no extra trust
// it is the standard verifying config. Otherwise the standard verification is
// replaced by one that accepts a pinned leaf, and verifies anything else
// against the system roots plus CAPEM with the usual hostname check.
func (t Trust) TLSConfig(serverName string) (*tls.Config, error) {
	cfg := &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}
	if t.IsZero() {
		return cfg, nil
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if strings.TrimSpace(t.CAPEM) != "" {
		cas, err := ParseCA(t.CAPEM)
		if err != nil {
			return nil, err
		}
		for _, ca := range cas {
			roots.AddCert(ca)
		}
	}
	pins := make(map[string]struct{}, len(t.Pins))
	for _, p := range t.Pins {
		pins[NormalizeFingerprint(p)] = struct{}{}
	}

	// InsecureSkipVerify only switches off the built-in check so VerifyConnection
	// can run this one in its place; it is never set without it.
	cfg.InsecureSkipVerify = true
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("certtrust: server sent no certificate")
		}
		leaf := cs.PeerCertificates[0]
		if _, ok := pins[Fingerprint(leaf)]; ok {
			return nil
		}
		intermediates := x509.NewCertPool()
		for _, c := range cs.PeerCertificates[1:] {
			intermediates.AddCert(c)
		}
		_, err := leaf.Verify(x509.VerifyOptions{
			DNSName:       serverName,
			Roots:         roots,
			Intermediates: intermediates,
		})
		if err != nil {
			return &tls.CertificateVerificationError{UnverifiedCertificates: cs.PeerCertificates, Err: err}
		}
		return nil
	}
	return cfg, nil
}

// Fingerprint is the SHA-256 fingerprint of a certificate, lowercase hex.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// NormalizeFingerprint turns a fingerprint in any common spelling ("AB:CD..",
// "ab cd..") into the stored form.
func NormalizeFingerprint(fp string) string {
	return strings.ToLower(strings.NewReplacer(":", "", " ", "", "-", "").Replace(strings.TrimSpace(fp)))
}

// Untrusted returns the certificate a server presented when err is a failed
// certificate verification, so the caller can show it and offer to trust it.
// ok is false for any other error.
func Untrusted(err error) (cert *x509.Certificate, ok bool) {
	var verr *tls.CertificateVerificationError
	if errors.As(err, &verr) && len(verr.UnverifiedCertificates) > 0 {
		return verr.UnverifiedCertificates[0], true
	}
	return nil, false
}

// ParseCA validates PEM text as a CA bundle and returns the certificates it
// holds, so a wrong file is refused when it is picked, not at the next
// connection.
func ParseCA(pemText string) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := []byte(pemText)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, ErrInvalidCA
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, ErrInvalidCA
	}
	return certs, nil
}

// Details is a certificate summarized for the user to review.
type Details struct {
	Subject   string    `json:"subject"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	// Names are the DNS names and IP addresses the certificate is for.
	Names []string `json:"names"`
	// SHA256 is the fingerprint as colon separated upper-case hex, the way
	// browsers and Thunderbird show it, so it can be compared by eye.
	SHA256     string `json:"sha256"`
	SelfSigned bool   `json:"selfSigned"`
}

// Describe summarizes a certificate for the trust prompt.
func Describe(cert *x509.Certificate) Details {
	names := append([]string{}, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		names = append(names, ip.String())
	}
	return Details{
		Subject:    cert.Subject.String(),
		Issuer:     cert.Issuer.String(),
		NotBefore:  cert.NotBefore,
		NotAfter:   cert.NotAfter,
		Names:      names,
		SHA256:     DisplayFingerprint(Fingerprint(cert)),
		SelfSigned: cert.CheckSignatureFrom(cert) == nil,
	}
}

// DisplayFingerprint formats a fingerprint as AB:CD:...
func DisplayFingerprint(fp string) string {
	fp = strings.ToUpper(NormalizeFingerprint(fp))
	parts := make([]string, 0, len(fp)/2)
	for i := 0; i+2 <= len(fp); i += 2 {
		parts = append(parts, fp[i:i+2])
	}
	return strings.Join(parts, ":")
}

// FormatPins is the stored text form of pins: one fingerprint per line.
func FormatPins(pins []string) string {
	return strings.Join(pins, "\n")
}

// ParsePins reads the stored text form back into a list.
func ParsePins(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if fp := NormalizeFingerprint(line); fp != "" {
			out = append(out, fp)
		}
	}
	return out
}

// AddPin returns pins with fp added once.
func AddPin(pins []string, fp string) []string {
	fp = NormalizeFingerprint(fp)
	for _, p := range pins {
		if NormalizeFingerprint(p) == fp {
			return pins
		}
	}
	return append(append([]string{}, pins...), fp)
}
