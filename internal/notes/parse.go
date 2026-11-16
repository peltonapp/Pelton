package notes

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// LinkKind says what a link points at.
type LinkKind string

// The kinds of link a note can make.
const (
	// LinkWiki is [[Note]], [[Note|alias]], [[Note#Heading]] or an embed.
	LinkWiki LinkKind = "wiki"
	// LinkMarkdown is [text](other/note.md), a link to a file in the vault.
	LinkMarkdown LinkKind = "markdown"
	// LinkExternal is a Markdown link to a web address or another scheme.
	LinkExternal LinkKind = "external"
	// LinkPelton is a Markdown link to a pelton:// object: a mail, an event,
	// a contact or a task.
	LinkPelton LinkKind = "pelton"
)

// Heading is one ATX heading. Line counts from 0.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

// Link is one link in a note. Target is what the link names without the part
// after '#' (decoded for a Markdown link), Subpath that part. Start and End
// span the whole link in bytes; TStart and TEnd span the target as written,
// which is what a rename rewrites.
type Link struct {
	Kind    LinkKind
	Embed   bool
	Target  string
	Subpath string
	Display string
	Line    int
	Start   int
	End     int
	TStart  int
	TEnd    int
	// Angle is set for a Markdown link written as [x](<target>).
	Angle bool
}

// Property is one front matter key with its value, or its items when the
// value is a list.
type Property struct {
	Key   string   `json:"key"`
	Value string   `json:"value"`
	List  []string `json:"list"`
}

// Parsed is what a note's text holds.
type Parsed struct {
	// BodyStart is the byte offset the text after the front matter starts at.
	BodyStart int
	Props     []Property
	Tags      []string
	Aliases   []string
	Headings  []Heading
	Links     []Link
}

// bom is the UTF-8 byte order mark some editors start a file with.
const bom = "\xef\xbb\xbf"

var (
	headingRe = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	fenceRe   = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	closingRe = regexp.MustCompile(`[ \t]+#+$`)
)

// Parse reads a note's text. It never fails: text it does not understand is
// simply not a heading, a tag or a link.
func Parse(text string) Parsed {
	var p Parsed
	p.BodyStart, p.Props = frontMatter(text)
	seenTag := map[string]bool{}
	addTag := func(tag string) {
		tag = strings.TrimPrefix(strings.TrimSpace(tag), "#")
		if tag == "" || !validTag(tag) {
			return
		}
		key := strings.ToLower(tag)
		if !seenTag[key] {
			seenTag[key] = true
			p.Tags = append(p.Tags, tag)
		}
	}
	for _, prop := range p.Props {
		switch strings.ToLower(prop.Key) {
		case "tags", "tag":
			for _, t := range propItems(prop) {
				addTag(t)
			}
		case "aliases", "alias":
			p.Aliases = append(p.Aliases, propItems(prop)...)
		}
	}

	line := strings.Count(text[:p.BodyStart], "\n")
	fence := ""
	pos := p.BodyStart
	for pos <= len(text) {
		end := strings.IndexByte(text[pos:], '\n')
		var raw string
		if end < 0 {
			raw = text[pos:]
			end = len(text)
		} else {
			raw = text[pos : pos+end]
			end = pos + end
		}
		content := strings.TrimSuffix(raw, "\r")
		if m := fenceRe.FindStringSubmatch(content); m != nil {
			marker := m[1]
			switch {
			case fence == "":
				fence = marker
			case marker[0] == fence[0] && len(marker) >= len(fence) && strings.TrimSpace(content[len(m[0]):]) == "":
				fence = ""
			}
		} else if fence == "" {
			if m := headingRe.FindStringSubmatch(content); m != nil {
				htext := closingRe.ReplaceAllString(m[2], "")
				if strings.Trim(m[2], "# \t") == "" {
					htext = ""
				}
				p.Headings = append(p.Headings, Heading{Level: len(m[1]), Text: strings.TrimSpace(htext), Line: line})
			}
			scanInline(content, pos, line, &p, addTag)
		}
		if end >= len(text) {
			break
		}
		pos = end + 1
		line++
	}
	return p
}

