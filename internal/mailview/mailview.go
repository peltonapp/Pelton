// Package mailview prepares stored mail for safe display in the ui. It owns the
// html sanitization policy, inline image (cid) resolution, a best effort pgp
// status probe and the list snippet. Keeping this here means app.go only
// orchestrates and never embeds rendering or security logic.
//
// Security note: the sanitizer is the single trusted boundary between untrusted
// mail html and the renderer. By default it strips remote resource urls so a
// message cannot phone home (tracking pixels). The caller asks for the remote
// variant explicitly when the user clicks "load remote images".
package mailview

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/microcosm-cc/bluemonday"
	htmltok "golang.org/x/net/html"
)

// PGPStatus is the detected protection state of a received message. It is a best
// effort read of the body, not a verification: it reports what the message
// claims, which the ui shows as an indicator.
type PGPStatus string

const (
	// PGPNone means no pgp markers were found.
	PGPNone PGPStatus = "none"
	// PGPSigned means an inline pgp signed block was found.
	PGPSigned PGPStatus = "signed"
	// PGPEncrypted means an inline pgp encrypted message block was found.
	PGPEncrypted PGPStatus = "encrypted"
)

// snippetLen is how many characters of plain text the list preview keeps.
const snippetLen = 140

// remoteURLPattern finds http(s) resource urls in src/href/style positions. It
// is used only to decide whether a "load remote content" affordance is needed,
// not for sanitization (bluemonday does the actual stripping by url scheme).
var remoteURLPattern = regexp.MustCompile(`(?i)(src|background|href)\s*=\s*["']?\s*https?://|url\(\s*["']?\s*https?://`)

// cidRefPattern matches a cid: url referencing an inline attachment by its
// content id, with or without surrounding quotes.
var cidRefPattern = regexp.MustCompile(`(?i)cid:([^"'>\s)]+)`)

// the policies are built once, one per combination of the two choices that
// change them. They share the ugc base. allowRemote decides which url schemes
// images and links may use: with remote blocked, only data: and cid: image
// sources survive, so http(s) images (and tracking pixels) are dropped
// entirely. allowFonts decides whether the sender's own typefaces survive.
var policies = map[[2]bool]*bluemonday.Policy{
	{false, false}: buildPolicy(false, false),
	{false, true}:  buildPolicy(false, true),
	{true, false}:  buildPolicy(true, false),
	{true, true}:   buildPolicy(true, true),
}

