package feeds

import (
	"bytes"
	"encoding/xml"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/peltonapp/Pelton/internal/charsetguess"
)

// Namespaces the parsers look for. A prefix the document never declared stays
// as the prefix itself, so each is matched by its usual prefix too.
const (
	nsAtom    = "http://www.w3.org/2005/Atom"
	nsContent = "http://purl.org/rss/1.0/modules/content/"
	nsDC      = "http://purl.org/dc/elements/1.1/"
	nsMedia   = "http://search.yahoo.com/mrss/"
	nsITunes  = "http://www.itunes.com/dtds/podcast-1.0.dtd"
	nsRDF     = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	nsRSS1    = "http://purl.org/rss/1.0/"
	nsXML     = "http://www.w3.org/XML/1998/namespace"
	nsXHTML   = "http://www.w3.org/1999/xhtml"
)

var nsPrefixes = map[string]string{
	nsAtom:    "atom",
	nsContent: "content",
	nsDC:      "dc",
	nsMedia:   "media",
	nsITunes:  "itunes",
	nsRDF:     "rdf",
	nsXML:     "xml",
}

// node is an element of a parsed document, or a run of text when name is
// empty. Children keep the order they had, so mixed content such as inline
// xhtml can be written back out.
type node struct {
	space, local string
	attrs        []xml.Attr
	text         string
	kids         []*node
	// base is the url relative references inside this element resolve
	// against: the document's own, or what xml:base made of it.
	base *url.URL
}

// is reports whether the element has the local name and lives in the
// namespace. ns "" matches an element in no namespace or in the default
// namespace of the formats that use one (Atom, RSS 1.0).
func (n *node) is(ns, local string) bool {
	if n == nil || n.local == "" || !strings.EqualFold(n.local, local) {
		return false
	}
	if ns == "" {
		return n.space == "" || n.space == nsAtom || n.space == nsRSS1 || n.space == nsRDF
	}
	return n.space == ns || n.space == nsPrefixes[ns]
}

// child returns the first child element matching ns and local.
func (n *node) child(ns, local string) *node {
	for _, k := range n.kids {
		if k.is(ns, local) {
			return k
		}
	}
	return nil
}

// children returns every child element matching ns and local.
func (n *node) children(ns, local string) []*node {
	var out []*node
	for _, k := range n.kids {
		if k.is(ns, local) {
			out = append(out, k)
		}
	}
	return out
}

