package mailview

import (
	"strings"
	"testing"

	htmltok "golang.org/x/net/html"
)

// Removing remote images and trackers used to cut matches out of the raw
// text, and the pieces left either side could join into a url that was never
// checked. Each case here is markup built to do that, or to make the scan and
// the strip read the same src differently.

// bypassHosts are the hosts these cases try to load from; none may survive.
var bypassHosts = []string{"tracker.example", "t.example", "x.example"}

// assertNoRemote fails if any tag in out still points at one of the hosts.
// Text that merely mentions one is fine: escaped, it shows as characters and
// loads nothing.
func assertNoRemote(t *testing.T, label, out string) {
	t.Helper()
	z := htmltok.NewTokenizer(strings.NewReader(out))
	for {
		tt := z.Next()
		if tt == htmltok.ErrorToken {
			return
		}
		if tt != htmltok.StartTagToken && tt != htmltok.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		for _, a := range tok.Attr {
			for _, host := range bypassHosts {
				if strings.Contains(a.Val, host) {
					t.Errorf("%s: <%s %s> still points at %s:\n%s", label, tok.Data, a.Key, host, out)
				}
			}
		}
	}
}

var blockedViewCases = map[string]string{
	"attribute cut from inside a src": `<img src="ht background='https://x.example/y'tps://tracker.example/p.png">`,
	"tag cut from inside a src":       `<img src=https://t.example/p.png width=1 height=1><img alt=">" src="ht<img src=https://t.example/p.png width=1 height=1>tps://tracker.example/q.png?u=victim@example.org">`,
	"text lt before a removed tag":    `x <<img src=https://t.example/p.png>img src=https://tracker.example/q.png>`,
	"newline inside the scheme":       "<img src=\"ht\ntps://tracker.example/p.png\">",
	"protocol relative":               `<img src="//tracker.example/p.png">`,
	"entity encoded scheme":           `<img src="&#104;ttps://tracker.example/p.png">`,
	"background attribute":            `<table><tr><td background="https://tracker.example/bg.png">x</td></tr></table>`,
	"video with remote source":        `<video controls><source src="https://tracker.example/v.mp4"></video>`,
	"unclosed audio":                  `<audio src="https://tracker.example/a.mp3">`,
	"stray source":                    `<source src="https://tracker.example/v.mp4">`,
}

func TestBlockedViewLoadsNothingRemote(t *testing.T) {
	for name, html := range blockedViewCases {
		for _, fonts := range []bool{false, true} {
			assertNoRemote(t, name, Sanitize(html, false, fonts))
		}
	}
}

var trackerCases = map[string]string{
	"double encoded ampersand": `<img src="https://t.example/p.png?u=me@example.org&amp;amp;x=1" width=1 height=1>`,
	"tag cut from inside a src": `<img src=https://t.example/p.png width=1 height=1>` +
		`<img alt=">" src="ht<img src=https://t.example/p.png width=1 height=1>tps://tracker.example/q.png?u=victim@example.org" width=1 height=1>`,
	"padded src":       `<img src="  https://t.example/p.png?u=me@example.org  " width=1 height=1>`,
	"newline in src":   "<img src=\"https://t.example/p.png?u=me@exa\nmple.org\" width=1 height=1>",
	"uppercase scheme": `<IMG SRC="HTTPS://t.example/p.png?u=me@example.org" WIDTH=1 HEIGHT=1>`,
}

// With remote content allowed and tracking pixels blocked, the desktop runs
// Sanitize(StripTrackers(html, ScanRemoteImages(html).TrackerURLs()), true).
func TestTrackerBlockingLoadsNoTracker(t *testing.T) {
	for name, html := range trackerCases {
		out := Sanitize(StripTrackers(html, ScanRemoteImages(html).TrackerURLs()), true, true)
		assertNoRemote(t, name, out)
	}
}

func TestStripTrackersKeepsTheRestOfTheMessage(t *testing.T) {
	html := `<p>Hello &amp; welcome</p><img src="https://cdn.example.com/hero.jpg" width="600">` +
		`<img src="https://t.example/p.png?u=me@example.org" width="1" height="1"><style>p > a { color: red }</style>`
	out := StripTrackers(html, ScanRemoteImages(html).TrackerURLs())
	assertNoRemote(t, "tracker", out)
	for _, want := range []string{"Hello &amp; welcome", "hero.jpg", "p > a { color: red }"} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %q:\n%s", want, out)
		}
	}
}

func TestBlockedViewKeepsInlineMediaAndText(t *testing.T) {
	html := `<p>a &lt; b</p><img src="data:image/png;base64,iVBORw0KGgo=" alt="x"><video controls src="cid:clip"></video>`
	out := Sanitize(html, false, false)
	for _, want := range []string{"a &lt; b", "data:image/png", "cid:clip"} {
		if !strings.Contains(out, want) {
			t.Errorf("lost %q:\n%s", want, out)
		}
	}
}
