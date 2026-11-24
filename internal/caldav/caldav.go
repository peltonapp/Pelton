// Package caldav talks to a CalDAV server (RFC 4791) so the calendars a user
// keeps on their own server (Nextcloud, Fastmail, Radicale, iCloud) show in
// Pelton, and so events made or changed here go back.
//
// The server is the user's own. Discovery only ever asks the domain of an
// address the user typed, and every request goes through the app's
// proxy-aware http client via internal/dav, the same plumbing the CardDAV
// client uses. Every write is conditional on the etag the local copy came
// from, so an edit made on a phone in the meantime is reported as a conflict
// instead of being overwritten.
package caldav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/peltonapp/Pelton/internal/dav"
	"github.com/peltonapp/Pelton/internal/ical"
)

// XML namespaces CalDAV servers report properties in.
const (
	nsDAV    = "DAV:"
	nsCalDAV = "urn:ietf:params:xml:ns:caldav"
	nsCS     = "http://calendarserver.org/ns/"
	nsApple  = "http://apple.com/ns/ical/"
)

// ErrConflict means the event changed on the server since the copy being
// written was fetched, so the write was refused.
var ErrConflict = errors.New("caldav: the event changed on the server")

// ErrNotFound means the server has no resource at that path.
var ErrNotFound = errors.New("caldav: not found")

// ErrSyncTokenInvalid means the server no longer accepts the sync token, so
// the caller has to read the calendar in full.
var ErrSyncTokenInvalid = errors.New("caldav: the sync token is no longer valid")

// ErrNoCalendars means the endpoint answered but offers no event calendars,
// which is what a wrong url or an address book url looks like.
var ErrNoCalendars = errors.New("caldav: the server offers no calendars")

// Config is one CalDAV connection.
type Config struct {
	// URL is anything the user pasted: a bare domain, the server root, a
	// principal, or a calendar collection.
	URL      string
	Username string
	Password string
	// HTTP is the proxy-aware client the app builds. Required.
	HTTP *http.Client
}

// Client is a connection to one CalDAV server.
type Client struct {
	conn *dav.Conn
}

// Calendar is one calendar collection on the server.
type Calendar struct {
	Path        string
	Name        string
	Description string
	// Color is the server's color for it as #rrggbb, or empty.
	Color string
	// CTag and SyncToken change whenever anything in the calendar does. CTag
	// is the older calendarserver extension, SyncToken RFC 6578's.
	CTag      string
	SyncToken string
	// ReadOnly is set when the user's privileges on it do not include writing,
	// which is what a shared or subscribed calendar usually looks like.
	ReadOnly bool
	// Events and Tasks say whether it takes VEVENTs and VTODOs, from
	// supported-calendar-component-set.
	Events bool
	Tasks  bool
}

// Components lists what the calendar takes as the comma separated names the
// store keeps.
func (c Calendar) Components() string {
	var parts []string
	if c.Events {
		parts = append(parts, "VEVENT")
	}
	if c.Tasks {
		parts = append(parts, "VTODO")
	}
	return strings.Join(parts, ",")
}

// Resource is one calendar object resource as the server holds it.
type Resource struct {
	Path string
	ETag string
	Data []byte
}

// Changes is one sync round.
type Changes struct {
	// Token is the sync token to hand back next time.
	Token   string
	Updated []Resource
	// Deleted are server paths that are gone.
	Deleted []string
}

// Connect builds a client for the configured server. It makes no request:
// Calendars is what proves the url and the login are right.
func Connect(cfg Config) (*Client, error) {
	if cfg.HTTP == nil {
		return nil, errors.New("caldav: no http client")
	}
	endpoint, err := dav.Endpoint(cfg.URL)
	if err != nil {
		return nil, err
	}
	return &Client{conn: &dav.Conn{HTTP: cfg.HTTP, Base: endpoint, User: cfg.Username, Pass: cfg.Password}}, nil
}

// Discover resolves the CalDAV endpoint for an email address's domain through
// the .well-known/caldav redirect. An error means the domain advertises
// nothing; the caller falls back to asking for a url.
func Discover(ctx context.Context, client *http.Client, address string) (string, error) {
	return dav.DiscoverWellKnown(ctx, client, address, "caldav")
}

