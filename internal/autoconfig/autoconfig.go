// Package autoconfig discovers IMAP/SMTP settings for an email address the way
// Thunderbird does: it consults the Mozilla ISPDB, then the domain's own
// autoconfig (the /.well-known and autoconfig.<domain> locations many servers,
// including mailcow, publish), and finally falls back to common host guesses.
// It does not log in; the caller verifies a result with a live connection test.
package autoconfig

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// httpTimeout bounds each discovery request so a slow host does not stall the
// wizard.
const httpTimeout = 6 * time.Second

// ispdbURL is the Mozilla provider database Thunderbird ships against.
const ispdbURL = "https://autoconfig.thunderbird.net/v1.1/"

// Discovered is the resolved configuration for an address.
type Discovered struct {
	IMAPHost string `json:"imapHost"`
	IMAPPort int    `json:"imapPort"`
	SMTPHost string `json:"smtpHost"`
	SMTPPort int    `json:"smtpPort"`
	// IMAPTLS and SMTPTLS are the connection security the source specified:
	// "ssl" or "starttls". Autoconfig documents state this outright, so it is
	// carried through rather than guessed back out of the port.
	IMAPTLS string `json:"imapTls"`
	SMTPTLS string `json:"smtpTls"`
	// OAuth is true when the provider's autoconfig says to authenticate with
	// OAuth2 (gmail, outlook). Otherwise password auth is expected.
	OAuth bool `json:"oauth"`
	// OAuthProvider is the oauth package's provider key when OAuth is set and
	// the servers belong to a provider Pelton can sign in to ("google"), so the
	// wizard can offer that sign-in. Empty otherwise.
	OAuthProvider string `json:"oauthProvider"`
	// Source records how the config was found: ispdb, wellknown, autoconfig, or
	// guess. The ui can show it and treat guesses as lower confidence.
	Source string `json:"source"`
}

// Discover resolves settings for an email address, trying each source in order
// and returning the first that yields an imap and smtp server.
func Discover(ctx context.Context, client *http.Client, email string) (Discovered, error) {
	domain := domainOf(email)
	if domain == "" {
		return Discovered{}, fmt.Errorf("autoconfig: invalid email %q", email)
	}

	for _, attempt := range sources(domain) {
		cfg, err := fetchAndParse(ctx, client, attempt.url, attempt.source)
		if err == nil && cfg.IMAPHost != "" && cfg.SMTPHost != "" {
			return cfg, nil
		}
	}

	// providers that host many custom domains (Namecheap Private Email,
	// Purelymail, Google Workspace) rarely publish autoconfig for each domain,
	// but their MX records point at a shared mail host we recognize. probing the
	// live MX lets us fill in the right servers for a custom domain.
	if cfg, ok := discoverByMX(ctx, domain); ok {
		return cfg, nil
	}

	// last resort: guess the common host pattern. the live test decides if it is
	// real; we never silently trust a guess.
	return Discovered{
		IMAPHost: "imap." + domain,
		IMAPPort: 993,
		SMTPHost: "smtp." + domain,
		SMTPPort: 465,
		IMAPTLS:  "ssl",
		SMTPTLS:  "ssl",
		Source:   "guess",
	}, nil
}

// oauthProviders maps an imap host to the oauth provider key that signs in to
// it, for autoconfig documents that ask for OAuth2.
var oauthProviders = map[string]string{
	"imap.gmail.com": "google",
}

// mxProvider maps a recognized MX hostname suffix to the imap/smtp servers that
// provider uses for all the domains it hosts.
type mxProvider struct {
	// suffix is matched against each MX target host (case-insensitive).
	suffix   string
	imapHost string
	imapPort int
	smtpHost string
	smtpPort int
	// imapTLS and smtpTLS are stated rather than inferred from the ports, so a
	// provider on a non-conventional port can be added here correctly.
	imapTLS string
	smtpTLS string
	// oauthProvider is set for providers that require oauth sign-in rather than
	// a password.
	oauthProvider string
}

// knownMXProviders are mail hosts that serve many custom domains behind a shared
// set of servers, so the domain's own autoconfig is usually absent but its MX is
// a reliable signal.
var knownMXProviders = []mxProvider{
	// Namecheap Private Email.
	{suffix: "privateemail.com", imapHost: "mail.privateemail.com", imapPort: 993, smtpHost: "mail.privateemail.com", smtpPort: 465, imapTLS: "ssl", smtpTLS: "ssl"},
	// Purelymail.
	{suffix: "purelymail.com", imapHost: "imap.purelymail.com", imapPort: 993, smtpHost: "smtp.purelymail.com", smtpPort: 465, imapTLS: "ssl", smtpTLS: "ssl"},
	// Google Workspace (aspmx.l.google.com, smtp.google.com, and the older
	// aspmx2.googlemail.com).
	{suffix: "google.com", imapHost: "imap.gmail.com", imapPort: 993, smtpHost: "smtp.gmail.com", smtpPort: 465, imapTLS: "ssl", smtpTLS: "ssl", oauthProvider: "google"},
	{suffix: "googlemail.com", imapHost: "imap.gmail.com", imapPort: 993, smtpHost: "smtp.gmail.com", smtpPort: 465, imapTLS: "ssl", smtpTLS: "ssl", oauthProvider: "google"},
}

