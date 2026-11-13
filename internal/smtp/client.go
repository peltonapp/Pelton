// Package smtp is Pelton's sending side: it builds RFC 5322 / MIME messages
// (builder.go, headers.go), optionally hands them to internal/crypto for
// PGP/MIME signing or encryption, transmits them over SMTP (client.go), and
// orchestrates the whole flow including append-to-Sent (send.go).
//
// DKIM note: Pelton deliberately does NOT DKIM-sign messages on the client.
// DKIM is the submitting server's responsibility (it signs with the domain key
// on its way out); a client signing would be wrong and usually rejected. So no
// DKIM code lives here by design.
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/peltonapp/Pelton/internal/certtrust"
)

const (
	// PortImplicitTLS is SMTPS, TLS from the first byte (RFC 8314).
	PortImplicitTLS = 465
	// PortStartTLS is submission, upgraded to TLS with STARTTLS.
	PortStartTLS = 587

	defaultLocalName = "localhost"
	xoauth2Mech      = "XOAUTH2"

	// dialTimeout bounds establishing the tcp connection when dialing through a
	// proxy (the direct Dial* helpers carry their own timeouts).
	dialTimeout = 30 * time.Second
)

// TLSMode selects how TLS is established.
type TLSMode int

const (
	// TLSAuto picks implicit or starttls from the port.
	TLSAuto TLSMode = iota
	// TLSImplicit dials straight into TLS (port 465).
	TLSImplicit
	// TLSStartTLS connects in clear then issues STARTTLS (port 587).
	TLSStartTLS
)

// AuthMechanism selects the SASL mechanism. AuthAuto picks XOAUTH2 when a token
// is present, otherwise PLAIN.
type AuthMechanism string

const (
	AuthAuto    AuthMechanism = ""
	AuthPlain   AuthMechanism = "PLAIN"
	AuthLogin   AuthMechanism = "LOGIN"
	AuthXOAuth2 AuthMechanism = "XOAUTH2"
)

// Config holds connection and auth parameters for one account's submission
// server. Credentials are supplied by the caller (from the keyring, keyed by
// account id) and are never persisted by this package.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string

	// OAuth2Token is the bearer token for XOAUTH2 (gmail and similar).
	OAuth2Token string
	// Auth forces a mechanism; zero value auto-selects.
	Auth AuthMechanism
	// TLS forces a mode; zero value derives it from the port.
	TLS TLSMode
	// LocalName is the EHLO name; defaults to localhost.
	LocalName string

	// Trust is what this mailbox trusts beyond the system roots: certificates
	// the user pinned and a CA they supplied. The zero value is the standard
	// verification.
	Trust certtrust.Trust
	// InsecureSkipVerify disables certificate verification. Debugging only.
	InsecureSkipVerify bool

	// Dial, when set, opens the tcp connection (used to route through a proxy).
	// nil keeps the default direct dial, leaving the non-proxy path unchanged.
	Dial DialFunc
}

// DialFunc opens a raw tcp connection; the proxy layer supplies one to route
// the connection through a proxy. nil means dial directly.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Distinct, clear failure categories so the ui can tell the user whether to fix
// their network/TLS or their credentials.
var (
	// ErrConnect covers dialing and TLS establishment failures.
	ErrConnect = errors.New("smtp: connection or tls failed")
	// ErrAuth covers authentication failures.
	ErrAuth = errors.New("smtp: authentication failed")
)

func (c Config) tlsMode() TLSMode {
	if c.TLS != TLSAuto {
		return c.TLS
	}
	if c.Port == PortStartTLS {
		return TLSStartTLS
	}
	return TLSImplicit
}

func (c Config) port() int {
	if c.Port != 0 {
		return c.Port
	}
	if c.tlsMode() == TLSStartTLS {
		return PortStartTLS
	}
	return PortImplicitTLS
}

func (c Config) localName() string {
	if c.LocalName != "" {
		return c.LocalName
	}
	return defaultLocalName
}

// Client is a connected, authenticated SMTP session. Not safe for concurrent
// use. One is dialed per send so a long-lived outbox worker never holds an idle
// socket.
type Client struct {
	raw *gosmtp.Client
	cfg Config
}

// Dial connects to the submission server and completes the EHLO handshake. Call
// Authenticate next, then Send.
func Dial(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("smtp: host is required")
	}

	raw, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	if err := raw.Hello(cfg.localName()); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("%w: ehlo: %v", ErrConnect, err)
	}
	return &Client{raw: raw, cfg: cfg}, nil
}