const propfindPrincipal = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:current-user-principal/><c:calendar-home-set/><d:resourcetype/></d:prop>
</d:propfind>`

const propfindCalendars = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:cs="http://calendarserver.org/ns/" xmlns:a="http://apple.com/ns/ical/">
  <d:prop>
    <d:resourcetype/>
    <d:displayname/>
    <c:calendar-description/>
    <a:calendar-color/>
    <cs:getctag/>
    <d:sync-token/>
    <c:supported-calendar-component-set/>
    <d:current-user-privilege-set/>
  </d:prop>
</d:propfind>`

// Calendars lists the event calendars the user has. It follows
// current-user-principal to calendar-home-set and lists the home; a url that
// already points at a calendar is reported as that one calendar.
func (c *Client) Calendars(ctx context.Context) ([]Calendar, error) {
	base := c.conn.Base.Path
	if base == "" {
		base = "/"
	}
	home, isCalendar, err := c.findHome(ctx, base)
	if err != nil {
		return nil, err
	}
	if isCalendar {
		cals, err := c.listCalendars(ctx, base, "0")
		if err != nil {
			return nil, err
		}
		return requireCalendars(cals)
	}
	cals, err := c.listCalendars(ctx, home, "1")
	if err != nil {
		return nil, err
	}
	return requireCalendars(cals)
}

func requireCalendars(cals []Calendar) ([]Calendar, error) {
	if len(cals) == 0 {
		return nil, ErrNoCalendars
	}
	return cals, nil
}

// findHome resolves the calendar home set starting from p. isCalendar reports
// that p is a calendar collection itself.
func (c *Client) findHome(ctx context.Context, p string) (string, bool, error) {
	ms, err := c.conn.Do(ctx, dav.MethodPropfind, p, "0", []byte(propfindPrincipal))
	if err != nil {
		return "", false, fmt.Errorf("caldav: find principal: %w", err)
	}
	if len(ms.Responses) == 0 {
		return "", false, ErrNoCalendars
	}
	r := ms.Responses[0]
	if isCalendarType(r) {
		return p, true, nil
	}
	if homes := r.Prop(nsCalDAV, "calendar-home-set").Hrefs(); len(homes) > 0 {
		return homes[0], false, nil
	}
	principals := r.Prop(nsDAV, "current-user-principal").Hrefs()
	if len(principals) == 0 || principals[0] == p {
		// a server that reports no principal is often one whose root is the
		// home, as a plain Radicale collection root is.
		return p, false, nil
	}
	ms, err = c.conn.Do(ctx, dav.MethodPropfind, principals[0], "0", []byte(propfindPrincipal))
	if err != nil {
		return "", false, fmt.Errorf("caldav: find calendar home: %w", err)
	}
	if len(ms.Responses) > 0 {
		if homes := ms.Responses[0].Prop(nsCalDAV, "calendar-home-set").Hrefs(); len(homes) > 0 {
			return homes[0], false, nil
		}
	}
	return principals[0], false, nil
}

func (c *Client) listCalendars(ctx context.Context, home, depth string) ([]Calendar, error) {
	ms, err := c.conn.Do(ctx, dav.MethodPropfind, home, depth, []byte(propfindCalendars))
	if err != nil {
		return nil, fmt.Errorf("caldav: list calendars: %w", err)
	}
	var out []Calendar
	for _, r := range ms.Responses {
		if !isCalendarType(r) {
			continue
		}
		events, tasks := supportedComponents(r)
		if !events && !tasks {
			continue
		}
		out = append(out, Calendar{
			Path:        r.Href,
			Name:        r.Text(nsDAV, "displayname"),
			Description: r.Text(nsCalDAV, "calendar-description"),
			Color:       NormalizeColor(r.Text(nsApple, "calendar-color")),
			CTag:        r.Text(nsCS, "getctag"),
			SyncToken:   r.Text(nsDAV, "sync-token"),
			ReadOnly:    readOnly(r),
			Events:      events,
			Tasks:       tasks,
		})
	}
	return out, nil
}

