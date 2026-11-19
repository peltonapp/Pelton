package mailview

import (
	"net/url"
	"regexp"
	"strings"

	htmltok "golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"
)

// The privacy shield: what a message tried to learn about its reader, beyond
// the tracking pixels in trackers.go. Everything here reads the html the
// message carries; nothing is fetched.

// TrackedLink is a link that reports the click to somebody before it gets to
// where it is going.
type TrackedLink struct {
	// URL is the href as written; Host the host it goes to first.
	URL  string
	Host string
	// Destination is where the link ends up when that is written in the url
	// itself, empty when only the tracker knows.
	Destination string
	// Reasons say why it was counted: "redirect" (the destination is carried
	// in the url), "known-host" (a click-tracking host on the list below),
	// "token" (a per-recipient token on a tracking subdomain).
	Reasons []string
}

// ExternalResource is something other than an image or a link that the
// message would load from the network.
type ExternalResource struct {
	// Kind is "stylesheet", "font", "form", "frame", "media" or "background".
	Kind string
	Host string
}

// maxShieldLinks caps the link scan, like the phishing one.
const maxShieldLinks = 300

// redirectParams are query parameters that carry a link's real destination
// in redirect wrappers. Parameters a sign-in flow uses for the page to come
// back to (redirect_uri, next, continue, return) are left out: those links
// are a login, not a tracker.
var redirectParams = []string{"url", "u", "q", "redirect", "redirect_url", "target", "dest", "destination", "link", "goto", "r", "rd", "out", "to"}

// flowPattern marks links of a sign-in, verification or unsubscribe flow,
// which are left alone however they look: the whole address can be the token.
var flowPattern = regexp.MustCompile(`(?i)verif|confirm|activat|unsub|opt-?out|reset|passw|log-?in|sign-?in|oauth|token=|invite|magic`)

// clickHosts are domains whose business is counting clicks in mail. Like
// trackerDomains it is a short hand-written list: a match only adds a name to
// what the redirect and token checks already find.
var clickHosts = []string{
	"list-manage.com", "sendgrid.net", "hubspotlinks.com", "hs-sites.com", "mandrillapp.com",
	"sparkpostmail.com", "rs6.net", "exct.net", "klclick.com", "klclick1.com", "awstrack.me",
	"convertkit-mail.com", "convertkit-mail2.com", "customeriomail.com", "mlsend.com", "mjt.lu",
	"createsend1.com", "cmail19.com", "cmail20.com", "sendibt2.com", "sendibt3.com", "pstmrk.it",
	"links.iterable.com", "mailgun.org", "emltrk.com", "mailtrack.io", "yesware.com",
	"bananatag.com", "streak.com", "doubleclick.net", "app.link", "trk.mailerlite.com",
}

// trackingLabels are subdomain labels click-tracking hosts are commonly given.
var trackingLabels = map[string]bool{
	"click": true, "clicks": true, "track": true, "tracking": true, "trk": true, "links": true,
	"link": true, "email": true, "mail": true, "go": true, "t": true, "e": true, "c": true, "l": true,
}

// ScanLinks returns the links of an html body that pass through a tracker,
// conservatively: a plain link to the sender's own site with campaign
// parameters is not counted, only one that bounces through somebody first.
func ScanLinks(html string) []TrackedLink {
	var out []TrackedLink
	seen := map[string]bool{}
	for _, m := range anchorHrefPattern.FindAllStringSubmatch(html, maxShieldLinks) {
		href := strings.TrimSpace(htmlUnescape(m[1] + m[2] + m[3]))
		if seen[href] {
			continue
		}
		seen[href] = true
		if link, ok := classifyLink(href); ok {
			out = append(out, link)
		}
	}
	return out
}

// ScanPlainLinks is ScanLinks for a plain-text body, over its bare urls.
func ScanPlainLinks(text string) []TrackedLink {
	var out []TrackedLink
	for _, href := range Links("", text) {
		if link, ok := classifyLink(href); ok {
			out = append(out, link)
		}
	}
	return out
}

// classifyLink decides whether one href is tracked.
func classifyLink(href string) (TrackedLink, bool) {
	u, err := url.Parse(href)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return TrackedLink{}, false
	}
	if flowPattern.MatchString(href) {
		return TrackedLink{}, false
	}
	host := strings.ToLower(u.Hostname())
	link := TrackedLink{URL: href, Host: host}
	if dest := Destination(href); dest != "" {
		link.Destination = dest
		link.Reasons = append(link.Reasons, "redirect")
	}
	if onList(host, clickHosts) {
		link.Reasons = append(link.Reasons, "known-host")
	} else if len(link.Reasons) == 0 && trackingSubdomain(host) && hasOpaqueID(u) {
		link.Reasons = append(link.Reasons, "token")
	}
	return link, len(link.Reasons) > 0
}

