// Package carddav talks to a CardDAV server so contacts the user keeps
// elsewhere (Nextcloud, Fastmail, a Radicale on a home server) are available in
// Pelton, and so edits made here go back (#168).
//
// The server is the user's own. Nothing here contacts anything the user did not
// configure: discovery only ever asks the domain of an address the user typed,
// and every request goes through the app's proxy-aware http client.
//
// Discovery, the sync-collection report and multiget come from
// github.com/emersion/go-webdav; the conditional PUT and DELETE are written
// here because that library's client sends neither If-Match nor If-None-Match,
// and without those a save would silently overwrite whatever another device
// wrote in the meantime.
package carddav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"
)

// methodPropfind is the webdav verb net/http has no constant for.
const methodPropfind = "PROPFIND"

// ErrConflict means the resource changed on the server since the copy being
// written was fetched, so the write was refused. The caller shows the user both
// versions rather than picking one.
var ErrConflict = errors.New("carddav: the contact changed on the server")

// ErrNotFound means the server has no resource at that path, which for a
// delete is the state the caller wanted anyway.
var ErrNotFound = errors.New("carddav: not found")

// Config is one address book connection.
type Config struct {
	// URL is anything the user pasted: a bare domain, the server root, a
	// principal, or a collection. Discovery starts from it.
	URL      string
	Username string
	Password string
	// HTTP is the proxy-aware client the app builds. Required: this package
	// never falls back to http.DefaultClient, so a request cannot escape the
	// user's proxy settings by accident.
	HTTP *http.Client
}

// Client is a connection to one CardDAV server.
type Client struct {
	dav  *carddav.Client
	http *http.Client
	// base is the endpoint every returned path is resolved against.
	base *url.URL
	// user and pass are kept for the raw requests this package makes itself.
	user, pass string
}

// Book is one address book collection on the server.
type Book struct {
	// Path is the collection's path on the server, which is what Sync and the
	// writes below take.
	Path string
	Name string
	// Description is the server's own description, usually empty.
	Description string
	// MaxResourceSize is the server's cap on one vCard in bytes, 0 when it did
	// not say.
	MaxResourceSize int64
}

// Changes is one sync round: what to write locally and what to drop.
type Changes struct {
	// Token is the sync token to hand back next time. An empty token from the
	// server means it does not support them and the next sync is a full one.
	Token   string
	Updated []Contact
	// Deleted are server paths that are gone.
	Deleted []string
}

// Connect opens a client and confirms the endpoint really speaks CardDAV, so a
// wrong url is an error at setup rather than an empty address book later.
func Connect(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.HTTP == nil {
		return nil, errors.New("carddav: no http client")
	}
	endpoint, err := endpointURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	httpClient := webdav.HTTPClientWithBasicAuth(cfg.HTTP, cfg.Username, cfg.Password)
	dav, err := carddav.NewClient(httpClient, endpoint.String())
	if err != nil {
		return nil, fmt.Errorf("carddav: connect %s: %w", endpoint.Host, err)
	}
	if err := dav.HasSupport(ctx); err != nil {
		return nil, fmt.Errorf("carddav: %s does not offer contacts: %w", endpoint.Host, err)
	}
	return &Client{dav: dav, http: cfg.HTTP, base: endpoint, user: cfg.Username, pass: cfg.Password}, nil
}

// Discover resolves the CardDAV endpoint for an email address's domain through
// the .well-known/carddav redirect, so a user who knows only their address can
// be offered their own server. An error means the domain advertises nothing,
// which is not a failure: the caller falls back to asking for a url.
func Discover(ctx context.Context, client *http.Client, address string) (string, error) {
	domain := address
	if _, after, ok := strings.CutLast(address, "@"); ok {
		domain = after
	}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", errors.New("carddav: no domain to discover")
	}
	// go-webdav's discovery uses the default resolver and client, which would
	// step outside the user's proxy, so the well-known request is made here.
	target := (&url.URL{Scheme: "https", Host: domain, Path: "/.well-known/carddav"}).String()
	req, err := http.NewRequestWithContext(ctx, methodPropfind, target, nil)
	if err != nil {
		return "", err
	}
	resp, err := noRedirect(client).Do(req)
	if err != nil {
		return "", fmt.Errorf("carddav: discover %s: %w", domain, err)
	}
	defer drain(resp)

	if location := resp.Header.Get("Location"); location != "" {
		resolved, err := resp.Request.URL.Parse(location)
		if err != nil {
			return "", fmt.Errorf("carddav: discover %s: %w", domain, err)
		}
		return resolved.String(), nil
	}
	// no redirect, but a server answering PROPFIND on the well-known path at
	// all is the endpoint.
	if resp.StatusCode < 400 {
		return target, nil
	}
	return "", fmt.Errorf("carddav: %s advertises no address book (%s)", domain, resp.Status)
}

