package keylookup

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// zbase32Alphabet is the human oriented base32 alphabet WKD names files with.
const zbase32Alphabet = "ybndrfg8ejkmcpqxot1uwisza345h769"

// zbase32 encodes b, most significant bit first, without padding.
func zbase32(b []byte) string {
	var out strings.Builder
	var acc uint32
	bits := 0
	for _, c := range b {
		acc = acc<<8 | uint32(c)
		bits += 8
		for bits >= 5 {
			out.WriteByte(zbase32Alphabet[(acc>>(bits-5))&31])
			bits -= 5
		}
	}
	if bits > 0 {
		out.WriteByte(zbase32Alphabet[(acc<<(5-bits))&31])
	}
	return out.String()
}

// splitAddress returns the local part as written and the domain in its ASCII
// form, lowercased.
func splitAddress(email string) (local, domain string, err error) {
	email = strings.TrimSpace(email)
	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return "", "", fmt.Errorf("keylookup: %q is not an address", email)
	}
	domain, err = idna.Lookup.ToASCII(strings.ToLower(email[at+1:]))
	if err != nil || strings.ContainsAny(domain, "/?#:[]@") {
		return "", "", fmt.Errorf("keylookup: %q has no valid domain", email)
	}
	return email[:at], domain, nil
}

// WKDHash is the file name WKD uses for a local part: the z-base-32 encoded
// SHA-1 of the local part with ASCII letters lowercased.
func WKDHash(local string) string {
	lower := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, local)
	sum := sha1.Sum([]byte(lower))
	return zbase32(sum[:])
}

// WKDURLs returns the advanced and the direct method URL for an address.
func WKDURLs(email string) (advanced, direct string, err error) {
	local, domain, err := splitAddress(email)
	if err != nil {
		return "", "", err
	}
	hash := WKDHash(local)
	query := "?l=" + url.QueryEscape(local)
	advanced = "https://openpgpkey." + domain + "/.well-known/openpgpkey/" + domain + "/hu/" + hash + query
	direct = "https://" + domain + "/.well-known/openpgpkey/hu/" + hash + query
	return advanced, direct, nil
}

// WKD asks the address's own domain for its key: the advanced method first,
// and the direct method only when the openpgpkey host does not exist or
// cannot be reached. An answer from the advanced host, including "not found",
// is final, as the draft asks.
func (c *Client) WKD(ctx context.Context, email string) ([]byte, error) {
	advanced, direct, err := WKDURLs(email)
	if err != nil {
		return nil, err
	}
	body, err := c.get(ctx, advanced, "")
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrRefused) || ctx.Err() != nil {
		return body, err
	}
	if !unreachable(err) {
		return nil, err
	}
	return c.get(ctx, direct, "")
}

// unreachable reports whether a request failed before any http answer: the
// name did not resolve, the connection or the TLS handshake failed.
func unreachable(err error) bool {
	var uerr *url.Error
	return errors.Is(err, ErrNoHost) || (errors.As(err, &uerr) && !strings.Contains(uerr.Err.Error(), "redirect"))
}

// KeysOpenPGPOrg is the public key server run by keys.openpgp.org.
const KeysOpenPGPOrg = "https://keys.openpgp.org"

// VKS asks a server speaking the Verifying Key Server API, such as
// keys.openpgp.org, for the key of an address. Such a server only hands out
// user ids whose owner confirmed the address by mail.
func (c *Client) VKS(ctx context.Context, server, email string) ([]byte, error) {
	base, err := NormalizeServer(server)
	if err != nil {
		return nil, err
	}
	return c.get(ctx, base+"/vks/v1/by-email/"+url.PathEscape(strings.TrimSpace(email)), "application/pgp-keys")
}

// HKP asks a key server through the HKP lookup over https.
func (c *Client) HKP(ctx context.Context, server, email string) ([]byte, error) {
	base, err := NormalizeServer(server)
	if err != nil {
		return nil, err
	}
	return c.get(ctx, base+"/pks/lookup?op=get&options=mr&search="+url.QueryEscape(strings.TrimSpace(email)), "application/pgp-keys")
}

// KeyServer asks a user-added server through the VKS API first and through
// HKP when the server does not answer that.
func (c *Client) KeyServer(ctx context.Context, server, email string) ([]byte, error) {
	body, err := c.VKS(ctx, server, email)
	if err == nil || errors.Is(err, ErrRefused) || unreachable(err) || ctx.Err() != nil {
		return body, err
	}
	return c.HKP(ctx, server, email)
}

// NormalizeServer turns what a user typed for a key server into the https
// base URL requests are made against. hkps:// and https:// are accepted, and
// a bare host name means https; plain http and hkp are refused, since a lookup
// in the clear would show anyone on the path who is being written to.
func NormalizeServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: no server given", ErrRefused)
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: %q is not a server address", ErrRefused, raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "hkps":
	default:
		return "", fmt.Errorf("%w: only https key servers are used", ErrRefused)
	}
	host := strings.ToLower(u.Host)
	if strings.HasSuffix(host, ":443") {
		host = strings.TrimSuffix(host, ":443")
	}
	return "https://" + host + strings.TrimRight(u.Path, "/"), nil
}
