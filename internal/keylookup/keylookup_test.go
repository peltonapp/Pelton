package keylookup

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
)

func TestWKDHashKnownVector(t *testing.T) {
	// the example from the Web Key Directory draft.
	if got := WKDHash("Joe.Doe"); got != "iy9q119eutrkn8s1mk4r39qejnbu3n5q" {
		t.Errorf("WKDHash = %q", got)
	}
}

func TestWKDURLs(t *testing.T) {
	adv, direct, err := WKDURLs("Joe.Doe@Example.ORG")
	if err != nil {
		t.Fatal(err)
	}
	if adv != "https://openpgpkey.example.org/.well-known/openpgpkey/example.org/hu/iy9q119eutrkn8s1mk4r39qejnbu3n5q?l=Joe.Doe" {
		t.Errorf("advanced = %s", adv)
	}
	if direct != "https://example.org/.well-known/openpgpkey/hu/iy9q119eutrkn8s1mk4r39qejnbu3n5q?l=Joe.Doe" {
		t.Errorf("direct = %s", direct)
	}
	if _, _, err := WKDURLs("not-an-address"); err == nil {
		t.Error("address without a domain accepted")
	}
	if _, _, err := WKDURLs("a@evil.example/path"); err == nil {
		t.Error("domain with a path accepted")
	}
	adv, _, err = WKDURLs("anna@bücher.example")
	if err != nil || !strings.Contains(adv, "openpgpkey.xn--bcher-kva.example") {
		t.Errorf("international domain = %s, %v", adv, err)
	}
}

// testNet is a fake network: names resolve through a table, every connection
// lands on one TLS test server, and each request is recorded.
type testNet struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	hosts   map[string]bool
	reqs    []*http.Request
	handler http.HandlerFunc
}

func newTestNet(t *testing.T, hosts ...string) *testNet {
	n := &testNet{t: t, hosts: map[string]bool{}}
	for _, h := range hosts {
		n.hosts[h] = true
	}
	n.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		n.reqs = append(n.reqs, r)
		n.mu.Unlock()
		n.handler(w, r)
	}))
	t.Cleanup(n.srv.Close)
	return n
}

func (n *testNet) client() *Client {
	tlsCfg := n.srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	return New(Config{
		Lookup: func(_ context.Context, host string) ([]netip.Addr, error) {
			if n.hosts[host] {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			}
			return nil, errors.New("no such host")
		},
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, n.srv.Listener.Addr().String())
		},
		TLS:       tlsCfg,
		AllowAddr: func(netip.Addr) bool { return true },
	})
}

func (n *testNet) requests() []*http.Request {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]*http.Request(nil), n.reqs...)
}

var keyBody = []byte("binary key bytes")

func TestWKDAdvancedMethod(t *testing.T) {
	n := newTestNet(t, "openpgpkey.example.com", "example.com")
	n.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "openpgpkey.example.com" || r.URL.Path != "/.well-known/openpgpkey/example.com/hu/"+WKDHash("anna") || r.URL.Query().Get("l") != "anna" {
			http.NotFound(w, r)
			return
		}
		w.Write(keyBody)
	}
	got, err := n.client().WKD(context.Background(), "anna@example.com")
	if err != nil || !bytes.Equal(got, keyBody) {
		t.Fatalf("WKD = %q, %v", got, err)
	}
	reqs := n.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	if ua := reqs[0].Header.Get("User-Agent"); ua != "Pelton" {
		t.Errorf("User-Agent = %q", ua)
	}
	if reqs[0].Header.Get("Cookie") != "" {
		t.Error("a cookie was sent")
	}
}

func TestWKDDirectWhenNoAdvancedHost(t *testing.T) {
	n := newTestNet(t, "example.com")
	n.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.URL.Path != "/.well-known/openpgpkey/hu/"+WKDHash("Anna") {
			http.NotFound(w, r)
			return
		}
		w.Write(keyBody)
	}
	got, err := n.client().WKD(context.Background(), "Anna@example.com")
	if err != nil || !bytes.Equal(got, keyBody) {
		t.Fatalf("WKD = %q, %v", got, err)
	}
}

func TestWKDAdvancedNotFoundIsFinal(t *testing.T) {
	n := newTestNet(t, "openpgpkey.example.com", "example.com")
	n.handler = http.NotFound
	if _, err := n.client().WKD(context.Background(), "anna@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("WKD = %v, want ErrNotFound", err)
	}
	if got := len(n.requests()); got != 1 {
		t.Errorf("%d requests, want only the advanced one", got)
	}
}

