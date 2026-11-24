package mailview

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

// fullSnippet is the preview read from the whole body: every field of the
// text joined, then cut. Snippet stops reading once it has enough and must
// come out the same.
func fullSnippet(plain, html string) string {
	text := strings.TrimSpace(plain)
	if text == "" && html != "" {
		text = PlainText(html)
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > snippetLen {
		cut := snippetLen
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		return strings.TrimSpace(text[:cut]) + "…"
	}
	return text
}

func TestSnippetMatchesWholeBody(t *testing.T) {
	cases := [][2]string{
		{"", ""},
		{"   \n\t ", ""},
		{"  hello   world  ", ""},
		{"", "<p>hello</p><div>world</div>"},
		{"   ", "<style>p{}</style><p>styled &amp; escaped</p>"},
		{strings.Repeat("x", snippetLen), ""},
		{strings.Repeat("x", snippetLen+1), ""},
		{strings.Repeat("ab ", 100), ""},
		{strings.Repeat("ü", 100), ""},
		{strings.Repeat(" 日本語", 60), ""},
		{"bad \xff\xfe bytes " + strings.Repeat("y", 200), ""},
		{"", "<html><head><title>t</title><script>var a</script></head><body>" + strings.Repeat("<td>cell</td>", 80) + "</body></html>"},
		{"", "<p>unclosed <b>bold"},
		{"", strings.Repeat("<br>", 50) + "late text"},
	}
	r := rand.New(rand.NewPCG(7, 9))
	pieces := []string{" ", "\n", "\t", " ", "a", "bb", "ü", "日", "\xff", "<p>", "</p>", "<br/>", "<style>", "</style>", "&amp;", "word"}
	for i := 0; i < 2000; i++ {
		var plain, html strings.Builder
		for j := r.IntN(120); j > 0; j-- {
			p := pieces[r.IntN(len(pieces))]
			if r.IntN(2) == 0 {
				plain.WriteString(p)
			} else {
				html.WriteString(p)
			}
		}
		if r.IntN(3) == 0 {
			cases = append(cases, [2]string{"", html.String()})
		} else {
			cases = append(cases, [2]string{plain.String(), html.String()})
		}
	}
	for _, c := range cases {
		if got, want := Snippet(c[0], c[1]), fullSnippet(c[0], c[1]); got != want {
			t.Fatalf("Snippet(%q, %q) = %q, want %q", c[0], c[1], got, want)
		}
	}
}

func TestDetectPGPAcrossBodies(t *testing.T) {
	tests := []struct {
		plain, html string
		want        PGPStatus
	}{
		{"", "", PGPNone},
		{"-----BEGIN PGP MESSAGE-----", "", PGPEncrypted},
		{"", "<pre>-----BEGIN PGP MESSAGE-----</pre>", PGPEncrypted},
		{"-----BEGIN PGP SIGNED MESSAGE-----", "-----BEGIN PGP MESSAGE-----", PGPEncrypted},
		{"", "-----BEGIN PGP SIGNATURE-----", PGPSigned},
		{"-----BEGIN PGP SIGNED MESSAGE-----", "", PGPSigned},
		{"-----BEGIN PGP", " MESSAGE-----", PGPNone},
	}
	for _, tt := range tests {
		if got := DetectPGP(tt.plain, tt.html); got != tt.want {
			t.Errorf("DetectPGP(%q, %q) = %q, want %q", tt.plain, tt.html, got, tt.want)
		}
	}
}

func BenchmarkSnippetLongPlain(b *testing.B) {
	body := strings.Repeat("quarterly results feedback design proposal\n\n", 400)
	for b.Loop() {
		Snippet(body, "")
	}
}

func BenchmarkSnippetHTMLOnly(b *testing.B) {
	html := "<html><body><table>" + strings.Repeat(`<tr><td style="font-family:Arial">quarterly results feedback design proposal</td></tr>`, 400) + "</table></body></html>"
	for b.Loop() {
		Snippet("", html)
	}
}

func BenchmarkDetectPGP(b *testing.B) {
	plain := strings.Repeat("quarterly results feedback design proposal\n\n", 400)
	html := "<html><body>" + strings.Repeat("<p>quarterly results feedback design proposal</p>", 400) + "</body></html>"
	for b.Loop() {
		DetectPGP(plain, html)
	}
}
