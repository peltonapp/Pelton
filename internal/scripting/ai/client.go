package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// UserAgent is sent with every request. It names the app and nothing more.
const UserAgent = "Pelton"

// Limits of one request to a provider.
const (
	// RequestTimeout bounds one request unless a step sets its own time,
	// a slow local model included.
	RequestTimeout = 120 * time.Second
	// MaxTimeout is the longest time a step may set.
	MaxTimeout = 10 * time.Minute
	// MaxRequestBytes caps what one request sends.
	MaxRequestBytes = 2 << 20
	// MaxResponseBytes caps what one answer may be.
	MaxResponseBytes = 4 << 20
)

// Message is one turn of a conversation with a model.
type Message struct {
	// Role is "user", "assistant" or "tool".
	Role string
	Text string
	// ToolCalls are the calls an assistant turn asked for.
	ToolCalls []ToolCall
	// ToolCallID and ToolName say which call a tool turn answers.
	ToolCallID string
	ToolName   string
	// ToolError marks a tool turn that reports a failure.
	ToolError bool
}

// ToolCall is one call of a tool the model asked for.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// Tool is a function the model may call, with a JSON schema of its
// parameters.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Request is one request to a model.
type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []Tool
	MaxTokens int
	// Schema asks for a JSON object of this schema, through the provider's
	// structured output or JSON mode where it has one. The answer is checked
	// against it either way.
	Schema map[string]any
}

// Response is a model's answer.
type Response struct {
	Text         string
	ToolCalls    []ToolCall
	InputTokens  int
	OutputTokens int
}

// Network is how requests reach a provider.
type Network struct {
	// Dial connects to a public address, through the app's proxy when one is
	// set. Nil dials directly. Addresses on this device or the local network
	// are always dialled directly.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Lookup resolves a host name. Nil uses the system resolver.
	Lookup func(ctx context.Context, host string) ([]netip.Addr, error)
}

// Target is where a provider's requests go: the address its host resolved
// to, checked once and used for the connection, so a second lookup cannot
// point it elsewhere.
type Target struct {
	addr netip.Addr
	// Local is set when every address the host resolved to is on this device
	// or a private network.
	Local bool
}

// Resolve looks up a provider's host once.
func (n Network) Resolve(ctx context.Context, p Provider) (Target, error) {
	u, err := parseBase(p.BaseURL)
	if err != nil {
		return Target{}, err
	}
	host := strings.ToLower(u.Hostname())
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else if n.Lookup != nil {
		ips, err = n.Lookup(ctx, host)
		if err != nil {
			return Target{}, fmt.Errorf("cannot find %s: %w", host, err)
		}
	} else {
		ips, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return Target{}, fmt.Errorf("cannot find %s: %w", host, err)
		}
	}
	if len(ips) == 0 {
		return Target{}, fmt.Errorf("cannot find %s", host)
	}
	local := true
	for _, ip := range ips {
		ip = ip.Unmap()
		if ip.IsUnspecified() || ip.IsMulticast() {
			return Target{}, fmt.Errorf("%s points to %s, which cannot be reached", host, ip)
		}
		if !localAddr(ip) {
			local = false
		}
	}
	return Target{addr: ips[0].Unmap(), Local: local}, nil
}

// errRedirect refuses every redirect: an API answers where it was asked.
var errRedirect = errors.New("the provider answered with a redirect, which is not followed")

func (n Network) client(t Target, timeout time.Duration) *http.Client {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		pinned := net.JoinHostPort(t.addr.String(), port)
		if localAddr(t.addr) || n.Dial == nil {
			return (&net.Dialer{Timeout: 20 * time.Second}).DialContext(ctx, network, pinned)
		}
		return n.Dial(ctx, network, pinned)
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:            dial,
			DisableKeepAlives:      true,
			TLSHandshakeTimeout:    10 * time.Second,
			ResponseHeaderTimeout:  timeout,
			MaxResponseHeaderBytes: 64 << 10,
		},
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirect
		},
	}
}

// Endpoint is one provider ready to be called, as a driver sees it.
type Endpoint struct {
	Provider Provider
	// Key is the API key, "" when the provider has none.
	Key    string
	client *http.Client
}

