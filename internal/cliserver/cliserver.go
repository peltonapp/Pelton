// Package cliserver is the app's end of the pelton command: a Unix domain
// socket in a folder only the user can open, an HTTP server on it, and the
// checks every request passes before the app acts on it. Command line access
// on, the token from the token file, the app unlocked and the command's own
// switch on, in that order. It knows nothing about mail; the desktop layer
// implements Backend.
//
// The socket stays up while command line access is off, answering only that
// it is off, so the command can tell "switched off" from "not running".
package cliserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/cliproto"
)

// maxBody bounds a request, attachments of a send included.
const maxBody = 64 << 20

// State is what the checks read, on every request.
type State struct {
	Access  bool
	Locked  bool
	Caps    map[string]bool
	Version string
}

// Response is what a command answers: Data as JSON, or Bytes as they are
// with ContentType, or Stream writing the body itself. Touched says what the
// request acted on, for the log; it never holds mail content.
type Response struct {
	Data        any
	Bytes       []byte
	Stream      func(w io.Writer) error
	ContentType string
	Touched     string
}

// Entry is one line of the request log.
type Entry struct {
	Command string
	Touched string
	// Result is "ok" or the error code.
	Result string
	Switch string
}

// Backend is the app behind the socket.
type Backend interface {
	State() State
	// Handle runs a command that passed every check. A *cliproto.Error says
	// why it failed; any other error is CodeError.
	Handle(ctx context.Context, command string, body []byte) (Response, error)
	Record(Entry)
}

// Server owns the socket and the token.
type Server struct {
	paths   cliproto.Paths
	backend Backend
	log     *slog.Logger

	mu    sync.Mutex
	srv   *http.Server
	token string
}

// New creates a Server for the socket and token at paths. It does not listen
// until Start.
func New(paths cliproto.Paths, b Backend, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{paths: paths, backend: b, log: log}
}

// Start makes the folder private to the user, takes over a socket file a
// crash left behind, and listens. It fails when another process is serving
// on the socket, or the folder belongs to someone else.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		return errors.New("cliserver: already running")
	}
	if err := privateDir(s.paths.Dir); err != nil {
		return err
	}
	if err := clearStaleSocket(s.paths.Socket); err != nil {
		return err
	}
	ln, err := net.Listen("unix", s.paths.Socket)
	if err != nil {
		return fmt.Errorf("cliserver: listen on %s: %w", s.paths.Socket, err)
	}
	if err := os.Chmod(s.paths.Socket, 0o600); err != nil {
		ln.Close()
		return fmt.Errorf("cliserver: restrict the socket: %w", err)
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	s.srv = srv
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("command line socket", "err", err)
		}
	}()
	return nil
}

// Stop closes the socket and removes its file and the token file.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	s.srv = nil
	s.token = ""
	s.mu.Unlock()
	_ = os.Remove(s.paths.Token)
	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	_ = os.Remove(s.paths.Socket)
	return err
}

// SetToken replaces the token requests must carry and writes it to the
// token file, readable only by the user. An empty token removes the file,
// and then no request gets past the token check.
func (s *Server) SetToken(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" {
		s.token = ""
		if err := os.Remove(s.paths.Token); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := privateDir(s.paths.Dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.paths.Dir, ".token-*")
	if err != nil {
		return fmt.Errorf("cliserver: write the token: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(token); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.paths.Token); err != nil {
		return fmt.Errorf("cliserver: write the token: %w", err)
	}
	s.token = token
	return nil
}

func (s *Server) currentToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// privateDir makes dir and leaves it open to its owner alone, refusing one
// that belongs to another user.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cliserver: make %s: %w", dir, err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("cliserver: %s is not a folder", dir)
	}
	if !ownedByUser(info) {
		return fmt.Errorf("cliserver: %s belongs to another user", dir)
	}
	return os.Chmod(dir, 0o700)
}

// clearStaleSocket removes a socket file nothing answers on.
func clearStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 && !socketLike(info) {
		return fmt.Errorf("cliserver: %s is in the way and is not a socket", path)
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
		return fmt.Errorf("cliserver: another copy of Pelton is serving %s", path)
	}
	return os.Remove(path)
}

