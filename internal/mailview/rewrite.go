package mailview

import (
	"strings"

	htmltok "golang.org/x/net/html"
)

// The remote content and tracker filters read the html with the tokenizer, the
// same reading the sanitizer and the browser make, and write it back out token
// by token. Cutting matches out of the raw text instead let what was left on
// either side of a cut join into markup that had not been there before: "ht",
// a removed tag, then "tps://..." closing up into a fresh https url, or an
// attribute cut out of the middle of a src doing the same. Rebuilt from
// tokens, text is escaped on the way out and a removed tag takes nothing with
// it but itself, so nothing new can form.

// rawTextElements are the elements whose content the tokenizer passes through
// unparsed. That content is written back as it came, not escaped.
var rawTextElements = map[string]bool{
	"iframe": true, "noembed": true, "noframes": true, "noscript": true, "plaintext": true,
	"script": true, "style": true, "textarea": true, "title": true, "xmp": true,
}

// filter says what rewrite takes out.
type filter struct {
	// dropImage removes an img whose source it returns true for.
	dropImage func(src string) bool
	// remoteOff also removes everything else that would load from a server:
	// remote background attributes, remote source elements, and whole video and
	// audio elements that would stream from one.
	remoteOff bool
}

// imageSource is a src attribute value read the way a browser reads a url:
// tabs and line breaks anywhere are dropped, and spaces and control characters
// at either end are trimmed. The tokenizer has already decoded entities.
func imageSource(v string) string {
	v = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, v)
	return strings.TrimFunc(v, func(r rune) bool { return r <= ' ' })
}

// isRemoteSource reports whether a source loads from a server. A
// protocol-relative "//host/path" counts: it borrows the page's scheme.
func isRemoteSource(src string) bool {
	lower := strings.ToLower(src)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//")
}

func attrValue(tok htmltok.Token, name string) string {
	for _, a := range tok.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			return imageSource(a.Val)
		}
	}
	return ""
}

// rewrite applies f to html.
func rewrite(html string, f filter) string {
	z := htmltok.NewTokenizer(strings.NewReader(html))
	var out strings.Builder
	out.Grow(len(html))

	// an open video or audio element is held back until its end tag, since a
	// remote source further in decides whether any of it is written.
	var held strings.Builder
	holding := ""
	depth := 0
	heldRemote := false

	write := func(s string) {
		if holding != "" {
			held.WriteString(s)
		} else {
			out.WriteString(s)
		}
	}

	raw := false
	for {
		tt := z.Next()
		if tt == htmltok.ErrorToken {
			break
		}
		if tt == htmltok.TextToken {
			if raw {
				write(string(z.Raw()))
			} else {
				write(htmltok.EscapeString(string(z.Text())))
			}
			raw = false
			continue
		}
		raw = false
		tok := z.Token()

		switch tt {
		case htmltok.StartTagToken, htmltok.SelfClosingTagToken:
			name := tok.Data
			src := attrValue(tok, "src")
			if name == "img" && f.dropImage != nil && f.dropImage(src) {
				continue
			}
			if f.remoteOff {
				if name == "source" && isRemoteSource(src) {
					if holding != "" {
						heldRemote = true
					}
					continue
				}
				if name == "video" || name == "audio" {
					if tt == htmltok.SelfClosingTagToken {
						if holding != "" && isRemoteSource(src) {
							heldRemote = true
						}
						if holding == "" && isRemoteSource(src) {
							continue
						}
					} else if holding == "" {
						holding, depth, heldRemote = name, 1, isRemoteSource(src)
						held.Reset()
					} else {
						if name == holding {
							depth++
						}
						if isRemoteSource(src) {
							heldRemote = true
						}
					}
				}
				tok.Attr = withoutRemoteBackground(tok.Attr)
			}
			if tt == htmltok.StartTagToken && rawTextElements[name] {
				raw = true
			}
			write(tok.String())
		case htmltok.EndTagToken:
			write(tok.String())
			if holding != "" && tok.Data == holding {
				depth--
				if depth == 0 {
					if !heldRemote {
						out.WriteString(held.String())
					}
					holding = ""
					held.Reset()
				}
			}
		default:
			write(tok.String())
		}
	}
	// an element never closed is written only if nothing in it was remote.
	if holding != "" && !heldRemote {
		out.WriteString(held.String())
	}
	return out.String()
}

// withoutRemoteBackground drops a background attribute that would load an
// image from a server, the old table-cell way of doing it.
func withoutRemoteBackground(attrs []htmltok.Attribute) []htmltok.Attribute {
	kept := attrs[:0]
	for _, a := range attrs {
		if strings.EqualFold(a.Key, "background") && isRemoteSource(imageSource(a.Val)) {
			continue
		}
		kept = append(kept, a)
	}
	return kept
}

// stripRemote removes everything in html that would load from a server. It
// backs the blocked view; see Sanitize for why the policy cannot do this
// itself.
func stripRemote(html string) string {
	if html == "" {
		return html
	}
	return rewrite(html, filter{dropImage: isRemoteSource, remoteOff: true})
}
