// Package carddav talks to a CardDAV server so contacts the user keeps
// elsewhere (Nextcloud, Fastmail, a Radicale on a home server) are available in
// Pelton, and so edits made here go back (#168).
//
// The server is the user's own. Nothing here contacts anything the user did not
// configure: discovery only ever asks the domain of an address the user typed,
// and every request goes through the app's proxy-aware http client.
//
// Discovery, the sync-collection report and multiget come from
// github.com/emersion/go-webdav; the conditional PUT and DELETE come from
// internal/dav because that library's client sends neither If-Match nor
// If-None-Match, and without those a save would silently overwrite whatever
// another device wrote in the meantime.
package carddav

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"
	"github.com/peltonapp/Pelton/internal/dav"
)

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
	dav *carddav.Client
	// conn makes the raw requests this package sends itself.
	conn *dav.Conn
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
	endpoint, err := dav.Endpoint(cfg.URL)
	if err != nil {
		return nil, err
	}
	httpClient := webdav.HTTPClientWithBasicAuth(cfg.HTTP, cfg.Username, cfg.Password)
	client, err := carddav.NewClient(httpClient, endpoint.String())
	if err != nil {
		return nil, fmt.Errorf("carddav: connect %s: %w", endpoint.Host, err)
	}
	if err := client.HasSupport(ctx); err != nil {
		return nil, fmt.Errorf("carddav: %s does not offer contacts: %w", endpoint.Host, err)
	}
	conn := &dav.Conn{HTTP: cfg.HTTP, Base: endpoint, User: cfg.Username, Pass: cfg.Password}
	return &Client{dav: client, conn: conn}, nil
}

// Discover resolves the CardDAV endpoint for an email address's domain through
// the .well-known/carddav redirect, so a user who knows only their address can
// be offered their own server. An error means the domain advertises nothing,
// which is not a failure: the caller falls back to asking for a url.
func Discover(ctx context.Context, client *http.Client, address string) (string, error) {
	// go-webdav's discovery uses the default resolver and client, which would
	// step outside the user's proxy, so the well-known request is made by dav.
	return dav.DiscoverWellKnown(ctx, client, address, "carddav")
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
type Precondition = dav.Precondition

const (
	// PreconditionNew refuses the write if anything already exists at the path,
	// so two clients creating the same contact at once cannot silently become
	// one.
	PreconditionNew = dav.PreconditionNew
	// PreconditionMatch refuses the write unless the server's copy is still the
	// one the caller started from.
	PreconditionMatch = dav.PreconditionMatch
	// PreconditionNone writes regardless. It is only for a user who has been
	// shown the other version and chose theirs.
	PreconditionNone = dav.PreconditionNone
)

// Put writes a contact under the given precondition, reporting ErrConflict when
// the server refuses it. The new etag is returned; empty means the server did
// not send one and the caller refetches to learn it.
func (c *Client) Put(ctx context.Context, path string, card vcard.Card, pre Precondition, etag string) (string, error) {
	body, err := encodeCard(card)
	if err != nil {
		return "", err
	}
	newETag, err := c.conn.Put(ctx, path, vcard.MIMEType, []byte(body), pre, etag)
	if errors.Is(err, dav.ErrPreconditionFailed) {
		return "", ErrConflict
	}
	return newETag, err
}

// Delete removes a contact, refusing while the server's copy is newer than the
// one the caller saw. A path the server does not have reports ErrNotFound.
func (c *Client) Delete(ctx context.Context, path, ifMatch string) error {
	err := c.conn.Delete(ctx, path, ifMatch)
	switch {
	case errors.Is(err, dav.ErrPreconditionFailed):
		return ErrConflict
	case errors.Is(err, dav.ErrNotFound):
		return ErrNotFound
	}
	return err
}
