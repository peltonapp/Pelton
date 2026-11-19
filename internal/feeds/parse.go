package feeds

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Parse reads a feed document of any of the supported formats. feedURL is
// where it was fetched from; relative links in it resolve against that (or
// against the xml:base the document sets). Content and summaries come back
// with their links made absolute but otherwise as the feed sent them: they are
// untrusted html, to be sanitized before display.
func Parse(data []byte, feedURL string) (*Feed, error) {
	base := parseBase(feedURL)
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	if len(trimmed) == 0 {
		return nil, ErrNotFeed
	}
	var (
		f   *Feed
		err error
	)
	if trimmed[0] == '{' {
		f, err = parseJSON(trimmed, base)
	} else {
		var root *node
		root, err = parseTree(trimmed, base)
		if err != nil {
			return nil, ErrNotFeed
		}
		switch {
		case root.is("", "rss"):
			f = parseRSS(root)
		case root.is("", "RDF") || strings.EqualFold(root.local, "rdf"):
			f = parseRDF(root)
		case root.is("", "feed"):
			f = parseAtom(root)
		case root.is("", "channel"):
			f = parseRSSChannel(root)
		default:
			return nil, ErrNotFeed
		}
	}
	if err != nil {
		return nil, err
	}
	f.finish()
	return f, nil
}

// --- RSS 2.0 ------------------------------------------------------------------

func parseRSS(root *node) *Feed {
	ch := root.child("", "channel")
	if ch == nil {
		return &Feed{Kind: "rss"}
	}
	f := parseRSSChannel(ch)
	// a few feeds put their items beside the channel rather than in it.
	for _, it := range root.children("", "item") {
		f.Entries = append(f.Entries, rssItem(it, siteBase(ch.base, f.SiteURL), ""))
	}
	return f
}

func parseRSSChannel(ch *node) *Feed {
	f := &Feed{Kind: "rss"}
	f.Title = ch.childValue("", "title")
	f.SiteURL = resolveURL(ch.base, rssLink(ch))
	f.Description = cleanText(ch.childValue("", "description"))
	if img := ch.child("", "image"); img != nil {
		f.IconURL = resolveURL(ch.base, img.childValue("", "url"))
	}
	if f.IconURL == "" {
		f.IconURL = resolveURL(ch.base, ch.child(nsITunes, "image").attr("href"))
	}
	feedAuthor := firstNonEmpty(ch.childValue(nsITunes, "author"), ch.childValue(nsDC, "creator"))
	base := siteBase(ch.base, f.SiteURL)
	for _, it := range ch.children("", "item") {
		f.Entries = append(f.Entries, rssItem(it, base, feedAuthor))
	}
	return f
}

// rssLink is the channel's or item's own link element: the plain one with its
// url as text, not an atom:link beside it.
func rssLink(n *node) string {
	for _, k := range n.kids {
		if k.local == "link" && (k.space == "" || k.space == nsRSS1) {
			if v := k.value(); v != "" {
				return v
			}
			if href := k.attr("href"); href != "" {
				return href
			}
		}
	}
	for _, k := range n.children(nsAtom, "link") {
		rel := k.attr("rel")
		if rel == "" || rel == "alternate" {
			return k.attr("href")
		}
	}
	return ""
}

// siteBase is what relative urls in content resolve against: the site the
// feed belongs to when it says, else the feed's own address.
func siteBase(feedBase *url.URL, site string) *url.URL {
	if u := parseBase(site); u != nil {
		return u
	}
	return feedBase
}