// buildPolicy returns a sanitizer policy. allowRemote decides whether remote
// http(s) resources are permitted; when false only inline data: and cid: image
// sources are kept. allowFonts keeps the font families the message asks for,
// so designed mail reads the way it was written instead of everything arriving
// in the reader's own font.
func buildPolicy(allowRemote, allowFonts bool) *bluemonday.Policy {
	p := bluemonday.UGCPolicy()

	// allow images with sizing/alt but constrain their source scheme below.
	p.AllowImages()
	p.AllowAttrs("width", "height", "alt", "title").OnElements("img")

	// keep simple inline styling that mail relies on.
	p.AllowAttrs("style").Globally()
	// "background" (shorthand) and "background-color" are both kept: many emails
	// design a dark section with light text and set the background this way. if
	// only the text color survived and the background was stripped, that light
	// text landed on our fixed white page and became unreadable. any remote
	// background image a shorthand might carry is still blocked by the iframe CSP
	// (img-src) when remote content is off, so keeping it does not leak.
	p.AllowStyles("color", "background-color", "background", "text-align",
		"font-weight", "font-style", "text-decoration", "font-size", "line-height",
		"margin", "padding", "border", "border-color",
		// direction and unicode-bidi are how html mail says it reads right to
		// left (#356). Stripping them was why Hebrew and Arabic mail arrived
		// left-aligned with its punctuation and numbers out of order: the sender
		// had said which way to read it and the sanitizer removed the answer.
		// Neither can do anything but lay text out.
		"direction", "unicode-bidi").Globally()

	// the dir attribute is the other half of that, and the more common one: it
	// is what a mail client writes on <html>, <body> or a <div> rather than a
	// style. Allowed everywhere, since mail sets it at whatever level it likes.
	p.AllowAttrs("dir").Globally()

	// font-family is the sender's own typeface. It cannot pull anything down:
	// the reading pane's csp limits font-src to data:, so a named family either
	// resolves from a font already on this machine or falls back to the reader's
	// own. Off, the message renders entirely in the reader font.
	if allowFonts {
		p.AllowStyles("font-family").Globally()
	}

	// the legacy bgcolor attribute is the other common way mail sets a section
	// background (on the body and table cells); preserve it for the same
	// readability reason, alongside <font color> so its paired text color stays
	// with the background instead of falling back to our dark default.
	p.AllowAttrs("bgcolor").OnElements("body", "table", "thead", "tbody", "tfoot", "tr", "td", "th")
	p.AllowElements("font")
	if allowFonts {
		p.AllowAttrs("color", "face", "size").OnElements("font")
	} else {
		// face is the legacy spelling of font-family, so it goes with it.
		p.AllowAttrs("color", "size").OnElements("font")
	}

	// video and audio a message embeds itself. autoplay is deliberately not in
	// the list: a message may offer to play, never start on its own. poster is
	// left out too, because bluemonday only scheme-checks src on these elements,
	// so a poster url would reach the renderer unchecked.
	//
	// These are remote content like an image, and they follow the same rule: the
	// url schemes below strip an http(s) source until the reader allows remote
	// content, and Sanitize takes the whole element with it so no dead player is
	// left behind.
	p.AllowElements("video", "audio", "source")
	p.AllowAttrs("controls", "loop", "muted", "preload", "width", "height").OnElements("video")
	p.AllowAttrs("controls", "loop", "muted", "preload").OnElements("audio")
	p.AllowAttrs("src").OnElements("video", "audio", "source")
	p.AllowAttrs("type").OnElements("source")

	// tables are heavily used by html mail.
	p.AllowTables()

	// urls have to be parsed before any scheme rule applies at all.
	// Relative urls go with it: mail has no base to be relative to, so a
	// relative src was never going to resolve to anything.
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(false)

	if allowRemote {
		p.AllowURLSchemes("http", "https", "mailto", "data", "cid")
	} else {
		// no http(s): remote images and remote links by url get stripped. cid and
		// data stay so inline images we resolve ourselves still render.
		p.AllowURLSchemes("mailto", "data", "cid")
	}

	// open links in a new context and never leak the referrer.
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}

// Sanitize cleans untrusted mail html for display. When allowRemote is false
// (the default for first view) remote images are removed. The result is safe to
// inject into a sandboxed renderer.
//
// The removal is done here rather than by the policy's url scheme list, which
// cannot do it: UGCPolicy and AllowImages both call AllowStandardURLs, that
// allows http and https, and AllowURLSchemes only ever adds to the list. The
// blocked policy was therefore keeping every remote src it was documented to
// strip, leaving the reading iframe's img-src csp as the only thing stopping
// them loading. Withdrawing the schemes with a deny policy does work, but it
// applies to links as well, and it would take every href in a message with it
// until the reader loaded images. A link is not a leak: it goes nowhere until
// it is clicked.
// allowFonts keeps the message's own font families; with it off everything
// renders in the reader's font.
func Sanitize(html string, allowRemote, allowFonts bool) string {
	p := policies[[2]bool{allowRemote, allowFonts}]
	if allowRemote {
		return p.Sanitize(html)
	}
	return p.Sanitize(stripRemoteMedia(stripRemoteImages(html)))
}

// HasRemoteContent reports whether the raw html references any remote http(s)
// resource, so the ui can show the "load remote content" affordance only when it
// would actually do something.
func HasRemoteContent(html string) bool {
	return remoteURLPattern.MatchString(html)
}