func isCalendarType(r dav.Response) bool {
	return len(r.Prop(nsDAV, "resourcetype").Find(nsCalDAV, "calendar")) > 0
}

// supportedComponents reads supported-calendar-component-set for events and
// tasks. A calendar that does not say supports everything, as RFC 4791 has
// it.
func supportedComponents(r dav.Response) (events, tasks bool) {
	if !r.Has(nsCalDAV, "supported-calendar-component-set") {
		return true, true
	}
	for _, comp := range r.Prop(nsCalDAV, "supported-calendar-component-set").Find(nsCalDAV, "comp") {
		switch strings.ToUpper(comp.Attr("name")) {
		case "VEVENT":
			events = true
		case "VTODO":
			tasks = true
		}
	}
	return events, tasks
}

// readOnly reads current-user-privilege-set. A server that does not report it
// is assumed to allow writes; a refused write says otherwise soon enough.
func readOnly(r dav.Response) bool {
	if !r.Has(nsDAV, "current-user-privilege-set") {
		return false
	}
	set := r.Prop(nsDAV, "current-user-privilege-set")
	for _, name := range []string{"write", "write-content", "all", "bind"} {
		if len(set.Find(nsDAV, name)) > 0 {
			return false
		}
	}
	return true
}

// NormalizeColor turns the #rrggbbaa Apple writes into #rrggbb, and drops a
// value that is not a hex color at all.
func NormalizeColor(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "#") {
		return ""
	}
	if len(v) == 9 {
		v = v[:7]
	}
	if len(v) != 7 {
		return ""
	}
	for _, ch := range v[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
			return ""
		}
	}
	return strings.ToLower(v)
}

const propfindState = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:prop><cs:getctag/><d:sync-token/></d:prop>
</d:propfind>`

// State returns a calendar's ctag and sync token, the cheap check for whether
// anything changed since the last sync.
func (c *Client) State(ctx context.Context, calendarPath string) (ctag, token string, err error) {
	ms, err := c.conn.Do(ctx, dav.MethodPropfind, calendarPath, "0", []byte(propfindState))
	if err != nil {
		return "", "", fmt.Errorf("caldav: calendar state: %w", err)
	}
	if len(ms.Responses) == 0 {
		return "", "", ErrNotFound
	}
	return ms.Responses[0].Text(nsCS, "getctag"), ms.Responses[0].Text(nsDAV, "sync-token"), nil
}

// Sync returns what changed in a calendar since token (RFC 6578). An empty
// token asks for everything. ErrSyncTokenInvalid means start over with List.
func (c *Client) Sync(ctx context.Context, calendarPath, token string) (Changes, error) {
	body := `<?xml version="1.0" encoding="utf-8"?>
<d:sync-collection xmlns:d="DAV:">
  <d:sync-token>` + xmlEscape(token) + `</d:sync-token>
  <d:sync-level>1</d:sync-level>
  <d:prop><d:getetag/></d:prop>
</d:sync-collection>`
	ms, err := c.conn.Do(ctx, dav.MethodReport, calendarPath, "", []byte(body))
	if err != nil {
		var status *dav.StatusError
		if errors.As(err, &status) && token != "" &&
			(status.Code == http.StatusForbidden || status.Code == http.StatusConflict || status.Code == http.StatusPreconditionFailed) {
			return Changes{}, ErrSyncTokenInvalid
		}
		return Changes{}, fmt.Errorf("caldav: sync %s: %w", calendarPath, err)
	}
	out := Changes{Token: ms.SyncToken}
	var changed []string
	for _, r := range ms.Responses {
		if samePath(r.Href, calendarPath) {
			continue
		}
		if r.Status == http.StatusNotFound {
			out.Deleted = append(out.Deleted, r.Href)
			continue
		}
		changed = append(changed, r.Href)
	}
	out.Updated, err = c.Multiget(ctx, calendarPath, changed)
	return out, err
}

const queryETags = `<?xml version="1.0" encoding="utf-8"?>
<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:getetag/></d:prop>
  <c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="%s"/></c:comp-filter></c:filter>
