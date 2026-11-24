// Package imap is Pelton's IMAP client over github.com/emersion/go-imap/v2:
// connect, auth, list folders, fetch headers and messages, flags, and IDLE.
package imap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/peltonapp/Pelton/internal/certtrust"
	"github.com/peltonapp/Pelton/internal/charsetguess"
)

// DialFunc opens a raw tcp connection; the proxy layer supplies one to route
// the connection through a proxy. nil means dial directly.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

const (
	// DefaultPort is IMAP over implicit TLS (RFC 8314).
	DefaultPort = 993
	// PortStartTLS is the cleartext IMAP port upgraded to TLS with STARTTLS.
	PortStartTLS = 143

	dialTimeout  = 30 * time.Second
	updateBuffer = 16
)

// TLSMode selects how the connection is secured.
type TLSMode int

const (
	// TLSAuto picks implicit TLS or STARTTLS from the port (143 = STARTTLS,
	// anything else = implicit TLS). This matches how the smtp layer behaves.
	TLSAuto TLSMode = iota
	// TLSImplicit dials straight into TLS (port 993).
	TLSImplicit
	// TLSStartTLS connects in cleartext then issues STARTTLS (port 143).
	TLSStartTLS
)

// Config holds connection and auth parameters. Gmail and iCloud need an
// app-specific password, not the account password.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string

	// TLS selects implicit TLS vs STARTTLS. The zero value (TLSAuto) derives the
	// mode from the port, so callers that only set a port get the right transport.
	TLS TLSMode

	// OAuth2Token, when set, authenticates with XOAUTH2 (gmail, outlook) instead
	// of a password. The caller obtains and refreshes it; this layer only uses it.
	OAuth2Token string

	// Trust is what this mailbox trusts beyond the system roots: certificates
	// the user pinned and a CA they supplied. The zero value is the standard
	// verification.
	Trust certtrust.Trust
	// InsecureSkipVerify disables TLS verification. Debugging only.
	InsecureSkipVerify bool
	// DebugWriter receives the raw protocol stream, including credentials.
	DebugWriter io.Writer

	// Dial, when set, opens the tcp connection (used to route through a proxy).
	// nil keeps the default direct dial, leaving the non-proxy path unchanged.
	Dial DialFunc
}

// tlsMode resolves the effective TLS mode, deriving it from the port when set to
// TLSAuto.
func (c Config) tlsMode() TLSMode {
	if c.TLS != TLSAuto {
		return c.TLS
	}
	if c.Port == PortStartTLS {
		return TLSStartTLS
	}
	return TLSImplicit
}

// MailboxUpdate is a server push delivered while idling; one field is set.
type MailboxUpdate struct {
	NumMessages    *uint32 // new mail (EXISTS)
	ExpungedSeqNum *uint32 // message removed (EXPUNGE)
}

// Client is a stateful IMAP client. Not safe for concurrent use.
type Client struct {
	raw     *imapclient.Client
	cfg     Config
	updates chan MailboxUpdate

	// selected is the mailbox Select last opened, guarded because the caller
	// and go-imap's reader goroutine both touch it.
	//
	// go-imap's own Client.Mailbox() cannot be used for this. completeCommand
	// closes the channel that unblocks Wait() before it assigns the client's
	// mailbox, so Select().Wait() can return while Mailbox() is still nil and
	// the very next call reports "no mailbox selected". The window is small but
	// a sync selects a mailbox and fetches from it immediately, which is
	// exactly the pattern that loses the race.
	selectedMu sync.Mutex
	selected   *Mailbox
}

// selectedMailbox returns the mailbox Select opened, or nil when none is open.
func (c *Client) selectedMailbox() *Mailbox {
	c.selectedMu.Lock()
	defer c.selectedMu.Unlock()
	return c.selected
}

// SelectedMailbox returns the mailbox Select last opened on this connection,
// or nil when none is open. The adapter uses it to skip a repeated SELECT.
func (c *Client) SelectedMailbox() *Mailbox {
	return c.selectedMailbox()
}

// setSelectedMailbox records what Select opened.
func (c *Client) setSelectedMailbox(m *Mailbox) {
	c.selectedMu.Lock()
	defer c.selectedMu.Unlock()
	c.selected = m
}

// Addr is the server this client is connected to, as host:port. Sync puts it in
// its log lines so a line in the debug overlay says which account it belongs to
// when several are syncing at once.
func (c *Client) Addr() string {
	port := c.cfg.Port
	if port == 0 {
		port = DefaultPort
	}
	return net.JoinHostPort(c.cfg.Host, strconv.Itoa(port))
}

// Connect opens a TLS connection but does not authenticate; call Login next.
func Connect(cfg Config) (*Client, error) {
	if cfg.Username == "" || (cfg.Password == "" && cfg.OAuth2Token == "") {
		return nil, fmt.Errorf("imap: username and a password or oauth token are required")
	}
	return connect(cfg)
}

// CheckTLS connects far enough to complete the TLS handshake and hangs up,
// without logging in. It needs no credentials, so the app can ask what
// certificate a server presents before anything is stored (#446).
func CheckTLS(cfg Config) error {
	client, err := connect(cfg)
	if err != nil {
		return err
	}
	return client.Close()
}

