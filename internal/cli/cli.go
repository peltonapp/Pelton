// Package cli is the pelton command: it reads the arguments, asks the
// running app over its socket (internal/cliproto) and prints the answer, as
// text for a person or with --json for a program. Text that came from mail
// is printed with control characters and terminal escape sequences made
// visible, so a subject cannot move the cursor, retitle the window or hide
// what follows.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/cliproto"
)

// Exit codes. They are part of the command's interface and do not change.
const (
	ExitOK         = 0
	ExitError      = 1
	ExitUsage      = 2
	ExitNotRunning = 3
	ExitAccessOff  = 4
	ExitPermission = 5
	ExitLocked     = 6
	ExitNotFound   = 7
	ExitRefused    = 8
)

// Options is what the command runs with besides its arguments.
type Options struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// StdinTerminal is set when stdin is a terminal, so a body is not read
	// from it.
	StdinTerminal bool
	// StdoutTerminal is set when stdout is a terminal, where even a raw
	// message source is printed cleaned.
	StdoutTerminal bool
	// Paths is where the socket and the token file are.
	Paths   cliproto.Paths
	Version string
	// Launch starts the app with args, for pelton open. Nil when this
	// build cannot start it.
	Launch func(args []string) error
	// WaitForApp is how long pelton open waits for a started app.
	WaitForApp time.Duration
}

// exitError ends the command with a code and a message.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func usageErr(format string, args ...any) error {
	return &exitError{code: ExitUsage, msg: fmt.Sprintf(format, args...)}
}

var errNotRunning = &exitError{code: ExitNotRunning, msg: "Pelton is not running. Start it, or run `pelton open`."}

// exitCode maps an error to the code the command exits with.
func exitCode(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	var pe *cliproto.Error
	if errors.As(err, &pe) {
		return codeExit(pe.Code)
	}
	return ExitError
}

func codeExit(code cliproto.Code) int {
	switch code {
	case cliproto.CodeBadRequest:
		return ExitUsage
	case cliproto.CodeUnauthorized, cliproto.CodeAccessOff:
		return ExitAccessOff
	case cliproto.CodePermissionOff:
		return ExitPermission
	case cliproto.CodeLocked:
		return ExitLocked
	case cliproto.CodeNotFound:
		return ExitNotFound
	case cliproto.CodeRefused:
		return ExitRefused
	}
	return ExitError
}

// env is one run of the command.
type env struct {
	ctx  context.Context
	opts Options
	json bool
	http *http.Client
}

// Main runs the command and returns its exit code.
func Main(args []string, opts Options) int {
	if opts.WaitForApp == 0 {
		opts.WaitForApp = 30 * time.Second
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	e := &env{ctx: ctx, opts: opts}
	e.http = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", opts.Paths.Socket)
	}}}
	var rest []string
	for _, a := range args {
		if a == "--json" {
			e.json = true
			continue
		}
		rest = append(rest, a)
	}
	err := e.dispatch(rest)
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if !errors.As(err, &ee) || ee.msg != "" {
		fmt.Fprintln(opts.Stderr, "pelton: "+clean(err.Error(), false))
	}
	return exitCode(err)
}

// IsCommand reports whether args ask for the command line tool rather than
// the app, for the app binary that answers both.
func IsCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "-h", "--help", "--version":
		return true
	}
	_, ok := lookup(args[0])
	return ok
}

// request sends one command and returns the reply, reading the token file
// fresh each time, since the app makes a new token whenever it starts.
func (e *env) request(command string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost, "http://pelton/v1/"+command, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(cliproto.VersionHeader, strconv.Itoa(cliproto.Version))
	if token, err := os.ReadFile(e.opts.Paths.Token); err == nil {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}
	resp, err := e.http.Do(req)
	if err != nil {
		if e.ctx.Err() != nil {
			return nil, &exitError{code: ExitError, msg: "interrupted"}
		}
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return nil, errNotRunning
		}
		return nil, err
	}
	if v := resp.Header.Get(cliproto.VersionHeader); v != strconv.Itoa(cliproto.Version) {
		resp.Body.Close()
		return nil, &exitError{code: ExitError, msg: "the running Pelton speaks another protocol version; use the pelton command that came with it"}
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, replyError(resp.Body)
	}
	return resp, nil
}

func replyError(r io.Reader) error {
	var reply cliproto.Reply
	if err := json.NewDecoder(r).Decode(&reply); err != nil || reply.Error == nil {
		return &exitError{code: ExitError, msg: "the app sent a reply that could not be read"}
	}
	return reply.Error
}

// call sends a command and decodes the data of its reply into out.
func (e *env) call(command string, body, out any) error {
	resp, err := e.request(command, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var reply struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error *cliproto.Error `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return &exitError{code: ExitError, msg: "the app sent a reply that could not be read"}
	}
	if !reply.OK {
		if reply.Error != nil {
			return reply.Error
		}
		return &exitError{code: ExitError, msg: "the app refused the request"}
	}
	if out == nil || len(reply.Data) == 0 {
		return nil
	}
	return json.Unmarshal(reply.Data, out)
}

// printJSON writes v for a program to read. The C1 controls and the
// bidirectional overrides, which JSON allows raw, are escaped as well, so
// the output is as safe in a terminal as the text form.
func (e *env) printJSON(v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	var out strings.Builder
	for _, r := range buf.String() {
		if (r >= 0x80 && r <= 0x9f) || isBidiControl(r) {
			fmt.Fprintf(&out, `\u%04x`, r)
			continue
		}
		out.WriteRune(r)
	}
	_, err := io.WriteString(e.opts.Stdout, out.String())
	return err
}

func (e *env) printf(format string, args ...any) {
	fmt.Fprintf(e.opts.Stdout, format, args...)
}
