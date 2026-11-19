// Package managesieve is a ManageSieve client (RFC 5804): it connects to a
// mail server's sieve service, upgrades to TLS, authenticates with the
// account's own credentials and lists, reads, checks, uploads, activates and
// deletes sieve scripts. It uses the standard library only and dials through
// the same trust and proxy settings the imap and smtp layers use.
package managesieve

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/certtrust"
)

// DefaultPort is the ManageSieve port (RFC 5804 section 1.8).
const DefaultPort = 4190

const (
	defaultDialTimeout    = 15 * time.Second
	defaultCommandTimeout = 60 * time.Second
)

// TLSMode selects how the connection is secured. There is no cleartext mode:
// credentials never cross the wire unencrypted.
type TLSMode int

const (
	// TLSStartTLS connects in cleartext and upgrades with STARTTLS before
	// anything else is sent. It is the standard for port 4190.
	TLSStartTLS TLSMode = iota
	// TLSImplicit dials straight into TLS.
	TLSImplicit
)

// DialFunc opens a raw tcp connection; the proxy layer supplies one. nil means
// a direct dial.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Config holds the connection and authentication parameters.
type Config struct {
	Host string
	// Port defaults to DefaultPort.
	Port int
	TLS  TLSMode

	Username string
	Password string
	// OAuth2Token, when set, authenticates with XOAUTH2 instead of a password.
	OAuth2Token string

	// Trust is what this mailbox trusts beyond the system roots.
	Trust certtrust.Trust
	// Dial, when set, opens the tcp connection (a proxy route).
	Dial DialFunc

	// DialTimeout and CommandTimeout bound connecting and each command. Zero
	// uses 15 and 60 seconds.
	DialTimeout    time.Duration
	CommandTimeout time.Duration
}

// Errors the caller can tell apart.
var (
	// ErrAuthFailed means the server refused the credentials.
	ErrAuthFailed = errors.New("managesieve: authentication failed")
	// ErrNoStartTLS means the server would not upgrade the connection to TLS,
	// so nothing was sent in the clear.
	ErrNoStartTLS = errors.New("managesieve: the server does not offer STARTTLS")
	// ErrNotManageSieve means something answered on the port, but not a
	// ManageSieve server.
	ErrNotManageSieve = errors.New("managesieve: the server did not answer as a ManageSieve server")
	// ErrNoMechanism means no SASL mechanism fits the credentials.
	ErrNoMechanism = errors.New("managesieve: no usable sign-in method")
	// ErrConnectionLost means the connection dropped in the middle of a
	// command.
	ErrConnectionLost = errors.New("managesieve: connection lost")
)

// ServerError is a NO or BYE the server sent in answer to a command.
type ServerError struct {
	Command string
	// Status is "NO" or "BYE".
	Status string
	// Code is the response code without its argument, e.g. "NONEXISTENT".
	Code string
	// Message is the server's human readable text.
	Message string
}

func (e *ServerError) Error() string {
	var sb strings.Builder
	sb.WriteString("managesieve: ")
	sb.WriteString(e.Command)
	sb.WriteString(" refused")
	if e.Code != "" {
		sb.WriteString(" (" + e.Code + ")")
	}
	if e.Message != "" {
		sb.WriteString(": " + e.Message)
	}
	return sb.String()
}

var lineRe = regexp.MustCompile(`(?i)\bline\s+(\d+)`)

