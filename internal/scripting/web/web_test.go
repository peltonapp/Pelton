package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
)

func registry(t *testing.T, cfg Config) *scripting.Registry {
	return scriptingtest.Registry(t, func(r *scripting.Registry) error { return Register(r, cfg) })
}

func run(t *testing.T, r *scripting.Registry, src string, perms ...string) *scripting.Run {
	t.Helper()
	return scriptingtest.Run(t, r, scriptingtest.Script(1, src, perms...), false)
}

func lines(run *scripting.Run) string {
	return strings.Join(scriptingtest.Logged(run), "\n")
}

func TestLocalized(t *testing.T) {
	scriptingtest.Localized(t, registry(t, Config{}))
}

func TestDeclaredLiteralAddress(t *testing.T) {
	var agents []string
	var cookies []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.UserAgent())
		cookies = append(cookies, r.Header.Get("Cookie"))
		mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "track", Value: "1"})
		fmt.Fprint(w, `{"hello":"world"}`)
	}))
	defer srv.Close()
	r := registry(t, Config{})
	src := fmt.Sprintf(`
local a = http.get(%q)
print(a.status, a.body)
local b = http.get_json(%q)
print(b.json.hello)`, srv.URL, srv.URL)
	got := run(t, r, src, "net:127.0.0.1")
	if got.Status != scripting.StatusOK {
		t.Fatalf("%s %s", got.Status, got.Error)
	}
	if lines(got) != "200 {\"hello\":\"world\"}\nworld" {
		t.Fatalf("logged %q", lines(got))
	}
	mu.Lock()
	defer mu.Unlock()
	for i := range agents {
		if agents[i] != UserAgent || cookies[i] != "" {
			t.Fatalf("request %d: agent %q cookie %q", i, agents[i], cookies[i])
		}
	}
}

func TestUndeclaredHostIsDenied(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	r := registry(t, Config{})
	for _, perms := range [][]string{nil, {"net:example.org"}, {"net"}} {
		got := run(t, r, fmt.Sprintf(`http.get(%q)`, srv.URL), perms...)
		if got.Status != scripting.StatusDenied {
			t.Fatalf("%v: %s %s", perms, got.Status, got.Error)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("an undeclared request reached the server")
	}
}

func TestNamesMustNotPointToLocalAddresses(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	cases := map[string][]netip.Addr{
		"loopback":   {netip.MustParseAddr("127.0.0.1")},
		"private":    {netip.MustParseAddr("192.168.1.4")},
		"link-local": {netip.MustParseAddr("169.254.169.254")},
		"mixed":      {netip.MustParseAddr("203.0.113.9"), netip.MustParseAddr("10.0.0.1")},
		"mapped":     {netip.MustParseAddr("::ffff:127.0.0.1")},
		"cgnat":      {netip.MustParseAddr("100.64.0.1")},
		"ula":        {netip.MustParseAddr("fd00::1")},
		"nat64":      {netip.MustParseAddr("64:ff9b::7f00:1")},
		"6to4":       {netip.MustParseAddr("2002:c0a8:0104::1")},
		"site-local": {netip.MustParseAddr("fec0::1")},
		"benchmark":  {netip.MustParseAddr("198.18.0.1")},
		"broadcast":  {netip.MustParseAddr("255.255.255.255")},
		"ietf":       {netip.MustParseAddr("192.0.0.8")},
	}
	for name, addrs := range cases {
		cfg := Config{
			Lookup: func(context.Context, string) ([]netip.Addr, error) { return addrs, nil },
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
			},
		}
		got := run(t, registry(t, cfg), fmt.Sprintf(`local r, err = http.get("http://nas.example:%s/") print(r == nil, err)`, port), "net:nas.example")
		if got.Status != scripting.StatusOK || !strings.HasPrefix(lines(got), "true ") || !strings.Contains(lines(got), "local address") {
			t.Fatalf("%s: %s %q", name, got.Status, lines(got))
		}
	}
	if hits.Load() != 0 {
		t.Fatal("a request reached a local address")
	}
	got := run(t, registry(t, Config{}), fmt.Sprintf(`local r, err = http.get("http://localhost:%s/") print(r == nil)`, port), "net:localhost")
	if lines(got) != "true" {
		t.Fatalf("localhost: %s %q", got.Status, lines(got))
	}
}

func TestDialsTheCheckedAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Host)
	}))
	defer srv.Close()
	srvAddr := strings.TrimPrefix(srv.URL, "http://")
	var lookups atomic.Int32
	var dialed []string
	cfg := Config{
		Lookup: func(context.Context, string) ([]netip.Addr, error) {
			if lookups.Add(1) > 1 {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("203.0.113.7")}, nil
		},
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return (&net.Dialer{}).DialContext(ctx, network, srvAddr)
		},
	}
	got := run(t, registry(t, cfg), `print(http.get("http://pub.example:8080/").body)`, "net:pub.example")
	if got.Status != scripting.StatusOK || lines(got) != "pub.example:8080" {
		t.Fatalf("%s %s %q", got.Status, got.Error, lines(got))
	}
	if len(dialed) != 1 || dialed[0] != "203.0.113.7:8080" || lookups.Load() != 1 {
		t.Fatalf("dialed %v after %d lookups", dialed, lookups.Load())
	}
}

func TestRedirects(t *testing.T) {
	var mux http.ServeMux
	srv := httptest.NewServer(&mux)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	mux.HandleFunc("/same", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/end", http.StatusFound) })
	mux.HandleFunc("/away", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+u.Port()+"/end", http.StatusFound)
	})
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	})
	mux.HandleFunc("/end", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "end") })
	r := registry(t, Config{})
	got := run(t, r, fmt.Sprintf(`
print(http.get(%q).body)
local a, err = http.get(%q) print(a == nil, err ~= nil)
local b, err2 = http.get(%q) print(b == nil, err2 ~= nil)`, srv.URL+"/same", srv.URL+"/away", srv.URL+"/file"), "net:127.0.0.1")
	if got.Status != scripting.StatusOK || lines(got) != "end\ntrue true\ntrue true" {
		t.Fatalf("%s %s %q", got.Status, got.Error, lines(got))
	}
}

func TestLimitsAndRefusals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", maxResponseBody+10)))
	}))
	defer srv.Close()
	r := registry(t, Config{})
	got := run(t, r, fmt.Sprintf(`local a, err = http.get(%q) print(a == nil, err)`, srv.URL), "net:127.0.0.1")
	if !strings.Contains(lines(got), "larger than") {
		t.Fatalf("%q", lines(got))
	}
	for _, src := range []string{
		`http.get("file:///etc/passwd")`,
		`http.get("ftp://127.0.0.1/")`,
		`http.get("http://user:pw@127.0.0.1/")`,
		`http.request("TRACE", "http://127.0.0.1/")`,
		`http.get("http://127.0.0.1/", {headers = {Host = "evil.example"}})`,
		`http.get("http://127.0.0.1/", {headers = {["X-A"] = "a\r\nB: c"}})`,
	} {
		if got := run(t, r, src, "net:127.0.0.1"); got.Status != scripting.StatusError {
			t.Fatalf("%s: %s %s", src, got.Status, got.Error)
		}
	}
}

func TestDryRunMakesNoRequest(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	r := registry(t, Config{})
	sc := scriptingtest.Script(1, fmt.Sprintf(`local a = http.post_json(%q, {x = 1}) print(a.status)`, srv.URL), "net:127.0.0.1")
	sc.Approved = nil
	got := scriptingtest.Run(t, r, sc, true)
	if got.Status != scripting.StatusOK || hits.Load() != 0 || got.Changes() != 1 || got.Steps[0].Kind != scripting.StepPlanned {
		t.Fatalf("%s %s hits %d steps %+v", got.Status, got.Error, hits.Load(), got.Steps)
	}
}

func TestRiskyNeedsConfirmation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	e := &scripting.Engine{Registry: registry(t, Config{}), Hosts: &scripting.Hosts{}, Limits: scripting.DefaultLimits()}
	sc := scriptingtest.Script(1, fmt.Sprintf(`http.get(%q)`, srv.URL), "net:127.0.0.1")
	got := e.Run(context.Background(), scripting.RunRequest{Script: sc, Trigger: scripting.TriggerManual})
	if got.Status != scripting.StatusDenied {
		t.Fatalf("%s", got.Status)
	}
	sc.AllowUnasked = []string{"http.get"}
	if got := e.Run(context.Background(), scripting.RunRequest{Script: sc, Trigger: scripting.TriggerManual}); got.Status != scripting.StatusOK {
		t.Fatalf("allowed: %s %s", got.Status, got.Error)
	}
}
