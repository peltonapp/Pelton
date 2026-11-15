// Package caldavtest is an in-memory CalDAV server for tests: one principal,
// one calendar home with an event calendar and a task list, basic auth,
// sync-collection, calendar-query, multiget and conditional writes.
package caldavtest

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The paths the server answers on.
const (
	PrincipalPath = "/dav/principals/me/"
	HomePath      = "/dav/calendars/me/"
	WorkPath      = "/dav/calendars/me/work cal/"
	TasksPath     = "/dav/calendars/me/tasks/"
)

// Server is the fake server. Its fields may be changed between requests.
type Server struct {
	mu        sync.Mutex
	resources map[string]resource
	version   int
	deleted   map[string]int
	// RejectToken makes sync-collection refuse every token.
	RejectToken bool
	// NoSync makes the server refuse sync-collection outright, as one without
	// RFC 6578 does, so clients fall back to etags.
	NoSync bool
	// Offline makes every request fail at the transport level from the
	// client's point of view: the handler aborts the connection.
	Offline    bool
	User, Pass string
	// Puts counts successful writes.
	Puts int
}

type resource struct {
	etag    string
	data    string
	version int
}

// NewServer returns an empty server accepting the given login.
func NewServer(user, pass string) *Server {
	return &Server{resources: map[string]resource{}, deleted: map[string]int{}, User: user, Pass: pass}
}

// Put stores a resource as if another client wrote it, returning its etag.
func (s *Server) Put(p, data string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.put(p, data)
}

// Remove deletes a resource as if another client did.
func (s *Server) Remove(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version++
	delete(s.resources, p)
	s.deleted[p] = s.version
}

// Data returns a resource's body and whether it exists.
func (s *Server) Data(p string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.resources[p]
	return r.data, ok
}

// Paths lists the stored resource paths, sorted.
func (s *Server) Paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sortedPaths("/")
}

func (s *Server) put(p, data string) string {
	s.version++
	etag := `"e` + strconv.Itoa(s.version) + `"`
	s.resources[p] = resource{etag: etag, data: data, version: s.version}
	delete(s.deleted, p)
	return etag
}

