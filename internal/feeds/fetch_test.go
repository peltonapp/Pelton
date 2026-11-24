package feeds

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestFetcher(gap time.Duration) *Fetcher {
	return NewFetcher(&http.Client{Timeout: 5 * time.Second}, gap)
}

func TestFetchConditional(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("User-Agent") != UserAgent {
			t.Errorf("user agent = %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
			t.Errorf("request carries more than it should: %v", r.Header)
		}
		if r.Header.Get("If-None-Match") == `"v1"` || r.Header.Get("If-Modified-Since") == "Mon, 09 Nov 2026 10:00:00 GMT" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 09 Nov 2026 10:00:00 GMT")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte("<rss/>"))
	}))
	defer srv.Close()

	f := newTestFetcher(time.Millisecond)
	ctx := context.Background()
	first, err := f.Fetch(ctx, Request{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if first.NotModified || string(first.Body) != "<rss/>" || first.ETag != `"v1"` || first.MaxAge != time.Hour {
		t.Fatalf("first = %+v", first)
	}
	second, err := f.Fetch(ctx, Request{URL: srv.URL, ETag: first.ETag})
	if err != nil {
		t.Fatal(err)
	}
	if !second.NotModified || second.Body != nil || second.ETag != `"v1"` {
		t.Fatalf("etag revalidation = %+v", second)
	}
	third, err := f.Fetch(ctx, Request{URL: srv.URL, LastModified: first.LastModified})
	if err != nil {
		t.Fatal(err)
	}
	if !third.NotModified || third.LastModified != first.LastModified {
		t.Fatalf("last-modified revalidation = %+v", third)
	}
	if hits.Load() != 3 {
		t.Errorf("hits = %d", hits.Load())
	}
}

func TestFetchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/busy":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
		case "/gone":
			w.WriteHeader(http.StatusGone)
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	}))
	defer srv.Close()
	f := newTestFetcher(time.Millisecond)
	ctx := context.Background()

	_, err := f.Fetch(ctx, Request{URL: srv.URL + "/busy"})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 429 || se.RetryAfter != 2*time.Minute {
		t.Errorf("429 = %v", err)
	}
	if _, err := f.Fetch(ctx, Request{URL: srv.URL + "/gone"}); !errors.As(err, &se) || se.Code != 410 {
		t.Errorf("410 = %v", err)
	}
	if _, err := f.Fetch(ctx, Request{URL: srv.URL + "/big", MaxBytes: 1024}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("size limit = %v", err)
	}
	if _, err := f.Fetch(ctx, Request{URL: srv.URL + "/loop"}); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Errorf("redirect loop = %v", err)
	}
	if _, err := f.Fetch(ctx, Request{URL: "file:///etc/passwd"}); err == nil {
		t.Error("file url fetched")
	}
}

func TestFetchRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, "/older", http.StatusMovedPermanently)
		case "/older":
			http.Redirect(w, r, "/new", http.StatusPermanentRedirect)
		case "/temp":
			http.Redirect(w, r, "/new", http.StatusFound)
		case "/new":
			_, _ = w.Write([]byte("<rss/>"))
		}
	}))
	defer srv.Close()
	f := newTestFetcher(time.Millisecond)
	moved, err := f.Fetch(context.Background(), Request{URL: srv.URL + "/old"})
	if err != nil {
		t.Fatal(err)
	}
	if moved.PermanentURL != srv.URL+"/new" {
		t.Errorf("permanent url = %q", moved.PermanentURL)
	}
	temp, err := f.Fetch(context.Background(), Request{URL: srv.URL + "/temp"})
	if err != nil {
		t.Fatal(err)
	}
	if temp.PermanentURL != "" || temp.URL != srv.URL+"/new" {
		t.Errorf("temporary redirect = %+v", temp)
	}
}

func TestFetchHostPoliteness(t *testing.T) {
	var (
		mu       sync.Mutex
		inFlight int
		maxSeen  int
		times    []time.Time
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		maxSeen = max(maxSeen, inFlight)
		times = append(times, time.Now())
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		_, _ = w.Write([]byte("<rss/>"))
	}))
	defer srv.Close()
	gap := 40 * time.Millisecond
	f := newTestFetcher(gap)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Fetch(context.Background(), Request{URL: srv.URL}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if maxSeen != 1 {
		t.Errorf("%d requests at once to one host", maxSeen)
	}
	for i := 1; i < len(times); i++ {
		if d := times[i].Sub(times[i-1]); d < gap-5*time.Millisecond {
			t.Errorf("requests %d and %d only %v apart", i-1, i, d)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Fetch(ctx, Request{URL: srv.URL}); err == nil {
		t.Error("cancelled fetch went ahead")
	}
}

func TestRetryAfterAndMaxAge(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	if d := retryAfter("Mon, 16 Nov 2026 12:10:00 GMT", now); d != 10*time.Minute {
		t.Errorf("date retry-after = %v", d)
	}
	if d := retryAfter("9999999", now); d != maxRetryAfter {
		t.Errorf("capped retry-after = %v", d)
	}
	if d := retryAfter("soon", now); d != 0 {
		t.Errorf("garbage retry-after = %v", d)
	}
	if d := maxAge("no-cache"); d != 0 {
		t.Errorf("no-cache = %v", d)
	}
	if d := maxAge(`private, max-age="600"`); d != 10*time.Minute {
		t.Errorf("quoted max-age = %v", d)
	}
}
