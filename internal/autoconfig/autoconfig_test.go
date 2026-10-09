package autoconfig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// a trimmed Thunderbird autoconfig document, shaped like the ones the ISPDB
// serves: pop3 is offered before imap, and the provider asks for OAuth2.
const oauthProviderXML = `<?xml version="1.0" encoding="UTF-8"?>
<clientConfig version="1.1">
  <emailProvider id="example.com">
    <incomingServer type="pop3">
      <hostname>pop.example.com</hostname>
      <port>995</port>
      <socketType>SSL</socketType>
      <authentication>password-cleartext</authentication>
    </incomingServer>
    <incomingServer type="imap">
      <hostname>imap.example.com</hostname>
      <port>993</port>
      <socketType>SSL</socketType>
      <authentication>OAuth2</authentication>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>smtp.example.com</hostname>
      <port>587</port>
      <socketType>STARTTLS</socketType>
      <authentication>OAuth2</authentication>
    </outgoingServer>
  </emailProvider>
</clientConfig>`

func TestParseReadsTheImapAndSmtpServers(t *testing.T) {
	got, err := parse([]byte(oauthProviderXML), "ispdb")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := Discovered{
		IMAPHost: "imap.example.com",
		IMAPPort: 993,
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
		IMAPTLS:  "ssl",
		SMTPTLS:  "starttls",
		OAuth:    true,
		Source:   "ispdb",
	}
	if got != want {
		t.Errorf("parse() = %+v, want %+v", got, want)
	}
}

// A document that offers pop3 first must not leave the imap fields empty: the
// wizard would then fall through to a guessed host for a provider that told us
// the answer.
func TestParseSkipsNonImapIncomingServers(t *testing.T) {
	const popOnly = `<clientConfig><emailProvider>
	  <incomingServer type="pop3"><hostname>pop.example.com</hostname><port>995</port><socketType>SSL</socketType></incomingServer>
	</emailProvider></clientConfig>`

	got, err := parse([]byte(popOnly), "autoconfig")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.IMAPHost != "" {
		t.Errorf("IMAPHost = %q, want empty; a pop3 server is not an imap one", got.IMAPHost)
	}
}

func TestParseRejectsMalformedXML(t *testing.T) {
	if _, err := parse([]byte("<clientConfig><emailProvider>"), "ispdb"); err == nil {
		t.Error("parse of a truncated document returned no error")
	}
}

// socketTLS decides whether the wizard offers implicit TLS, STARTTLS or leaves
// the choice alone. Anything it does not recognize has to fall in the last
// group: returning "ssl" for an unknown value would claim a security level the
// document never stated, and returning something plaintext-shaped would hand
// the user a clear connection.
func TestSocketTLS(t *testing.T) {
	cases := []struct {
		socketType string
		want       string
	}{
		{"SSL", "ssl"},
		{"ssl", "ssl"},
		{"TLS", "ssl"},
		{"STARTTLS", "starttls"},
		{"starttls", "starttls"},
		{"plain", ""},
		{"PLAIN", ""},
		{"", ""},
		{"NONE", ""},
	}
	for _, c := range cases {
		if got := socketTLS(c.socketType); got != c.want {
			t.Errorf("socketTLS(%q) = %q, want %q", c.socketType, got, c.want)
		}
	}
}