// remoteHostPattern captures the host of any http(s) url in the body.
var remoteHostPattern = regexp.MustCompile(`(?i)https?://([^/"'\s)>]+)`)

// RemoteHosts returns the unique hosts referenced by remote http(s) urls in the
// body, so the blocked-images banner can show the user exactly where the content
// would be loaded from. The list is capped to keep the banner readable.
func RemoteHosts(html string) []string {
	matches := remoteHostPattern.FindAllStringSubmatch(html, -1)
	seen := make(map[string]bool)
	out := make([]string, 0, 4)
	for _, m := range matches {
		host := strings.ToLower(strings.TrimSpace(m[1]))
		// drop any leading userinfo and trailing port for a clean host.
		if _, after, ok := strings.CutLast(host, "@"); ok {
			host = after
		}
		if colon := strings.IndexByte(host, ':'); colon >= 0 {
			host = host[:colon]
		}
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

// maxLinks caps how many distinct links Links returns, so a message built out
// of thousands of anchors cannot turn one scan into thousands of lookups.
const maxLinks = 100

// anchorHrefPattern captures the href of an anchor, quoted or bare.
var anchorHrefPattern = regexp.MustCompile(`(?i)<a\b[^>]*?\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

// bareURLPattern finds http(s) urls in plain text, where nothing has marked
// them up as links. It matches what the ui's own linkifier treats as a link, so
// a scanned url is the same string the reader sees underlined.
var bareURLPattern = regexp.MustCompile(`(?i)https?://[^\s<>"')\]]+`)

// plainTrailing is sentence punctuation that follows a bare url rather than
// belonging to it. Anchor hrefs are never trimmed this way: there the target is
// stated exactly, and a url that genuinely ends in a bracket would otherwise be
// scanned as a different address than the one the link points at.
const plainTrailing = ".,;:!?"

// Links returns the unique http(s) urls a message points at: anchor targets in
// the html part, and bare urls in the plain text part. Image sources and other
// embedded resources are deliberately excluded, since those are remote content
// (which the sanitizer already handles) rather than something the reader can
// click. Order is the order of first appearance, and the result is capped at
// maxLinks.
//
// Each url is exactly the string the reader can click, so a verdict can be
// matched back to the link it belongs to: an anchor's href verbatim, and for
// plain text the same span the ui's own linkifier underlines.
func Links(html, plain string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 8)

	add := func(raw string, trimTrailing bool) bool {
		url := strings.TrimSpace(stdhtml.UnescapeString(raw))
		if trimTrailing {
			url = strings.TrimRight(url, plainTrailing)
		}
		lower := strings.ToLower(url)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			return true
		}
		if seen[url] {
			return true
		}
		seen[url] = true
		out = append(out, url)
		return len(out) < maxLinks
	}

	for _, m := range anchorHrefPattern.FindAllStringSubmatch(html, -1) {
		// exactly one of the three alternatives matched.
		href := m[1] + m[2] + m[3]
		if !add(href, false) {
			return out
		}
	}
	for _, m := range bareURLPattern.FindAllString(plain, -1) {
		if !add(m, true) {
			return out
		}
	}
	return out
}

// ResolveCIDs rewrites cid: references to the data urls of inline attachments.
// byContentID maps a bare content id (no angle brackets) to a full data url. A
// cid with no matching attachment is left untouched.
func ResolveCIDs(html string, byContentID map[string]string) string {
	if len(byContentID) == 0 {
		return html
	}
	return cidRefPattern.ReplaceAllStringFunc(html, func(match string) string {
		id := strings.TrimPrefix(strings.ToLower(match), "cid:")
		if url, ok := byContentID[id]; ok {
			return url
		}
		return match
	})
}

// ReferencedCIDs returns the set of content ids actually referenced by cid: urls
// in the html, lowercased and without angle brackets. The ui uses it to decide
// which attachments are truly inline (shown in the body) versus real downloadable
// attachments that merely happen to carry a content id. Without this check a
// normal pdf with a content id would be wrongly hidden from the attachment list.
func ReferencedCIDs(html string) map[string]bool {
	out := make(map[string]bool)
	for _, match := range cidRefPattern.FindAllString(html, -1) {
		id := strings.TrimPrefix(strings.ToLower(match), "cid:")
		out[id] = true
	}
	return out
}

// DetectPGP probes the stored bodies for inline pgp markers. It does not verify
// signatures or decrypt; it only reports what is present so the ui shows an
// honest indicator. Proper verification belongs to the crypto layer in a later
// step.
func DetectPGP(plain, html string) PGPStatus {
	hay := plain + "\n" + html
	switch {
	case strings.Contains(hay, "-----BEGIN PGP MESSAGE-----"):
		return PGPEncrypted
	case strings.Contains(hay, "-----BEGIN PGP SIGNED MESSAGE-----"),
		strings.Contains(hay, "-----BEGIN PGP SIGNATURE-----"):
		return PGPSigned
	default:
		return PGPNone
	}
}

// Entity is the readable content of a MIME entity: its text and html bodies.
type Entity struct {
	Text string
	HTML string
}

// ParseEntity reads a standalone MIME entity into its text and html parts.
//
// It exists for decrypted mail. The plaintext inside an encrypted message is a
// MIME entity in its own right, with its own headers, parts and transfer
// encodings, and showing it raw would put MIME source in front of the reader.
// The bodies are returned rather than stored, since caching decrypted content
// would undo the encryption.
func ParseEntity(raw []byte) (Entity, error) {
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) {
		return Entity{}, fmt.Errorf("mailview: read entity: %w", err)
	}

	var out Entity
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			// keep whatever was already recovered: a truncated multipart is
			// better shown in part than discarded.
			break
		}
		header, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		body, err := io.ReadAll(part.Body)
		if err != nil {
			break
		}
		contentType, _, _ := header.ContentType()
		if strings.EqualFold(contentType, "text/html") {
			if out.HTML == "" {
				out.HTML = string(body)
			}
			continue
		}
		if out.Text == "" {
			out.Text = string(body)
		}
	}
	if out.Text == "" && out.HTML == "" {
		return Entity{}, fmt.Errorf("mailview: entity has no readable body")
	}
	return out, nil
}

