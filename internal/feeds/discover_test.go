package feeds

import (
	"strings"
	"testing"
	"time"
)

const samplePage = `<!doctype html>
<html><head>
<base href="https://www.example.com/blog/">
<title>Blog</title>
<link rel="stylesheet" href="/style.css">
<link rel="alternate" type="application/rss+xml" title="Posts &amp; notes" href="feed.xml">
<link rel="alternate" type="application/atom+xml" href="https://www.example.com/atom.xml">
<link rel="alternate" type="application/rss+xml" href="feed.xml">
<link rel="alternate" type="application/json+oembed" href="/oembed">
<link rel="alternate" hreflang="de" href="/de/">
<link rel="icon" type="image/png" href="/icon-32.png">
<link rel="apple-touch-icon" href="https://cdn.example.com/touch.png">
</head><body>
<link rel="alternate" type="application/rss+xml" href="/in-body.xml">
</body></html>`

func TestDiscoverLinks(t *testing.T) {
	links := DiscoverLinks([]byte(samplePage), "https://www.example.com/blog")
	if len(links) != 2 {
		t.Fatalf("links = %+v", links)
	}
	if links[0].URL != "https://www.example.com/blog/feed.xml" || links[0].Title != "Posts & notes" {
		t.Errorf("first = %+v", links[0])
	}
	if links[1].URL != "https://www.example.com/atom.xml" || links[1].Type != "application/atom+xml" {
		t.Errorf("second = %+v", links[1])
	}
}

func TestIconLinks(t *testing.T) {
	icons := IconLinks([]byte(samplePage), "https://www.example.com/blog")
	want := []string{"https://www.example.com/icon-32.png", "https://cdn.example.com/touch.png", "https://www.example.com/favicon.ico"}
	if strings.Join(icons, " ") != strings.Join(want, " ") {
		t.Errorf("icons = %v", icons)
	}
}

func TestLooksLikeFeed(t *testing.T) {
	cases := []struct {
		body, ct string
		want     bool
	}{
		{"<?xml version='1.0'?><rss version='2.0'>", "text/xml", true},
		{"<feed xmlns='http://www.w3.org/2005/Atom'>", "", true},
		{`{"version":"https://jsonfeed.org/version/1.1"}`, "application/json", true},
		{"<!DOCTYPE html><html>", "text/html", false},
		{"anything", "application/atom+xml; charset=utf-8", true},
		{`{"a":1}`, "application/json", false},
	}
	for _, c := range cases {
		if got := LooksLikeFeed([]byte(c.body), c.ct); got != c.want {
			t.Errorf("LooksLikeFeed(%q, %q) = %v", c.body, c.ct, got)
		}
	}
}

func TestSameSite(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"https://www.example.com/x", "https://example.com/", true},
		{"https://cdn.example.com/i.png", "https://example.com/", true},
		{"https://example.com/", "https://blog.example.com/", true},
		{"https://example.com.evil.net/", "https://example.com/", false},
		{"https://notexample.com/", "https://example.com/", false},
		{"https://icons.duckduckgo.com/ip3/example.com.ico", "https://example.com/", false},
	}
	for _, c := range cases {
		if got := SameSite(c.a, c.b); got != c.want {
			t.Errorf("SameSite(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestOPMLRoundTrip(t *testing.T) {
	in := `<?xml version="1.0"?>
<opml version="1.0"><head><title>Subs</title></head><body>
  <outline text="Loose" type="rss" xmlUrl="https://loose.example/feed" htmlUrl="https://loose.example/"/>
  <outline text="Tech">
    <outline text="A &amp; B" title="A &amp; B" type="rss" xmlUrl="https://a.example/feed"/>
    <outline text="Nested">
      <outline text="Deep" xmlUrl="https://deep.example/rss"/>
    </outline>
  </outline>
  <outline text="Dup" xmlUrl="https://a.example/feed"/>
</body></opml>`
	subs, err := ParseOPML([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 3 {
		t.Fatalf("subs = %+v", subs)
	}
	if subs[0].Folder != "" || subs[1].Folder != "Tech" || subs[1].Title != "A & B" || subs[2].Folder != "Tech" {
		t.Errorf("subs = %+v", subs)
	}

	out, err := WriteOPML("Pelton feeds", subs, time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseOPML(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(subs) {
		t.Fatalf("round trip = %+v\n%s", again, out)
	}
	for i := range subs {
		if again[i] != subs[i] {
			t.Errorf("round trip %d = %+v, want %+v", i, again[i], subs[i])
		}
	}
	if !strings.Contains(string(out), `<opml version="2.0">`) {
		t.Errorf("output = %s", out)
	}
}
