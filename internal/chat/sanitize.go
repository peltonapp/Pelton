package chat

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

// htmlPolicy is the sanitiser for message HTML from other people: the app's
// bluemonday, narrowed to the tags and attributes the Matrix spec allows in
// m.room.message formatted bodies. Images survive only with an mxc: source,
// which is media on the user's own homeserver; anything pointing elsewhere
// is dropped, the same rule that blocks remote images in mail.
var htmlPolicy = newHTMLPolicy()

var (
	mxcSource   = regexp.MustCompile(`^mxc://[A-Za-z0-9.:\-\[\]]+/[A-Za-z0-9_\-]+$`)
	hexColor    = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	codeClass   = regexp.MustCompile(`^language-[A-Za-z0-9_+\-]+$`)
	smallNumber = regexp.MustCompile(`^[0-9]{1,4}$`)
)

func newHTMLPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("font", "del", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "p", "a", "ul", "ol",
		"sup", "sub", "li", "b", "i", "u", "strong", "em", "s", "code", "hr", "br", "div", "table", "thead",
		"tbody", "tr", "th", "td", "caption", "pre", "span", "img", "details", "summary")
	p.AllowURLSchemes("https", "http", "mailto", "mxc")
	p.RequireParseableURLs(true)
	p.AllowAttrs("href").OnElements("a")
	p.RequireNoFollowOnLinks(true)
	p.AllowAttrs("data-mx-color", "data-mx-bg-color", "color").Matching(hexColor).OnElements("font", "span")
	p.AllowAttrs("data-mx-spoiler").OnElements("span")
	p.AllowAttrs("start").Matching(smallNumber).OnElements("ol")
	p.AllowAttrs("class").Matching(codeClass).OnElements("code")
	p.AllowAttrs("src").Matching(mxcSource).OnElements("img")
	p.AllowAttrs("width", "height").Matching(smallNumber).OnElements("img")
	p.AllowAttrs("alt", "title").OnElements("img")
	p.AllowAttrs("data-mx-emoticon").OnElements("img")
	return p
}

// sanitizeHTML cleans a formatted body for display.
func sanitizeHTML(html string) string {
	if html == "" {
		return ""
	}
	return htmlPolicy.Sanitize(html)
}