</c:calendar-query>`

// List returns the etag of every resource in a calendar holding the given
// component (VEVENT or VTODO), by path, for a server without sync-collection:
// the caller compares it with what it has and fetches only what differs.
func (c *Client) List(ctx context.Context, calendarPath, component string) (map[string]string, error) {
	if component != "VTODO" {
		component = "VEVENT"
	}
	ms, err := c.conn.Do(ctx, dav.MethodReport, calendarPath, "1", []byte(fmt.Sprintf(queryETags, component)))
	if err != nil {
		return nil, fmt.Errorf("caldav: list %s: %w", calendarPath, err)
	}
	out := map[string]string{}
	for _, r := range ms.Responses {
		if samePath(r.Href, calendarPath) || (r.Status != 0 && r.Status >= 300) {
			continue
		}
		out[r.Href] = r.Text(nsDAV, "getetag")
	}
	return out, nil
}

// multigetBatch bounds the hrefs one multiget names. Servers cap the request
// size, and a first sync of a large calendar names thousands.
const multigetBatch = 100

// Multiget fetches the named resources of a calendar in batches.
func (c *Client) Multiget(ctx context.Context, calendarPath string, hrefs []string) ([]Resource, error) {
	var out []Resource
	for len(hrefs) > 0 {
		batch := hrefs[:min(len(hrefs), multigetBatch)]
		hrefs = hrefs[len(batch):]
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="utf-8"?>
<c:calendar-multiget xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:getetag/><c:calendar-data/></d:prop>`)
		for _, h := range batch {
			b.WriteString("\n  <d:href>" + xmlEscape(escapePath(h)) + "</d:href>")
		}
		b.WriteString("\n</c:calendar-multiget>")
		ms, err := c.conn.Do(ctx, dav.MethodReport, calendarPath, "1", []byte(b.String()))
		if err != nil {
			return nil, fmt.Errorf("caldav: fetch events: %w", err)
		}
		for _, r := range ms.Responses {
			data := r.Text(nsCalDAV, "calendar-data")
			if data == "" || samePath(r.Href, calendarPath) {
				continue
			}
			out = append(out, Resource{Path: r.Href, ETag: r.Text(nsDAV, "getetag"), Data: []byte(data)})
		}
	}
	return out, nil
}

// Get fetches one resource.
func (c *Client) Get(ctx context.Context, p string) (Resource, error) {
	data, etag, err := c.conn.Get(ctx, p)
	if errors.Is(err, dav.ErrNotFound) {
		return Resource{}, ErrNotFound
	}
	if err != nil {
		return Resource{}, err
	}
	return Resource{Path: p, ETag: etag, Data: data}, nil
}

// Put writes a resource under the precondition, reporting ErrConflict when
// the server refuses it. The new etag is returned; empty means the server did
// not send one and the caller refetches to learn it.
func (c *Client) Put(ctx context.Context, p string, data []byte, pre dav.Precondition, etag string) (string, error) {
	newETag, err := c.conn.Put(ctx, p, ical.MIMEType+"; charset=utf-8", data, pre, etag)
	if errors.Is(err, dav.ErrPreconditionFailed) {
		return "", ErrConflict
	}
	return newETag, err
}

// Delete removes a resource, refusing while the server's copy is newer than
// ifMatch. A path the server does not have reports ErrNotFound.
func (c *Client) Delete(ctx context.Context, p, ifMatch string) error {
	err := c.conn.Delete(ctx, p, ifMatch)
	switch {
	case errors.Is(err, dav.ErrPreconditionFailed):
		return ErrConflict
	case errors.Is(err, dav.ErrNotFound):
		return ErrNotFound
	}
	return err
}

// ResourcePath is where a new event is written: its UID under the calendar,
// which is what other clients do. A UID with characters a path cannot carry
// cleanly is hashed by the caller before it gets here.
func ResourcePath(calendarPath, uid string) string {
	return path.Join(calendarPath, uid+".ics")
}

// escapePath percent-encodes a server path for an href in a request body.
func escapePath(p string) string {
	return (&url.URL{Path: p}).EscapedPath()
}

func samePath(a, b string) bool {
	return strings.TrimSuffix(a, "/") == strings.TrimSuffix(b, "/")
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
