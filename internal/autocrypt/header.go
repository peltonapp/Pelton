// Package autocrypt implements the parts of Autocrypt Level 1 that need no
// cryptography: reading and writing the Autocrypt and Autocrypt-Gossip
// headers, the per-peer state and its update rules, and the recommendation
// that decides whether a message is encrypted by default.
//
// Keys travel through this package as opaque binary OpenPGP data. Checking
// that a key is usable is the caller's job, through a function it passes in,
// so the rules here stay testable without any key material.
package autocrypt

import (
	"encoding/base64"
	"errors"
	"strings"
)

// Prefer-encrypt values. Anything other than mutual reads as no preference.
const (
	PreferMutual       = "mutual"
	PreferNoPreference = "nopreference"
)

// Header names.
const (
	HeaderName       = "Autocrypt"
	GossipHeaderName = "Autocrypt-Gossip"
)

// foldWidth is the longest line a folded keydata line is given, well under
// the 78 characters RFC 5322 recommends.
const foldWidth = 72

// ErrInvalidHeader means a header broke one of the specification's rules and
// must be ignored.
var ErrInvalidHeader = errors.New("autocrypt: invalid header")

// Header is one parsed Autocrypt or Autocrypt-Gossip header.
type Header struct {
	// Addr is the address the key belongs to, lowercased.
	Addr string
	// PreferEncrypt is PreferMutual or PreferNoPreference. Gossip headers
	// always carry PreferNoPreference.
	PreferEncrypt string
	// KeyData is the binary OpenPGP transferable public key.
	KeyData []byte
}

// Parse reads the value of an Autocrypt header. Per the specification the
// header is invalid when addr or keydata is missing, when keydata is not
// base64, or when it carries an attribute that is neither known nor starts
// with an underscore.
func Parse(value string) (Header, error) {
	return parse(value, false)
}

// ParseGossip reads the value of an Autocrypt-Gossip header, which has the
// same syntax but no prefer-encrypt attribute.
func ParseGossip(value string) (Header, error) {
	return parse(value, true)
}

func parse(value string, gossip bool) (Header, error) {
	h := Header{PreferEncrypt: PreferNoPreference}
	var haveAddr, haveKey bool
	for _, attr := range strings.Split(value, ";") {
		attr = strings.TrimSpace(attr)
		if attr == "" {
			continue
		}
		name, val, ok := strings.Cut(attr, "=")
		if !ok {
			return Header{}, ErrInvalidHeader
		}
		name = strings.ToLower(strings.TrimSpace(name))
		val = strings.TrimSpace(val)
		switch {
		case name == "addr":
			if haveAddr {
				return Header{}, ErrInvalidHeader
			}
			h.Addr = NormalizeAddr(val)
			haveAddr = h.Addr != ""
		case name == "prefer-encrypt" && !gossip:
			if strings.EqualFold(val, PreferMutual) {
				h.PreferEncrypt = PreferMutual
			}
		case name == "keydata":
			if haveKey {
				return Header{}, ErrInvalidHeader
			}
			data, err := base64.StdEncoding.DecodeString(stripSpace(val))
			if err != nil || len(data) == 0 {
				return Header{}, ErrInvalidHeader
			}
			h.KeyData = data
			haveKey = true
		case strings.HasPrefix(name, "_"):
		default:
			return Header{}, ErrInvalidHeader
		}
	}
	if !haveAddr || !haveKey {
		return Header{}, ErrInvalidHeader
	}
	return h, nil
}

// Format renders an Autocrypt header value, folded so no line runs long. The
// prefer-encrypt attribute is only written for mutual, as the specification
// asks.
func Format(h Header) string {
	return format(h, false)
}

// FormatGossip renders an Autocrypt-Gossip header value.
func FormatGossip(h Header) string {
	return format(h, true)
}

func format(h Header, gossip bool) string {
	var b strings.Builder
	b.WriteString("addr=")
	b.WriteString(h.Addr)
	b.WriteString(";")
	if !gossip && h.PreferEncrypt == PreferMutual {
		b.WriteString(" prefer-encrypt=mutual;")
	}
	b.WriteString(" keydata=")
	encoded := base64.StdEncoding.EncodeToString(h.KeyData)
	for len(encoded) > 0 {
		n := min(foldWidth, len(encoded))
		b.WriteString("\r\n ")
		b.WriteString(encoded[:n])
		encoded = encoded[n:]
	}
	return b.String()
}

// NormalizeAddr lowercases and trims an address for comparison. Autocrypt
// compares addresses without case.
func NormalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(addr, "<")
	addr = strings.TrimSuffix(addr, ">")
	return strings.ToLower(addr)
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		}
		return r
	}, s)
}

// SelectHeader picks the Autocrypt header that applies to a message from the
// raw header values found on it. Only a header whose addr matches the From
// address counts, and when more than one valid header matches, none does.
func SelectHeader(values []string, from string) (Header, bool) {
	from = NormalizeAddr(from)
	var found []Header
	for _, v := range values {
		h, err := Parse(v)
		if err != nil || h.Addr != from {
			continue
		}
		found = append(found, h)
	}
	if len(found) != 1 {
		return Header{}, false
	}
	return found[0], true
}