// attr returns an attribute in no namespace.
func (n *node) attr(name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.attrs {
		if (a.Name.Space == "" || a.Name.Space == n.space) && strings.EqualFold(a.Name.Local, name) {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}

// value is the text directly inside the element, CDATA included.
func (n *node) value() string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	for _, k := range n.kids {
		if k.local == "" {
			b.WriteString(k.text)
		}
	}
	return strings.TrimSpace(b.String())
}

// childValue is the text of the first matching child, or "".
func (n *node) childValue(ns, local string) string {
	if n == nil {
		return ""
	}
	return n.child(ns, local).value()
}

// inner writes the element's children back out as html, for Atom's inline
// xhtml content. Namespaces are dropped; the result is sanitized later anyway.
func (n *node) inner() string {
	var b strings.Builder
	for _, k := range n.kids {
		k.render(&b)
	}
	return strings.TrimSpace(b.String())
}

func (n *node) render(b *strings.Builder) {
	if n.local == "" {
		b.WriteString(html.EscapeString(n.text))
		return
	}
	name := strings.ToLower(n.local)
	b.WriteByte('<')
	b.WriteString(name)
	for _, a := range n.attrs {
		if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" || a.Name.Space == nsXML {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(a.Name.Local)
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(a.Value))
		b.WriteByte('"')
	}
	b.WriteByte('>')
	if voidElements[name] {
		return
	}
	for _, k := range n.kids {
		k.render(b)
	}
	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
	"img": true, "input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// autoClose are the html void elements the lenient decoder closes by itself
// when raw html sits unescaped in a feed. It is xml.HTMLAutoClose without link
// and meta: in RSS, link is an element with content.
var autoClose = []string{"area", "base", "basefont", "br", "col", "frame", "hr", "img", "input", "isindex", "param", "wbr"}

// maxDepth stops a document nested past any sensible structure from growing
// the tree without bound.
const maxDepth = 256

// encodingDecl finds the encoding the xml declaration names.
var encodingDecl = regexp.MustCompile(`^\s*<\?xml[^>]*?encoding\s*=\s*["']([^"']*)["']`)

// prepareXML trims what comes before the first tag and settles the encoding
// question: text that is not utf-8 but says nothing (or says utf-8) is decoded
// by detection, and a document that names a single-byte charset while being
// valid utf-8 with non-ascii text in it is read as the utf-8 it is.
func prepareXML(data []byte) []byte {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if i := bytes.IndexByte(data, '<'); i > 0 {
		data = data[i:]
	}
	m := encodingDecl.FindSubmatchIndex(data)
	declared := ""
	if m != nil {
		declared = strings.ToLower(strings.TrimSpace(string(data[m[2]:m[3]])))
	}
	isUTF8 := declared == "" || declared == "utf-8" || declared == "utf8"
	switch {
	case isUTF8 && !utf8.Valid(data):
		text, _ := charsetguess.Decode(data)
		data = withEncoding([]byte(text), m)
	case !isUTF8 && utf8.Valid(data) && hasNonASCII(data):
		data = withEncoding(data, m)
	case !isUTF8 && !charsetguess.Known(declared):
		text, _ := charsetguess.Decode(data)
		data = withEncoding([]byte(text), m)
	}
	return data
}

// withEncoding rewrites the declared encoding to utf-8. m is the match of
// encodingDecl on the original bytes, which a decode may have shifted, so it
// is looked up again.
func withEncoding(data []byte, m []int) []byte {
	if m == nil {
		return data
	}
	again := encodingDecl.FindSubmatchIndex(data)
	if again == nil {
		return data
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:again[2]]...)
	out = append(out, "UTF-8"...)
	out = append(out, data[again[3]:]...)
	return out
}

func hasNonASCII(data []byte) bool {
	for _, c := range data {
		if c >= 0x80 {
			return true
		}
	}
	return false
}

// parseTree reads an xml document into nodes. The decoder runs in its lenient
// mode, which takes html entities and unclosed html elements in its stride,
// and an error after the root element has started keeps what was read so far:
// a feed cut off halfway still has its first entries.
func parseTree(data []byte, docURL *url.URL) (*node, error) {
	d := xml.NewDecoder(bytes.NewReader(prepareXML(data)))
	d.Strict = false
	d.AutoClose = autoClose
	d.Entity = xml.HTMLEntity
	d.CharsetReader = charsetguess.Reader

	var root *node
	var stack []*node
	depth := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if root != nil {
				break
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > maxDepth || len(stack) != depth-1 || (depth == 1 && root != nil) {
				continue
			}
			base := docURL
			if len(stack) > 0 {
				base = stack[len(stack)-1].base
			}
			for _, a := range t.Attr {
				if (a.Name.Space == nsXML || a.Name.Space == "xml") && a.Name.Local == "base" {
					if u, err := url.Parse(strings.TrimSpace(a.Value)); err == nil {
						if base != nil {
							u = base.ResolveReference(u)
						}
						if u.IsAbs() {
							base = u
						}
					}
				}
			}
			n := &node{space: t.Name.Space, local: t.Name.Local, attrs: t.Attr, base: base}
			if depth == 1 {
				root = n
			} else if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.kids = append(parent.kids, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if depth <= maxDepth && len(stack) > 0 && len(stack) == depth {
				stack = stack[:len(stack)-1]
			}
			if depth > 0 {
				depth--
			}
		case xml.CharData:
			if len(stack) > 0 && len(stack) == depth {
				parent := stack[len(stack)-1]
				parent.kids = append(parent.kids, &node{text: string(t)})
			}
		}
	}
	if root == nil {
		return nil, ErrNotFeed
	}
	return root, nil
}
