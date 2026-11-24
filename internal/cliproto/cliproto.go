// Package cliproto is what the pelton command and the running app agree on:
// where the socket and the token live, the commands and the switch each one
// needs, the error codes, and the shapes of the data that crosses. It has no
// dependencies beyond the standard library, so the command stays small.
//
// The wire is HTTP over a Unix domain socket in a folder only the user can
// open. Every request is a POST to /v1/<command> with a JSON body and the
// token from the token file as a bearer token. A reply is a JSON Reply, or for
// the commands that carry files, the bytes themselves.
package cliproto

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Version is the protocol version. A reply from an app speaking another
// version is refused by the command rather than misread.
const Version = 1

// VersionHeader carries Version on every request and reply.
const VersionHeader = "X-Pelton-Cli"

// File names inside the command line folder.
const (
	dirName    = "cli"
	socketName = "pelton.sock"
	tokenName  = "token"
)

// Paths are where one install keeps its socket and token.
type Paths struct {
	Dir    string
	Socket string
	Token  string
}

// PathsIn returns the paths inside an app state folder.
func PathsIn(stateDir string) Paths {
	dir := filepath.Join(stateDir, dirName)
	return Paths{Dir: dir, Socket: filepath.Join(dir, socketName), Token: filepath.Join(dir, tokenName)}
}

// StateDir returns the folder the app keeps its state in for a build
// channel: os.UserConfigDir()/Pelton, Pelton-<channel> for a channel, and
// Pelton-dev whenever PELTON_DEV is set. It matches storage.DefaultPath.
func StateDir(channel string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate the user config folder: %w", err)
	}
	name := "Pelton"
	switch {
	case os.Getenv("PELTON_DEV") != "":
		name += "-dev"
	case channel != "":
		name += "-" + channel
	}
	return filepath.Join(dir, name), nil
}

// Code names why a request failed. Each maps to one exit code of the command.
type Code string

// The codes.
const (
	CodeBadRequest    Code = "bad_request"
	CodeUnauthorized  Code = "unauthorized"
	CodeAccessOff     Code = "access_off"
	CodePermissionOff Code = "permission_off"
	CodeLocked        Code = "locked"
	CodeNotFound      Code = "not_found"
	CodeRefused       Code = "refused"
	CodeError         Code = "error"
)

// Error is a failed request as the reply carries it. Switch names the
// capability that is off, for CodePermissionOff.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Switch  string `json:"switch,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Errorf builds an Error.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// AsError returns err as an Error, wrapping anything else as CodeError.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeError, Message: err.Error()}
}

// Reply is the JSON body of every reply but the file streams.
type Reply struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}