// discoverByMX looks up the domain's MX records and, if one matches a known
// shared-host provider, returns that provider's settings. ok is false when the
// lookup fails or nothing matches, so the caller falls through to a guess.
func discoverByMX(ctx context.Context, domain string) (Discovered, bool) {
	resolver := net.DefaultResolver
	mxCtx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	records, err := resolver.LookupMX(mxCtx, domain)
	if err != nil {
		return Discovered{}, false
	}
	hosts := make([]string, len(records))
	for i, rec := range records {
		hosts[i] = rec.Host
	}
	return matchMX(hosts)
}

// matchMX returns the settings of the first known provider one of the MX hosts
// belongs to. ok is false when none match.
func matchMX(hosts []string) (Discovered, bool) {
	for _, h := range hosts {
		host := strings.ToLower(strings.TrimSuffix(h, "."))
		for _, p := range knownMXProviders {
			if host == p.suffix || strings.HasSuffix(host, "."+p.suffix) {
				return Discovered{
					IMAPHost:      p.imapHost,
					IMAPPort:      p.imapPort,
					SMTPHost:      p.smtpHost,
					SMTPPort:      p.smtpPort,
					IMAPTLS:       p.imapTLS,
					SMTPTLS:       p.smtpTLS,
					OAuth:         p.oauthProvider != "",
					OAuthProvider: p.oauthProvider,
					Source:        "mx",
				}, true
			}
		}
	}
	return Discovered{}, false
}

// source is one discovery location to try.
type source struct {
	url    string
	source string
}

// sources lists the discovery urls in priority order for a domain.
func sources(domain string) []source {
	return []source{
		{ispdbURL + domain, "ispdb"},
		{"https://autoconfig." + domain + "/mail/config-v1.1.xml", "autoconfig"},
		{"https://" + domain + "/.well-known/autoconfig/mail/config-v1.1.xml", "wellknown"},
	}
}

// fetchAndParse retrieves and parses a Thunderbird-format autoconfig document.
func fetchAndParse(ctx context.Context, client *http.Client, url, src string) (Discovered, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Discovered{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Discovered{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Discovered{}, fmt.Errorf("autoconfig: %s returned %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Discovered{}, err
	}
	return parse(body, src)
}

// clientConfig mirrors the parts of the Thunderbird autoconfig schema we use.
type clientConfig struct {
	Provider struct {
		Incoming []serverXML `xml:"incomingServer"`
		Outgoing []serverXML `xml:"outgoingServer"`
	} `xml:"emailProvider"`
}

type serverXML struct {
	Type           string `xml:"type,attr"`
	Hostname       string `xml:"hostname"`
	Port           int    `xml:"port"`
	SocketType     string `xml:"socketType"`
	Authentication string `xml:"authentication"`
}

// parse turns an autoconfig document into a Discovered, choosing the first imap
// incoming server and first smtp outgoing server.
func parse(body []byte, src string) (Discovered, error) {
	var cfg clientConfig
	if err := xml.Unmarshal(body, &cfg); err != nil {
		return Discovered{}, fmt.Errorf("autoconfig: parse: %w", err)
	}

	out := Discovered{Source: src}
	for _, s := range cfg.Provider.Incoming {
		if strings.EqualFold(s.Type, "imap") {
			out.IMAPHost = s.Hostname
			out.IMAPPort = s.Port
			out.IMAPTLS = socketTLS(s.SocketType)
			out.OAuth = strings.EqualFold(s.Authentication, "OAuth2")
			if out.OAuth {
				out.OAuthProvider = oauthProviders[strings.ToLower(s.Hostname)]
			}
			break
		}
	}
	for _, s := range cfg.Provider.Outgoing {
		if strings.EqualFold(s.Type, "smtp") {
			out.SMTPHost = s.Hostname
			out.SMTPPort = s.Port
			out.SMTPTLS = socketTLS(s.SocketType)
			break
		}
	}
	return out, nil
}

// socketTLS maps an autoconfig socketType onto the security values the rest of
// the app uses. SSL and TLS both mean implicit TLS in these documents. Anything
// else, including the plain and unencrypted values, returns empty: the caller
// then leaves the choice alone rather than being told to connect in clear.
func socketTLS(socketType string) string {
	switch strings.ToUpper(socketType) {
	case "SSL", "TLS":
		return "ssl"
	case "STARTTLS":
		return "starttls"
	default:
		return ""
	}
}

// domainOf returns the part after @ in an email address, lowercased.
func domainOf(email string) string {
	_, domain, ok := strings.CutLast(email, "@")
	if !ok || domain == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(domain))
}
