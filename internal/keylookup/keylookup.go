// Package keylookup asks the network for a correspondent's OpenPGP key: the
// Web Key Directory of the address's own domain, keys.openpgp.org and key
// servers the user added. Every request goes out over https, through the
// app's proxy when one is set, with a fixed User-Agent, a size and time limit,
// no cookies, and no redirect to another host.
//
// The host check is made on the connection, as the scripting web module does:
// the name is resolved once, every address it resolves to must be public, and
// the connection goes to the checked address, so a second lookup cannot point
// it somewhere else.
package keylookup

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// UserAgent names the app and nothing else: no version, no platform.
const UserAgent = "Pelton"

// Limits of one request.
const (
	maxResponse    = 1 << 20
	requestTimeout = 15 * time.Second
	maxRedirects   = 3
)

var (
	// ErrNotFound means the source answered and has no key for the address.
	ErrNotFound = errors.New("keylookup: no key published for this address")
	// ErrNoHost means the host name does not resolve.
	ErrNoHost = errors.New("keylookup: host not found")
	// ErrRefused means a request was stopped by one of the rules above.
	ErrRefused = errors.New("keylookup: request refused")
)

// Config is how the client reaches the network. The zero value dials
// directly, resolves with the system resolver and trusts the system roots.
type Config struct {
	// Dial opens a connection to an already checked address, through the
	// app's proxy when one is set.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Lookup resolves a host name.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	// TLS is the client TLS configuration; nil uses the system roots.
	TLS *tls.Config
	// AllowAddr says whether an address may be connected to; nil allows
	// public addresses only.
	AllowAddr func(netip.Addr) bool
}

// Client makes lookups. It is safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client
}

// New returns a client for cfg.
func New(cfg Config) *Client {
	c := &Client{cfg: cfg}
	transport := &http.Transport{
		DialContext:            c.dial,
		TLSClientConfig:        cfg.TLS,
		DisableKeepAlives:      true,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  requestTimeout,
		MaxResponseHeaderBytes: 64 << 10,
		ForceAttemptHTTP2:      true,
	}
	c.http = &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("%w: too many redirects", ErrRefused)
			}
			if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Hostname(), via[0].URL.Hostname()) {
				return fmt.Errorf("%w: redirect to %s", ErrRefused, req.URL.Host)
			}
			req.Header.Set("User-Agent", UserAgent)
			return nil
		},
	}
	return c
}

func (c *Client) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip, err := c.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	target := net.JoinHostPort(ip.String(), port)
	if c.cfg.Dial != nil {
		return c.cfg.Dial(ctx, network, target)
	}
	return (&net.Dialer{Timeout: requestTimeout}).DialContext(ctx, network, target)
}

func (c *Client) resolve(ctx context.Context, host string) (netip.Addr, error) {
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		lookup := c.cfg.Lookup
		if lookup == nil {
			lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
				return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			}
		}
		ips, err = lookup(ctx, host)
		if err != nil || len(ips) == 0 {
			return netip.Addr{}, fmt.Errorf("%w: %s", ErrNoHost, host)
		}
	}
	allow := c.cfg.AllowAddr
	if allow == nil {
		allow = Public
	}
	for _, ip := range ips {
		if !allow(ip.Unmap()) {
			return netip.Addr{}, fmt.Errorf("%w: %s points to a local address", ErrRefused, host)
		}
	}
	return ips[0].Unmap(), nil
}

// special are ranges that are not ordinary public hosts: carrier grade NAT,
// IETF protocol assignments, benchmarking, reserved, and old site-local.
var special = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2002::/16"),
}

// Public reports whether ip is an ordinary public address. NAT64 and 6to4
// addresses are refused outright rather than unpacked, since a key lookup has
// no reason to use them.
func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || (ip.Is4() && ip.As4()[0] == 0) {
		return false
	}
	for _, p := range special {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// get fetches one https URL and returns its body, or ErrNotFound for a 404 or
// 410.
func (c *Client) get(ctx context.Context, rawURL, accept string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: %s is not a plain https address", ErrRefused, rawURL)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("keylookup: %s answered %s", u.Host, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponse {
		return nil, fmt.Errorf("%w: answer from %s is too large", ErrRefused, u.Host)
	}
	if len(body) == 0 {
		return nil, ErrNotFound
	}
	return body, nil
}
