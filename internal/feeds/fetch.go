package feeds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UserAgent is sent with every request. It names the program and nothing about
// the person running it.
const UserAgent = "Pelton feed reader"

const (
	// DefaultMaxBytes caps a feed document.
	DefaultMaxBytes = 8 << 20
	// maxRedirects is how many redirects one fetch follows.
	maxRedirects = 5
	// DefaultHostGap is the least time between two requests to one host.
	DefaultHostGap = time.Second
	// maxRetryAfter caps how long a Retry-After or max-age can push a fetch
	// out, so a misconfigured server cannot silence a feed for months.
	maxRetryAfter = 24 * time.Hour
)

// ErrTooLarge is returned when a response is bigger than the fetch allows.
var ErrTooLarge = errors.New("feeds: response too large")

// StatusError is a response with a status other than success or not
// modified. RetryAfter is what the server asked for with a 429 or 503, or 0.
type StatusError struct {
	Code       int
	RetryAfter time.Duration
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("feeds: server answered %d %s", e.Code, http.StatusText(e.Code))
}

// Request is one fetch. ETag and LastModified are the validators of the
// previous response, sent so an unchanged feed costs the server nothing.
type Request struct {
	URL          string
	ETag         string
	LastModified string
	// Accept is the Accept header; empty asks for feeds.
	Accept string
	// MaxBytes caps the body; 0 means DefaultMaxBytes.
	MaxBytes int64
}

// Response is the outcome of a fetch.
type Response struct {
	// URL is where the body came from after redirects.
	URL string
	// PermanentURL is set when every redirect on the way was permanent, which
	// is a feed telling its subscribers it has moved.
	PermanentURL string
	NotModified  bool
	Body         []byte
	ContentType  string
	ETag         string
	LastModified string
	// MaxAge is the freshness the server gave the response, 0 when none.
	MaxAge time.Duration
}

// Fetcher gets documents straight from the servers that publish them. It
// keeps one request at a time per host with a gap between them, follows a
// bounded number of redirects and reads a bounded number of bytes.
type Fetcher struct {
	client  *http.Client
	gap     time.Duration
	mu      sync.Mutex
	hosts   map[string]*hostGate
	sleeper func(ctx context.Context, d time.Duration) error
}

type hostGate struct {
	turn chan struct{}
	last time.Time
}

// NewFetcher wraps an http client (the app's proxy-aware one). The client is
// copied, so the caller's redirect policy is left alone. gap is the least time
// between requests to one host; 0 means DefaultHostGap.
func NewFetcher(client *http.Client, gap time.Duration) *Fetcher {
	c := *client
	c.CheckRedirect = checkRedirect
	if gap <= 0 {
		gap = DefaultHostGap
	}
	return &Fetcher{client: &c, gap: gap, hosts: map[string]*hostGate{}, sleeper: sleepCtx}
}

type redirectKey struct{}

// redirectState tracks one fetch's redirects.
type redirectState struct {
	allPermanent bool
	count        int
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("feeds: more than %d redirects", maxRedirects)
	}
	if st, ok := req.Context().Value(redirectKey{}).(*redirectState); ok {
		st.count++
		if req.Response != nil {
			code := req.Response.StatusCode
			if code != http.StatusMovedPermanently && code != http.StatusPermanentRedirect {
				st.allPermanent = false
			}
		}
	}
	req.Header.Del("If-None-Match")
	req.Header.Del("If-Modified-Since")
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// acquire waits for the host's turn and for the gap since its last request.
func (f *Fetcher) acquire(ctx context.Context, host string) (*hostGate, error) {
	f.mu.Lock()
	g := f.hosts[host]
	if g == nil {
		g = &hostGate{turn: make(chan struct{}, 1)}
		f.hosts[host] = g
	}
	f.mu.Unlock()
	select {
	case g.turn <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if wait := f.gap - time.Since(g.last); !g.last.IsZero() && wait > 0 {
		if err := f.sleeper(ctx, wait); err != nil {
			<-g.turn
			return nil, err
		}
	}
	return g, nil
}

func release(g *hostGate) {
	g.last = time.Now()
	<-g.turn
}

const feedAccept = "application/rss+xml, application/atom+xml, application/feed+json, application/rdf+xml, application/xml;q=0.9, text/xml;q=0.9, application/json;q=0.8, */*;q=0.5"

// Fetch gets one document. A 304 comes back as NotModified with no body. Any
// other status outside 2xx is a *StatusError.
func (f *Fetcher) Fetch(ctx context.Context, r Request) (*Response, error) {
	u, err := url.Parse(strings.TrimSpace(r.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("feeds: not a web address: %q", r.URL)
	}
	gate, err := f.acquire(ctx, strings.ToLower(u.Host))
	if err != nil {
		return nil, err
	}
	defer release(gate)

	st := &redirectState{allPermanent: true}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, redirectKey{}, st), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	accept := r.Accept
	if accept == "" {
		accept = feedAccept
	}
	req.Header.Set("Accept", accept)
	if r.ETag != "" {
		req.Header.Set("If-None-Match", r.ETag)
	}
	if r.LastModified != "" {
		req.Header.Set("If-Modified-Since", r.LastModified)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	out := &Response{
		URL:          resp.Request.URL.String(),
		ContentType:  resp.Header.Get("Content-Type"),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		MaxAge:       maxAge(resp.Header.Get("Cache-Control")),
	}
	if st.count > 0 && st.allPermanent && out.URL != u.String() {
		out.PermanentURL = out.URL
	}
	switch {
	case resp.StatusCode == http.StatusNotModified:
		out.NotModified = true
		if out.ETag == "" {
			out.ETag = r.ETag
		}
		if out.LastModified == "" {
			out.LastModified = r.LastModified
		}
		return out, nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, &StatusError{Code: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	limit := r.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, ErrTooLarge
	}
	out.Body = body
	return out, nil
}

// maxAge reads max-age from a Cache-Control header. no-cache and no-store
// count as none.
func maxAge(header string) time.Duration {
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(strings.ToLower(part)), "=")
		if k != "max-age" && k != "s-maxage" {
			continue
		}
		secs, err := strconv.Atoi(strings.Trim(v, `"`))
		if err != nil || secs <= 0 {
			return 0
		}
		return min(time.Duration(secs)*time.Second, maxRetryAfter)
	}
	return 0
}

// retryAfter reads a Retry-After header, in seconds or as a date.
func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs <= 0 {
			return 0
		}
		return min(time.Duration(secs)*time.Second, maxRetryAfter)
	}
	if t, err := http.ParseTime(header); err == nil {
		if d := t.Sub(now); d > 0 {
			return min(d, maxRetryAfter)
		}
	}
	return 0
}
