package feeds

import (
	"html"
	"net/url"
	"slices"
	"strings"

	htmltok "golang.org/x/net/html"
)

// cleanText turns a title or name into plain text: markup is dropped, entities
// are decoded and runs of whitespace become one space. Feeds put both escaped
// and raw html into fields that are meant to be text.
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, "<&") {
		s = textOf(s)
	}
	return strings.Join(strings.Fields(s), " ")
}

// textOf returns the text of an html fragment.
func textOf(fragment string) string {
	z := htmltok.NewTokenizer(strings.NewReader(fragment))
	var b strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case htmltok.ErrorToken:
			return b.String()
		case htmltok.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		case htmltok.StartTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style":
				skip++
			case "br", "p", "div", "li":
				b.WriteByte(' ')
			}
		case htmltok.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style":
				if skip > 0 {
					skip--
				}
			case "p", "div", "li":
				b.WriteByte(' ')
			}
		}
	}
}

// PlainText returns the text of an html fragment with whitespace collapsed,
// for snippets and the search index.
func PlainText(fragment string) string {
	return strings.Join(strings.Fields(textOf(fragment)), " ")
}

// textToHTML wraps plain text as html, keeping its paragraphs.
func textToHTML(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(strings.ReplaceAll(html.EscapeString(para), "\n", "<br>"))
		b.WriteString("</p>")
	}
	return b.String()
}

// parseBase parses a base url, returning nil when it is not absolute.
func parseBase(raw string) *url.URL {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() {
		return nil
	}
	return u
}

// resolveURL resolves ref against base. An empty ref stays empty, and a ref
// that does not parse is returned trimmed, since a feed's own text is still
// better than nothing.
func resolveURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	if base == nil || u.IsAbs() {
		return u.String()
	}
	return base.ResolveReference(u).String()
}

// urlAttrs are the attributes, by element, that hold a url resolveHTML makes
// absolute.
var urlAttrs = map[string][]string{
	"a":      {"href"},
	"img":    {"src", "srcset"},
	"source": {"src", "srcset"},
	"video":  {"src", "poster"},
	"audio":  {"src"},
	"iframe": {"src"},
	"track":  {"src"},
	"area":   {"href"},
}

// resolveHTML makes the links and sources in an html fragment absolute. A
// relative url in a feed is relative to the feed, and once the html is shown
// somewhere else it would resolve against nothing. Tags without a relative url
// are written back byte for byte.
func resolveHTML(fragment string, base *url.URL) string {
	if base == nil || !strings.Contains(fragment, "<") {
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
		if tt != htmltok.StartTagToken && tt != htmltok.SelfClosingTagToken {
			b.Write(z.Raw())
			continue
		}
		raw := string(z.Raw())
		tok := z.Token()
		names := urlAttrs[tok.Data]
		changed := false
		for i, a := range tok.Attr {
			if a.Namespace != "" || !slices.Contains(names, a.Key) {
				continue
			}
			var next string
			if a.Key == "srcset" {
				next = resolveSrcset(base, a.Val)
			} else {
				next = resolveURL(base, a.Val)
			}
			if next != a.Val {
				tok.Attr[i].Val = next
				changed = true
			}
		}
		if changed {
			b.WriteString(tok.String())
		} else {
			b.WriteString(raw)
		}
	}
}

// resolveSrcset resolves every candidate url of a srcset value.
func resolveSrcset(base *url.URL, v string) string {
	parts := strings.Split(v, ",")
	for i, p := range parts {
		fields := strings.Fields(p)
		if len(fields) == 0 {
			continue
		}
		fields[0] = resolveURL(base, fields[0])
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}
