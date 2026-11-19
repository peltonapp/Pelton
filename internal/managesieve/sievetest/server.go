// Package sievetest is an in-process ManageSieve server for tests. It speaks
// enough of RFC 5804 to drive a client through the whole dialogue: the
// capability greeting, STARTTLS, PLAIN and LOGIN authentication, literals in
// both directions, the script commands, a CHECKSCRIPT that rejects scripts the
// test marks as bad, and a dropped connection on request.
package sievetest

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Options configures a Server.
type Options struct {
	// Username and Password are the one account the server accepts.
	Username string
	Password string
	// Extensions is the SIEVE capability. Empty means a common set.
	Extensions []string
	// SASL lists the mechanisms on offer after STARTTLS. Empty means PLAIN.
	SASL []string
	// TLS, when set, makes the server offer STARTTLS with this config.
	TLS *tls.Config
	// RefuseStartTLS advertises STARTTLS but answers it with NO.
	RefuseStartTLS bool
	// Check validates a script for CHECKSCRIPT and PUTSCRIPT. It returns a
	// non-empty message to reject it. nil accepts everything.
	Check func(script string) string
	// DropOn names a command after which the server hangs up without
	// answering, to simulate a lost connection.
	DropOn string
	// Greeting replaces the whole greeting with raw text, for a server that is
	// not a ManageSieve server at all.
	Greeting string
}

// Server is a running fake.
type Server struct {
	opts Options
	ln   net.Listener

	mu       sync.Mutex
	scripts  map[string]string
	active   string
	commands []string
	conns    map[net.Conn]struct{}
	wg       sync.WaitGroup
}

// Start listens on a loopback port and serves until Close.
func Start(opts Options) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	if len(opts.Extensions) == 0 {
		opts.Extensions = []string{"fileinto", "reject", "envelope", "vacation", "imap4flags", "date", "relational", "include"}
	}
	if len(opts.SASL) == 0 {
		opts.SASL = []string{"PLAIN"}
	}
	s := &Server{opts: opts, ln: ln, scripts: map[string]string{}, conns: map[net.Conn]struct{}{}}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Addr returns host and port.
func (s *Server) Addr() (string, int) {
	addr := s.ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

// Close stops the server and waits for its connections to end.
func (s *Server) Close() {
	_ = s.ln.Close()
	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// SetScript stores a script directly, active when active is true.
func (s *Server) SetScript(name, content string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts[name] = content
	if active {
		s.active = name
	}
}

// Script returns a stored script and whether it exists.
func (s *Server) Script(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.scripts[name]
	return v, ok
}

// Active returns the name of the active script, "" when none is.
func (s *Server) Active() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// Commands returns the command names received so far, in order.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.conns, conn)
				s.mu.Unlock()
				_ = conn.Close()
			}()
			s.session(conn)
		}()
	}
}

type session struct {
	s      *Server
	conn   net.Conn
	br     *bufio.Reader
	w      *bufio.Writer
	tls    bool
	authed bool
}

func (s *Server) session(conn net.Conn) {
	ss := &session{s: s, conn: conn, br: bufio.NewReader(conn), w: bufio.NewWriter(conn)}
	if s.opts.Greeting != "" {
		ss.w.WriteString(s.opts.Greeting)
		ss.w.Flush()
		return
	}
	ss.capabilities()
	ss.ok("ready")
	for {
		args, err := ss.readCommand()
		if err != nil {
			return
		}
		if len(args) == 0 {
			ss.no("", "empty command")
			continue
		}
		name := strings.ToUpper(args[0])
		s.mu.Lock()
		s.commands = append(s.commands, name)
		s.mu.Unlock()
		if s.opts.DropOn != "" && strings.EqualFold(s.opts.DropOn, name) {
			return
		}
		if !ss.handle(name, args[1:]) {
			return
		}
	}
}

