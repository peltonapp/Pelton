// Package dav holds the WebDAV plumbing the CardDAV and CalDAV clients share:
// turning what the user typed into an endpoint, the .well-known lookup, the
// authenticated request, conditional writes and the multistatus reply.
//
// Every request goes through the http client the caller hands in, which is the
// app's proxy-aware one. Nothing here falls back to http.DefaultClient, so a
// request cannot step outside the user's proxy settings by accident.
package dav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// MethodPropfind and MethodReport are the WebDAV verbs net/http has no
// constant for.
const (
	MethodPropfind = "PROPFIND"
	MethodReport   = "REPORT"
)

// ErrPreconditionFailed means the server refused a conditional write because
// the resource changed since the copy being written was fetched.
var ErrPreconditionFailed = errors.New("dav: precondition failed")

// ErrNotFound means the server has no resource at that path.
var ErrNotFound = errors.New("dav: not found")

// Precondition is what a write requires of the server's current state.
type Precondition int

const (
	// PreconditionNew refuses the write if anything already exists at the path,
	// so two clients creating the same resource at once cannot silently become
	// one.
	PreconditionNew Precondition = iota
	// PreconditionMatch refuses the write unless the server's copy is still the
	// one the caller started from.
	PreconditionMatch
	// PreconditionNone writes regardless. It is only for a user who has been
	// shown the other version and chose theirs.
	PreconditionNone
)

// Conn is an authenticated connection to one WebDAV server.
type Conn struct {
	// HTTP is the proxy-aware client every request goes through.
	HTTP *http.Client
	// Base is the endpoint every path is resolved against.
	Base *url.URL
	// User and Pass are sent as basic auth when either is set.
	User, Pass string
}

// Request builds an authenticated request against a server path.
func (c *Conn) Request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	target, err := c.Base.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("dav: bad path %q: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	if c.User != "" || c.Pass != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}
	return req, nil
}

// Put writes body to path under the given precondition, reporting
// ErrPreconditionFailed when the server refuses it. The new etag is returned;
// empty means the server did not send one and the caller refetches to learn it.
func (c *Conn) Put(ctx context.Context, path, contentType string, body []byte, pre Precondition, etag string) (string, error) {
	req, err := c.Request(ctx, http.MethodPut, path, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	switch pre {
	case PreconditionNew:
		req.Header.Set("If-None-Match", "*")
	case PreconditionMatch:
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("dav: put %s: %w", path, err)
	}
	defer Drain(resp)

	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return "", ErrPreconditionFailed
	case resp.StatusCode >= 400:
		return "", &StatusError{Method: http.MethodPut, Path: path, Code: resp.StatusCode, Status: resp.Status}
	}
	return resp.Header.Get("ETag"), nil
}

// Delete removes the resource at path, refusing while the server's copy is
// newer than ifMatch. A path the server does not have reports ErrNotFound.
func (c *Conn) Delete(ctx context.Context, path, ifMatch string) error {
	req, err := c.Request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("dav: delete %s: %w", path, err)
	}
	defer Drain(resp)

	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return ErrPreconditionFailed
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 400:
		return &StatusError{Method: http.MethodDelete, Path: path, Code: resp.StatusCode, Status: resp.Status}
	}
	return nil
}

// Get fetches the resource at path and returns its body and etag.
func (c *Conn) Get(ctx context.Context, path string) ([]byte, string, error) {
	req, err := c.Request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("dav: get %s: %w", path, err)
	}
	defer Drain(resp)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, "", ErrNotFound
	case resp.StatusCode >= 400:
		return nil, "", &StatusError{Method: http.MethodGet, Path: path, Code: resp.StatusCode, Status: resp.Status}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, "", fmt.Errorf("dav: read %s: %w", path, err)
	}
	return body, resp.Header.Get("ETag"), nil
}

// maxBody caps one response read into memory. A calendar with years of
// history is a few megabytes; anything past this is not a calendar.
const maxBody = 64 << 20

// StatusError is a request the server answered with an error status.
type StatusError struct {
	Method string
	Path   string
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("dav: %s %s: %s", strings.ToLower(e.Method), e.Path, e.Status)
}

// Endpoint turns whatever the user pasted into a url. A bare host gets https,
// never http: a plain-text server would put the user's data and their password
// on the wire.
func Endpoint(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("dav: no server address")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("dav: %q is not a server address: %w", raw, err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("dav: %q has no host", raw)
	}
	return parsed, nil
}

// DiscoverWellKnown resolves the endpoint for an email address's domain
// through the /.well-known/<service> redirect (RFC 6764), so a user who knows
// only their address can be offered their own server. service is "carddav" or
// "caldav". An error means the domain advertises nothing, which is not a
// failure: the caller falls back to asking for a url.
func DiscoverWellKnown(ctx context.Context, client *http.Client, address, service string) (string, error) {
	domain := address
	if at := strings.LastIndex(address, "@"); at >= 0 {
		domain = address[at+1:]
	}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", fmt.Errorf("%s: no domain to discover", service)
	}
	target := (&url.URL{Scheme: "https", Host: domain, Path: "/.well-known/" + service}).String()
	req, err := http.NewRequestWithContext(ctx, MethodPropfind, target, nil)
	if err != nil {
		return "", err
	}
	resp, err := NoRedirect(client).Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: discover %s: %w", service, domain, err)
	}
	defer Drain(resp)

	if location := resp.Header.Get("Location"); location != "" {
		resolved, err := resp.Request.URL.Parse(location)
		if err != nil {
			return "", fmt.Errorf("%s: discover %s: %w", service, domain, err)
		}
		return resolved.String(), nil
	}
	// no redirect, but a server answering PROPFIND on the well-known path at
	// all is the endpoint.
	if resp.StatusCode < 400 {
		return target, nil
	}
	return "", fmt.Errorf("%s: %s advertises nothing (%s)", service, domain, resp.Status)
}

// NoRedirect copies a client with redirects left to the caller, so discovery
// can read the Location header the well-known path answers with.
func NoRedirect(client *http.Client) *http.Client {
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &copied
}

// Drain closes a response body, reading what is left so the connection can be
// reused rather than dropped.
func Drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
}
