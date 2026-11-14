package dav

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEndpointDefaultsToHTTPS(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "cloud.example.com", want: "https://cloud.example.com"},
		{raw: "https://cloud.example.com/remote.php/dav", want: "https://cloud.example.com/remote.php/dav"},
		{raw: "http://localhost:5232", want: "http://localhost:5232"},
		{raw: "   ", wantErr: true},
		{raw: "https://", wantErr: true},
	}
	for _, tt := range tests {
		got, err := Endpoint(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Errorf("Endpoint(%q) = %v, want an error", tt.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Endpoint(%q): %v", tt.raw, err)
			continue
		}
		if got.String() != tt.want {
			t.Errorf("Endpoint(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

const sampleMultistatus = `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/">
  <d:response>
    <d:href>/cal/home/work%20stuff/</d:href>
    <d:propstat>
      <d:prop>
        <d:displayname>Work</d:displayname>
        <d:resourcetype><d:collection/><c:calendar xmlns:c="urn:ietf:params:xml:ns:caldav"/></d:resourcetype>
        <cs:getctag>42</cs:getctag>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
    <d:propstat>
      <d:prop><d:getetag/></d:prop>
      <d:status>HTTP/1.1 404 Not Found</d:status>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>https://cloud.example.com/cal/home/gone.ics</d:href>
    <d:status>HTTP/1.1 404 Not Found</d:status>
  </d:response>
  <d:sync-token>http://example.com/sync/7</d:sync-token>
</d:multistatus>`

func TestParseMultistatus(t *testing.T) {
	ms, err := ParseMultistatus(strings.NewReader(sampleMultistatus))
	if err != nil {
		t.Fatal(err)
	}
	if ms.SyncToken != "http://example.com/sync/7" {
		t.Errorf("sync token = %q", ms.SyncToken)
	}
	if len(ms.Responses) != 2 {
		t.Fatalf("responses = %d", len(ms.Responses))
	}
	first := ms.Responses[0]
	if first.Href != "/cal/home/work stuff/" {
		t.Errorf("href = %q", first.Href)
	}
	if first.Text("DAV:", "displayname") != "Work" {
		t.Errorf("displayname = %q", first.Text("DAV:", "displayname"))
	}
	if first.Text("http://calendarserver.org/ns/", "getctag") != "42" {
		t.Error("ctag missing")
	}
	if first.Has("DAV:", "getetag") {
		t.Error("a 404 propstat was reported as present")
	}
	types := Elements(first.Inner("DAV:", "resourcetype"))
	if len(types) != 2 || types[1].Local != "calendar" {
		t.Errorf("resourcetype = %v", types)
	}
	second := ms.Responses[1]
	if second.Href != "/cal/home/gone.ics" || second.Status != 404 {
		t.Errorf("second = %+v", second)
	}
}

func TestHrefs(t *testing.T) {
	got := Hrefs([]byte(`<d:href xmlns:d="DAV:">/principals/me/</d:href>`))
	if len(got) != 1 || got[0] != "/principals/me/" {
		t.Errorf("hrefs = %v", got)
	}
}

func TestConditionalWrites(t *testing.T) {
	etag := `"v1"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m := r.Header.Get("If-Match"); m != "" && m != etag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if r.Header.Get("If-None-Match") == "*" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", `"v2"`)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	base, _ := Endpoint(srv.URL)
	conn := &Conn{HTTP: srv.Client(), Base: base}
	ctx := context.Background()

	if got, err := conn.Put(ctx, "/a", "text/plain", []byte("x"), PreconditionMatch, `"v1"`); err != nil || got != `"v2"` {
		t.Errorf("put = %q, %v", got, err)
	}
	if _, err := conn.Put(ctx, "/a", "text/plain", []byte("x"), PreconditionMatch, `"old"`); !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("stale put = %v", err)
	}
	if _, err := conn.Put(ctx, "/a", "text/plain", []byte("x"), PreconditionNew, ""); !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("create over existing = %v", err)
	}
	if err := conn.Delete(ctx, "/missing", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing = %v", err)
	}
}