func (ss *session) handle(name string, args []string) bool {
	s := ss.s
	switch name {
	case "CAPABILITY":
		ss.capabilities()
		ss.ok("")
	case "STARTTLS":
		if s.opts.TLS == nil || ss.tls || s.opts.RefuseStartTLS {
			ss.no("", "STARTTLS not available")
			return true
		}
		ss.ok("begin TLS")
		tlsConn := tls.Server(ss.conn, s.opts.TLS)
		if err := tlsConn.Handshake(); err != nil {
			return false
		}
		ss.conn = tlsConn
		ss.br = bufio.NewReader(tlsConn)
		ss.w = bufio.NewWriter(tlsConn)
		ss.tls = true
		ss.capabilities()
		ss.ok("TLS active")
	case "AUTHENTICATE":
		ss.authenticate(args)
	case "LOGOUT":
		ss.ok("bye")
		return false
	case "NOOP":
		ss.ok("")
	default:
		if !ss.authed {
			ss.no("", "authenticate first")
			return true
		}
		ss.scriptCommand(name, args)
	}
	return true
}

func (ss *session) scriptCommand(name string, args []string) {
	s := ss.s
	s.mu.Lock()
	defer s.mu.Unlock()
	switch name {
	case "LISTSCRIPTS":
		names := make([]string, 0, len(s.scripts))
		for n := range s.scripts {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if n == s.active {
				fmt.Fprintf(ss.w, "%s ACTIVE\r\n", quote(n))
			} else {
				fmt.Fprintf(ss.w, "%s\r\n", quote(n))
			}
		}
		ss.ok("")
	case "GETSCRIPT":
		if len(args) < 1 {
			ss.no("", "missing name")
			return
		}
		body, ok := s.scripts[args[0]]
		if !ok {
			ss.no("NONEXISTENT", "no such script")
			return
		}
		fmt.Fprintf(ss.w, "{%d}\r\n%s\r\n", len(body), body)
		ss.ok("")
	case "CHECKSCRIPT":
		if len(args) < 1 {
			ss.no("", "missing script")
			return
		}
		if msg := ss.check(args[0]); msg != "" {
			ss.noLiteral(msg)
			return
		}
		ss.ok("")
	case "PUTSCRIPT":
		if len(args) < 2 {
			ss.no("", "missing arguments")
			return
		}
		if msg := ss.check(args[1]); msg != "" {
			ss.noLiteral(msg)
			return
		}
		s.scripts[args[0]] = args[1]
		ss.ok("")
	case "SETACTIVE":
		if len(args) < 1 {
			ss.no("", "missing name")
			return
		}
		if args[0] == "" {
			s.active = ""
			ss.ok("")
			return
		}
		if _, ok := s.scripts[args[0]]; !ok {
			ss.no("NONEXISTENT", "no such script")
			return
		}
		s.active = args[0]
		ss.ok("")
	case "DELETESCRIPT":
		if len(args) < 1 {
			ss.no("", "missing name")
			return
		}
		if _, ok := s.scripts[args[0]]; !ok {
			ss.no("NONEXISTENT", "no such script")
			return
		}
		if s.active == args[0] {
			ss.no("ACTIVE", "script is active")
			return
		}
		delete(s.scripts, args[0])
		ss.ok("")
	default:
		ss.no("", "unknown command")
	}
}

func (ss *session) check(script string) string {
	if ss.s.opts.Check == nil {
		return ""
	}
	return ss.s.opts.Check(script)
}

func (ss *session) authenticate(args []string) {
	s := ss.s
	if s.opts.TLS != nil && !ss.tls {
		ss.no("ENCRYPT-NEEDED", "use STARTTLS first")
		return
	}
	if len(args) < 1 {
		ss.no("", "missing mechanism")
		return
	}
	mech := strings.ToUpper(args[0])
	offered := false
	for _, m := range s.opts.SASL {
		if strings.EqualFold(m, mech) {
			offered = true
		}
	}
	if !offered {
		ss.no("", "mechanism not offered")
		return
	}
	var user, pass string
	switch mech {
	case "PLAIN":
		var ir string
		if len(args) > 1 {
			ir = args[1]
		} else {
			resp, ok := ss.challenge("")
			if !ok {
				return
			}
			ir = resp
		}
		raw, err := base64.StdEncoding.DecodeString(ir)
		if err != nil {
			ss.no("", "bad base64")
			return
		}
		parts := strings.Split(string(raw), "\x00")
		if len(parts) != 3 {
			ss.no("", "bad PLAIN response")
			return
		}
		user, pass = parts[1], parts[2]
	case "LOGIN":
		u, ok := ss.challenge("Username:")
		if !ok {
			return
		}
		p, ok := ss.challenge("Password:")
		if !ok {
			return
		}
		ub, _ := base64.StdEncoding.DecodeString(u)
		pb, _ := base64.StdEncoding.DecodeString(p)
		user, pass = string(ub), string(pb)
	default:
		ss.no("", "mechanism not implemented")
		return
	}
	if user != s.opts.Username || pass != s.opts.Password {
		ss.no("AUTH-TOO-WEAK", "authentication failed")
		return
	}
	ss.authed = true
	ss.ok("logged in")
}

