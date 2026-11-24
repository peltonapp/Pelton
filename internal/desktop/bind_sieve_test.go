package desktop

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/certtrust"
	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/managesieve"
	"github.com/peltonapp/Pelton/internal/managesieve/sievetest"
	"github.com/peltonapp/Pelton/internal/sieve"
	"github.com/peltonapp/Pelton/internal/storage"
)

const sieveTestEmail = "me@example.test"

// sieveFixture is an app with one account whose sieve service is the fake,
// reached over STARTTLS with the fake's self-signed certificate pinned, and
// whose password sits in the mocked keyring.
type sieveFixture struct {
	app *App
	srv *sievetest.Server
	id  int64
}

func newSieveFixture(t *testing.T, extensions ...string) sieveFixture {
	t.Helper()
	certSrv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certSrv.Close()
	tlsCfg := certSrv.TLS.Clone()
	tlsCfg.NextProtos = nil

	if len(extensions) == 0 {
		extensions = []string{"fileinto", "envelope", "vacation", "imap4flags", "date", "relational", "include"}
	}
	srv, err := sievetest.Start(sievetest.Options{
		Username: sieveTestEmail, Password: "secret", TLS: tlsCfg, Extensions: extensions,
		Check: func(script string) string {
			for i, line := range strings.Split(script, "\r\n") {
				if strings.Contains(line, "bogus") {
					return "line " + strconv.Itoa(i+1) + ": unknown command bogus"
				}
				if strings.HasPrefix(line, "require ") {
					for _, ext := range []string{"vacation", "include", "date", "fileinto", "imap4flags", "envelope", "relational"} {
						if strings.Contains(line, `"`+ext+`"`) && !slices.Contains(extensions, ext) {
							return "line " + strconv.Itoa(i+1) + ": unsupported extension " + ext
						}
					}
				}
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("start fake: %v", err)
	}
	t.Cleanup(srv.Close)

	a := newAccountTestApp(t)
	id, err := a.store.CreateAccount(a.ctx, &storage.Account{
		Email: sieveTestEmail, IMAPHost: "127.0.0.1", IMAPPort: 993,
		TrustedCerts: []string{certtrust.Fingerprint(certSrv.Certificate())},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := credentials.Store(id, credentials.Secret{Method: credentials.MethodPassword, Password: "secret"}); err != nil {
		t.Fatalf("store mock secret: %v", err)
	}
	t.Cleanup(func() { _ = credentials.Delete(id) })
	_, port := srv.Addr()
	state, err := a.SetSieveConnection(id, "127.0.0.1", port, "starttls")
	if err != nil {
		t.Fatalf("set connection: %v", err)
	}
	if state.Status != sieveOK {
		t.Fatalf("status = %q (%s), want ok", state.Status, state.Detail)
	}
	return sieveFixture{app: a, srv: srv, id: id}
}

func vacation() sieve.Vacation {
	return sieve.Vacation{
		Enabled: true, Start: "2026-12-20", End: "2027-01-06", Days: 5,
		Subject: "Away", Addresses: []string{sieveTestEmail, " "}, Body: "Back in January.",
	}
}

func TestSaveVacationWritesAndActivatesPeltonsScript(t *testing.T) {
	f := newSieveFixture(t)
	got, err := f.app.SaveSieveVacation(f.id, vacation())
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.Problem != "" {
		t.Fatalf("server refused the script: %s", got.Problem)
	}
	if f.srv.Active() != sieveScriptName {
		t.Fatalf("active = %q, want %q", f.srv.Active(), sieveScriptName)
	}
	text, _ := f.srv.Script(sieveScriptName)
	if !strings.Contains(text, "\r\n") {
		t.Error("the script was not sent with CRLF line ends")
	}
	if _, err := sieve.Parse(text); err != nil {
		t.Fatalf("stored script does not parse back: %v", err)
	}
	st := got.State
	if !st.Exists || !st.Active || !st.Editable || !st.HasPrevious {
		t.Errorf("state = %+v, want an active editable script with a previous", st)
	}
	v := st.Script.Vacation
	if v == nil || v.Days != 5 || !slices.Equal(v.Addresses, []string{sieveTestEmail}) {
		t.Errorf("vacation = %+v", v)
	}
	if !slices.Equal(st.Addresses, []string{sieveTestEmail}) {
		t.Errorf("addresses = %v", st.Addresses)
	}
}

func TestSaveKeepsTheOtherActiveScriptRunning(t *testing.T) {
	f := newSieveFixture(t)
	other := "require \"fileinto\";\r\nif header :contains \"x-spam\" \"yes\" { fileinto \"Junk\"; }\r\n"
	f.srv.SetScript("roundcube", other, true)

	before, err := f.app.SieveState(f.id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if before.OtherActive != "roundcube" {
		t.Fatalf("other active = %q, want roundcube", before.OtherActive)
	}

	rules := []sieve.Rule{{
		Name: "Lists", Enabled: true,
		Conditions: []sieve.Condition{{Field: sieve.FieldSize, Op: sieve.OpOver, Value: "2m"}},
		Actions:    []sieve.Action{{Kind: sieve.ActionFileInto, Arg: "Lists"}},
	}}
	got, err := f.app.SaveSieveRules(f.id, rules)
	if err != nil || got.Problem != "" {
		t.Fatalf("save: %v %s", err, got.Problem)
	}
	if !slices.Equal(got.State.Script.Includes, []string{"roundcube"}) {
		t.Errorf("includes = %v, want the other script", got.State.Script.Includes)
	}
	if got.State.Script.Rules[0].Conditions[0].Value != "2M" {
		t.Errorf("size = %q, want it upper cased", got.State.Script.Rules[0].Conditions[0].Value)
	}
	if text, _ := f.srv.Script("roundcube"); text != other {
		t.Error("the other script was rewritten")
	}
}

func TestRestorePreviousSwapsBackAndForth(t *testing.T) {
	f := newSieveFixture(t, "fileinto", "vacation")
	f.srv.SetScript("roundcube", "keep;\r\n", true)

	first := vacation()
	first.Start, first.End = "", ""
	if _, err := f.app.SaveSieveVacation(f.id, first); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if f.srv.Active() != sieveScriptName {
		t.Fatal("pelton's script is not active after a save")
	}
	firstText, _ := f.srv.Script(sieveScriptName)
	if strings.Contains(firstText, "include") {
		t.Error("include written for a server without the extension")
	}

	second := first
	second.Subject = "Still away"
	if _, err := f.app.SaveSieveVacation(f.id, second); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if backup, _ := f.srv.Script(sieveBackupName); backup != firstText {
		t.Fatal("the backup is not the version before the save")
	}
	secondText, _ := f.srv.Script(sieveScriptName)

	state, err := f.app.RestoreSievePrevious(f.id)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if text, _ := f.srv.Script(sieveScriptName); text != firstText {
		t.Error("restore did not bring the previous version back")
	}
	if backup, _ := f.srv.Script(sieveBackupName); backup != secondText {
		t.Error("restore lost the version it replaced")
	}
	if state.Script.Vacation.Subject != "Away" {
		t.Errorf("state after restore = %+v", state.Script.Vacation)
	}
	if _, err := f.app.RestoreSievePrevious(f.id); err != nil {
		t.Fatalf("restore again: %v", err)
	}
	if text, _ := f.srv.Script(sieveScriptName); text != secondText {
		t.Error("restoring twice did not return to the saved version")
	}
}

func TestRestoreReactivatesTheScriptActiveBeforeTheFirstSave(t *testing.T) {
	f := newSieveFixture(t, "vacation")
	f.srv.SetScript("roundcube", "keep;\r\n", true)
	v := vacation()
	v.Start, v.End = "", ""
	if _, err := f.app.SaveSieveVacation(f.id, v); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := f.app.RestoreSievePrevious(f.id); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if f.srv.Active() != "roundcube" {
		t.Fatalf("active after restore = %q, want roundcube", f.srv.Active())
	}
}

func TestRestoreWithoutASaveIsRefused(t *testing.T) {
	f := newSieveFixture(t)
	if _, err := f.app.RestoreSievePrevious(f.id); !errors.Is(err, errNothingToRestore) {
		t.Fatalf("restore = %v, want errNothingToRestore", err)
	}
}

func TestForeignScriptIsNeverRewrittenFromTheModel(t *testing.T) {
	f := newSieveFixture(t)
	foreign := "require \"fileinto\";\r\nif true { fileinto \"X\"; }\r\n"
	f.srv.SetScript(sieveScriptName, foreign, true)

	state, err := f.app.SieveState(f.id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.Editable || state.Raw != foreign {
		t.Fatalf("state = editable %v raw %q, want read-only with the raw text", state.Editable, state.Raw)
	}
	if _, err := f.app.SaveSieveVacation(f.id, vacation()); !errors.Is(err, errSieveNotEditable) {
		t.Fatalf("model save over a foreign script = %v, want errSieveNotEditable", err)
	}
	if text, _ := f.srv.Script(sieveScriptName); text != foreign {
		t.Fatal("the foreign script was changed")
	}

	edited := "require \"fileinto\";\nif true { fileinto \"Y\"; }\n"
	got, err := f.app.SaveSieveScript(f.id, edited)
	if err != nil || got.Problem != "" {
		t.Fatalf("raw save: %v %s", err, got.Problem)
	}
	if backup, _ := f.srv.Script(sieveBackupName); backup != foreign {
		t.Error("the raw save kept no backup")
	}
}

func TestRejectedScriptWritesNothing(t *testing.T) {
	f := newSieveFixture(t)
	got, err := f.app.SaveSieveScript(f.id, "keep;\nbogus;\n")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.Problem == "" || got.Line != 2 {
		t.Fatalf("problem = %q line %d, want the server's error on line 2", got.Problem, got.Line)
	}
	if slices.Contains(f.srv.Commands(), "PUTSCRIPT") || f.srv.Active() != "" {
		t.Fatal("a script the server refused was written")
	}

	check, err := f.app.CheckSieveScript(f.id, "keep;\n")
	if err != nil || check.Problem != "" {
		t.Fatalf("check of a good script: %v %q", err, check.Problem)
	}
}

func TestMissingExtensionIsRefusedBeforeTheServer(t *testing.T) {
	f := newSieveFixture(t, "fileinto")
	state, err := f.app.SieveState(f.id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.Caps.Vacation || state.Caps.Dates || state.Caps.Include || !state.Caps.FileInto {
		t.Errorf("caps = %+v", state.Caps)
	}
	var unsupported *sieve.UnsupportedError
	if _, err := f.app.SaveSieveVacation(f.id, vacation()); !errors.As(err, &unsupported) {
		t.Fatalf("save = %v, want an UnsupportedError", err)
	}
	if slices.Contains(f.srv.Commands(), "CHECKSCRIPT") {
		t.Error("a script the server cannot run was sent")
	}
}

func TestUnsupportedServerIsRememberedAndNotDialed(t *testing.T) {
	a := newAccountTestApp(t)
	id, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "x@example.test", IMAPHost: "mail.example.test"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := a.store.SetAccountSieve(a.ctx, storage.AccountSieve{AccountID: id, Unsupported: true}); err != nil {
		t.Fatalf("set: %v", err)
	}
	state, err := a.SieveState(id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.Status != sieveUnsupported || !state.Remembered {
		t.Fatalf("state = %+v, want a remembered unsupported", state)
	}

	gmail, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "y@gmail.com", IMAPHost: "imap.gmail.com"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	state, err = a.SieveState(gmail)
	if err != nil || state.Status != sieveUnsupported || state.Remembered {
		t.Fatalf("gmail = %+v %v, want unsupported without a check", state, err)
	}
}

func TestSieveWithoutCredentials(t *testing.T) {
	f := newSieveFixture(t)
	if err := credentials.Delete(f.id); err != nil {
		t.Fatalf("delete mock secret: %v", err)
	}
	state, err := f.app.SieveState(f.id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state.Status != sieveCredentials {
		t.Fatalf("status = %q, want credentials", state.Status)
	}
}

func TestClassifySieveDial(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"refused", &net.OpError{Op: "dial", Err: errors.New("connect: connection refused")}, sieveUnsupported},
		{"silent port", context.DeadlineExceeded, sieveUnsupported},
		{"imap on the port", managesieve.ErrNotManageSieve, sieveUnsupported},
		{"no dns", &net.DNSError{Err: "no such host", Name: "x"}, sieveUnreachable},
		{"offline", errors.New("dial tcp: connect: network is unreachable"), sieveUnreachable},
		{"no starttls", managesieve.ErrNoStartTLS, sieveTLS},
		{"other", errors.New("something odd"), sieveError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := classifySieveDial(tt.err); got != tt.want {
				t.Errorf("classify(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