// ServeHTTP runs the checks and hands what passes to the backend.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(cliproto.VersionHeader, strconv.Itoa(cliproto.Version))
	command, ok := strings.CutPrefix(r.URL.Path, "/v1/")
	need, known := cliproto.Needs[command]
	if r.Method != http.MethodPost || !ok || !known {
		s.fail(w, Entry{Command: command}, cliproto.Errorf(cliproto.CodeBadRequest, "unknown command %q", command))
		return
	}
	entry := Entry{Command: command}
	if r.Header.Get(cliproto.VersionHeader) != strconv.Itoa(cliproto.Version) {
		s.fail(w, entry, cliproto.Errorf(cliproto.CodeBadRequest, "this pelton command speaks another protocol version than the app; use the one that came with it"))
		return
	}
	st := s.backend.State()
	if !st.Access {
		s.fail(w, entry, cliproto.Errorf(cliproto.CodeAccessOff, "command line access is switched off in Pelton's settings"))
		return
	}
	token := s.currentToken()
	got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		s.fail(w, entry, cliproto.Errorf(cliproto.CodeUnauthorized, "the access token was refused; it changes whenever Pelton starts, so read it again from the token file"))
		return
	}
	if command == cliproto.CmdStatus {
		s.ok(w, entry, Response{Data: status(st)})
		return
	}
	if need.Unlocked && st.Locked {
		s.fail(w, entry, cliproto.Errorf(cliproto.CodeLocked, "Pelton is locked; unlock it in the app first"))
		return
	}
	if need.Cap != "" && !st.Caps[need.Cap] {
		e := cliproto.Errorf(cliproto.CodePermissionOff, "the %q switch of command line access is off; turn it on in Pelton's settings to use this command", cliproto.CapabilityLabels[need.Cap])
		e.Switch = need.Cap
		s.fail(w, entry, e)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		s.fail(w, entry, cliproto.Errorf(cliproto.CodeBadRequest, "the request is too large"))
		return
	}
	resp, err := s.backend.Handle(r.Context(), command, body)
	entry.Touched = resp.Touched
	if err != nil {
		s.fail(w, entry, cliproto.AsError(err))
		return
	}
	s.ok(w, entry, resp)
}

func status(st State) cliproto.Status {
	out := cliproto.Status{Version: st.Version, Protocol: cliproto.Version, Access: st.Access, Locked: st.Locked}
	for _, c := range cliproto.Capabilities {
		out.Capabilities = append(out.Capabilities, cliproto.Capability{Name: c, Label: cliproto.CapabilityLabels[c], On: st.Caps[c]})
	}
	return out
}

func (s *Server) ok(w http.ResponseWriter, entry Entry, resp Response) {
	entry.Result = "ok"
	s.backend.Record(entry)
	switch {
	case resp.Stream != nil:
		w.Header().Set("Content-Type", resp.ContentType)
		w.WriteHeader(http.StatusOK)
		if err := resp.Stream(w); err != nil {
			s.log.Warn("command line stream", "command", entry.Command, "err", err)
		}
	case resp.Bytes != nil:
		w.Header().Set("Content-Type", resp.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(resp.Bytes)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp.Bytes)
	default:
		writeJSON(w, http.StatusOK, cliproto.Reply{OK: true, Data: resp.Data})
	}
}

func (s *Server) fail(w http.ResponseWriter, entry Entry, e *cliproto.Error) {
	entry.Result = string(e.Code)
	entry.Switch = e.Switch
	s.backend.Record(entry)
	writeJSON(w, httpStatus(e.Code), cliproto.Reply{Error: e})
}

func httpStatus(code cliproto.Code) int {
	switch code {
	case cliproto.CodeBadRequest:
		return http.StatusBadRequest
	case cliproto.CodeUnauthorized:
		return http.StatusUnauthorized
	case cliproto.CodeAccessOff, cliproto.CodePermissionOff:
		return http.StatusForbidden
	case cliproto.CodeLocked:
		return http.StatusLocked
	case cliproto.CodeNotFound:
		return http.StatusNotFound
	case cliproto.CodeRefused:
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
