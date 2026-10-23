package desktop

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/peltonapp/Pelton/internal/certtrust"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/storage"
)

// serverCert is a self-signed certificate like the one Proton Mail Bridge
// presents, and its PEM encoding.
func serverCert(t *testing.T) (*x509.Certificate, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	cert := srv.Certificate()
	return cert, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
}

// presenting makes every handshake present cert, verified the way certtrust
// verifies it: accepted when pinned, refused otherwise. It records the servers
// that were checked.
func presenting(a *App, cert *x509.Certificate) *[]string {
	checked := &[]string{}
	a.checkTLS = func(server, host string, port int, trust certtrust.Trust) error {
		*checked = append(*checked, fmt.Sprintf("%s %s:%d", server, host, port))
		if slices.Contains(trust.Pins, certtrust.Fingerprint(cert)) {
			return nil
		}
		return fmt.Errorf("dial: %w", &tls.CertificateVerificationError{
			UnverifiedCertificates: []*x509.Certificate{cert},
			Err:                    x509.UnknownAuthorityError{Cert: cert},
		})
	}
	return checked
}

func bridgeRequest() TestConnectionRequest {
	return TestConnectionRequest{
		Email: "me@proton.example", Password: "bridge-pass",
		IMAPHost: "127.0.0.1", IMAPPort: 1143, IMAPTLS: "starttls",
		SMTPHost: "127.0.0.1", SMTPPort: 1025, SMTPTLS: "starttls",
	}
}

// The first test against Bridge has to come back with its certificate from
// both servers, and must not try to log in past a certificate nobody
// accepted: that would send the password to an unverified server.
func TestTestConnectionReportsUntrustedCertificatesBeforeLoggingIn(t *testing.T) {
	a := newAccountTestApp(t)
	cert, _ := serverCert(t)
	checked := presenting(a, cert)
	a.newIMAPClient = func(pimap.Config) (mailClient, error) {
		t.Error("logged in past an untrusted certificate")
		return &fakeIMAP{}, nil
	}

	got, err := a.TestConnection(bridgeRequest())
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if len(got.Untrusted) != 2 || got.Untrusted[0].Server != "imap" || got.Untrusted[1].Server != "smtp" {
		t.Fatalf("Untrusted = %+v, want the imap and smtp certificates", got.Untrusted)
	}
	u := got.Untrusted[0]
	if u.Fingerprint != certtrust.Fingerprint(cert) || u.Display != certtrust.DisplayFingerprint(u.Fingerprint) || u.Port != 1143 {
		t.Errorf("Untrusted[0] = %+v, want the certificate at 127.0.0.1:1143", u)
	}
	if u.Reason == "" {
		t.Error("Untrusted[0] carries no verification reason")
	}
	if want := []string{"imap 127.0.0.1:1143", "smtp 127.0.0.1:1025"}; !slices.Equal(*checked, want) {
		t.Errorf("checked %v, want %v", *checked, want)
	}
}

func TestTestConnectionLogsInOnceTheCertificateIsTrusted(t *testing.T) {
	a := newAccountTestApp(t)
	cert, _ := serverCert(t)
	presenting(a, cert)
	client := &fakeIMAP{}
	var trust certtrust.Trust
	a.newIMAPClient = func(cfg pimap.Config) (mailClient, error) {
		trust = cfg.Trust
		return client, nil
	}

	req := bridgeRequest()
	req.TrustedCerts = []string{certtrust.DisplayFingerprint(certtrust.Fingerprint(cert))}
	got, err := a.TestConnection(req)
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if len(got.Untrusted) != 0 {
		t.Errorf("Untrusted = %+v, want none once trusted", got.Untrusted)
	}
	if !client.loggedOut.Load() {
		t.Error("test did not log in and out")
	}
	if len(trust.Pins) != 1 {
		t.Errorf("login used trust %+v, want the accepted certificate", trust)
	}
}

func TestTestConnectionRefusesAnInvalidCA(t *testing.T) {
	a := newAccountTestApp(t)
	req := bridgeRequest()
	req.CAPEM = "not a certificate"
	if _, err := a.TestConnection(req); !errors.Is(err, certtrust.ErrInvalidCA) {
		t.Errorf("TestConnection() error = %v, want ErrInvalidCA", err)
	}
}