func rssItem(it *node, base *url.URL, feedAuthor string) Entry {
	if it.base != nil && base != nil && it.base.String() != base.String() {
		base = it.base
	}
	e := Entry{
		Title:   it.childValue("", "title"),
		URL:     resolveURL(base, rssLink(it)),
		Summary: resolveHTML(it.childValue("", "description"), base),
		Content: resolveHTML(it.childValue(nsContent, "encoded"), base),
	}
	if g := it.child("", "guid"); g != nil {
		e.GUID = g.value()
		if e.URL == "" && !strings.EqualFold(g.attr("isPermaLink"), "false") && looksLikeURL(e.GUID) {
			e.URL = resolveURL(base, e.GUID)
		}
	}
	e.Author = firstNonEmpty(it.childValue(nsDC, "creator"), authorName(it.childValue("", "author")), it.childValue(nsITunes, "author"), feedAuthor)
	e.Published = ParseDate(firstNonEmpty(it.childValue("", "pubDate"), it.childValue(nsDC, "date"), it.childValue(nsAtom, "published")))
	e.Updated = ParseDate(it.childValue(nsAtom, "updated"))
	for _, enc := range it.children("", "enclosure") {
		e.Enclosures = append(e.Enclosures, Enclosure{
			URL:    resolveURL(base, enc.attr("url")),
			Type:   enc.attr("type"),
			Length: parseLength(enc.attr("length")),
		})
	}
	readMedia(it, base, &e)
	if e.Image == "" {
		e.Image = resolveURL(base, it.child(nsITunes, "image").attr("href"))
	}
	if e.Content == "" && e.Summary == "" {
		e.Summary = textToHTML(firstNonEmpty(it.childValue(nsITunes, "summary"), it.childValue(nsMedia, "description")))
	}
	return e
}

// readMedia picks up Media RSS: media:content files, a thumbnail, and a
// description for entries with no other text, inside a media:group or not.
func readMedia(n *node, base *url.URL, e *Entry) {
	scopes := append([]*node{n}, n.children(nsMedia, "group")...)
	for _, s := range scopes {
		for _, m := range s.children(nsMedia, "content") {
			u := resolveURL(base, m.attr("url"))
			if u == "" {
				continue
			}
			typ := m.attr("type")
			medium := strings.ToLower(m.attr("medium"))
			if medium == "image" || strings.HasPrefix(typ, "image/") {
				if e.Image == "" {
					e.Image = u
				}
				continue
			}
			if typ == "" && medium != "" {
				typ = medium
			}
			e.Enclosures = append(e.Enclosures, Enclosure{URL: u, Type: typ, Length: parseLength(m.attr("fileSize"))})
			if t := m.child(nsMedia, "thumbnail"); t != nil && e.Image == "" {
				e.Image = resolveURL(base, t.attr("url"))
			}
		}
		if t := s.child(nsMedia, "thumbnail"); t != nil && e.Image == "" {
			e.Image = resolveURL(base, t.attr("url"))
		}
		if e.Summary == "" && e.Content == "" {
			if d := s.child(nsMedia, "description"); d != nil {
				if strings.EqualFold(d.attr("type"), "html") {
					e.Summary = resolveHTML(d.value(), base)
				} else {
					e.Summary = textToHTML(d.value())
				}
			}
		}
		if e.Title == "" {
			e.Title = s.childValue(nsMedia, "title")
		}
	}
}

// authorName takes the name out of RSS's "mail@example.com (Name)" form, or
// returns the value as it is.
func authorName(v string) string {
	v = strings.TrimSpace(v)
	if open := strings.LastIndex(v, "("); open > 0 && strings.HasSuffix(v, ")") {
		if name := strings.TrimSpace(v[open+1 : len(v)-1]); name != "" {
			return name
		}
	}
	return v
}

func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "/")
}

