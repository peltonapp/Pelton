package feeds

import (
	"bytes"
	"net/url"
	"strings"

	htmltok "golang.org/x/net/html"
)

// Link is a feed a web page points at.
type Link struct {
	URL   string
	Title string
	// Type is the media type the page declared for it.
	Type string
}

// feedTypes are the media types a <link rel="alternate"> names a feed with.
var feedTypes = map[string]bool{
	"application/rss+xml":    true,
	"application/atom+xml":   true,
	"application/rdf+xml":    true,
	"application/feed+json":  true,
	"application/xml":        true,
	"text/xml":               true,
	"application/x.atom+xml": true,
	"application/x-atom+xml": true,
}

// CommonPaths are where sites keep their feeds when the page does not say,
// tried in this order when discovery finds no link.
var CommonPaths = []string{
	"/feed",
	"/feed/",
	"/rss",
	"/rss.xml",
	"/atom.xml",
	"/feed.xml",
	"/index.xml",
	"/feed.json",
	"/atom",
	"/index.rss",
}

// LooksLikeFeed reports whether a document is a feed rather than a web page,
// from its first bytes, so discovery can tell which it was handed.
func LooksLikeFeed(data []byte, contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "rss") || strings.Contains(ct, "atom") || strings.Contains(ct, "feed+json") {
		return true
	}
	head := bytes.ToLower(bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	if len(head) > 1024 {
		head = head[:1024]
	}
	if bytes.HasPrefix(head, []byte("{")) {
		return bytes.Contains(head, []byte("jsonfeed.org"))
	}
	if bytes.Contains(head, []byte("<html")) || bytes.Contains(head, []byte("<!doctype html")) {
		return false
	}
	return bytes.Contains(head, []byte("<rss")) || bytes.Contains(head, []byte("<feed")) || bytes.Contains(head, []byte("<rdf:rdf")) || bytes.Contains(head, []byte("<channel"))
}

// page is what discovery reads out of an html page.
type page struct {
	feeds []Link
	icons []string
}

// readPage collects the feed links and icon links of an html page, resolved
// against pageURL or the page's own <base>.
func readPage(data []byte, pageURL string) page {
	base := parseBase(pageURL)
	z := htmltok.NewTokenizer(bytes.NewReader(data))
	var out page
	seen := map[string]bool{}
	for {
		tt := z.Next()
		if tt == htmltok.ErrorToken {
			return out
		}
		if tt != htmltok.StartTagToken && tt != htmltok.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		switch tok.Data {
		case "base":
			if href := tokAttr(tok, "href"); href != "" {
				if u := parseBase(resolveURL(base, href)); u != nil {
					base = u
				}
			}
		case "link":
			rels := strings.Fields(strings.ToLower(tokAttr(tok, "rel")))
			href := tokAttr(tok, "href")
			if href == "" {
				continue
			}
			abs := resolveURL(base, href)
			typ := strings.ToLower(strings.TrimSpace(strings.Split(tokAttr(tok, "type"), ";")[0]))
			switch {
			case hasAny(rels, "alternate", "feed") && feedTypes[typ]:
				if !seen[abs] {
					seen[abs] = true
					out.feeds = append(out.feeds, Link{URL: abs, Title: cleanText(tokAttr(tok, "title")), Type: typ})
				}
			case hasAny(rels, "icon", "apple-touch-icon", "apple-touch-icon-precomposed"):
				out.icons = append(out.icons, abs)
			}
		case "body":
			// feed and icon links live in the head; a body can be large.
			return out
		}
	}
}

// DiscoverLinks returns the feeds an html page links to, in page order.
func DiscoverLinks(data []byte, pageURL string) []Link {
	return readPage(data, pageURL).feeds
}

// IconLinks returns the icons an html page names, best first: the page's own
// icon links, then the site's /favicon.ico.
func IconLinks(data []byte, pageURL string) []string {
	icons := readPage(data, pageURL).icons
	if u := parseBase(pageURL); u != nil {
		icons = append(icons, (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/favicon.ico"}).String())
	}
	return icons
}

func tokAttr(tok htmltok.Token, name string) string {
	for _, a := range tok.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func hasAny(list []string, want ...string) bool {
	for _, v := range list {
		for _, w := range want {
			if v == w {
				return true
			}
		}
	}
	return false
}

// SameSite reports whether two urls belong to the same site: the same host, or
// one a subdomain of the other, with a leading www. ignored. Icons are only
// fetched from the site a feed belongs to.
func SameSite(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	ha := strings.TrimPrefix(strings.ToLower(ua.Hostname()), "www.")
	hb := strings.TrimPrefix(strings.ToLower(ub.Hostname()), "www.")
	if ha == "" || hb == "" {
		return false
	}
	return ha == hb || strings.HasSuffix(ha, "."+hb) || strings.HasSuffix(hb, "."+ha)
}