// frontMatter finds a YAML block at the very top of a note and reads its
// simple key: value and list entries. It returns where the body starts.
func frontMatter(text string) (int, []Property) {
	start := 0
	if strings.HasPrefix(text, bom) {
		start = len(bom)
	}
	first, rest, ok := strings.Cut(text[start:], "\n")
	if !ok || strings.TrimRight(first, " \t\r") != "---" {
		return 0, nil
	}
	offset := start + len(first) + 1
	var lines []string
	for {
		l, after, found := strings.Cut(rest, "\n")
		trimmed := strings.TrimRight(l, " \t\r")
		if trimmed == "---" || trimmed == "..." {
			end := offset + len(l)
			if found {
				end++
			}
			return end, parseYAML(lines)
		}
		if !found {
			return 0, nil
		}
		lines = append(lines, strings.TrimSuffix(l, "\r"))
		offset += len(l) + 1
		rest = after
	}
}

// parseYAML reads the subset of YAML front matter holds in practice: top
// level keys with a scalar, an inline [a, b] list or a block list of "- item"
// lines. Anything nested deeper is kept as its raw text.
func parseYAML(lines []string) []Property {
	var out []Property
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") || l[0] == ' ' || l[0] == '\t' || l[0] == '-' {
			continue
		}
		key, value, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		prop := Property{Key: strings.TrimSpace(key)}
		value = strings.TrimSpace(value)
		switch {
		case strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]"):
			prop.List = []string{}
			for _, item := range strings.Split(value[1:len(value)-1], ",") {
				if item = unquote(strings.TrimSpace(item)); item != "" {
					prop.List = append(prop.List, item)
				}
			}
		case value == "":
			var items []string
			var raw []string
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t") || strings.HasPrefix(lines[i+1], "-")) {
				i++
				t := strings.TrimSpace(lines[i])
				raw = append(raw, t)
				if strings.HasPrefix(t, "- ") || t == "-" {
					if item := unquote(strings.TrimSpace(strings.TrimPrefix(t, "-"))); item != "" {
						items = append(items, item)
					}
				}
			}
			if len(items) > 0 {
				prop.List = items
			} else {
				prop.Value = strings.Join(raw, " ")
			}
		default:
			prop.Value = unquote(value)
		}
		out = append(out, prop)
	}
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// propItems is a property as a list: its items, or its value split on commas
// and spaces the way Obsidian reads a tags string.
func propItems(p Property) []string {
	if p.List != nil {
		return p.List
	}
	var out []string
	for _, f := range strings.FieldsFunc(p.Value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		out = append(out, f)
	}
	return out
}

// validTag reports whether a tag body is one Obsidian would accept: letters,
// digits, _, - and / for nesting, and not only digits.
func validTag(tag string) bool {
	digits := true
	for _, r := range tag {
		if !isTagRune(r) {
			return false
		}
		if !unicode.IsDigit(r) {
			digits = false
		}
	}
	return !digits
}

func isTagRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '/' || unicode.Is(unicode.Mn, r)
}

// scanInline finds links and tags in one line outside code spans. base is the
// line's byte offset in the note.
func scanInline(s string, base, line int, p *Parsed, addTag func(string)) {
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			i += 2
			continue
		case c == '`':
			n := run(s, i, '`')
			if close := strings.Index(s[i+n:], strings.Repeat("`", n)); close >= 0 {
				i += n + close + n
			} else {
				i += n
			}
			continue
		case c == '[' && strings.HasPrefix(s[i:], "[["):
			embed := i > 0 && s[i-1] == '!'
			if l, ok := wikiLink(s, i, embed); ok {
				l.Line = line
				shift(&l, base)
				p.Links = append(p.Links, l)
				i = l.End - base
				continue
			}
		case c == '[':
			embed := i > 0 && s[i-1] == '!'
			if l, ok := markdownLink(s, i, embed); ok {
				l.Line = line
				shift(&l, base)
				p.Links = append(p.Links, l)
				i = l.End - base
				continue
			}
		case c == '#' && (i == 0 || unicode.IsSpace(rune(s[i-1])) || s[i-1] == '(' || s[i-1] == ','):
			j := i + 1
			for j < len(s) {
				r, size := utf8.DecodeRuneInString(s[j:])
				if !isTagRune(r) {
					break
				}
				j += size
			}
			if j > i+1 {
				addTag(s[i+1 : j])
				i = j
				continue
			}
		}
		i++
	}
}

