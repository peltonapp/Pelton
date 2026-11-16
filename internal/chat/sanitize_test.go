package chat

import (
	"strings"
	"testing"
)

func TestSanitizeKeepsTheMatrixSubset(t *testing.T) {
	cases := []struct {
		name, in   string
		want, lose []string
	}{
		{
			name: "formatting survives",
			in:   `<strong>bold</strong> <em>it</em> <code class="language-go">x</code><pre><code>y</code></pre><blockquote>q</blockquote>`,
			want: []string{"<strong>bold</strong>", "<em>it</em>", `class="language-go"`, "<blockquote>q</blockquote>"},
		},
		{
			name: "scripts and handlers go",
			in:   `<p onclick="steal()">hi</p><script>alert(1)</script><img src="x" onerror="steal()">`,
			want: []string{"<p>hi</p>"},
			lose: []string{"onclick", "script", "onerror", "alert"},
		},
		{
			name: "remote images are blocked, homeserver media stays",
			in:   `<img src="https://tracker.example/p.gif"><img src="mxc://example.org/abc123" alt="cat">`,
			want: []string{`src="mxc://example.org/abc123"`, `alt="cat"`},
			lose: []string{"tracker.example"},
		},
		{
			name: "links keep safe schemes only",
			in:   `<a href="https://example.org">ok</a><a href="javascript:alert(1)">bad</a>`,
			want: []string{`href="https://example.org"`, `rel="nofollow"`},
			lose: []string{"javascript"},
		},
		{
			name: "colours must be hex",
			in:   `<font data-mx-color="#ff0000">red</font><span data-mx-bg-color="url(x)">bg</span><span style="position:fixed">s</span>`,
			want: []string{`data-mx-color="#ff0000"`},
			lose: []string{"url(x)", "style", "position"},
		},
		{
			name: "unknown tags lose their markup",
			in:   `<iframe src="https://example.org"></iframe><form><input></form><style>body{}</style>`,
			lose: []string{"iframe", "form", "input", "style"},
		},
	}
	for _, c := range cases {
		got := sanitizeHTML(c.in)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q missing from %q", c.name, w, got)
			}
		}
		for _, l := range c.lose {
			if strings.Contains(got, l) {
				t.Errorf("%s: %q survived in %q", c.name, l, got)
			}
		}
	}
}