// Open resolves a provider's host and returns its endpoint and target.
// timeout bounds each request, RequestTimeout when 0.
func (n Network) Open(ctx context.Context, p Provider, key string, timeout time.Duration) (*Endpoint, Target, error) {
	t, err := n.Resolve(ctx, p)
	if err != nil {
		return nil, Target{}, err
	}
	return n.Connect(p, key, t, timeout), t, nil
}

// Connect returns the endpoint of a provider whose host already resolved to
// t.
func (n Network) Connect(p Provider, key string, t Target, timeout time.Duration) *Endpoint {
	if timeout <= 0 {
		timeout = RequestTimeout
	}
	timeout = min(timeout, MaxTimeout)
	return &Endpoint{Provider: p, Key: key, client: n.client(t, timeout)}
}

// URL joins a path to the provider's base URL.
func (e *Endpoint) URL(path string) string {
	return strings.TrimRight(strings.TrimSpace(e.Provider.BaseURL), "/") + path
}

// Error is a provider's refusal or failure, with the key taken out.
type Error struct {
	// Status is the HTTP status, 0 when the request never got an answer.
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return e.Message
	}
	return fmt.Sprintf("the provider answered %d: %s", e.Status, e.Message)
}

// redact removes the key from text that may echo it.
func (e *Endpoint) redact(s string) string {
	if e.Key != "" {
		s = strings.ReplaceAll(s, e.Key, "[key]")
	}
	return s
}

// DoJSON sends in as JSON (no body when nil) and decodes the answer into out.
// A status outside 2xx is an *Error carrying the provider's message. Nothing
// it returns contains the key.
func (e *Endpoint) DoJSON(ctx context.Context, method, path string, headers map[string]string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode the request: %w", err)
		}
		if len(b) > MaxRequestBytes {
			return &Error{Message: fmt.Sprintf("the request is larger than %d bytes", MaxRequestBytes)}
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.URL(path), body)
	if err != nil {
		return &Error{Message: e.redact(err.Error())}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := e.client.Do(req)
	if err != nil {
		return &Error{Message: e.redact(transportError(err))}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return &Error{Status: resp.StatusCode, Message: e.redact("read the answer: " + err.Error())}
	}
	if len(data) > MaxResponseBytes {
		return &Error{Status: resp.StatusCode, Message: fmt.Sprintf("the answer is larger than %d bytes", MaxResponseBytes)}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &Error{Status: resp.StatusCode, Message: e.redact(errorText(data))}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &Error{Status: resp.StatusCode, Message: "the answer is not the JSON expected"}
	}
	return nil
}

// transportError keeps the reason of a failed request without the URL,
// which net/http puts first.
func transportError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "the provider did not answer in time"
	}
	return err.Error()
}

// errorText finds the message in an error body, whichever shape the
// provider uses, and keeps it short.
func errorText(data []byte) string {
	var v struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Detail  json.RawMessage `json:"detail"`
	}
	msg := ""
	if json.Unmarshal(data, &v) == nil {
		var inner struct {
			Message string `json:"message"`
		}
		var plain string
		switch {
		case json.Unmarshal(v.Error, &inner) == nil && inner.Message != "":
			msg = inner.Message
		case json.Unmarshal(v.Error, &plain) == nil && plain != "":
			msg = plain
		case v.Message != "":
			msg = v.Message
		case len(v.Detail) > 0:
			if json.Unmarshal(v.Detail, &plain) == nil {
				msg = plain
			} else {
				msg = string(v.Detail)
			}
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(data))
	}
	if msg == "" {
		msg = "no reason given"
	}
	return clip(msg, 300)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && s[cut]&0xc0 == 0x80 {
		cut--
	}
	return s[:cut] + "..."
}

// argsJSON encodes tool call arguments as the JSON text the OpenAI protocol
// carries.
func argsJSON(args map[string]any) string {
	if args == nil {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// parseArgs reads tool call arguments sent as JSON text; anything that is
// not an object becomes no arguments.
func parseArgs(s string) map[string]any {
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// callID makes an id for a tool call a provider sent without one.
func callID(i int) string {
	return "call_" + strconv.Itoa(i+1)
}
