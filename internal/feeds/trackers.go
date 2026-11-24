package feeds

import (
	"net/url"
	"strings"

	htmltok "golang.org/x/net/html"
)

// feedTrackers are the counting pixels feed services append to entries. They
// show nothing and exist only to report the read, so they are taken out
// whatever the image setting says. Each is a host and, optionally, a path
// prefix on it.
var feedTrackers = []struct{ host, path string }{
	{"feeds.feedburner.com", "/~r/"},
	{"feedproxy.google.com", "/~r/"},
	{"feeds.feedblitz.com", "/~/i/"},
	{"pixel.wp.com", ""},
	{"stats.wordpress.com", ""},
	{"feeds.wordpress.com", "/1.0/"},
	{"pi.feedsportal.com", ""},
	{"da.feedsportal.com", ""},
	{"rss.feedsportal.com", "/c/"},
	{"www.feedburner.com", ""},
}

// isFeedTracker reports whether an image source is one of the known feed
// counting pixels.
func isFeedTracker(src string) bool {
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, t := range feedTrackers {
		if host == t.host && strings.HasPrefix(u.Path, t.path) {
			return true
		}
	}
	return false
}

// StripFeedTrackers removes the img elements that load a known feed counting
// pixel. Everything else is written back byte for byte.
func StripFeedTrackers(fragment string) string {
	if !strings.Contains(fragment, "<img") && !strings.Contains(fragment, "<IMG") {
		return fragment
	}
	z := htmltok.NewTokenizer(strings.NewReader(fragment))
	var b strings.Builder
	b.Grow(len(fragment))
	for {
		tt := z.Next()
		if tt == htmltok.ErrorToken {
			return b.String()
		}
		raw := z.Raw()
		if tt == htmltok.StartTagToken || tt == htmltok.SelfClosingTagToken {
			name, hasAttr := z.TagName()
			if string(name) == "img" && hasAttr {
				drop := false
				for {
					key, val, more := z.TagAttr()
					if string(key) == "src" && isFeedTracker(string(val)) {
						drop = true
					}
					if !more {
						break
					}
				}
				if drop {
					continue
				}
			}
		}
		b.Write(raw)
	}
}