func TestDomainOf(t *testing.T) {
	cases := []struct {
		email string
		want  string
	}{
		{"me@example.com", "example.com"},
		{"ME@EXAMPLE.COM", "example.com"},
		{"me@example.com ", "example.com"},
		{"first@second@example.com", "example.com"},
		{"me@", ""},
		{"@example.com", "example.com"},
		{"example.com", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := domainOf(c.email); got != c.want {
			t.Errorf("domainOf(%q) = %q, want %q", c.email, got, c.want)
		}
	}
}

// The ISPDB is consulted before the domain's own documents, and both of the
// domain-hosted locations are tried, since servers publish one or the other.
func TestSourcesOrder(t *testing.T) {
	got := sources("example.com")
	if len(got) != 3 {
		t.Fatalf("sources() returned %d entries, want 3", len(got))
	}
	wantNames := []string{"ispdb", "autoconfig", "wellknown"}
	for i, name := range wantNames {
		if got[i].source != name {
			t.Errorf("sources()[%d].source = %q, want %q", i, got[i].source, name)
		}
		if !strings.HasPrefix(got[i].url, "https://") {
			t.Errorf("sources()[%d].url = %q, want an https url", i, got[i].url)
		}
		if !strings.Contains(got[i].url, "example.com") {
			t.Errorf("sources()[%d].url = %q, want the domain in it", i, got[i].url)
		}
	}
}

func TestFetchAndParseReadsAServedDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(oauthProviderXML))
	}))
	defer server.Close()

	got, err := fetchAndParse(context.Background(), server.Client(), server.URL, "wellknown")
	if err != nil {
		t.Fatalf("fetchAndParse: %v", err)
	}
	if got.IMAPHost != "imap.example.com" || got.Source != "wellknown" {
		t.Errorf("fetchAndParse() = %+v, want the served imap host tagged wellknown", got)
	}
}

// A domain that serves a 404 html page for every path is the common case for
// the .well-known location. Treating that as a config would poison the wizard
// with whatever the page happened to contain.
func TestFetchAndParseRejectsANonOKResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := fetchAndParse(context.Background(), server.Client(), server.URL, "wellknown"); err == nil {
		t.Error("fetchAndParse of a 404 returned no error")
	}
}

// A Google Workspace domain publishes no autoconfig of its own, so its MX is
// the only signal that it signs in with Google rather than a password (#445).
func TestMatchMXRecognizesGoogleWorkspace(t *testing.T) {
	for _, host := range []string{"ASPMX.L.GOOGLE.COM.", "alt1.aspmx.l.google.com", "smtp.google.com.", "aspmx2.googlemail.com."} {
		got, ok := matchMX([]string{host})
		if !ok {
			t.Errorf("matchMX(%q) matched nothing", host)
			continue
		}
		if got.IMAPHost != "imap.gmail.com" || got.SMTPHost != "smtp.gmail.com" || !got.OAuth || got.OAuthProvider != "google" {
			t.Errorf("matchMX(%q) = %+v, want gmail servers with google sign-in", host, got)
		}
	}
}

func TestMatchMXLeavesPasswordProvidersWithoutOAuth(t *testing.T) {
	got, ok := matchMX([]string{"mx1.example.net.", "mx.purelymail.com."})
	if !ok || got.IMAPHost != "imap.purelymail.com" {
		t.Fatalf("matchMX() = %+v, %v, want purelymail", got, ok)
	}
	if got.OAuth || got.OAuthProvider != "" {
		t.Errorf("matchMX() = %+v, want password auth", got)
	}
}

// A lookalike host must not be taken for Google just by ending in the letters.
func TestMatchMXRejectsASuffixLookalike(t *testing.T) {
	if got, ok := matchMX([]string{"mail.notgoogle.com."}); ok {
		t.Errorf("matchMX() = %+v, want no match", got)
	}
}

// Only servers Pelton can sign in to get a provider key; an OAuth2 document
// for anything else keeps it empty so the wizard does not offer a dead end.
func TestParseNamesTheOAuthProviderForGmail(t *testing.T) {
	doc := strings.ReplaceAll(oauthProviderXML, "imap.example.com", "imap.gmail.com")
	got, err := parse([]byte(doc), "ispdb")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.OAuthProvider != "google" {
		t.Errorf("OAuthProvider = %q, want google", got.OAuthProvider)
	}

	other, err := parse([]byte(oauthProviderXML), "ispdb")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if other.OAuthProvider != "" {
		t.Errorf("OAuthProvider = %q for an unknown host, want empty", other.OAuthProvider)
	}
}
