package feeds

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustParse(t *testing.T, data []byte, url string) *Feed {
	t.Helper()
	f, err := Parse(data, url)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

func TestParseRSS2(t *testing.T) {
	f := mustParse(t, readFixture(t, "rss2.xml"), "https://example.com/feed.xml")
	if f.Kind != "rss" || f.Title != "Example & Co. Blog" || f.SiteURL != "https://example.com/" {
		t.Fatalf("channel = %q %q %q", f.Kind, f.Title, f.SiteURL)
	}
	if f.Description != "Notes from Example" {
		t.Errorf("description = %q", f.Description)
	}
	if f.IconURL != "https://example.com/logo.png" {
		t.Errorf("icon = %q", f.IconURL)
	}
	if len(f.Entries) != 4 {
		t.Fatalf("entries = %d", len(f.Entries))
	}

	first := f.Entries[0]
	if first.GUID != "tag:example.com,2026:1" || first.URL != "https://example.com/posts/first" {
		t.Errorf("first id/url = %q %q", first.GUID, first.URL)
	}
	if first.Author != "Ada Lovelace" {
		t.Errorf("author = %q", first.Author)
	}
	want := time.Date(2026, 11, 10, 17, 30, 0, 0, time.UTC)
	if !first.Published.Equal(want) {
		t.Errorf("published = %v, want %v (PST taken as -0800)", first.Published, want)
	}
	for _, s := range []string{`href="https://example.com/about"`, `src="https://example.com/img/a.png"`, `https://example.com/img/a2.png 2x`} {
		if !strings.Contains(first.Content, s) {
			t.Errorf("content lacks %s: %s", s, first.Content)
		}
	}
	if len(first.Enclosures) != 1 || first.Enclosures[0].URL != "https://example.com/audio/ep1.mp3" || first.Enclosures[0].Length != 12345 {
		t.Errorf("enclosures = %+v", first.Enclosures)
	}
	if first.Image != "https://cdn.example.com/photo.jpg" {
		t.Errorf("image = %q", first.Image)
	}

	second := f.Entries[1]
	if second.Title != "Second post" {
		t.Errorf("title = %q", second.Title)
	}
	if second.Author != "Bob Builder" {
		t.Errorf("author = %q", second.Author)
	}
	if second.GUID != "https://example.com/posts/second" {
		t.Errorf("guid falls back to the link: %q", second.GUID)
	}
	if second.Published.IsZero() || second.Published.Day() != 9 {
		t.Errorf("published = %v", second.Published)
	}

	third := f.Entries[2]
	if !strings.HasPrefix(third.GUID, "sha256:") {
		t.Errorf("guid = %q, want a hash", third.GUID)
	}
	if !third.Published.IsZero() {
		t.Errorf("published = %v, want zero for garbage", third.Published)
	}
	if !strings.Contains(third.Summary, "Only text") {
		t.Errorf("summary = %q", third.Summary)
	}
	if len(third.Enclosures) != 1 || third.Enclosures[0].Type != "video/mp4" || third.Image != "https://video.example.com/thumb.jpg" {
		t.Errorf("media group = %+v image %q", third.Enclosures, third.Image)
	}

	fourth := f.Entries[3]
	if fourth.URL != "https://example.com/posts/fourth" {
		t.Errorf("permalink guid as url: %q", fourth.URL)
	}
}

func TestParseAtom(t *testing.T) {
	f := mustParse(t, readFixture(t, "atom.xml"), "https://blog.example.org/atom.xml")
	if f.Kind != "atom" || f.Title != "Atom Example" || f.SiteURL != "https://blog.example.org/" {
		t.Fatalf("feed = %q %q %q", f.Kind, f.Title, f.SiteURL)
	}
	if f.IconURL != "https://blog.example.org/favicon.png" {
		t.Errorf("icon = %q", f.IconURL)
	}
	if len(f.Entries) != 3 {
		t.Fatalf("entries = %d", len(f.Entries))
	}
	x := f.Entries[0]
	if x.URL != "https://blog.example.org/2026/11/xhtml-post" {
		t.Errorf("xml:base url = %q", x.URL)
	}
	if !strings.Contains(x.Content, `<a href="https://blog.example.org/2026/11/img/pic.png">here</a>`) || !strings.Contains(x.Content, "<br>") {
		t.Errorf("xhtml content = %q", x.Content)
	}
	if strings.Contains(x.Content, "xmlns") || strings.HasPrefix(x.Content, "<div") {
		t.Errorf("wrapper div kept: %q", x.Content)
	}
	if x.Author != "Grace Hopper" {
		t.Errorf("feed author fallback = %q", x.Author)
	}
	if len(x.Enclosures) != 1 || x.Enclosures[0].URL != "https://blog.example.org/2026/11/sound.ogg" || x.Enclosures[0].Length != 42 {
		t.Errorf("enclosure = %+v", x.Enclosures)
	}
	if x.Published.UTC().Hour() != 6 {
		t.Errorf("published = %v", x.Published)
	}

	h := f.Entries[1]
	if h.Author != "Alan Turing" || !strings.Contains(h.Content, `src="https://blog.example.org/x.png"`) {
		t.Errorf("html entry = %q %q", h.Author, h.Content)
	}
	if h.Summary != "<p>Plain summary &amp; more</p>" {
		t.Errorf("text summary = %q", h.Summary)
	}
	if !h.Published.Equal(h.Updated) || h.Published.IsZero() {
		t.Errorf("published falls back to updated: %v", h.Published)
	}

	v := f.Entries[2]
	if v.Image != "https://i.example.org/1.jpg" || !strings.Contains(v.Summary, "Watch this<br>second line") {
		t.Errorf("media entry = %q %q", v.Image, v.Summary)
	}
}

func TestParseRDF(t *testing.T) {
	f := mustParse(t, readFixture(t, "rdf.xml"), "https://rdf.example.net/index.rdf")
	if f.Kind != "rdf" || f.Title != "RDF Site" || len(f.Entries) != 2 {
		t.Fatalf("feed = %+v", f)
	}
	if f.Entries[0].GUID != "https://rdf.example.net/one" || f.Entries[0].Author != "Dennis" {
		t.Errorf("first = %+v", f.Entries[0])
	}
	if f.Entries[1].URL != "https://rdf.example.net/two" || f.Entries[1].Published.Day() != 2 {
		t.Errorf("second = %+v", f.Entries[1])
	}
}

func TestParseJSONFeed(t *testing.T) {
	f := mustParse(t, readFixture(t, "feed.json"), "https://json.example.com/feed.json")
	if f.Kind != "json" || f.Title != "JSON Example" || f.IconURL != "https://json.example.com/favicon.ico" {
		t.Fatalf("feed = %+v", f)
	}
	a, b := f.Entries[0], f.Entries[1]
	if a.GUID != "1" || a.URL != "https://json.example.com/posts/1" || a.Author != "Margaret" {
		t.Errorf("first = %+v", a)
	}
	if !strings.Contains(a.Content, `src="https://json.example.com/i.png"`) {
		t.Errorf("content = %q", a.Content)
	}
	if len(a.Enclosures) != 1 || a.Enclosures[0].Title != "Episode" {
		t.Errorf("attachments = %+v", a.Enclosures)
	}
	if b.GUID != "2" || b.Author != "Ken" || b.Content != "<p>Line one</p><p>Line two</p>" {
		t.Errorf("second = %+v", b)
	}
	if b.Published.IsZero() {
		t.Error("published should fall back to date_modified")
	}
}

func TestParseCharsets(t *testing.T) {
	latin1 := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><rss><channel><title>Caf\xe9</title><item><title>Cr\xe8me</title><link>https://a.example/1</link></item></channel></rss>")
	f := mustParse(t, latin1, "https://a.example/feed")
	if f.Title != "Café" || f.Entries[0].Title != "Crème" {
		t.Errorf("declared latin-1 = %q %q", f.Title, f.Entries[0].Title)
	}

	mislabeledUTF8 := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?><rss><channel><title>Caf\xe9 au lait et cr\xe8me br\xfbl\xe9e</title></channel></rss>")
	f = mustParse(t, mislabeledUTF8, "https://a.example/feed")
	if !strings.HasPrefix(f.Title, "Café") {
		t.Errorf("latin-1 labelled utf-8 = %q", f.Title)
	}

	mislabeledLatin := []byte("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><rss><channel><title>Café</title></channel></rss>")
	f = mustParse(t, mislabeledLatin, "https://a.example/feed")
	if f.Title != "Café" {
		t.Errorf("utf-8 labelled latin-1 = %q", f.Title)
	}
}

func TestParseTolerance(t *testing.T) {
	cases := map[string]string{
		"leading junk":    "\n\nWarning: something\n<rss><channel><title>T</title><item><title>A</title><link>https://x.example/a</link></item></channel></rss>",
		"html entity":     "<rss><channel><title>T&nbsp;&mdash;</title><item><title>A&hellip;</title><link>https://x.example/a</link></item></channel></rss>",
		"truncated":       "<rss><channel><title>T</title><item><title>A</title><link>https://x.example/a</link></item><item><title>B",
		"unescaped amp":   "<rss><channel><title>Tom & Jerry</title><item><title>A</title><link>https://x.example/a?x=1&y=2</link></item></channel></rss>",
		"bom and no decl": "\xef\xbb\xbf<rss><channel><title>T</title><item><title>A</title><link>https://x.example/a</link></item></channel></rss>",
	}
	for name, doc := range cases {
		f, err := Parse([]byte(doc), "https://x.example/feed")
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(f.Entries) == 0 || f.Entries[0].Title == "" {
			t.Errorf("%s: entries = %+v", name, f.Entries)
		}
	}
	if _, err := Parse([]byte("<html><body>nope</body></html>"), ""); err != ErrNotFeed {
		t.Errorf("html page: err = %v", err)
	}
	if _, err := Parse([]byte(`{"hello":"world"}`), ""); err != ErrNotFeed {
		t.Errorf("other json: err = %v", err)
	}
	if _, err := Parse(nil, ""); err != ErrNotFeed {
		t.Errorf("empty: err = %v", err)
	}
}

func TestParseDate(t *testing.T) {
	utc := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, time.UTC) }
	cases := []struct {
		in   string
		want time.Time
	}{
		{"Tue, 10 Nov 2026 09:30:00 +0000", utc(2026, 11, 10, 9, 30)},
		{"Tue, 10 Nov 2026 09:30:00 GMT", utc(2026, 11, 10, 9, 30)},
		{"Wed, 10 Nov 2026 09:30:00 EST", utc(2026, 11, 10, 14, 30)},
		{"10 Nov 2026 09:30 +0100", utc(2026, 11, 10, 8, 30)},
		{"Tuesday, 10 November 2026 09:30:00 -0000", utc(2026, 11, 10, 9, 30)},
		{"2026-11-10T09:30:00Z", utc(2026, 11, 10, 9, 30)},
		{"2026-11-10T09:30:00+0200", utc(2026, 11, 10, 7, 30)},
		{"2026-11-10 09:30:00", utc(2026, 11, 10, 9, 30)},
		{"2026-11-10", utc(2026, 11, 10, 0, 0)},
		{"Nov 10, 2026", utc(2026, 11, 10, 0, 0)},
		{"November 10th, 2026", utc(2026, 11, 10, 0, 0)},
		{"Tue, 10 Nov 2026 09:30:00 GMT+2", utc(2026, 11, 10, 7, 30)},
		{"Sept 10, 2026", utc(2026, 9, 10, 0, 0)},
		{"  Tue,  10   Nov 2026 09:30:00   +0000 ", utc(2026, 11, 10, 9, 30)},
		{"1794268800", time.Unix(1794268800, 0).UTC()},
		{"", time.Time{}},
		{"yesterday", time.Time{}},
	}
	for _, c := range cases {
		if got := ParseDate(c.in); !got.Equal(c.want) {
			t.Errorf("ParseDate(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestClampDates(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	f := &Feed{Entries: []Entry{{Published: now.Add(72 * time.Hour)}, {Published: now.Add(time.Hour)}}}
	f.ClampDates(now)
	if !f.Entries[0].Published.Equal(now) || !f.Entries[1].Published.Equal(now.Add(time.Hour)) {
		t.Errorf("clamped = %v", f.Entries)
	}
}

func TestStripFeedTrackers(t *testing.T) {
	in := `<p>Text</p><img src="https://feeds.feedburner.com/~r/x/~4/abc" height="1" width="1"><img src="https://pixel.wp.com/b.gif?v=1"><img src="https://example.com/real.png">`
	out := StripFeedTrackers(in)
	if strings.Contains(out, "feedburner") || strings.Contains(out, "pixel.wp.com") {
		t.Errorf("tracker kept: %s", out)
	}
	if !strings.Contains(out, "real.png") || !strings.Contains(out, "<p>Text</p>") {
		t.Errorf("content lost: %s", out)
	}
}