// Destination returns the address a redirect wrapper carries in its own url,
// following up to three wrappers, or empty when the url names none. Only a
// destination on another organisation counts: a site linking to itself
// through its own redirect is not passing the click to anybody.
func Destination(href string) string {
	current := href
	found := ""
	for range 3 {
		u, err := url.Parse(current)
		if err != nil || u.Host == "" {
			break
		}
		next := ""
		q := u.Query()
		for _, name := range redirectParams {
			v := strings.TrimSpace(q.Get(name))
			if d, err := url.Parse(v); err == nil && (d.Scheme == "http" || d.Scheme == "https") && d.Host != "" {
				if !sameSite(d.Hostname(), u.Hostname()) {
					next = v
				}
				break
			}
		}
		if next == "" {
			break
		}
		found, current = next, next
	}
	return found
}

// trackingSubdomain reports whether the first label of a host is one click
// trackers are named with, on a host that has a subdomain at all.
func trackingSubdomain(host string) bool {
	reg, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil || reg == host {
		return false
	}
	first, _, _ := strings.Cut(host, ".")
	return trackingLabels[first]
}

func sameSite(a, b string) bool {
	ra, errA := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(a))
	rb, errB := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(b))
	return errA == nil && errB == nil && ra == rb
}

func onList(host string, list []string) bool {
	for _, d := range list {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// cssURLPattern finds url(...) references to http(s) in css text.
var cssURLPattern = regexp.MustCompile(`(?i)url\(\s*["']?\s*(https?://[^"')\s]+)`)

// importPattern finds @import of a remote stylesheet.
var importPattern = regexp.MustCompile(`(?i)@import\s+(?:url\()?\s*["']?\s*(https?://[^"')\s;]+)`)

// fontFacePattern finds the body of each @font-face rule.
var fontFacePattern = regexp.MustCompile(`(?is)@font-face\s*\{[^}]*\}`)

// maxExternal caps the resources reported.
const maxExternal = 100

// ScanExternal lists the remote resources of an html body that are neither
// images nor links: stylesheets, web fonts, forms, frames, media and css
// background images.
func ScanExternal(html string) []ExternalResource {
	var out []ExternalResource
	seen := map[string]bool{}
	add := func(kind, raw string) {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(out) >= maxExternal {
			return
		}
		host := strings.ToLower(u.Hostname())
		if key := kind + "|" + host; !seen[key] {
			seen[key] = true
			out = append(out, ExternalResource{Kind: kind, Host: host})
		}
	}
	css := func(text string) {
		for _, face := range fontFacePattern.FindAllString(text, -1) {
			for _, m := range cssURLPattern.FindAllStringSubmatch(face, -1) {
				add("font", m[1])
			}
		}
		rest := fontFacePattern.ReplaceAllString(text, "")
		for _, m := range importPattern.FindAllStringSubmatch(rest, -1) {
			add("stylesheet", m[1])
		}
		for _, m := range cssURLPattern.FindAllStringSubmatch(importPattern.ReplaceAllString(rest, ""), -1) {
			add("background", m[1])
		}
	}

	z := htmltok.NewTokenizer(strings.NewReader(html))
	inStyle := false
	for {
		tt := z.Next()
		if tt == htmltok.ErrorToken {
			break
		}
		switch tt {
		case htmltok.TextToken:
			if inStyle {
				css(string(z.Text()))
			}
		case htmltok.EndTagToken:
			if name, _ := z.TagName(); string(name) == "style" {
				inStyle = false
			}
		case htmltok.StartTagToken, htmltok.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tag == "style" {
				inStyle = tt == htmltok.StartTagToken
			}
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[strings.ToLower(string(k))] = string(v)
			}
			if style := attrs["style"]; style != "" {
				css(style)
			}
			if bg := attrs["background"]; bg != "" {
				add("background", bg)
			}
			switch tag {
			case "link":
				if strings.Contains(strings.ToLower(attrs["rel"]), "stylesheet") {
					add("stylesheet", attrs["href"])
				}
			case "form":
				add("form", attrs["action"])
			case "iframe", "frame", "embed":
				add("frame", attrs["src"])
			case "object":
				add("frame", attrs["data"])
			case "video", "audio", "source":
				add("media", attrs["src"])
			}
		}
	}
	return out
}

// htmlUnescape decodes the entities an href can carry, such as &amp;.
func htmlUnescape(s string) string {
	return htmltok.UnescapeString(s)
}
