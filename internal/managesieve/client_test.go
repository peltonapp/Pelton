package managesieve

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/peltonapp/Pelton/internal/certtrust"
	"github.com/peltonapp/Pelton/internal/managesieve/sievetest"
)

// selfSigned returns httptest's self-signed server config and the
// fingerprint to pin it by.
func selfSigned(t *testing.T) (*tls.Config, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	cfg := srv.TLS.Clone()
	cfg.NextProtos = nil
	return cfg, certtrust.Fingerprint(srv.Certificate())
}

func startServer(t *testing.T, opts sievetest.Options) (*sievetest.Server, Config) {
	t.Helper()
	if opts.Username == "" {
		opts.Username, opts.Password = "user", "pass"
	}
	srv, err := sievetest.Start(opts)
	if err != nil {
		t.Fatalf("start fake: %v", err)
	}
	t.Cleanup(srv.Close)
	host, port := srv.Addr()
	return srv, Config{Host: host, Port: port, Username: "user", Password: "pass"}
}

func dialAuthed(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := Dial(cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Authenticate(); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	return c
}

func TestFullDialogueOverStartTLS(t *testing.T) {
	tlsCfg, pin := selfSigned(t)
	srv, cfg := startServer(t, sievetest.Options{
		TLS: tlsCfg,
		Check: func(script string) string {
			if strings.Contains(script, "bogus") {
				return "line 2: unknown command 'bogus'"
			}
			return ""
		},
	})
	cfg.Trust = certtrust.Trust{Pins: []string{pin}}
	c := dialAuthed(t, cfg)

	if !c.Capabilities().HasExtension("vacation") || !c.Capabilities().HasSASL("plain") {
		t.Fatalf("capabilities after STARTTLS = %+v", c.Capabilities())
	}
	if c.Capabilities().StartTLS {
		t.Error("STARTTLS still advertised after the upgrade")
	}

	script := "require \"fileinto\";\r\n# \"quoted\" and \\ backslash\r\nfileinto \"Lists\";\r\n"
	if err := c.CheckScript(script); err != nil {
		t.Fatalf("checkscript: %v", err)
	}
	if err := c.PutScript("pelton", script); err != nil {
		t.Fatalf("putscript: %v", err)
	}
	if err := c.PutScript("other", "keep;"); err != nil {
		t.Fatalf("putscript other: %v", err)
	}
	if err := c.SetActive("pelton"); err != nil {
		t.Fatalf("setactive: %v", err)
	}
	list, err := c.ListScripts()
	if err != nil {
		t.Fatalf("listscripts: %v", err)
	}
	want := []Script{{Name: "other"}, {Name: "pelton", Active: true}}
	if !slices.Equal(list, want) {
		t.Fatalf("listscripts = %+v, want %+v", list, want)
	}
	got, err := c.GetScript("pelton")
	if err != nil {
		t.Fatalf("getscript: %v", err)
	}
	if got != script {
		t.Fatalf("getscript = %q, want %q", got, script)
	}

	err = c.CheckScript("keep;\nbogus;\n")
	var serr *ServerError
	if !errors.As(err, &serr) {
		t.Fatalf("checkscript of a bad script = %v, want a ServerError", err)
	}
	if serr.Line() != 2 || !strings.Contains(serr.Message, "bogus") {
		t.Errorf("error = %q line %d, want line 2 naming bogus", serr.Message, serr.Line())
	}
	if _, ok := srv.Script("bad"); ok {
		t.Error("a rejected script was stored")
	}

	if err := c.DeleteScript("pelton"); err == nil {
		t.Error("deleting the active script succeeded")
	}
	if err := c.DeleteScript("other"); err != nil {
		t.Errorf("deletescript: %v", err)
	}
	if _, err := c.GetScript("other"); !errors.As(err, &serr) || serr.Code != "NONEXISTENT" {
		t.Errorf("getscript of a deleted script = %v, want NONEXISTENT", err)
	}
	if err := c.SetActive(""); err != nil || srv.Active() != "" {
		t.Errorf("deactivate: %v, active %q", err, srv.Active())
	}
	if err := c.Logout(); err != nil {
		t.Errorf("logout: %v", err)
	}
}

func TestUntrustedCertificateIsReported(t *testing.T) {
	tlsCfg, _ := selfSigned(t)
	_, cfg := startServer(t, sievetest.Options{TLS: tlsCfg})
	_, err := Dial(cfg)
	if _, ok := certtrust.Untrusted(err); !ok {
		t.Fatalf("dial with an unpinned self-signed certificate = %v, want an untrusted certificate", err)
	}
}

func TestStartTLSRefusedSendsNoCredentials(t *testing.T) {
	tlsCfg, pin := selfSigned(t)
	for _, tt := range []struct {
		name string
		opts sievetest.Options
	}{
		{"refused", sievetest.Options{TLS: tlsCfg, RefuseStartTLS: true}},
		{"not offered", sievetest.Options{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv, cfg := startServer(t, tt.opts)
			cfg.Trust = certtrust.Trust{Pins: []string{pin}}
			_, err := Dial(cfg)
			if !errors.Is(err, ErrNoStartTLS) {
				t.Fatalf("dial = %v, want ErrNoStartTLS", err)
			}
			if slices.Contains(srv.Commands(), "AUTHENTICATE") {
				t.Error("credentials were sent without TLS")
			}
		})
	}
}

func TestWrongPasswordIsAnAuthFailure(t *testing.T) {
	tlsCfg, pin := selfSigned(t)
	_, cfg := startServer(t, sievetest.Options{TLS: tlsCfg})
	cfg.Trust = certtrust.Trust{Pins: []string{pin}}
	cfg.Password = "wrong"
	c, err := Dial(cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.Authenticate(); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("authenticate = %v, want ErrAuthFailed", err)
	}
}

func TestLoginMechanismWhenPlainIsMissing(t *testing.T) {
	tlsCfg, pin := selfSigned(t)
	_, cfg := startServer(t, sievetest.Options{TLS: tlsCfg, SASL: []string{"LOGIN"}})
	cfg.Trust = certtrust.Trust{Pins: []string{pin}}
	c := dialAuthed(t, cfg)
	if _, err := c.ListScripts(); err != nil {
		t.Fatalf("listscripts after LOGIN: %v", err)
	}
}

func TestOAuthNeedsXOAuth2(t *testing.T) {
	tlsCfg, pin := selfSigned(t)
	_, cfg := startServer(t, sievetest.Options{TLS: tlsCfg})
	cfg.Trust = certtrust.Trust{Pins: []string{pin}}
	cfg.Password = ""
	cfg.OAuth2Token = "token"
	c, err := Dial(cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if err := c.Authenticate(); !errors.Is(err, ErrNoMechanism) {
		t.Fatalf("authenticate = %v, want ErrNoMechanism", err)
	}
}

func TestConnectionLossMidCommand(t *testing.T) {
	tlsCfg, pin := selfSigned(t)
	_, cfg := startServer(t, sievetest.Options{TLS: tlsCfg, DropOn: "LISTSCRIPTS"})
	cfg.Trust = certtrust.Trust{Pins: []string{pin}}
	c := dialAuthed(t, cfg)
	if _, err := c.ListScripts(); !errors.Is(err, ErrConnectionLost) {
		t.Fatalf("listscripts on a dropped connection = %v, want ErrConnectionLost", err)
	}
}

func TestNotAManageSieveServer(t *testing.T) {
	_, cfg := startServer(t, sievetest.Options{Greeting: "* OK [CAPABILITY IMAP4rev1] ready\r\n"})
	if _, err := Dial(cfg); !errors.Is(err, ErrNotManageSieve) {
		t.Fatalf("dial an imap greeting = %v, want ErrNotManageSieve", err)
	}
}

func TestDialGoesThroughTheProxyDialer(t *testing.T) {
	tlsCfg, pin := selfSigned(t)
	_, cfg := startServer(t, sievetest.Options{TLS: tlsCfg})
	cfg.Trust = certtrust.Trust{Pins: []string{pin}}
	var used atomic.Int32
	cfg.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		used.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	dialAuthed(t, cfg)
	if used.Load() != 1 {
		t.Fatalf("proxy dialer used %d times, want 1", used.Load())
	}
}

func TestResponseParsing(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want response
	}{
		{"OK\r\n", response{status: "OK"}},
		{"NO \"plain text\"\r\n", response{status: "NO", text: "plain text"}},
		{"NO (NONEXISTENT) \"gone\"\r\n", response{status: "NO", code: "NONEXISTENT", text: "gone"}},
		{"NO (TAG \"x1\") \"tagged\"\r\n", response{status: "NO", code: "TAG", codeArg: "x1", text: "tagged"}},
		{"NO {13}\r\nline 1: \"bad\"\r\n", response{status: "NO", text: "line 1: \"bad\""}},
		{"BYE \"esc \\\" quote\"\r\n", response{status: "BYE", text: "esc \" quote"}},
	} {
		r := reader{br: bufio.NewReader(strings.NewReader(tt.in))}
		line, err := r.readLine()
		if err != nil {
			t.Fatalf("%q: %v", tt.in, err)
		}
		got, ok := asResponse(line)
		if !ok || got != tt.want {
			t.Errorf("%q = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestQuote(t *testing.T) {
	if got := quote(`a "b" \c`); got != `"a \"b\" \\c"` {
		t.Errorf("quote = %s", got)
	}
	if got := quote("two\nlines"); got != "{9+}\r\ntwo\nlines" {
		t.Errorf("quote of a multi-line value = %q", got)
	}
}