func dial(cfg Config) (*gosmtp.Client, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.port()))
	tlsCfg, err := cfg.Trust.TLSConfig(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConnect, err)
	}
	if cfg.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
		tlsCfg.VerifyConnection = nil
	}

	if cfg.Dial != nil {
		return dialVia(cfg, addr, tlsCfg)
	}

	switch cfg.tlsMode() {
	case TLSStartTLS:
		c, err := gosmtp.DialStartTLS(addr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("%w: starttls %s: %w", ErrConnect, addr, err)
		}
		return c, nil
	default:
		c, err := gosmtp.DialTLS(addr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("%w: tls dial %s: %w", ErrConnect, addr, err)
		}
		return c, nil
	}
}

// dialVia opens the tcp connection through cfg.Dial (a proxy) and builds the
// go-smtp client from it, applying implicit TLS or STARTTLS to match the port.
func dialVia(cfg Config, addr string, tlsCfg *tls.Config) (*gosmtp.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := cfg.Dial(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: proxy dial %s: %v", ErrConnect, addr, err)
	}
	if cfg.tlsMode() == TLSStartTLS {
		c, err := gosmtp.NewClientStartTLS(conn, tlsCfg)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%w: starttls %s: %w", ErrConnect, addr, err)
		}
		return c, nil
	}
	tlsConn := tls.Client(conn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: tls handshake %s: %w", ErrConnect, addr, err)
	}
	return gosmtp.NewClient(tlsConn), nil
}

// Authenticate runs SASL auth with the configured mechanism.
func (c *Client) Authenticate() error {
	cfg := c.cfg
	// Some Exim setups advertise only LOGIN and answer AUTH PLAIN with 504.
	if cfg.Auth == AuthAuto && cfg.OAuth2Token == "" &&
		!c.raw.SupportsAuth("PLAIN") && c.raw.SupportsAuth("LOGIN") {
		cfg.Auth = AuthLogin
	}
	auth, err := authClient(cfg)
	if err != nil {
		return err
	}
	if err := c.raw.Auth(auth); err != nil {
		return fmt.Errorf("%w: %v", ErrAuth, err)
	}
	return nil
}

// authClient builds the SASL client for the configured mechanism, defaulting to
// XOAUTH2 when a token is present and PLAIN otherwise.
func authClient(cfg Config) (sasl.Client, error) {
	mech := cfg.Auth
	if mech == AuthAuto {
		if cfg.OAuth2Token != "" {
			mech = AuthXOAuth2
		} else {
			mech = AuthPlain
		}
	}

	switch mech {
	case AuthPlain:
		return sasl.NewPlainClient("", cfg.Username, cfg.Password), nil
	case AuthLogin:
		return sasl.NewLoginClient(cfg.Username, cfg.Password), nil
	case AuthXOAuth2:
		return newXOAuth2Client(cfg.Username, cfg.OAuth2Token), nil
	default:
		return nil, fmt.Errorf("smtp: unknown auth mechanism %q", mech)
	}
}

// Send transmits one message. Bcc recipients are included in to even though they
// are absent from the message headers, which is how Bcc is delivered. The
// context is honoured before the exchange starts; go-smtp itself does not take a
// context mid-command.
func (c *Client) Send(ctx context.Context, from string, to []string, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(to) == 0 {
		return fmt.Errorf("smtp: no recipients")
	}

	if err := c.raw.Mail(from, nil); err != nil {
		return fmt.Errorf("smtp: MAIL FROM %q: %w", from, err)
	}
	for _, rcpt := range to {
		if err := c.raw.Rcpt(rcpt, nil); err != nil {
			return fmt.Errorf("smtp: RCPT TO %q: %w", rcpt, err)
		}
	}

	w, err := c.raw.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp: write message body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: finalize message: %w", err)
	}
	return nil
}

// Close ends the session cleanly with QUIT.
func (c *Client) Close() error {
	if err := c.raw.Quit(); err != nil {
		return fmt.Errorf("smtp: quit: %w", err)
	}
	return nil
}

// xoauth2Client implements the XOAUTH2 SASL mechanism, which go-sasl does not
// ship. The initial response is "user=<user>^Aauth=Bearer <token>^A^A".
type xoauth2Client struct {
	username string
	token    string
}

func newXOAuth2Client(username, token string) sasl.Client {
	return &xoauth2Client{username: username, token: token}
}

func (a *xoauth2Client) Start() (mech string, ir []byte, err error) {
	ir = []byte("user=" + a.username + "\x01auth=Bearer " + a.token + "\x01\x01")
	return xoauth2Mech, ir, nil
}

func (a *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	// a challenge here means the server rejected the token and is sending an
	// error payload; there is no second step to a valid xoauth2 exchange.
	return nil, fmt.Errorf("smtp: xoauth2 rejected: %s", challenge)
}
