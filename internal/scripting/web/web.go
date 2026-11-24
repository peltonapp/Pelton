// Package web gives scripts the http module: web requests to the hosts a
// script declared as "net:<host>" and the user approved, and nothing else.
//
// The host check is made on the connection itself, not only on the URL: the
// name is resolved once, every address it resolves to is checked, and the
// connection goes to the checked address, so a second lookup cannot point it
// somewhere else. Loopback, private, link-local and other local addresses are
// refused unless the script declared that address literally, e.g.
// "net:192.168.1.10". Redirects are followed only to declared hosts, and a
// request that starts on https never moves to plain http.
package web

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// PermNet is the permission a request needs, targeted at its host:
// "net:example.org".
const PermNet = "net"

// UserAgent is sent with every request. It names the app and nothing more.
const UserAgent = "Pelton"

// Limits of one request.
const (
	maxRedirects    = 5
	maxRequestBody  = 1 << 20
	maxResponseBody = 2 << 20
	maxHeaders      = 50
	requestTimeout  = 20 * time.Second
)

// Config is how the module reaches the network.
type Config struct {
	// Dial opens the connection to an address the module already checked,
	// through the app's proxy when one is set. Nil dials directly.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Lookup resolves a host name. Nil uses the system resolver.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
	// RootCAs are the certificate authorities https trusts. Nil uses the
	// system's.
	RootCAs *x509.CertPool
}

// errDowngrade refuses a redirect from https to plain http.
var errDowngrade = errors.New("refused a redirect from https to plain http")

func (c Config) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if c.Dial != nil {
		return c.Dial(ctx, network, addr)
	}
	return (&net.Dialer{Timeout: requestTimeout}).DialContext(ctx, network, addr)
}

func (c Config) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if c.Lookup != nil {
		return c.Lookup(ctx, host)
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// special are ranges that are not ordinary public hosts: carrier grade NAT,
// IETF protocol assignments, benchmarking, reserved and broadcast, and the
// old IPv6 site-local range.
var special = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("fec0::/10"),
}

// embedded4 returns the IPv4 address a NAT64 (64:ff9b::/96) or 6to4
// (2002::/16) address carries.
func embedded4(ip netip.Addr) (netip.Addr, bool) {
	if !ip.Is6() {
		return netip.Addr{}, false
	}
	b := ip.As16()
	switch {
	case netip.MustParsePrefix("64:ff9b::/96").Contains(ip):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case netip.MustParsePrefix("2002::/16").Contains(ip):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return netip.Addr{}, false
}

// local reports whether an address is one a script may only reach when it
// declared it literally.
func local(ip netip.Addr) bool {
	ip = ip.Unmap()
	if v4, ok := embedded4(ip); ok {
		return local(v4)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || (ip.Is4() && ip.As4()[0] == 0) {
		return true
	}
	for _, p := range special {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// hostOf returns the lower case host of a URL without brackets.
func hostOf(u *url.URL) string {
	return strings.ToLower(u.Hostname())
}

// checkURL refuses anything but http and https to a declared host.
func checkURL(c *scripting.Call, u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s: only http and https addresses", c.Function)
	}
	if u.User != nil {
		return fmt.Errorf("%s: an address with a user name or password is not allowed", c.Function)
	}
	if hostOf(u) == "" {
		return fmt.Errorf("%s: the address has no host", c.Function)
	}
	return nil
}

// dialer returns the transport's dial function for one call: it checks the
// host is declared, resolves it once, checks every address and connects to
// the first that passes.
func (cfg Config) dialer(c *scripting.Call) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		host = strings.ToLower(host)
		if !c.Granted(PermNet + ":" + host) {
			return nil, fmt.Errorf("%s: %s is not a declared host", c.Function, host)
		}
		ip, err := cfg.resolve(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Function, err)
		}
		return cfg.dial(ctx, network, net.JoinHostPort(ip.String(), port))
	}
}

// resolve returns the address to connect to for a declared host. A literal
// address is used as it is, since the script named it; a name must resolve
// to public addresses only.
func (cfg Config) resolve(ctx context.Context, host string) (netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Unmap(), nil
	}
	ips, err := cfg.lookup(ctx, host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("cannot find %s: %w", host, err)
	}
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("cannot find %s", host)
	}
	for _, ip := range ips {
		if local(ip) {
			return netip.Addr{}, fmt.Errorf("%s points to a local address (%s); declare the address itself to reach it", host, ip.Unmap())
		}
	}
	return ips[0].Unmap(), nil
}

