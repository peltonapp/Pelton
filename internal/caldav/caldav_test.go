package caldav

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/peltonapp/Pelton/internal/dav"
)

// fakeServer is a small in-memory CalDAV server: one principal, one home and
// two collections, one of them a task list that must not be offered.
type fakeServer struct {
	mu        sync.Mutex
	resources map[string]fakeResource
	version   int
	// deleted records what went away after each version, for sync-collection.
	deleted map[string]int
	// rejectToken makes sync-collection refuse any token.
	rejectToken bool
	user, pass  string
	lastAuthOK  bool
}

type fakeResource struct {
	etag    string
	data    string
	version int
}

const (
	principalPath = "/dav/principals/me/"
	homePath      = "/dav/calendars/me/"
	workPath      = "/dav/calendars/me/work cal/"
	tasksPath     = "/dav/calendars/me/tasks/"
)

func newFakeServer() *fakeServer {
	return &fakeServer{resources: map[string]fakeResource{}, deleted: map[string]int{}, user: "me", pass: "secret"}
}

func (f *fakeServer) put(p, data string) string {
	f.version++
	etag := `"e` + strconv.Itoa(f.version) + `"`
	f.resources[p] = fakeResource{etag: etag, data: data, version: f.version}
	delete(f.deleted, p)
	return etag
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	user, pass, ok := r.BasicAuth()
	f.lastAuthOK = ok && user == f.user && pass == f.pass
	if !f.lastAuthOK {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	p := r.URL.Path
	switch r.Method {
	case dav.MethodPropfind:
		f.propfind(w, r, p, string(body))
	case dav.MethodReport:
		f.report(w, p, string(body))
	case http.MethodGet:
		res, ok := f.resources[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", res.etag)
		io.WriteString(w, res.data)
	case http.MethodPut:
		res, exists := f.resources[p]
		if m := r.Header.Get("If-Match"); m != "" && (!exists || m != res.etag) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if r.Header.Get("If-None-Match") == "*" && exists {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		w.Header().Set("ETag", f.put(p, string(body)))
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		res, exists := f.resources[p]
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if m := r.Header.Get("If-Match"); m != "" && m != res.etag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		f.version++
		delete(f.resources, p)
		f.deleted[p] = f.version
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func multistatus(w http.ResponseWriter, inner string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusMultiStatus)
	fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav" xmlns:cs="http://calendarserver.org/ns/" xmlns:a="http://apple.com/ns/ical/">%s</d:multistatus>`, inner)
}

func href(p string) string {
	return "<d:href>" + (&url.URL{Path: p}).EscapedPath() + "</d:href>"
}

func ok(props string) string {
	return "<d:propstat><d:prop>" + props + "</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat>"
}

func (f *fakeServer) propfind(w http.ResponseWriter, r *http.Request, p, body string) {
	switch {
	case p == "/" || p == "/dav/":
		multistatus(w, "<d:response>"+href(p)+ok("<d:current-user-principal>"+href(principalPath)+"</d:current-user-principal><d:resourcetype><d:collection/></d:resourcetype>")+"</d:response>")
	case p == principalPath:
		multistatus(w, "<d:response>"+href(p)+ok("<c:calendar-home-set>"+href(homePath)+"</c:calendar-home-set>")+"</d:response>")
	case p == homePath && r.Header.Get("Depth") == "1":
		multistatus(w,
			"<d:response>"+href(homePath)+ok("<d:resourcetype><d:collection/></d:resourcetype>")+"</d:response>"+
				"<d:response>"+href(workPath)+ok(`<d:resourcetype><d:collection/><c:calendar/></d:resourcetype>
<d:displayname>Work</d:displayname><a:calendar-color>#FF8800FF</a:calendar-color>
<cs:getctag>`+strconv.Itoa(f.version)+`</cs:getctag><d:sync-token>tok-`+strconv.Itoa(f.version)+`</d:sync-token>
<c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set>
<d:current-user-privilege-set><d:privilege><d:read/></d:privilege><d:privilege><d:write/></d:privilege></d:current-user-privilege-set>`)+"</d:response>"+
				"<d:response>"+href(tasksPath)+ok(`<d:resourcetype><d:collection/><c:calendar/></d:resourcetype>
<d:displayname>Tasks</d:displayname><c:supported-calendar-component-set><c:comp name="VTODO"/></c:supported-calendar-component-set>`)+"</d:response>")
	case p == workPath:
		multistatus(w, "<d:response>"+href(p)+ok("<cs:getctag>"+strconv.Itoa(f.version)+"</cs:getctag><d:sync-token>tok-"+strconv.Itoa(f.version)+"</d:sync-token><d:resourcetype><d:collection/><c:calendar/></d:resourcetype>")+"</d:response>")
	default:
		w.WriteHeader(http.StatusNotFound)
	}
	_ = body
}

var hrefPattern = regexp.MustCompile(`<d:href>([^<]+)</d:href>`)
var tokenPattern = regexp.MustCompile(`<d:sync-token>([^<]*)</d:sync-token>`)

func (f *fakeServer) report(w http.ResponseWriter, p, body string) {
	var out strings.Builder
	switch {
	case strings.Contains(body, "sync-collection"):
		token := tokenPattern.FindStringSubmatch(body)[1]
		since := 0
		if token != "" {
			if f.rejectToken {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			since, _ = strconv.Atoi(strings.TrimPrefix(token, "tok-"))
		}
		for _, rp := range f.sortedPaths(p) {
			if res := f.resources[rp]; res.version > since {
				out.WriteString("<d:response>" + href(rp) + ok("<d:getetag>"+res.etag+"</d:getetag>") + "</d:response>")
			}
		}
		for rp, v := range f.deleted {
			if v > since && strings.HasPrefix(rp, p) && token != "" {
				out.WriteString("<d:response>" + href(rp) + "<d:status>HTTP/1.1 404 Not Found</d:status></d:response>")
			}
		}
		out.WriteString("<d:sync-token>tok-" + strconv.Itoa(f.version) + "</d:sync-token>")
	case strings.Contains(body, "calendar-query"):
		for _, rp := range f.sortedPaths(p) {
			out.WriteString("<d:response>" + href(rp) + ok("<d:getetag>"+f.resources[rp].etag+"</d:getetag>") + "</d:response>")
		}
	case strings.Contains(body, "calendar-multiget"):
		for _, m := range hrefPattern.FindAllStringSubmatch(body, -1) {
			rp, _ := url.PathUnescape(m[1])
			res, ok2 := f.resources[rp]
			if !ok2 {
				out.WriteString("<d:response>" + href(rp) + "<d:status>HTTP/1.1 404 Not Found</d:status></d:response>")
				continue
			}
			out.WriteString("<d:response>" + href(rp) + ok("<d:getetag>"+res.etag+"</d:getetag><c:calendar-data><![CDATA["+res.data+"]]></c:calendar-data>") + "</d:response>")
		}
	}
	multistatus(w, out.String())
}

func (f *fakeServer) sortedPaths(prefix string) []string {
	var out []string
	for rp := range f.resources {
		if strings.HasPrefix(rp, prefix) {
			out = append(out, rp)
		}
	}
	sort.Strings(out)
	return out
}

func event(uid, summary string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:test\r\nBEGIN:VEVENT\r\nUID:" + uid +
		"\r\nDTSTART:20260301T090000Z\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func setup(t *testing.T) (*fakeServer, *Client) {
	t.Helper()
	f := newFakeServer()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	client, err := Connect(Config{URL: srv.URL + "/dav/", Username: "me", Password: "secret", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return f, client
}

func TestCalendarsFollowsPrincipalAndHome(t *testing.T) {
	_, client := setup(t)
	cals, err := client.Calendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 1 {
		t.Fatalf("got %d calendars, want only the event one: %+v", len(cals), cals)
	}
	c := cals[0]
	if c.Path != workPath || c.Name != "Work" || c.Color != "#ff8800" || c.ReadOnly || c.SyncToken == "" {
		t.Errorf("calendar = %+v", c)
	}
}

func TestSyncThenIncremental(t *testing.T) {
	f, client := setup(t)
	ctx := context.Background()
	f.put(workPath+"a.ics", event("a", "First"))
	f.put(workPath+"b b.ics", event("b", "Second"))

	first, err := client.Sync(ctx, workPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Updated) != 2 || len(first.Deleted) != 0 || first.Token == "" {
		t.Fatalf("first sync = %+v", first)
	}
	if !strings.Contains(string(first.Updated[1].Data), "SUMMARY:Second") || first.Updated[1].Path != workPath+"b b.ics" {
		t.Errorf("resource = %+v", first.Updated[1])
	}

	f.mu.Lock()
	f.put(workPath+"a.ics", event("a", "First, edited"))
	f.version++
	delete(f.resources, workPath+"b b.ics")
	f.deleted[workPath+"b b.ics"] = f.version
	f.mu.Unlock()

	next, err := client.Sync(ctx, workPath, first.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Updated) != 1 || !strings.Contains(string(next.Updated[0].Data), "First, edited") {
		t.Errorf("updated = %+v", next.Updated)
	}
	if len(next.Deleted) != 1 || next.Deleted[0] != workPath+"b b.ics" {
		t.Errorf("deleted = %v", next.Deleted)
	}
}

func TestSyncReportsInvalidToken(t *testing.T) {
	f, client := setup(t)
	f.rejectToken = true
	if _, err := client.Sync(context.Background(), workPath, "tok-stale"); !errors.Is(err, ErrSyncTokenInvalid) {
		t.Errorf("err = %v, want ErrSyncTokenInvalid", err)
	}
}

func TestListAndMultiget(t *testing.T) {
	f, client := setup(t)
	ctx := context.Background()
	f.put(workPath+"a.ics", event("a", "A"))
	etags, err := client.List(ctx, workPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(etags) != 1 || etags[workPath+"a.ics"] == "" {
		t.Fatalf("etags = %v", etags)
	}
	res, err := client.Multiget(ctx, workPath, []string{workPath + "a.ics", workPath + "missing.ics"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Errorf("multiget = %+v", res)
	}
}

func TestWritesAreConditional(t *testing.T) {
	f, client := setup(t)
	ctx := context.Background()
	p := ResourcePath(workPath, "new")

	etag, err := client.Put(ctx, p, []byte(event("new", "Created")), dav.PreconditionNew, "")
	if err != nil || etag == "" {
		t.Fatalf("create = %q, %v", etag, err)
	}
	if _, err := client.Put(ctx, p, []byte(event("new", "Again")), dav.PreconditionNew, ""); !errors.Is(err, ErrConflict) {
		t.Errorf("create over existing = %v", err)
	}

	f.mu.Lock()
	f.put(p, event("new", "Changed on the phone"))
	f.mu.Unlock()
	if _, err := client.Put(ctx, p, []byte(event("new", "Mine")), dav.PreconditionMatch, etag); !errors.Is(err, ErrConflict) {
		t.Errorf("stale update = %v, want ErrConflict", err)
	}
	if err := client.Delete(ctx, p, etag); !errors.Is(err, ErrConflict) {
		t.Errorf("stale delete = %v, want ErrConflict", err)
	}
	server, err := client.Get(ctx, p)
	if err != nil || !strings.Contains(string(server.Data), "Changed on the phone") {
		t.Fatalf("get = %+v, %v", server, err)
	}
	if err := client.Delete(ctx, p, server.ETag); err != nil {
		t.Errorf("delete = %v", err)
	}
	if err := client.Delete(ctx, p, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestWrongPasswordIsAnError(t *testing.T) {
	f := newFakeServer()
	srv := httptest.NewServer(f)
	defer srv.Close()
	client, _ := Connect(Config{URL: srv.URL, Username: "me", Password: "wrong", HTTP: srv.Client()})
	if _, err := client.Calendars(context.Background()); err == nil {
		t.Error("a refused login listed calendars")
	}
}

func TestNormalizeColor(t *testing.T) {
	for in, want := range map[string]string{"#FF8800FF": "#ff8800", "#123abc": "#123abc", "red": "", "#12": "", "": ""} {
		if got := NormalizeColor(in); got != want {
			t.Errorf("NormalizeColor(%q) = %q, want %q", in, got, want)
		}
	}
}
