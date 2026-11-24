// Package crypto isolates Pelton's message signing and encryption behind a
// small, library-agnostic surface so the rest of the app never imports an
// openpgp or x509 package directly. PGP/MIME (RFC 3156) is implemented in full.
// S/MIME (RFC 8551) verifies signatures on received mail but cannot produce
// them, so sending S/MIME hard-fails; see smime.go for why.
//
// # The one safety rule that shapes this package
//
// If a caller asks for a message to be signed or encrypted and that operation
// cannot be completed for any reason (missing recipient key, missing or locked
// private key, library error), Wrap returns an error and a nil Part. It never
// returns a partial or plaintext result. The orchestration in internal/smtp
// only ever transmits bytes that came back from a successful Wrap, so a message
// that was meant to be encrypted can never leave as plaintext. The plaintext
// entity stays a local variable inside Wrap and is dropped on any error path.
package crypto

import "errors"

// Mode selects which crypto operation Wrap performs.
type Mode int

const (
	// ModeNone means no crypto. Callers should not pass this to Wrap; the smtp
	// layer skips Wrap entirely for an unprotected message.
	ModeNone Mode = iota
	// ModeSign produces a PGP/MIME multipart/signed message.
	ModeSign
	// ModeEncrypt produces a PGP/MIME multipart/encrypted message.
	ModeEncrypt
	// ModeSignEncrypt signs then encrypts; the signature rides inside the
	// encrypted message. See pgp.go for the ordering rationale.
	ModeSignEncrypt
)

// String renders a Mode for logs and errors.
func (m Mode) String() string {
	switch m {
	case ModeNone:
		return "none"
	case ModeSign:
		return "sign"
	case ModeEncrypt:
		return "encrypt"
	case ModeSignEncrypt:
		return "sign+encrypt"
	default:
		return "unknown"
	}
}

// Options carries the per-message inputs a crypto operation needs. Recipients
// and SenderEmail are addresses used to look keys up in the store; Passphrase
// unlocks the sender's private key and is pulled from the keyring by the caller,
// never stored or logged here.
type Options struct {
	SenderEmail string
	Recipients  []string
	Passphrase  []byte
	// RecipientKeys, when set, are encrypted to in place of looking Recipients
	// up in the keyring: binary or armored public keys, each named on the
	// message. HiddenKeys are encrypted to without being named, for Bcc
	// recipients. Either being set selects this path.
	RecipientKeys [][]byte
	HiddenKeys    [][]byte
}

// Part is the result of a crypto operation: the full Content-Type header value
// for the outer message (including boundary and protocol parameters) and the
// already-encoded body bytes that follow the headers. The smtp layer puts the
// Content-Type on the message and appends the body, touching no crypto types.
type Part struct {
	ContentType string
	Body        []byte
}

// Engine wraps an already-built MIME entity in a signed or encrypted structure.
// PGP implements it; SMIME implements it as a hard failure for now.
type Engine interface {
	Wrap(entity []byte, mode Mode, opts Options) (*Part, error)
}

// Sentinel errors. Callers can match these to give the user an accurate reason
// a protected send was refused. They all mean the same thing operationally: the
// send must not proceed in plaintext.
var (
	// ErrRecipientKeyNotFound means no public key was found for a recipient, so
	// the message cannot be encrypted to them.
	ErrRecipientKeyNotFound = errors.New("crypto: recipient public key not found")
	// ErrSenderKeyNotFound means the sender's own key for signing was not found.
	ErrSenderKeyNotFound = errors.New("crypto: sender private key not found")
	// ErrPassphraseRequired means the private key is locked and no passphrase was
	// supplied to unlock it. Per the safety rule this is a hard failure, not a
	// prompt-and-continue.
	ErrPassphraseRequired = errors.New("crypto: private key is locked and no passphrase was provided")
	// ErrNoRecipients means encryption was requested with an empty recipient list.
	ErrNoRecipients = errors.New("crypto: no recipients for encryption")
	// ErrUnsupportedMode means Wrap was called with a mode it cannot handle.
	ErrUnsupportedMode = errors.New("crypto: unsupported mode")
)