// client builds a client for one call: no cookies, no connection reuse, and
// redirects only to declared hosts and never from https to http.
func (cfg Config) client(c *scripting.Call) *http.Client {
	transport := &http.Transport{
		DialContext:            cfg.dialer(c),
		DisableKeepAlives:      true,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  requestTimeout,
		MaxResponseHeaderBytes: 64 << 10,
	}
	if cfg.RootCAs != nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: cfg.RootCAs, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("%s: more than %d redirects", c.Function, maxRedirects)
			}
			if err := checkURL(c, req.URL); err != nil {
				return err
			}
			if req.URL.Scheme == "http" && slices.ContainsFunc(via, func(r *http.Request) bool { return r.URL.Scheme == "https" }) {
				return fmt.Errorf("%s: %w (%s)", c.Function, errDowngrade, hostOf(req.URL))
			}
			if !c.Granted(PermNet + ":" + hostOf(req.URL)) {
				return fmt.Errorf("%s: refused a redirect to %s, which the script did not declare", c.Function, hostOf(req.URL))
			}
			req.Header.Set("User-Agent", UserAgent)
			return nil
		},
	}
}

// request is what a call asks for.
type request struct {
	method  string
	url     string
	headers map[string]string
	body    string
}

// forbiddenHeaders are set by the module or the transport, never a script.
var forbiddenHeaders = []string{"host", "user-agent", "content-length", "connection", "transfer-encoding", "proxy-authorization", "te", "upgrade"}

func headersArg(c *scripting.Call, t map[string]any) (map[string]string, error) {
	raw, _ := t["headers"].(map[string]any)
	if len(raw) > maxHeaders {
		return nil, fmt.Errorf("%s: at most %d headers", c.Function, maxHeaders)
	}
	out := map[string]string{}
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			if f, isNum := v.(float64); isNum {
				s = strconv.FormatFloat(f, 'f', -1, 64)
			} else {
				return nil, fmt.Errorf("%s: header %q must be text", c.Function, k)
			}
		}
		if slices.Contains(forbiddenHeaders, strings.ToLower(k)) {
			return nil, fmt.Errorf("%s: the header %q cannot be set", c.Function, k)
		}
		if strings.ContainsAny(k+s, "\r\n") {
			return nil, fmt.Errorf("%s: header %q has a line break", c.Function, k)
		}
		out[k] = s
	}
	return out, nil
}

// do makes the request. A bad request or a host the script did not declare
// is a Lua error; a failure on the way, such as a refused connection, comes
// back as nil and the message, which a script can handle.
func (cfg Config) do(c *scripting.Call, r request, decode bool) ([]any, error) {
	u, err := url.Parse(r.url)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid address: %w", c.Function, err)
	}
	if err := checkURL(c, u); err != nil {
		return nil, err
	}
	if err := c.Require(PermNet + ":" + hostOf(u)); err != nil {
		return nil, err
	}
	if len(r.body) > maxRequestBody {
		return nil, fmt.Errorf("%s: the body is larger than %d bytes", c.Function, maxRequestBody)
	}
	resp, err := cfg.send(c, u, r)
	if err != nil {
		return []any{nil, err.Error()}, nil
	}
	if decode {
		var v any
		if body, _ := resp["body"].(string); json.Unmarshal([]byte(body), &v) == nil {
			resp["json"] = v
		}
	}
	return []any{resp}, nil
}

// send makes a checked request and returns the response table.
func (cfg Config) send(c *scripting.Call, u *url.URL, r request) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(c.Ctx, requestTimeout)
	defer cancel()
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.Function, err)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := cfg.client(c).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.Is(err, errDowngrade) && errors.As(err, &ue) {
			return nil, ue.Err
		}
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, fmt.Errorf("%s: read the response: %w", c.Function, err)
	}
	if len(data) > maxResponseBody {
		return nil, fmt.Errorf("%s: the response is larger than %d bytes", c.Function, maxResponseBody)
	}
	headers := map[string]any{}
	for k, v := range resp.Header {
		headers[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return map[string]any{
		"status":  resp.StatusCode,
		"ok":      resp.StatusCode >= 200 && resp.StatusCode < 300,
		"headers": headers,
		"body":    string(data),
		"url":     resp.Request.URL.String(),
	}, nil
}

// methods a script may use.
var methods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"}

// opts reads the options table at argument i: headers and body.
func opts(c *scripting.Call, i int) (map[string]string, string, error) {
	t := c.Table(i)
	headers, err := headersArg(c, t)
	if err != nil {
		return nil, "", err
	}
	body, _ := t["body"].(string)
	return headers, body, nil
}

func encodeJSON(c *scripting.Call, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("%s: %w", c.Function, err)
	}
	return string(b), nil
}

func dryResponse(*scripting.Call) ([]any, error) {
	return []any{map[string]any{"status": 0, "ok": true, "headers": map[string]any{}, "body": "", "url": "", "dry_run": true}}, nil
}