// challenge sends a continuation and reads the client's base64 answer.
func (ss *session) challenge(prompt string) (string, bool) {
	fmt.Fprintf(ss.w, "%s\r\n", quote(base64.StdEncoding.EncodeToString([]byte(prompt))))
	ss.w.Flush()
	args, err := ss.readCommand()
	if err != nil || len(args) == 0 {
		return "", false
	}
	if args[0] == "*" {
		ss.no("", "authentication aborted")
		return "", false
	}
	return args[0], true
}

func (ss *session) capabilities() {
	s := ss.s
	w := ss.w
	fmt.Fprintf(w, "\"IMPLEMENTATION\" \"Pelton sievetest\"\r\n")
	fmt.Fprintf(w, "\"SIEVE\" %s\r\n", quote(strings.Join(s.opts.Extensions, " ")))
	if s.opts.TLS == nil || ss.tls {
		fmt.Fprintf(w, "\"SASL\" %s\r\n", quote(strings.Join(s.opts.SASL, " ")))
	} else {
		fmt.Fprintf(w, "\"SASL\" \"\"\r\n")
	}
	if (s.opts.TLS != nil || s.opts.RefuseStartTLS) && !ss.tls {
		fmt.Fprintf(w, "\"STARTTLS\"\r\n")
	}
	fmt.Fprintf(w, "\"VERSION\" \"1.0\"\r\n")
}

func (ss *session) ok(text string) {
	if text == "" {
		ss.w.WriteString("OK\r\n")
	} else {
		fmt.Fprintf(ss.w, "OK %s\r\n", quote(text))
	}
	ss.w.Flush()
}

func (ss *session) no(code, text string) {
	if code != "" {
		fmt.Fprintf(ss.w, "NO (%s) %s\r\n", code, quote(text))
	} else {
		fmt.Fprintf(ss.w, "NO %s\r\n", quote(text))
	}
	ss.w.Flush()
}

// noLiteral answers NO with the text as a literal, the way servers send
// multi-line script errors.
func (ss *session) noLiteral(text string) {
	fmt.Fprintf(ss.w, "NO {%d}\r\n%s\r\n", len(text), text)
	ss.w.Flush()
}

// readCommand reads one command line as its words, quoted strings and
// literals decoded.
func (ss *session) readCommand() ([]string, error) {
	var out []string
	for {
		b, err := ss.br.ReadByte()
		if err != nil {
			return nil, err
		}
		switch {
		case b == ' ' || b == '\r':
		case b == '\n':
			return out, nil
		case b == '"':
			var sb strings.Builder
			for {
				c, err := ss.br.ReadByte()
				if err != nil {
					return nil, err
				}
				if c == '"' {
					break
				}
				if c == '\\' {
					if c, err = ss.br.ReadByte(); err != nil {
						return nil, err
					}
				}
				sb.WriteByte(c)
			}
			out = append(out, sb.String())
		case b == '{':
			spec, err := ss.br.ReadString('}')
			if err != nil {
				return nil, err
			}
			spec = strings.TrimSuffix(strings.TrimSuffix(spec, "}"), "+")
			n, err := strconv.Atoi(spec)
			if err != nil {
				return nil, err
			}
			if _, err := ss.br.ReadString('\n'); err != nil {
				return nil, err
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(ss.br, buf); err != nil {
				return nil, err
			}
			out = append(out, string(buf))
		default:
			var sb strings.Builder
			sb.WriteByte(b)
			for {
				c, err := ss.br.ReadByte()
				if err != nil {
					return nil, err
				}
				if c == ' ' || c == '\r' || c == '\n' {
					_ = ss.br.UnreadByte()
					break
				}
				sb.WriteByte(c)
			}
			out = append(out, sb.String())
		}
	}
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
