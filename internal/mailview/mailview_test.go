package mailview

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestLinks(t *testing.T) {
	tests := []struct {
		name  string
		html  string
		plain string
		want  []string
	}{
		{
			name: "anchor hrefs, quoted and bare",
			html: `<a href="https://one.example/">a</a> <a href='https://two.example/'>b</a> <a href=https://three.example/>c</a>`,
			want: []string{"https://one.example/", "https://two.example/", "https://three.example/"},
		},
		{
			name: "entities in the href are decoded",
			html: `<a href="https://example.com/p?a=1&amp;b=2">go</a>`,
			want: []string{"https://example.com/p?a=1&b=2"},
		},
		{
			// an image is remote content, not something the reader can click,
			// and scanning every tracking pixel would burn the request quota.
			name: "image sources are not links",
			html: `<img src="https://tracker.example/pixel.gif"><a href="https://real.example/">x</a>`,
			want: []string{"https://real.example/"},
		},
		{
			name: "other schemes are skipped",
			html: `<a href="mailto:a@b.example">mail</a><a href="cid:abc">inline</a><a href="ftp://x.example/">ftp</a>`,
			want: []string{},
		},
		{
			name:  "bare urls in plain text",
			plain: "see https://plain.example/path for details",
			want:  []string{"https://plain.example/path"},
		},
		{
			name:  "sentence punctuation is not part of a bare url",
			plain: "go to https://example.com/page, or https://example.com/other.",
			want:  []string{"https://example.com/page", "https://example.com/other"},
		},
		{
			// an href states the target exactly. trimming it would scan (and
			// badge) a different address than the one the link opens, which is
			// worse than scanning one character too many.
			name: "an href keeps punctuation the reader would click",
			html: `<a href="https://en.example.org/wiki/Foo_(bar)">x</a><a href="https://example.com/end.">y</a>`,
			want: []string{"https://en.example.org/wiki/Foo_(bar)", "https://example.com/end."},
		},
		{
			// the ui's linkifier stops a bare url at these characters, so the
			// backend has to as well or the badge lands on nothing.
			name:  "a bare url stops where the ui stops it",
			plain: "see (https://example.com/a) and [https://example.com/b]",
			want:  []string{"https://example.com/a", "https://example.com/b"},
		},
		{
			name:  "the same target is only returned once",
			html:  `<a href="https://dup.example/">one</a><a href="https://dup.example/">two</a>`,
			plain: "https://dup.example/",
			want:  []string{"https://dup.example/"},
		},
		{
			name: "attributes before href do not swallow it",
			html: `<a class="btn" target="_blank" href="https://late.example/">x</a>`,
			want: []string{"https://late.example/"},
		},
		{
			name: "nothing to find",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Links(tt.html, tt.plain)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Links() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A message can carry more anchors than anyone would ever scan; the cap is what
// keeps one open message from turning into thousands of api lookups.
func TestLinksIsCapped(t *testing.T) {
	var b strings.Builder
	for i := range maxLinks * 2 {
		fmt.Fprintf(&b, `<a href="https://example.com/%d">x</a>`, i)
	}
	if got := len(Links(b.String(), "")); got != maxLinks {
		t.Errorf("len(Links()) = %d, want %d", got, maxLinks)
	}
}

func TestPlainText(t *testing.T) {
	for _, tt := range []struct {
		name string
		html string
		want string
	}{
		{
			// the reason this exists: tag removal without a separator welds the
			// last word of one block onto the first of the next, and the result
			// is a token no search will ever match.
			name: "block boundaries separate words",
			html: "<p>hello friends</p><div>invoice 42</div>",
			want: "hello friends invoice 42",
		},
		{
			name: "entities are decoded",
			html: "<p>Jane &amp; Co &mdash; caf&eacute;</p>",
			want: "Jane & Co — café",
		},
		{
			name: "script and style are not prose",
			html: "<head><style>.a{color:red}</style></head><body><script>var x=1;</script><p>real text</p></body>",
			want: "real text",
		},
		{
			name: "whitespace collapses",
			html: "<p>a\n\n   b\t\tc</p>",
			want: "a b c",
		},
		{
			// mail html is frequently malformed; recovering what is readable
			// beats returning nothing.
			name: "unclosed tags still yield text",
			html: "<div><p>dangling",
			want: "dangling",
		},
		{
			name: "empty input",
			html: "",
			want: "",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlainText(tt.html); got != tt.want {
				t.Errorf("PlainText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Snippet falls back to the html body, so html-only mail still previews.
func TestSnippetFallsBackToHTML(t *testing.T) {
	got := Snippet("", "<p>hello</p><div>world</div>")
	if got != "hello world" {
		t.Errorf("Snippet() = %q, want %q", got, "hello world")
	}
}

// Mail that says which way it reads keeps saying it (#356). Stripping these
// was why right-to-left mail arrived left-aligned with its numbers and
// punctuation out of order: the sender had answered the question and the
// sanitizer removed the answer.
func TestSanitizeKeepsDirection(t *testing.T) {
	for _, tt := range []struct {
		name string
		html string
		want string
	}{
		{"dir on a block", `<div dir="rtl">שלום</div>`, `dir="rtl"`},
		{"dir auto", `<p dir="auto">مرحبا</p>`, `dir="auto"`},
		{"direction style", `<p style="direction: rtl">مرحبا</p>`, "direction"},
		{"unicode-bidi style", `<p style="unicode-bidi: embed">مرحبا</p>`, "unicode-bidi"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sanitize(tt.html, false, false); !strings.Contains(got, tt.want) {
				t.Errorf("Sanitize(%q) = %q, want it to contain %q", tt.html, got, tt.want)
			}
		})
	}
}

// The direction attribute carries no behaviour, but the elements it is allowed
// on still have to obey every other rule: allowing it globally must not let an
// otherwise-stripped element through with it.
func TestSanitizeDirectionDoesNotRescueBlockedElements(t *testing.T) {
	got := Sanitize(`<script dir="rtl">alert(1)</script><p dir="rtl">ok</p>`, false, false)
	if strings.Contains(got, "script") || strings.Contains(got, "alert") {
		t.Errorf("Sanitize() kept a script: %q", got)
	}
	if !strings.Contains(got, `dir="rtl"`) {
		t.Errorf("Sanitize() dropped the direction of the surviving paragraph: %q", got)
	}
}

func TestRemoteHostsDropsUserinfoAndPort(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string
	}{
		{"plain", `<img src="https://CDN.example.com/a.png">`, []string{"cdn.example.com"}},
		{"userinfo", `<img src="https://user@cdn.example.com/a.png">`, []string{"cdn.example.com"}},
		{"several at signs", `<img src="https://a@b@cdn.example.com/a.png">`, []string{"cdn.example.com"}},
		{"userinfo and port", `<img src="https://user:pw@cdn.example.com:8443/a.png">`, []string{"cdn.example.com"}},
		{"empty host after at", `<img src="https://user@/a.png">`, []string{}},
		{"leading at", `<img src="https://@cdn.example.com/a.png">`, []string{"cdn.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RemoteHosts(tt.html); !slices.Equal(got, tt.want) {
				t.Errorf("RemoteHosts = %v, want %v", got, tt.want)
			}
		})
	}
}