// trustTestAccount creates the account a changed certificate turns up on.
func trustTestAccount(t *testing.T, a *App) int64 {
	t.Helper()
	id, err := a.store.CreateAccount(a.ctx, &storage.Account{
		Email: "me@proton.example", IMAPHost: "127.0.0.1", IMAPPort: 1143, SMTPHost: "127.0.0.1", SMTPPort: 1025,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return id
}

func TestTrustAccountCertificatePinsWhatTheServerPresents(t *testing.T) {
	a := newAccountTestApp(t)
	cert, _ := serverCert(t)
	presenting(a, cert)
	id := trustTestAccount(t, a)
	fp := certtrust.Fingerprint(cert)

	probed, err := a.ProbeAccountCertificates(id)
	if err != nil || len(probed) != 2 {
		t.Fatalf("ProbeAccountCertificates() = %+v, %v, want both servers' certificate", probed, err)
	}
	if err := a.TrustAccountCertificate(id, probed[0].Fingerprint); err != nil {
		t.Fatalf("TrustAccountCertificate: %v", err)
	}

	account, err := a.store.GetAccount(a.ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if !slices.Equal(account.TrustedCerts, []string{fp}) {
		t.Errorf("TrustedCerts = %v, want [%s]", account.TrustedCerts, fp)
	}
	if again, _ := a.ProbeAccountCertificates(id); len(again) != 0 {
		t.Errorf("still untrusted after trusting: %+v", again)
	}
	if dto := toAccountDTO(*account); !slices.Equal(dto.TrustedCerts, []string{certtrust.DisplayFingerprint(fp)}) {
		t.Errorf("AccountDTO.TrustedCerts = %v, want the display fingerprint", dto.TrustedCerts)
	}
}

// Only a certificate a server presents right now can be trusted, so the ui
// cannot pin something the user never saw against this server.
func TestTrustAccountCertificateRefusesACertificateNotPresented(t *testing.T) {
	a := newAccountTestApp(t)
	cert, _ := serverCert(t)
	presenting(a, cert)
	id := trustTestAccount(t, a)

	other := "ab" + certtrust.Fingerprint(cert)[2:]
	if err := a.TrustAccountCertificate(id, other); !errors.Is(err, errCertNotPresented) {
		t.Errorf("TrustAccountCertificate(other) error = %v, want errCertNotPresented", err)
	}
	account, _ := a.store.GetAccount(a.ctx, id)
	if len(account.TrustedCerts) != 0 {
		t.Errorf("TrustedCerts = %v, want nothing pinned", account.TrustedCerts)
	}
}

func TestRemoveAccountTrustedCertificate(t *testing.T) {
	a := newAccountTestApp(t)
	id := trustTestAccount(t, a)
	keep, drop := "aa"+fmt.Sprint(1), "bb"+fmt.Sprint(2)
	if err := a.store.SetAccountCertTrust(a.ctx, id, []string{keep, drop}, ""); err != nil {
		t.Fatalf("set trust: %v", err)
	}

	if err := a.RemoveAccountTrustedCertificate(id, "BB:2"); err != nil {
		t.Fatalf("RemoveAccountTrustedCertificate: %v", err)
	}
	account, _ := a.store.GetAccount(a.ctx, id)
	if !slices.Equal(account.TrustedCerts, []string{keep}) {
		t.Errorf("TrustedCerts = %v, want [%s]", account.TrustedCerts, keep)
	}
}

func TestSetAccountCA(t *testing.T) {
	a := newAccountTestApp(t)
	cert, caPEM := serverCert(t)
	id := trustTestAccount(t, a)

	if err := a.SetAccountCA(id, "not a certificate"); !errors.Is(err, certtrust.ErrInvalidCA) {
		t.Errorf("SetAccountCA(garbage) error = %v, want ErrInvalidCA", err)
	}
	if err := a.SetAccountCA(id, caPEM); err != nil {
		t.Fatalf("SetAccountCA: %v", err)
	}
	account, _ := a.store.GetAccount(a.ctx, id)
	if dto := toAccountDTO(*account); !slices.Equal(dto.CASubjects, []string{cert.Subject.String()}) {
		t.Errorf("AccountDTO.CASubjects = %v, want the CA's subject", dto.CASubjects)
	}
	if err := a.SetAccountCA(id, ""); err != nil {
		t.Fatalf("SetAccountCA(clear): %v", err)
	}
	account, _ = a.store.GetAccount(a.ctx, id)
	if account.CAPEM != "" {
		t.Error("CA still stored after clearing")
	}
}

// A mailbox that trusts nothing extra, which is nearly every mailbox, has to
// reach the ui with empty lists: the editor reads their length, and null
// kept it from opening at all (#470).
func TestAccountDTOSendsEmptyTrustAsEmptyLists(t *testing.T) {
	for name, caPEM := range map[string]string{"no CA": "", "unreadable CA": "not a certificate"} {
		raw, err := json.Marshal(toAccountDTO(storage.Account{CAPEM: caPEM}))
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		for _, key := range []string{"trustedCerts", "caSubjects"} {
			if string(got[key]) != "[]" {
				t.Errorf("%s: %s = %s, want []", name, key, got[key])
			}
		}
	}
}

// A sync stopped by the certificate gets its own reason, so the failure dialog
// offers the certificate instead of blaming the network or the password.
func TestSyncFailureReasonNamesAnUntrustedCertificate(t *testing.T) {
	cert, _ := serverCert(t)
	err := fmt.Errorf("imap: dial 127.0.0.1:1143: %w", &tls.CertificateVerificationError{
		UnverifiedCertificates: []*x509.Certificate{cert},
		Err:                    x509.UnknownAuthorityError{Cert: cert},
	})
	if got := syncFailureReason(err); got != syncFailCertificate {
		t.Errorf("syncFailureReason() = %q, want %q", got, syncFailCertificate)
	}
}
