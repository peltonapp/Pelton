package desktop

import (
	"crypto/tls"
	"errors"
	"io"
	"os"

	"github.com/peltonapp/Pelton/internal/certtrust"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	psmtp "github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
)

// maxCAFileSize bounds what the CA picker reads. A CA bundle is a few
// kilobytes; anything near this is the wrong file.
const maxCAFileSize = 1 << 20

// errCertNotPresented means the certificate asked to be trusted is not the one
// the server presents now, so trusting it would pin something never reviewed
// against this server.
var errCertNotPresented = errors.New("pelton: the server no longer presents that certificate, check it again")

// UntrustedCertDTO is a server certificate that failed verification, laid out
// for the user to review before trusting it (#446).
type UntrustedCertDTO struct {
	// Server is "imap" or "smtp", Host and Port where it was presented.
	Server string `json:"server"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	// Fingerprint is the SHA-256 fingerprint in stored form, what trusting the
	// certificate passes back. Display is the same, formatted for reading.
	Fingerprint string   `json:"fingerprint"`
	Display     string   `json:"display"`
	Subject     string   `json:"subject"`
	Issuer      string   `json:"issuer"`
	NotBefore   string   `json:"notBefore"`
	NotAfter    string   `json:"notAfter"`
	Names       []string `json:"names"`
	SelfSigned  bool     `json:"selfSigned"`
	// Reason is what verification said, for the reader who wants it.
	Reason string `json:"reason"`
}

// ConnectionTestDTO is the result of a connection test that reached the
// servers. Untrusted lists certificates that stopped it; empty means the test
// logged in.
type ConnectionTestDTO struct {
	Untrusted []UntrustedCertDTO `json:"untrusted"`
}

// CAFileDTO is a CA file the user picked: its PEM text, which the wizard sends
// back with the new account, and the subjects of the certificates in it.
type CAFileDTO struct {
	PEM      string   `json:"pem"`
	Subjects []string `json:"subjects"`
}

// accountTrust is the certificate trust stored for an account.
func accountTrust(account storage.Account) certtrust.Trust {
	return certtrust.Trust{Pins: account.TrustedCerts, CAPEM: account.CAPEM}
}

// untrustedCert describes the certificate behind a failed verification, or
// returns nil when err is some other failure.
func untrustedCert(server, host string, port int, err error) *UntrustedCertDTO {
	cert, ok := certtrust.Untrusted(err)
	if !ok {
		return nil
	}
	d := certtrust.Describe(cert)
	var reason string
	var verr *tls.CertificateVerificationError
	if errors.As(err, &verr) && verr.Err != nil {
		reason = verr.Err.Error()
	}
	return &UntrustedCertDTO{
		Server:      server,
		Host:        host,
		Port:        port,
		Fingerprint: certtrust.Fingerprint(cert),
		Display:     d.SHA256,
		Subject:     d.Subject,
		Issuer:      d.Issuer,
		NotBefore:   formatDate(d.NotBefore),
		NotAfter:    formatDate(d.NotAfter),
		Names:       d.Names,
		SelfSigned:  d.SelfSigned,
		Reason:      reason,
	}
}

// probeCertificates checks the TLS handshake of both servers without logging
// in and returns the certificates that did not verify. Failures other than an
// untrusted certificate (a server that is down) are not reported: they say
// nothing about trust.
func (a *App) probeCertificates(imapCfg pimap.Config, smtpCfg psmtp.Config) []UntrustedCertDTO {
	var out []UntrustedCertDTO
	if imapCfg.Host != "" {
		if u := untrustedCert("imap", imapCfg.Host, imapCfg.Port, a.checkIMAPTLS(imapCfg)); u != nil {
			out = append(out, *u)
		}
	}
	if smtpCfg.Host != "" {
		if u := untrustedCert("smtp", smtpCfg.Host, smtpCfg.Port, a.checkSMTPTLS(smtpCfg)); u != nil {
			out = append(out, *u)
		}
	}
	return out
}

// checkIMAPTLS and checkSMTPTLS complete one server's handshake and hang up.
// A test answers through a.checkTLS instead of reaching a server.
func (a *App) checkIMAPTLS(cfg pimap.Config) error {
	if a.checkTLS != nil {
		return a.checkTLS("imap", cfg.Host, cfg.Port, cfg.Trust)
	}
	return pimap.CheckTLS(cfg)
}

func (a *App) checkSMTPTLS(cfg psmtp.Config) error {
	if a.checkTLS != nil {
		return a.checkTLS("smtp", cfg.Host, cfg.Port, cfg.Trust)
	}
	client, err := psmtp.Dial(cfg)
	if err != nil {
		return err
	}
	return client.Close()
}

// ProbeAccountCertificates reports the certificates an account's servers
// present that its trust does not cover. The sync failure dialog and the
// mailbox editor call it to show what changed before offering to trust it.
func (a *App) ProbeAccountCertificates(accountID int64) ([]UntrustedCertDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return nil, err
	}
	return a.probeAccount(*account)
}

// probeAccount builds the account's server configs, credentials left out since
// the handshake does not need them.
func (a *App) probeAccount(account storage.Account) ([]UntrustedCertDTO, error) {
	dial, err := a.accountDial(account)
	if err != nil {
		return nil, err
	}
	trust := accountTrust(account)
	return a.probeCertificates(
		pimap.Config{Host: account.IMAPHost, Port: account.IMAPPort, TLS: imapTLSMode(account.IMAPTLS), Trust: trust, Dial: dial},
		psmtp.Config{Host: account.SMTPHost, Port: account.SMTPPort, TLS: smtpTLSMode(account.SMTPTLS), Trust: trust, Dial: dial},
	), nil
}

// TrustAccountCertificate trusts one certificate for an account. It checks the
// servers again first and only pins a certificate one of them presents right
// now, so what gets trusted is always what the user was just shown.
func (a *App) TrustAccountCertificate(accountID int64, fingerprint string) error {
	if err := a.ready(); err != nil {
		return err
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}
	fp := certtrust.NormalizeFingerprint(fingerprint)
	untrusted, err := a.probeAccount(*account)
	if err != nil {
		return err
	}
	presented := false
	for _, u := range untrusted {
		if u.Fingerprint == fp {
			presented = true
			break
		}
	}
	if !presented {
		return errCertNotPresented
	}
	return a.store.SetAccountCertTrust(a.ctx, accountID, certtrust.AddPin(account.TrustedCerts, fp), account.CAPEM)
}

// RemoveAccountTrustedCertificate stops trusting one pinned certificate.
func (a *App) RemoveAccountTrustedCertificate(accountID int64, fingerprint string) error {
	if err := a.ready(); err != nil {
		return err
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}
	fp := certtrust.NormalizeFingerprint(fingerprint)
	kept := make([]string, 0, len(account.TrustedCerts))
	for _, p := range account.TrustedCerts {
		if certtrust.NormalizeFingerprint(p) != fp {
			kept = append(kept, p)
		}
	}
	return a.store.SetAccountCertTrust(a.ctx, accountID, kept, account.CAPEM)
}

// ChooseCAFile opens a file picker for a CA certificate and returns its
// contents once they parse. A cancelled dialog returns an empty result and no
// error.
func (a *App) ChooseCAFile() (CAFileDTO, error) {
	path, err := a.openFileDialog(openDialogOptions{
		Title: "Choose CA certificate",
		Filters: []fileFilter{
			{DisplayName: "Certificates (*.pem, *.crt, *.cer)", Pattern: "*.pem;*.crt;*.cer"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
	if err != nil {
		return CAFileDTO{}, err
	}
	if path == "" {
		return CAFileDTO{}, nil
	}
	return readCAFile(path)
}

// readCAFile reads and validates a CA file.
func readCAFile(path string) (CAFileDTO, error) {
	f, err := os.Open(path)
	if err != nil {
		return CAFileDTO{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCAFileSize+1))
	if err != nil {
		return CAFileDTO{}, err
	}
	if len(data) > maxCAFileSize {
		return CAFileDTO{}, certtrust.ErrInvalidCA
	}
	return describeCA(string(data))
}

// describeCA validates PEM text and lists the subjects in it.
func describeCA(pemText string) (CAFileDTO, error) {
	certs, err := certtrust.ParseCA(pemText)
	if err != nil {
		return CAFileDTO{}, err
	}
	subjects := make([]string, 0, len(certs))
	for _, c := range certs {
		subjects = append(subjects, c.Subject.String())
	}
	return CAFileDTO{PEM: pemText, Subjects: subjects}, nil
}

// SetAccountCA stores the CA an account trusts beyond the system roots, empty
// to remove it. The text is validated again here rather than trusted from the
// ui.
func (a *App) SetAccountCA(accountID int64, pemText string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if pemText != "" {
		if _, err := certtrust.ParseCA(pemText); err != nil {
			return err
		}
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return err
	}
	return a.store.SetAccountCertTrust(a.ctx, accountID, account.TrustedCerts, pemText)
}

// caSubjects lists the subjects of an account's CA for the mailbox editor. A
// CA that no longer parses shows as nothing rather than failing the listing.
// Nothing is an empty list, never nil: nil reaches the ui as null, and the
// editor cannot open on it.
func caSubjects(pemText string) []string {
	if pemText == "" {
		return []string{}
	}
	ca, err := describeCA(pemText)
	if err != nil {
		return []string{}
	}
	return ca.Subjects
}

// displayPins formats an account's pinned fingerprints for the mailbox editor.
func displayPins(pins []string) []string {
	out := make([]string, 0, len(pins))
	for _, p := range pins {
		out = append(out, certtrust.DisplayFingerprint(p))
	}
	return out
}

// isUntrustedCert reports whether err is a server certificate that failed
// verification, which a sync failure names on its own: the fix is reviewing
// the certificate, not the network or the password.
func isUntrustedCert(err error) bool {
	_, ok := certtrust.Untrusted(err)
	return ok
}

// normalizePins puts fingerprints from the ui into stored form, once each.
func normalizePins(pins []string) []string {
	var out []string
	for _, p := range pins {
		if certtrust.NormalizeFingerprint(p) != "" {
			out = certtrust.AddPin(out, p)
		}
	}
	return out
}
