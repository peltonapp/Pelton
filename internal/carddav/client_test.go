package carddav

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/emersion/go-vcard"
)

// fakeServer answers the handful of requests the write paths make. Discovery
// and the sync report come from go-webdav and are exercised against a real
// server rather than a hand-rolled xml stub; what is tested here is the part
// this package implements itself, which is the conditional write.
type fakeServer struct {
	etag string
	// gone makes the resource answer 404, for the delete case.
	gone bool
	// lastPut and lastHeaders record what the client actually sent.
	lastPut     string
	lastHeaders http.Header
	lastMethod  string
}

func (f *fakeServer) start(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}
		f.lastPut = string(body)
		f.lastHeaders = r.Header.Clone()
		f.lastMethod = r.Method

		if f.gone {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if match := r.Header.Get("If-Match"); match != "" && match != f.etag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		if r.Header.Get("If-None-Match") == "*" && f.etag != "" {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		w.Header().Set("ETag", `"written-1"`)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	client, err := clientFor(srv.URL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	return client
}

// clientFor builds a Client without the HasSupport handshake, which the fake
// server above does not answer.
func clientFor(endpoint string) (*Client, error) {
	parsed, err := endpointURL(endpoint)
	if err != nil {
		return nil, err
	}
	return &Client{http: http.DefaultClient, base: parsed, user: "me", pass: "secret"}, nil
}

func testContact() vcard.Card {
	return Contact{UID: "u1", FullName: "Arne Kock", Emails: []Labelled{{Value: "arne@example.com"}}}.ToCard()
}

func TestPutCreatesWithIfNoneMatch(t *testing.T) {
	server := &fakeServer{}
	client := server.start(t)

	etag, err := client.Put(context.Background(), "/books/default/u1.vcf", testContact(), PreconditionNew, "")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if etag != `"written-1"` {
		t.Errorf("etag = %q", etag)
	}
	if got := server.lastHeaders.Get("If-None-Match"); got != "*" {
		t.Errorf("If-None-Match = %q, want *", got)
	}
	if server.lastHeaders.Get("If-Match") != "" {
		t.Error("a create sent If-Match")
	}
	if !strings.Contains(server.lastPut, "FN:Arne Kock") {
		t.Errorf("body did not carry the card:\n%s", server.lastPut)
	}
	if _, _, ok := requestAuth(server.lastHeaders); !ok {
		t.Error("the request was not authenticated")
	}
}

func TestPutUpdatesWithIfMatch(t *testing.T) {
	server := &fakeServer{etag: `"server-2"`}
	client := server.start(t)

	if _, err := client.Put(context.Background(), "/books/default/u1.vcf", testContact(), PreconditionMatch, `"server-2"`); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := server.lastHeaders.Get("If-Match"); got != `"server-2"` {
		t.Errorf("If-Match = %q", got)
	}
}

// The conflict is the whole reason this package sends If-Match: someone else
// changed the contact, and the caller has to be told rather than clobber it.
func TestPutReportsConflict(t *testing.T) {
	server := &fakeServer{etag: `"server-3"`}
	client := server.start(t)

	_, err := client.Put(context.Background(), "/books/default/u1.vcf", testContact(), PreconditionMatch, `"stale-1"`)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("put with a stale etag = %v, want ErrConflict", err)
	}
}

// Once the user has seen both versions and chosen theirs, the write goes
// through whatever the server holds. Nothing else is allowed to do this.
func TestPutWithoutPreconditionOverwrites(t *testing.T) {
	server := &fakeServer{etag: `"server-6"`}
	client := server.start(t)

	if _, err := client.Put(context.Background(), "/books/default/u1.vcf", testContact(), PreconditionNone, ""); err != nil {
		t.Fatalf("forced put: %v", err)
	}
	if got := server.lastHeaders.Get("If-Match"); got != "" {
		t.Errorf("forced put sent If-Match %q", got)
	}
	if got := server.lastHeaders.Get("If-None-Match"); got != "" {
		t.Errorf("forced put sent If-None-Match %q", got)
	}
}

// creating where something already exists is the same collision seen from the
// other side.
func TestPutReportsConflictOnCreateOverExisting(t *testing.T) {
	server := &fakeServer{etag: `"server-4"`}
	client := server.start(t)

	_, err := client.Put(context.Background(), "/books/default/u1.vcf", testContact(), PreconditionNew, "")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("create over an existing card = %v, want ErrConflict", err)
	}
}

func TestDeleteSendsIfMatchAndReportsConflict(t *testing.T) {
	server := &fakeServer{etag: `"server-5"`}
	client := server.start(t)

	if err := client.Delete(context.Background(), "/books/default/u1.vcf", `"server-5"`); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if server.lastMethod != http.MethodDelete {
		t.Errorf("method = %s", server.lastMethod)
	}

	if err := client.Delete(context.Background(), "/books/default/u1.vcf", `"stale"`); !errors.Is(err, ErrConflict) {
		t.Errorf("stale delete = %v, want ErrConflict", err)
	}
}

// a contact already gone is the state the caller wanted, but the caller still
// needs to know it was not this delete that did it.
func TestDeleteReportsNotFound(t *testing.T) {
	server := &fakeServer{gone: true}
	client := server.start(t)

	if err := client.Delete(context.Background(), "/books/default/u1.vcf", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete of a missing card = %v, want ErrNotFound", err)
	}
}

func TestEndpointURLDefaultsToHTTPS(t *testing.T) {
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
		got, err := endpointURL(tt.raw)
		if tt.wantErr {
			if err == nil {
				t.Errorf("endpointURL(%q) = %v, want an error", tt.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("endpointURL(%q): %v", tt.raw, err)
			continue
		}
		if got.String() != tt.want {
			t.Errorf("endpointURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// requestAuth reports the basic auth on a recorded request.
func requestAuth(headers http.Header) (string, string, bool) {
	req := &http.Request{Header: headers}
	return req.BasicAuth()
}

func TestDiscoverTakesTheDomainAfterTheLastAt(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "https://")
	want := srv.URL + "/.well-known/carddav"

	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{"address", "me@" + host, false},
		{"bare domain", host, false},
		{"several at signs", "a@b@" + host, false},
		{"leading at", "@" + host, false},
		{"surrounding space", "me@" + host + " ", false},
		{"at last", "me@", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Discover(context.Background(), srv.Client(), tt.address)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Discover(%q) = %q, want an error", tt.address, got)
				}
				return
			}
			if err != nil || got != want {
				t.Errorf("Discover(%q) = (%q, %v), want %q", tt.address, got, err, want)
			}
		})
	}
}