// connect opens the connection Connect and CheckTLS share.
func connect(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("imap: host is required")
	}

	port := cfg.Port
	if port == 0 {
		port = DefaultPort
	}

	tlsConfig, err := cfg.Trust.TLSConfig(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("imap: %w", err)
	}
	if cfg.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
		tlsConfig.VerifyConnection = nil
	}

	updates := make(chan MailboxUpdate, updateBuffer)

	options := &imapclient.Options{
		// go-imap's default word decoder knows utf-8 and latin-1 and gives up on
		// anything else, which leaves a cyrillic or japanese subject sitting in
		// the message list as its raw =?...?= source. Ours decodes the legacy
		// tables and guesses at what is left.
		WordDecoder: charsetguess.WordDecoder(),
		TLSConfig:   tlsConfig,
		DebugWriter: cfg.DebugWriter,
		Dialer:      &net.Dialer{Timeout: dialTimeout},
		// only way go-imap surfaces unsolicited EXISTS/EXPUNGE during IDLE
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if data.NumMessages != nil {
					sendUpdate(updates, MailboxUpdate{NumMessages: data.NumMessages})
				}
			},
			Expunge: func(seqNum uint32) {
				n := seqNum
				sendUpdate(updates, MailboxUpdate{ExpungedSeqNum: &n})
			},
		},
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	// with a proxy dialer the connection is opened here and handed to go-imap,
	// since its Dial* helpers only accept a concrete *net.Dialer. without one the
	// original direct Dial* path is used unchanged.
	if cfg.Dial != nil {
		raw, err := connectVia(cfg, addr, options)
		if err != nil {
			return nil, err
		}
		return &Client{raw: raw, cfg: cfg, updates: updates}, nil
	}

	// implicit TLS dials straight into TLS; STARTTLS connects in cleartext and
	// upgrades. the live connection test in the wizard validates the choice.
	dial := imapclient.DialTLS
	if cfg.tlsMode() == TLSStartTLS {
		dial = imapclient.DialStartTLS
	}
	raw, err := dial(addr, options)
	if err != nil {
		return nil, fmt.Errorf("imap: dial %s: %w", addr, err)
	}

	return &Client{raw: raw, cfg: cfg, updates: updates}, nil
}

// connectVia opens the tcp connection through cfg.Dial (a proxy) and builds the
// go-imap client from it, applying implicit TLS or STARTTLS to match the port.
func connectVia(cfg Config, addr string, options *imapclient.Options) (*imapclient.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := cfg.Dial(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("imap: proxy dial %s: %w", addr, err)
	}
	if cfg.tlsMode() == TLSStartTLS {
		raw, err := imapclient.NewStartTLS(conn, options)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("imap: starttls %s: %w", addr, err)
		}
		return raw, nil
	}
	tlsConn := tls.Client(conn, options.TLSConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("imap: tls handshake %s: %w", addr, err)
	}
	return imapclient.New(tlsConn, options), nil
}

// sendUpdate never blocks the read loop; drops if the consumer is behind.
func sendUpdate(ch chan MailboxUpdate, u MailboxUpdate) {
	select {
	case ch <- u:
	default:
	}
}

// ErrAuthFailed reports that the server rejected the credentials. It is wrapped
// into whatever Login returns in that case, so callers can tell a wrong password
// from a server that is merely unreachable and act on it: the app marks the
// mailbox and offers to re-enter the password instead of retrying forever.
var ErrAuthFailed = errors.New("imap: authentication failed")

// Login authenticates with the credentials from Config: XOAUTH2 when an oauth
// token is present, otherwise a password LOGIN.
func (c *Client) Login() error {
	if c.cfg.OAuth2Token != "" {
		if err := c.raw.Authenticate(newXOAuth2Client(c.cfg.Username, c.cfg.OAuth2Token)); err != nil {
			return fmt.Errorf("imap: xoauth2 auth as %q: %w", c.cfg.Username, authError(err))
		}
		return nil
	}
	if err := c.raw.Login(c.cfg.Username, c.cfg.Password).Wait(); err != nil {
		return fmt.Errorf("imap: login as %q: %w", c.cfg.Username, authError(err))
	}
	return nil
}

// authError adds ErrAuthFailed to an error the server returned to LOGIN or
// AUTHENTICATE.
//
// A NO to an authentication command is the protocol's way of saying the
// credentials were refused (RFC 9051 section 6.2.3), whether or not the server
// bothers with an AUTHENTICATIONFAILED code, and many do not. Anything that is
// not a status response at all, a dropped connection or a tls failure, is left
// alone: the password may well be fine.
func authError(err error) error {
	var status *imap.Error
	if !errors.As(err, &status) || status.Type != imap.StatusResponseTypeNo {
		return err
	}
	return fmt.Errorf("%w: %w", ErrAuthFailed, err)
}

// SupportsIdle reports whether the server advertises IDLE.
func (c *Client) SupportsIdle() bool {
	return c.raw.Caps().Has(imap.CapIdle)
}

// Logout ends the session; still call Close afterwards.
func (c *Client) Logout() error {
	if err := c.raw.Logout().Wait(); err != nil {
		return fmt.Errorf("imap: logout: %w", err)
	}
	return nil
}

// Close terminates the connection.
func (c *Client) Close() error {
	if err := c.raw.Close(); err != nil {
		return fmt.Errorf("imap: close connection: %w", err)
	}
	return nil
}