func TestRedirects(t *testing.T) {
	n := newTestNet(t, "openpgpkey.example.com", "other.example.com", "example.com")
	n.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Host == "openpgpkey.example.com" && r.URL.Path != "/moved":
			http.Redirect(w, r, "https://openpgpkey.example.com/moved", http.StatusFound)
		case r.Host == "openpgpkey.example.com":
			w.Write(keyBody)
		default:
			t.Errorf("request to %s", r.Host)
		}
	}
	if got, err := n.client().WKD(context.Background(), "anna@example.com"); err != nil || !bytes.Equal(got, keyBody) {
		t.Fatalf("same host redirect = %q, %v", got, err)
	}

	n.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "openpgpkey.example.com" {
			http.Redirect(w, r, "https://other.example.com/key", http.StatusFound)
			return
		}
		t.Errorf("followed a redirect to %s", r.Host)
	}
	if _, err := n.client().WKD(context.Background(), "anna@example.com"); !errors.Is(err, ErrRefused) {
		t.Errorf("cross host redirect = %v, want ErrRefused", err)
	}
}

func TestResponseSizeLimit(t *testing.T) {
	n := newTestNet(t, "keys.example.com")
	n.handler = func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), maxResponse+10))
	}
	if _, err := n.client().VKS(context.Background(), "keys.example.com", "a@example.org"); !errors.Is(err, ErrRefused) {
		t.Errorf("oversized answer = %v", err)
	}
}

func TestLocalAddressesAreRefusedBeforeConnecting(t *testing.T) {
	c := New(Config{
		Lookup: func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("192.168.1.2")}, nil
		},
		Dial: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("connected to a host that resolves to a private address")
			return nil, nil
		},
	})
	if _, err := c.WKD(context.Background(), "anna@example.com"); !errors.Is(err, ErrRefused) {
		t.Errorf("WKD = %v, want ErrRefused", err)
	}
}

func TestKeyServerVKSThenHKP(t *testing.T) {
	n := newTestNet(t, "keys.example.com")
	n.handler = func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vks/v1/by-email/anna@example.org":
			http.NotFound(w, r)
		case "/pks/lookup":
			q := r.URL.Query()
			if q.Get("op") != "get" || q.Get("options") != "mr" || q.Get("search") != "anna@example.org" {
				t.Errorf("hkp query %v", q)
			}
			w.Write(keyBody)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}
	got, err := n.client().KeyServer(context.Background(), "hkps://keys.example.com", "anna@example.org")
	if err != nil || !bytes.Equal(got, keyBody) {
		t.Fatalf("KeyServer = %q, %v", got, err)
	}
	if len(n.requests()) != 2 {
		t.Errorf("%d requests, want VKS then HKP", len(n.requests()))
	}

	n.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vks/v1/by-email/anna@example.org" {
			t.Errorf("asked %s after VKS answered", r.URL.Path)
		}
		w.Write(keyBody)
	}
	if got, err := n.client().KeyServer(context.Background(), "keys.example.com", "anna@example.org"); err != nil || !bytes.Equal(got, keyBody) {
		t.Errorf("VKS answer = %q, %v", got, err)
	}
}

func TestNormalizeServer(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"keys.openpgp.org", "https://keys.openpgp.org", true},
		{"hkps://keyserver.ubuntu.com", "https://keyserver.ubuntu.com", true},
		{"https://Keys.Example.org:443/", "https://keys.example.org", true},
		{"https://keys.example.org:8443/pgp/", "https://keys.example.org:8443/pgp", true},
		{"http://keys.example.org", "", false},
		{"hkp://keys.example.org", "", false},
		{"https://user:pw@keys.example.org", "", false},
		{"https://keys.example.org/?x=1", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, err := NormalizeServer(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("NormalizeServer(%q) = %q, %v", tt.in, got, err)
		}
	}
}

func TestPublic(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "192.168.0.1", "172.16.0.1", "169.254.1.1", "::1", "fe80::1", "fc00::1", "100.64.0.1", "0.1.2.3", "64:ff9b::7f00:1", "2002:7f00:1::1", "::ffff:127.0.0.1"} {
		if Public(netip.MustParseAddr(s)) {
			t.Errorf("%s counted as public", s)
		}
	}
	for _, s := range []string{"93.184.216.34", "2606:2800:220:1::248"} {
		if !Public(netip.MustParseAddr(s)) {
			t.Errorf("%s counted as local", s)
		}
	}
}

func TestPlainHTTPIsNeverUsed(t *testing.T) {
	c := New(Config{Dial: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dialed for a plain http address")
		return nil, nil
	}})
	if _, err := c.get(context.Background(), "http://keys.example.org/x", ""); !errors.Is(err, ErrRefused) {
		t.Errorf("get(http) = %v", err)
	}
}