// Line is the script line the message names ("line 4: ..."), or 0 when it
// names none. Dovecot, Cyrus and Stalwart all report errors in that form.
func (e *ServerError) Line() int {
	m := lineRe.FindStringSubmatch(e.Message)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// Capabilities is what the server advertises.
type Capabilities struct {
	Implementation string
	// Sieve lists the sieve extensions the server supports.
	Sieve []string
	// SASL lists the authentication mechanisms on offer right now.
	SASL     []string
	StartTLS bool
	Version  string
	// MaxRedirects is how many redirect actions one script may run, 0 when
	// the server sets no limit.
	MaxRedirects int
	Owner        string
}

// HasExtension reports whether the server supports a sieve extension.
func (c Capabilities) HasExtension(name string) bool {
	for _, ext := range c.Sieve {
		if strings.EqualFold(ext, name) {
			return true
		}
	}
	return false
}

// HasSASL reports whether the server offers a SASL mechanism.
func (c Capabilities) HasSASL(mech string) bool {
	for _, m := range c.SASL {
		if strings.EqualFold(m, mech) {
			return true
		}
	}
	return false
}

// Script is one entry of LISTSCRIPTS.
type Script struct {
	Name   string
	Active bool
}

// Client is a ManageSieve session. Not safe for concurrent use.
type Client struct {
	conn    net.Conn
	r       reader
	w       *bufio.Writer
	cfg     Config
	caps    Capabilities
	timeout time.Duration
}

// Dial connects, secures the connection and reads the capabilities, but does
// not authenticate; call Authenticate next.
func Dial(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, errors.New("managesieve: host is required")
	}
	port := cfg.Port
	if port == 0 {
		port = DefaultPort
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = defaultDialTimeout
	}
	tlsConfig, err := cfg.Trust.TLSConfig(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("managesieve: %w", err)
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	var conn net.Conn
	if cfg.Dial != nil {
		conn, err = cfg.Dial(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("managesieve: dial %s: %w", addr, err)
	}

	c := &Client{cfg: cfg, timeout: cfg.CommandTimeout}
	if c.timeout == 0 {
		c.timeout = defaultCommandTimeout
	}
	if cfg.TLS == TLSImplicit {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("managesieve: tls handshake %s: %w", addr, err)
		}
		conn = tlsConn
	}
	c.setConn(conn)

	if err := c.readGreeting(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if cfg.TLS == TLSStartTLS {
		if err := c.startTLS(ctx, tlsConfig); err != nil {
			_ = c.conn.Close()
			return nil, err
		}
	}
	return c, nil
}

func (c *Client) setConn(conn net.Conn) {
	c.conn = conn
	c.r = reader{br: bufio.NewReader(conn)}
	c.w = bufio.NewWriter(conn)
}

// readGreeting reads the capability listing the server opens with.
func (c *Client) readGreeting() error {
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	caps, err := c.readCapabilities("greeting")
	if err != nil {
		var serr *ServerError
		if errors.As(err, &serr) || errors.Is(err, ErrConnectionLost) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrNotManageSieve, err)
	}
	c.caps = caps
	return nil
}

// startTLS upgrades the connection and reads the capabilities again, which
// the server re-sends once TLS is up (RFC 5804 section 2.2).
func (c *Client) startTLS(ctx context.Context, tlsConfig *tls.Config) error {
	if !c.caps.StartTLS {
		return ErrNoStartTLS
	}
	if _, err := c.command("STARTTLS", "STARTTLS"); err != nil {
		var serr *ServerError
		if errors.As(err, &serr) {
			return fmt.Errorf("%w: %v", ErrNoStartTLS, err)
		}
		return err
	}
	if c.r.br.Buffered() > 0 {
		return errors.New("managesieve: unexpected data before the tls handshake")
	}
	tlsConn := tls.Client(c.conn, tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("managesieve: tls handshake: %w", err)
	}
	c.setConn(tlsConn)
	return c.readGreeting()
}

// Capabilities returns what the server advertised most recently.
func (c *Client) Capabilities() Capabilities { return c.caps }

// Authenticate signs in with the credentials from Config: XOAUTH2 with an
// oauth token, otherwise PLAIN, or LOGIN when PLAIN is not offered. A refusal
// wraps ErrAuthFailed.
func (c *Client) Authenticate() error {
	if c.cfg.Username == "" || (c.cfg.Password == "" && c.cfg.OAuth2Token == "") {
		return errors.New("managesieve: username and a password or oauth token are required")
	}
	mech, err := pickMechanism(c.cfg, c.caps)
	if err != nil {
		return err
	}
	name, ir, err := mech.Start()
	if err != nil {
		return err
	}
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	line := "AUTHENTICATE " + quote(name)
	if ir != nil {
		line += " " + quote(base64.StdEncoding.EncodeToString(ir))
	}
	if err := c.send(line); err != nil {
		return err
	}
	for {
		tokens, err := c.r.readLine()
		if err != nil {
			return lost(err)
		}
		if resp, ok := asResponse(tokens); ok {
			if resp.status == "OK" {
				return nil
			}
			serr := &ServerError{Command: "AUTHENTICATE", Status: resp.status, Code: resp.code, Message: resp.text}
			if resp.status == "NO" {
				return fmt.Errorf("%w: %w", ErrAuthFailed, serr)
			}
			return serr
		}
		if len(tokens) == 0 || tokens[0].kind != tokString {
			return fmt.Errorf("managesieve: unexpected line during AUTHENTICATE")
		}
		challenge, err := base64.StdEncoding.DecodeString(tokens[0].value)
		if err != nil {
			return fmt.Errorf("managesieve: bad sasl challenge: %w", err)
		}
		answer, err := mech.Next(challenge)
		if err != nil {
			_ = c.send(`"*"`)
			return err
		}
		if err := c.send(quote(base64.StdEncoding.EncodeToString(answer))); err != nil {
			return err
		}
	}
}

// ListScripts lists the scripts on the server; at most one is active.
func (c *Client) ListScripts() ([]Script, error) {
	lines, err := c.command("LISTSCRIPTS", "LISTSCRIPTS")
	if err != nil {
		return nil, err
	}
	out := make([]Script, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 || line[0].kind != tokString {
			continue
		}
		s := Script{Name: line[0].value}
		if len(line) > 1 && line[1].kind == tokAtom && strings.EqualFold(line[1].value, "ACTIVE") {
			s.Active = true
		}
		out = append(out, s)
	}
	return out, nil
}

// GetScript returns the content of a script. A missing script is a
// ServerError with Code "NONEXISTENT" on servers that send codes.
func (c *Client) GetScript(name string) (string, error) {
	lines, err := c.command("GETSCRIPT", "GETSCRIPT "+quote(name))
	if err != nil {
		return "", err
	}
	for _, line := range lines {
		if len(line) > 0 && line[0].kind == tokString {
			return line[0].value, nil
		}
	}
	return "", errors.New("managesieve: GETSCRIPT returned no script")
}

// PutScript uploads a script, replacing one of the same name. The server
// checks it first and refuses an invalid script without storing it.
func (c *Client) PutScript(name, content string) error {
	_, err := c.command("PUTSCRIPT", "PUTSCRIPT "+quote(name)+" "+literal(content))
	return err
}

// CheckScript asks the server to validate a script without storing it. An
// invalid script is a *ServerError whose Message (and Line) say what is wrong.
func (c *Client) CheckScript(content string) error {
	_, err := c.command("CHECKSCRIPT", "CHECKSCRIPT "+literal(content))
	return err
}

// SetActive makes a script the active one. An empty name deactivates every
// script.
func (c *Client) SetActive(name string) error {
	_, err := c.command("SETACTIVE", "SETACTIVE "+quote(name))
	return err
}

// DeleteScript removes a script. Servers refuse to delete the active one.
func (c *Client) DeleteScript(name string) error {
	_, err := c.command("DELETESCRIPT", "DELETESCRIPT "+quote(name))
	return err
}

// Logout ends the session; still call Close afterwards.
func (c *Client) Logout() error {
	_, err := c.command("LOGOUT", "LOGOUT")
	return err
}

// Close drops the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// send writes one command line.
func (c *Client) send(line string) error {
	if _, err := c.w.WriteString(line + "\r\n"); err != nil {
		return lost(err)
	}
	if err := c.w.Flush(); err != nil {
		return lost(err)
	}
	return nil
}

// command sends a command and collects its data lines up to the final
// response, turning NO and BYE into a *ServerError.
func (c *Client) command(name, line string) ([][]token, error) {
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	if err := c.send(line); err != nil {
		return nil, err
	}
	var data [][]token
	for {
		tokens, err := c.r.readLine()
		if err != nil {
			return nil, lost(err)
		}
		resp, ok := asResponse(tokens)
		if !ok {
			data = append(data, tokens)
			continue
		}
		if resp.status == "OK" {
			return data, nil
		}
		return nil, &ServerError{Command: name, Status: resp.status, Code: resp.code, Message: resp.text}
	}
}

// readCapabilities reads capability lines up to the final response.
func (c *Client) readCapabilities(name string) (Capabilities, error) {
	var caps Capabilities
	for {
		tokens, err := c.r.readLine()
		if err != nil {
			return caps, lost(err)
		}
		if resp, ok := asResponse(tokens); ok {
			if resp.status != "OK" {
				return caps, &ServerError{Command: name, Status: resp.status, Code: resp.code, Message: resp.text}
			}
			return caps, nil
		}
		if len(tokens) == 0 || tokens[0].kind != tokString {
			return caps, fmt.Errorf("unexpected capability line")
		}
		value := ""
		if len(tokens) > 1 {
			value = tokens[1].value
		}
		switch strings.ToUpper(tokens[0].value) {
		case "IMPLEMENTATION":
			caps.Implementation = value
		case "SIEVE":
			caps.Sieve = strings.Fields(value)
		case "SASL":
			caps.SASL = strings.Fields(value)
		case "STARTTLS":
			caps.StartTLS = true
		case "VERSION":
			caps.Version = value
		case "MAXREDIRECTS":
			caps.MaxRedirects, _ = strconv.Atoi(value)
		case "OWNER":
			caps.Owner = value
		}
	}
}

// lost marks a read or write failure as a dropped connection, keeping the
// underlying error for a timeout check.
func lost(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("%w: %v", ErrConnectionLost, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %w", ErrConnectionLost, err)
	}
	return err
}