func parseLength(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// --- RSS 1.0 ------------------------------------------------------------------

func parseRDF(root *node) *Feed {
	f := &Feed{Kind: "rdf"}
	ch := root.child("", "channel")
	if ch != nil {
		f.Title = ch.childValue("", "title")
		f.SiteURL = resolveURL(ch.base, ch.childValue("", "link"))
		f.Description = cleanText(ch.childValue("", "description"))
	}
	if img := root.child("", "image"); img != nil {
		f.IconURL = resolveURL(img.base, img.childValue("", "url"))
	}
	base := siteBase(root.base, f.SiteURL)
	for _, it := range root.children("", "item") {
		e := Entry{
			Title:     it.childValue("", "title"),
			URL:       resolveURL(base, it.childValue("", "link")),
			Summary:   resolveHTML(it.childValue("", "description"), base),
			Content:   resolveHTML(it.childValue(nsContent, "encoded"), base),
			Author:    it.childValue(nsDC, "creator"),
			Published: ParseDate(it.childValue(nsDC, "date")),
		}
		for _, a := range it.attrs {
			if a.Name.Local == "about" {
				e.GUID = strings.TrimSpace(a.Value)
			}
		}
		readMedia(it, base, &e)
		f.Entries = append(f.Entries, e)
	}
	return f
}

// --- Atom 1.0 -----------------------------------------------------------------

func parseAtom(root *node) *Feed {
	f := &Feed{Kind: "atom"}
	f.Title = atomText(root.child("", "title"))
	f.Description = atomText(root.child("", "subtitle"))
	f.SiteURL = atomLink(root, "alternate")
	f.IconURL = resolveURL(root.base, firstNonEmpty(root.childValue("", "icon"), root.childValue("", "logo")))
	feedAuthor := atomAuthor(root)
	for _, en := range root.children("", "entry") {
		e := Entry{
			GUID:      en.childValue("", "id"),
			Title:     atomText(en.child("", "title")),
			URL:       atomLink(en, "alternate"),
			Published: ParseDate(firstNonEmpty(en.childValue("", "published"), en.childValue("", "issued"), en.childValue("", "created"))),
			Updated:   ParseDate(firstNonEmpty(en.childValue("", "updated"), en.childValue("", "modified"))),
			Author:    firstNonEmpty(atomAuthor(en), feedAuthor),
		}
		e.Summary = atomHTML(en.child("", "summary"))
		if c := en.child("", "content"); c != nil && c.attr("src") == "" {
			e.Content = atomHTML(c)
		}
		for _, l := range en.children("", "link") {
			if strings.EqualFold(l.attr("rel"), "enclosure") {
				e.Enclosures = append(e.Enclosures, Enclosure{
					URL:    resolveURL(l.base, l.attr("href")),
					Type:   l.attr("type"),
					Length: parseLength(l.attr("length")),
					Title:  l.attr("title"),
				})
			}
		}
		readMedia(en, en.base, &e)
		f.Entries = append(f.Entries, e)
	}
	return f
}

// atomLink returns the href of the first link with the relation, a link with
// no rel counting as alternate, resolved against its own base.
func atomLink(n *node, rel string) string {
	for _, l := range n.children("", "link") {
		r := strings.ToLower(l.attr("rel"))
		if r == rel || (r == "" && rel == "alternate") {
			if href := l.attr("href"); href != "" {
				return resolveURL(l.base, href)
			}
		}
	}
	return ""
}

func atomAuthor(n *node) string {
	var names []string
	for _, a := range n.children("", "author") {
		if name := firstNonEmpty(a.childValue("", "name"), a.childValue("", "email"), a.value()); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

// atomText is an Atom text construct as plain text.
func atomText(n *node) string {
	if n == nil {
		return ""
	}
	if strings.EqualFold(n.attr("type"), "xhtml") {
		return cleanText(n.inner())
	}
	return n.value()
}

// atomHTML is an Atom text construct as html, whatever type it declares.
func atomHTML(n *node) string {
	if n == nil {
		return ""
	}
	switch t := strings.ToLower(n.attr("type")); {
	case t == "xhtml":
		inner := n
		if div := n.child(nsXHTML, "div"); div != nil {
			inner = div
		}
		return resolveHTML(inner.inner(), n.base)
	case t == "html" || t == "text/html":
		return resolveHTML(n.value(), n.base)
	case t == "" || t == "text" || t == "text/plain":
		return textToHTML(n.value())
	default:
		return ""
	}
}

// --- shared -------------------------------------------------------------------

// clampDate keeps entries dated in the future from sitting at the top of
// every list until their day comes: anything more than a day ahead is taken as
// arriving now.
func clampDate(t, now time.Time) time.Time {
	if t.After(now.Add(24 * time.Hour)) {
		return now
	}
	return t
}

// ClampDates applies clampDate to every entry of a parsed feed.
func (f *Feed) ClampDates(now time.Time) {
	for i := range f.Entries {
		f.Entries[i].Published = clampDate(f.Entries[i].Published, now)
		f.Entries[i].Updated = clampDate(f.Entries[i].Updated, now)
	}
}