// ServeHTTP answers one request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Offline {
		panic(http.ErrAbortHandler)
	}
	user, pass, ok := r.BasicAuth()
	if !ok || user != s.User || pass != s.Pass {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	p := r.URL.Path
	switch r.Method {
	case "PROPFIND":
		s.propfind(w, r, p)
	case "REPORT":
		s.report(w, p, string(body))
	case http.MethodGet:
		res, ok := s.resources[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", res.etag)
		_, _ = io.WriteString(w, res.data)
	case http.MethodPut:
		res, exists := s.resources[p]
		if m := r.Header.Get("If-Match"); m != "" && (!exists || m != res.etag) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if r.Header.Get("If-None-Match") == "*" && exists {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		s.Puts++
		w.Header().Set("ETag", s.put(p, string(body)))
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		res, exists := s.resources[p]
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if m := r.Header.Get("If-Match"); m != "" && m != res.etag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		s.version++
		delete(s.resources, p)
		s.deleted[p] = s.version
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

func (s *Server) syncToken() string {
	if s.NoSync {
		return ""
	}
	return "<d:sync-token>tok-" + strconv.Itoa(s.version) + "</d:sync-token>"
}

func (s *Server) propfind(w http.ResponseWriter, r *http.Request, p string) {
	switch {
	case p == "/" || p == "/dav/":
		multistatus(w, "<d:response>"+href(p)+ok("<d:current-user-principal>"+href(PrincipalPath)+"</d:current-user-principal><d:resourcetype><d:collection/></d:resourcetype>")+"</d:response>")
	case p == PrincipalPath:
		multistatus(w, "<d:response>"+href(p)+ok("<c:calendar-home-set>"+href(HomePath)+"</c:calendar-home-set>")+"</d:response>")
	case p == HomePath && r.Header.Get("Depth") == "1":
		multistatus(w,
			"<d:response>"+href(HomePath)+ok("<d:resourcetype><d:collection/></d:resourcetype>")+"</d:response>"+
				"<d:response>"+href(WorkPath)+ok(`<d:resourcetype><d:collection/><c:calendar/></d:resourcetype>
<d:displayname>Work</d:displayname><a:calendar-color>#FF8800FF</a:calendar-color>
<cs:getctag>`+strconv.Itoa(s.version)+`</cs:getctag>`+s.syncToken()+`
<c:supported-calendar-component-set><c:comp name="VEVENT"/></c:supported-calendar-component-set>
<d:current-user-privilege-set><d:privilege><d:read/></d:privilege><d:privilege><d:write/></d:privilege></d:current-user-privilege-set>`)+"</d:response>"+
				"<d:response>"+href(TasksPath)+ok(`<d:resourcetype><d:collection/><c:calendar/></d:resourcetype>
<d:displayname>Tasks</d:displayname><c:supported-calendar-component-set><c:comp name="VTODO"/></c:supported-calendar-component-set>`)+"</d:response>")
	case p == WorkPath:
		multistatus(w, "<d:response>"+href(p)+ok("<cs:getctag>"+strconv.Itoa(s.version)+"</cs:getctag>"+s.syncToken()+"<d:resourcetype><d:collection/><c:calendar/></d:resourcetype>")+"</d:response>")
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

var (
	hrefPattern  = regexp.MustCompile(`<d:href>([^<]+)</d:href>`)
	tokenPattern = regexp.MustCompile(`<d:sync-token>([^<]*)</d:sync-token>`)
)

func (s *Server) report(w http.ResponseWriter, p, body string) {
	var out strings.Builder
	switch {
	case strings.Contains(body, "sync-collection"):
		if s.NoSync {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		token := tokenPattern.FindStringSubmatch(body)[1]
		since := 0
		if token != "" {
			if s.RejectToken {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			since, _ = strconv.Atoi(strings.TrimPrefix(token, "tok-"))
		}
		for _, rp := range s.sortedPaths(p) {
			if res := s.resources[rp]; res.version > since {
				out.WriteString("<d:response>" + href(rp) + ok("<d:getetag>"+res.etag+"</d:getetag>") + "</d:response>")
			}
		}
		for rp, v := range s.deleted {
			if v > since && strings.HasPrefix(rp, p) && token != "" {
				out.WriteString("<d:response>" + href(rp) + "<d:status>HTTP/1.1 404 Not Found</d:status></d:response>")
			}
		}
		out.WriteString("<d:sync-token>tok-" + strconv.Itoa(s.version) + "</d:sync-token>")
	case strings.Contains(body, "calendar-query"):
		comp := "VEVENT"
		if strings.Contains(body, `name="VTODO"`) {
			comp = "VTODO"
		}
		for _, rp := range s.sortedPaths(p) {
			if !strings.Contains(s.resources[rp].data, "BEGIN:"+comp) {
				continue
			}
			out.WriteString("<d:response>" + href(rp) + ok("<d:getetag>"+s.resources[rp].etag+"</d:getetag>") + "</d:response>")
		}
	case strings.Contains(body, "calendar-multiget"):
		for _, m := range hrefPattern.FindAllStringSubmatch(body, -1) {
			rp, _ := url.PathUnescape(m[1])
			res, found := s.resources[rp]
			if !found {
				out.WriteString("<d:response>" + href(rp) + "<d:status>HTTP/1.1 404 Not Found</d:status></d:response>")
				continue
			}
			out.WriteString("<d:response>" + href(rp) + ok("<d:getetag>"+res.etag+"</d:getetag><c:calendar-data><![CDATA["+res.data+"]]></c:calendar-data>") + "</d:response>")
		}
	}
	multistatus(w, out.String())
}

func (s *Server) sortedPaths(prefix string) []string {
	var out []string
	for rp := range s.resources {
		if strings.HasPrefix(rp, prefix) {
			out = append(out, rp)
		}
	}
	sort.Strings(out)
	return out
}