// Register adds the net permission, the http module and its block actions.
func Register(r *scripting.Registry, cfg Config) error {
	urlParam := scripting.Required(scripting.NewParam("url", scripting.TypeString))
	optsParam := scripting.NewParam("options", scripting.TypeRef)
	bodyParam := scripting.NewParam("body", scripting.TypeText)
	valueParam := scripting.NewParam("value", scripting.TypeRef)
	method := scripting.Required(scripting.WithDefault(scripting.NewParam("method", scripting.TypeEnum), "GET"))
	method.Options = make([]scripting.Option, len(methods))
	for i, m := range methods {
		method.Options[i] = scripting.Option{Value: m, LabelKey: "scripts.option." + strings.ToLower(m)}
	}
	response := []scripting.Field{
		scripting.NewField("status", "number"), scripting.NewField("ok", "bool"),
		scripting.NewField("headers", "table"), scripting.NewField("body", "text"),
		scripting.NewField("url", "string"),
	}
	jsonResponse := append(slices.Clone(response), scripting.NewField("json", "table"))

	call := func(name string, h scripting.Handler, returns []scripting.Field, params ...scripting.Param) scripting.Function {
		f := scripting.NewFunction("http", name, h, params...)
		f.Permission = PermNet
		f.Risky = true
		f.Effect = true
		f.Returns = returns
		f.Dry = dryResponse
		return f
	}
	fns := []scripting.Function{
		call("request", func(c *scripting.Call) ([]any, error) {
			m, err := c.String(0)
			if err != nil {
				return nil, err
			}
			m = strings.ToUpper(m)
			if !slices.Contains(methods, m) {
				return nil, fmt.Errorf("http.request: the method is one of %s", strings.Join(methods, ", "))
			}
			u, err := c.String(1)
			if err != nil {
				return nil, err
			}
			headers, body, err := opts(c, 2)
			if err != nil {
				return nil, err
			}
			return cfg.do(c, request{method: m, url: u, headers: headers, body: body}, false)
		}, response, method, urlParam, optsParam),
		call("get", func(c *scripting.Call) ([]any, error) {
			u, err := c.String(0)
			if err != nil {
				return nil, err
			}
			headers, _, err := opts(c, 1)
			if err != nil {
				return nil, err
			}
			return cfg.do(c, request{method: "GET", url: u, headers: headers}, false)
		}, response, urlParam, optsParam),
		call("post", func(c *scripting.Call) ([]any, error) {
			u, err := c.String(0)
			if err != nil {
				return nil, err
			}
			body, err := c.OptString(1, "")
			if err != nil {
				return nil, err
			}
			headers, _, err := opts(c, 2)
			if err != nil {
				return nil, err
			}
			return cfg.do(c, request{method: "POST", url: u, headers: headers, body: body}, false)
		}, response, urlParam, bodyParam, optsParam),
		call("get_json", func(c *scripting.Call) ([]any, error) {
			u, err := c.String(0)
			if err != nil {
				return nil, err
			}
			headers, _, err := opts(c, 1)
			if err != nil {
				return nil, err
			}
			if _, set := headers["Accept"]; !set {
				headers["Accept"] = "application/json"
			}
			return cfg.do(c, request{method: "GET", url: u, headers: headers}, true)
		}, jsonResponse, urlParam, optsParam),
		call("post_json", func(c *scripting.Call) ([]any, error) {
			u, err := c.String(0)
			if err != nil {
				return nil, err
			}
			body, err := encodeJSON(c, c.Arg(1))
			if err != nil {
				return nil, err
			}
			headers, _, err := opts(c, 2)
			if err != nil {
				return nil, err
			}
			headers["Content-Type"] = "application/json"
			if _, set := headers["Accept"]; !set {
				headers["Accept"] = "application/json"
			}
			return cfg.do(c, request{method: "POST", url: u, headers: headers, body: body}, true)
		}, jsonResponse, urlParam, valueParam, optsParam),
	}

	action := func(id, lua string, params ...scripting.Param) scripting.Action {
		return scripting.Action{
			Meta: scripting.EntryMeta("action", id, "web"), Params: params, Outputs: response,
			Permission: PermNet, Risky: true, Lua: lua,
		}
	}
	contentType := scripting.Required(scripting.WithDefault(scripting.NewParam("content_type", scripting.TypeEnum), "json"))
	contentType.Options = scripting.NewOptions("json", "text", "form")
	return r.Register(scripting.Entries{
		Permissions: []scripting.Permission{{Meta: scripting.EntryMeta("perm", PermNet, "web"), Risk: "high"}},
		Modules:     []scripting.Module{{Meta: scripting.EntryMeta("module", "http", "web"), Functions: fns}},
		Actions: []scripting.Action{
			action("http.get", "http.get({{url}})", urlParam),
			action("http.post", `http.post({{url}}, {{body}}, {headers = {["Content-Type"] = ({json = "application/json", text = "text/plain; charset=utf-8", form = "application/x-www-form-urlencoded"})[{{content_type}}]}})`,
				urlParam, bodyParam, contentType),
		},
	})
}