// skipTextIn are elements whose text content is markup rather than prose, so it
// never belongs in a snippet or the search index.
var skipTextIn = map[string]bool{"script": true, "style": true, "head": true, "title": true}

// PlainText renders html as readable text: element boundaries become
// whitespace, entities are decoded, and script/style content is dropped. It
// exists so html-only mail is still searchable and previewable. Callers get
// prose, not layout: runs of whitespace collapse to a single space.
func PlainText(markup string) string {
	if markup == "" {
		return ""
	}
	z := htmltok.NewTokenizer(strings.NewReader(markup))
	var b strings.Builder
	// depth of nested elements whose text we are discarding.
	skip := 0
	for {
		switch z.Next() {
		case htmltok.ErrorToken:
			// io.EOF or malformed markup: keep whatever was recovered.
			return strings.Join(strings.Fields(b.String()), " ")
		case htmltok.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		case htmltok.StartTagToken:
			name, _ := z.TagName()
			if skipTextIn[string(name)] {
				skip++
			}
			// a tag is a word boundary: without this, adjacent blocks glue into
			// one unsearchable token.
			b.WriteByte(' ')
		case htmltok.EndTagToken:
			name, _ := z.TagName()
			if skipTextIn[string(name)] && skip > 0 {
				skip--
			}
			b.WriteByte(' ')
		case htmltok.SelfClosingTagToken:
			b.WriteByte(' ')
		}
	}
}

// Snippet returns a short plain text preview for the message list. It prefers
// the plain body and falls back to the text of the html body.
func Snippet(plain, html string) string {
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