func run(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

func shift(l *Link, base int) {
	l.Start += base
	l.End += base
	l.TStart += base
	l.TEnd += base
}

// wikiLink reads [[target#sub|alias]] starting at the first bracket.
func wikiLink(s string, i int, embed bool) (Link, bool) {
	inner := i + 2
	close := strings.Index(s[inner:], "]]")
	if close <= 0 {
		return Link{}, false
	}
	body := s[inner : inner+close]
	if strings.Contains(body, "[[") {
		return Link{}, false
	}
	l := Link{Kind: LinkWiki, Embed: embed, Start: i, End: inner + close + 2}
	if embed {
		l.Start = i - 1
	}
	target := body
	if bar := strings.IndexByte(body, '|'); bar >= 0 {
		target = body[:bar]
		l.Display = strings.TrimSpace(body[bar+1:])
		// inside a table the bar is escaped as \|.
		target = strings.TrimSuffix(target, "\\")
	}
	tEnd := inner + len(target)
	name := target
	if hash := strings.IndexByte(target, '#'); hash >= 0 {
		name = target[:hash]
		l.Subpath = strings.TrimSpace(target[hash+1:])
		tEnd = inner + hash
	}
	l.TStart = inner
	l.TEnd = tEnd
	l.Target = strings.TrimSpace(name)
	if l.Target == "" && l.Subpath == "" {
		return Link{}, false
	}
	return l, true
}

// markdownLink reads [text](url) starting at the opening bracket.
func markdownLink(s string, i int, embed bool) (Link, bool) {
	depth := 0
	j := i
	for ; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
		}
		if depth == 0 {
			break
		}
	}
	if j >= len(s) || j+1 >= len(s) || s[j+1] != '(' {
		return Link{}, false
	}
	text := s[i+1 : j]
	open := j + 2
	l := Link{Embed: embed, Start: i, Display: text}
	if embed {
		l.Start = i - 1
	}
	var raw string
	if open < len(s) && s[open] == '<' {
		close := strings.IndexByte(s[open:], '>')
		if close < 0 {
			return Link{}, false
		}
		l.Angle = true
		l.TStart = open + 1
		raw = s[open+1 : open+close]
		rest := s[open+close+1:]
		paren := strings.IndexByte(rest, ')')
		if paren < 0 {
			return Link{}, false
		}
		l.End = open + close + 1 + paren + 1
	} else {
		k := open
		depth := 0
		for k < len(s) {
			c := s[k]
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth == 0 {
					break
				}
				depth--
			} else if c == ' ' || c == '\t' {
				break
			}
			k++
		}
		raw = s[open:k]
		l.TStart = open
		paren := strings.IndexByte(s[k:], ')')
		if paren < 0 {
			return Link{}, false
		}
		l.End = k + paren + 1
	}
	if raw == "" {
		return Link{}, false
	}
	l.TEnd = l.TStart + len(raw)
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "pelton://"):
		l.Kind = LinkPelton
		l.Target = raw
		return l, true
	case hasScheme(lower) || strings.HasPrefix(lower, "#"):
		l.Kind = LinkExternal
		l.Target = raw
		return l, true
	}
	l.Kind = LinkMarkdown
	name := raw
	if hash := strings.IndexByte(raw, '#'); hash >= 0 {
		name = raw[:hash]
		l.Subpath = raw[hash+1:]
		l.TEnd = l.TStart + hash
	}
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	if dec, err := url.PathUnescape(l.Subpath); err == nil {
		l.Subpath = dec
	}
	l.Target = name
	return l, true
}

var schemeRe = regexp.MustCompile(`^[a-z][a-z0-9+.-]*:`)

func hasScheme(s string) bool {
	return schemeRe.MatchString(s)
}

// Title is the name a note goes by: its file name without the extension.
func Title(rel string) string {
	base := path.Base(rel)
	return strings.TrimSuffix(base, path.Ext(base))
}

// PlainBody is the searchable text of a note: everything after the front
// matter.
func PlainBody(text string, p Parsed) string {
	return text[p.BodyStart:]
}
