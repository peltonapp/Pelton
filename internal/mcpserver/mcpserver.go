// Package mcpserver exposes Pelton's cached mail over the Model Context Protocol
// so external AI agents can browse, read and search mail. It is read-only: no
// tool here sends, moves, flags or deletes anything (write actions are tracked
// separately). The server is off by default in the app; the desktop layer owns
// the enable toggle, port and bearer token, and drives Start/Stop.
//
// Transport is streamable HTTP bound to loopback only, guarded by a bearer
// token, so another process or a web page on the machine cannot silently drive
// a user's mailbox. The protocol plumbing lives here behind the narrow Mailbox
// interface; the desktop package adapts the store and search index to it.
package mcpserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverName is the MCP implementation name reported to clients.
const serverName = "pelton"

// Config is the runtime configuration for one Start. Addr must be a loopback
// host:port (the desktop layer builds it from 127.0.0.1 and the port setting);
// Token is the bearer token every request must present. Version is Pelton's
// build version, surfaced to clients in the initialize handshake.
type Config struct {
	Addr    string
	Token   string
	Version string
	// Permissions says which write tools this run allows. The zero value allows
	// none, so the server is read-only unless something says otherwise.
	Permissions Permissions
}

// Server owns the HTTP listener and the MCP server. It is safe for concurrent
// Start/Stop calls; the desktop layer calls Stop then Start to apply a changed
// port or a regenerated token.
type Server struct {
	mb Mailbox
	// w is nil on a read-only server, and then no write tool is registered at
	// all. The permission check is the second line, not the only one.
	w   Writer
	log *slog.Logger
	// scripts is nil on a server without the script tools, and then none of
	// them is registered. scriptLimits outlives a restart when the caller
	// keeps it, so stopping and starting the server does not reset its caps.
	scripts      Scripts
	scriptLimits *ScriptLimiter

	// perms is read on every call rather than captured when a tool is
	// registered, so changing a permission does not mean restarting the
	// listener. Restarting drops the agent's connection and, when a second copy
	// of Pelton holds the port, fails outright and leaves the server down.
	perms atomic.Value

	mu      sync.Mutex
	httpSrv *http.Server
	ln      net.Listener
}

// New creates a read-only Server backed by mb. It does not listen until Start
// is called.
func New(mb Mailbox, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{mb: mb, log: log}
}

// NewWithWriter creates a Server that can also act on mail, subject to the
// permissions given to Start.
func NewWithWriter(mb Mailbox, w Writer, log *slog.Logger) *Server {
	s := New(mb, log)
	s.w = w
	return s
}

// WithScripts adds the script tools, held to the caps in lim. Call it before
// Start. A nil lim uses DefaultScriptLimits.
func (s *Server) WithScripts(sc Scripts, lim *ScriptLimiter) *Server {
	if lim == nil {
		lim = NewScriptLimiter(DefaultScriptLimits())
	}
	s.scripts = sc
	s.scriptLimits = lim
	return s
}

// SetPermissions replaces the write permissions in place. Tools read them on
// every call, so this takes effect immediately, including for an agent that is
// already connected.
func (s *Server) SetPermissions(p Permissions) {
	s.perms.Store(p)
}

// permissions is what the tools consult. A server that never had any stored
// allows nothing.
func (s *Server) permissions() Permissions {
	p, _ := s.perms.Load().(Permissions)
	return p
}

// Running reports whether the server is currently listening.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.httpSrv != nil
}

// Addr returns the address the server listens on, "" when it is not running.
// It tells a caller that started it on port 0 which port it got.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Start begins listening on cfg.Addr. It fails if the token is empty (an
// unauthenticated local mail endpoint is never acceptable), if the address is
// not loopback, or if the port is already in use. Calling Start while already
// running returns an error; call Stop first.
func (s *Server) Start(cfg Config) error {
	if cfg.Token == "" {
		return fmt.Errorf("mcpserver: refusing to start without a bearer token")
	}
	if err := requireLoopback(cfg.Addr); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpSrv != nil {
		return fmt.Errorf("mcpserver: already running")
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return listenError(cfg.Addr, err)
	}

	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: cfg.Version,
		Title:   "Pelton",
	}, &mcp.ServerOptions{Instructions: handshake(s.w != nil, s.scripts != nil)})
	s.perms.Store(cfg.Permissions)
	registerTools(mcpSrv, s.mb)
	registerWriteTools(mcpSrv, s.w, s.permissions)
	registerScriptTools(mcpSrv, s.scripts, s.scriptLimits, s.permissions)

	// Stateless: every request is self-contained. The read-only tools need no
	// per-session state, and this makes the server immune to losing a client's
	// session across a restart (dev hot-reload) - the failure that otherwise
	// surfaces to the client as "session not found".
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpSrv
	}, &mcp.StreamableHTTPOptions{Stateless: true})

	httpSrv := &http.Server{
		Handler:           withBearerAuth(cfg.Token, handler),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.httpSrv = httpSrv
	s.ln = ln

	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Error("mcp server serve", "err", err)
		}
	}()
	s.log.Info("mcp server started", "addr", cfg.Addr)
	return nil
}

// Stop shuts the listener down gracefully, bounded by ctx. It is a no-op when
// the server is not running.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	httpSrv := s.httpSrv
	s.httpSrv = nil
	s.ln = nil
	s.mu.Unlock()

	if httpSrv == nil {
		return nil
	}
	s.log.Info("mcp server stopping")
	return httpSrv.Shutdown(ctx)
}

// listenError explains a failed bind. A port already in use is nearly always a
// second copy of Pelton holding it, which the operating system's own wording
// gives no hint of, so the message says so and says what to do about it.
func listenError(addr string, err error) error {
	if addrInUse(err) {
		return fmt.Errorf("mcpserver: port %s is already in use. Another copy of Pelton is probably running with its MCP server on: quit it, or give this one a different port", portOf(addr))
	}
	return fmt.Errorf("mcpserver: listen on %s: %w", addr, err)
}

// portOf is the port half of a host:port, or the whole string when it will not
// split, so the message never comes out with a gap in it.
func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return addr
}

// withBearerAuth wraps next so every request must carry an Authorization:
// Bearer <token> header matching token. The comparison is constant-time.
// A missing or wrong token gets 401 before the request ever reaches the MCP
// handler, so an unauthorized caller cannot even enumerate the tools.
func withBearerAuth(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireLoopback reports an error unless addr's host resolves to loopback
// only. It is a defense-in-depth guard: the desktop layer always builds a
// 127.0.0.1 address, and this makes binding to a routable interface impossible
// even if that ever changes.
func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("mcpserver: bad address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("mcpserver: address %q is not loopback", addr)
	}
	return nil
}