// Books lists the address books the configured user has, resolving the
// principal and its home set first. A server that exposes the collection
// directly still answers, because the collection is reported as its own book.
func (c *Client) Books(ctx context.Context) ([]Book, error) {
	principal, err := c.dav.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return nil, fmt.Errorf("carddav: find principal: %w", err)
	}
	home, err := c.dav.FindAddressBookHomeSet(ctx, principal)
	if err != nil {
		return nil, fmt.Errorf("carddav: find address book home: %w", err)
	}
	found, err := c.dav.FindAddressBooks(ctx, home)
	if err != nil {
		return nil, fmt.Errorf("carddav: list address books: %w", err)
	}
	books := make([]Book, 0, len(found))
	for _, b := range found {
		books = append(books, Book{
			Path:            b.Path,
			Name:            b.Name,
			Description:     b.Description,
			MaxResourceSize: b.MaxResourceSize,
		})
	}
	return books, nil
}

// Sync returns everything that changed in a book since token. An empty token
// asks for the whole book, which is also what a server that has forgotten a
// token makes the caller do.
func (c *Client) Sync(ctx context.Context, bookPath, token string) (Changes, error) {
	res, err := c.dav.SyncCollection(ctx, bookPath, &carddav.SyncQuery{
		SyncToken: token,
		Limit:     0,
	})
	if err != nil {
		return Changes{}, fmt.Errorf("carddav: sync %s: %w", bookPath, err)
	}
	out := Changes{Token: res.SyncToken, Deleted: res.Deleted}
	for _, obj := range res.Updated {
		contact, err := fromAddressObject(obj)
		if err != nil {
			// one unreadable card must not cost the whole sync, and there is
			// nothing local to fix: skip it and take the rest.
			continue
		}
		out.Updated = append(out.Updated, contact)
	}
	return out, nil
}

// Get fetches one contact by its server path.
func (c *Client) Get(ctx context.Context, path string) (Contact, error) {
	obj, err := c.dav.GetAddressObject(ctx, path)
	if err != nil {
		return Contact{}, fmt.Errorf("carddav: get %s: %w", path, err)
	}
	return fromAddressObject(*obj)
}

// Precondition is what a write requires of the server's current state.
type Precondition int

const (
	// PreconditionNew refuses the write if anything already exists at the path,
	// so two clients creating the same contact at once cannot silently become
	// one.
	PreconditionNew Precondition = iota
	// PreconditionMatch refuses the write unless the server's copy is still the
	// one the caller started from.
	PreconditionMatch
	// PreconditionNone writes regardless. It is only for a user who has been
	// shown the other version and chose theirs.
	PreconditionNone
)

// Put writes a contact under the given precondition, reporting ErrConflict when
// the server refuses it. The new etag is returned; empty means the server did
// not send one and the caller refetches to learn it.
func (c *Client) Put(ctx context.Context, path string, card vcard.Card, pre Precondition, etag string) (string, error) {
	body, err := encodeCard(card)
	if err != nil {
		return "", err
	}
	req, err := c.request(ctx, http.MethodPut, path, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", vcard.MIMEType)
	switch pre {
	case PreconditionNew:
		req.Header.Set("If-None-Match", "*")
	case PreconditionMatch:
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("carddav: put %s: %w", path, err)
	}
	defer drain(resp)

	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return "", ErrConflict
	case resp.StatusCode >= 400:
		return "", fmt.Errorf("carddav: put %s: %s", path, resp.Status)
	}
	return resp.Header.Get("ETag"), nil
}

// Delete removes a contact, refusing while the server's copy is newer than the
// one the caller saw. A path the server does not have reports ErrNotFound.
func (c *Client) Delete(ctx context.Context, path, ifMatch string) error {
	req, err := c.request(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("carddav: delete %s: %w", path, err)
	}
	defer drain(resp)

	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return ErrConflict
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 400:
		return fmt.Errorf("carddav: delete %s: %s", path, resp.Status)
	}
	return nil
}

// request builds an authenticated request against a server path.
func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	target, err := c.base.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("carddav: bad path %q: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	if c.user != "" || c.pass != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	return req, nil
}

// endpointURL turns whatever the user pasted into a url. A bare host gets
// https, never http: a plain-text address book would put the user's contacts
// and their password on the wire.
func endpointURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("carddav: no server address")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("carddav: %q is not a server address: %w", raw, err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("carddav: %q has no host", raw)
	}
	return parsed, nil
}

// noRedirect copies a client with redirects left to the caller, so discovery
// can read the Location header the well-known path answers with.
func noRedirect(client *http.Client) *http.Client {
	copied := *client
	copied.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &copied
}

// drain closes a response body, reading what is left so the connection can be
// reused rather than dropped.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
}
