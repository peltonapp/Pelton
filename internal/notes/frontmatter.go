package notes

import (
	"net/url"
	"regexp"
	"strings"
)

// The front matter keys that put a note in a project: the project's name,
// for people and other apps, and its uid, which is what Pelton goes by.
const (
	PropProject   = "project"
	PropProjectID = "project-id"
)

// SetProperties sets the scalar front matter properties in set and removes
// the ones named in remove, changing nothing else in the text: other keys,
// comments, their order and quoting, the line endings and a byte order mark
// stay as they were byte for byte. A key that is already there with the same
// value keeps its line. New keys go at the end of the block; a note without
// front matter gets a block at the top when there is something to set.
func SetProperties(text string, set []Property, remove []string) string {
	lead := ""
	if strings.HasPrefix(text, bom) {
		lead = bom
	}
	nl := "\n"
	if i := strings.IndexByte(text, '\n'); i > 0 && text[i-1] == '\r' {
		nl = "\r\n"
	}
	want := map[string]int{}
	for i, p := range set {
		want[strings.ToLower(p.Key)] = i
	}
	gone := map[string]bool{}
	for _, k := range remove {
		gone[strings.ToLower(k)] = true
	}
	end, _ := frontMatter(text)
	if end == 0 {
		if len(set) == 0 {
			return text
		}
		var b strings.Builder
		b.WriteString(lead)
		b.WriteString("---" + nl)
		for _, p := range set {
			b.WriteString(p.Key + ": " + yamlScalar(p.Value) + nl)
		}
		b.WriteString("---" + nl)
		b.WriteString(text[len(lead):])
		return b.String()
	}
	lines := strings.SplitAfter(text[len(lead):end], "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	b.WriteString(lead)
	b.WriteString(lines[0])
	done := map[int]bool{}
	body := lines[1 : len(lines)-1]
	for i := 0; i < len(body); i++ {
		line := body[i]
		key, ok := topKey(line)
		if !ok {
			b.WriteString(line)
			continue
		}
		lower := strings.ToLower(key)
		idx, setting := want[lower]
		if !setting && !gone[lower] {
			b.WriteString(line)
			continue
		}
		// the key's own value may run over the lines below it.
		j := i + 1
		for j < len(body) && continues(body[j]) {
			j++
		}
		if setting && !done[idx] {
			done[idx] = true
			p := set[idx]
			if j == i+1 && scalarOf(line) == p.Value {
				b.WriteString(line)
			} else {
				b.WriteString(p.Key + ": " + yamlScalar(p.Value) + lineEnd(line, nl))
			}
		}
		i = j - 1
	}
	for i, p := range set {
		if !done[i] {
			b.WriteString(p.Key + ": " + yamlScalar(p.Value) + nl)
		}
	}
	b.WriteString(lines[len(lines)-1])
	b.WriteString(text[end:])
	return b.String()
}

// topKey returns the key of a top level "key: value" line of front matter.
func topKey(line string) (string, bool) {
	t := strings.TrimRight(line, "\r\n")
	if t == "" || t[0] == ' ' || t[0] == '\t' || t[0] == '-' || t[0] == '#' {
		return "", false
	}
	key, _, ok := strings.Cut(t, ":")
	key = strings.TrimSpace(key)
	return key, ok && key != ""
}

// continues reports whether a front matter line belongs to the value of the
// key above it: an indented line or a list item.
func continues(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "-")
}

func scalarOf(line string) string {
	_, v, _ := strings.Cut(strings.TrimRight(line, "\r\n"), ":")
	return unquote(strings.TrimSpace(v))
}

func lineEnd(line, fallback string) string {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(line, "\n"):
		return "\n"
	}
	return fallback
}

var plainScalar = regexp.MustCompile(`^\p{L}[\p{L}\p{N} _./()&+-]*$`)

// yamlScalar writes a value so YAML reads it back as the same string: plain
// when that is safe, double-quoted otherwise.
func yamlScalar(v string) string {
	switch strings.ToLower(v) {
	case "true", "false", "yes", "no", "on", "off", "null", "y", "n":
		return `"` + v + `"`
	}
	if plainScalar.MatchString(v) && strings.TrimSpace(v) == v {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}

// Prop returns the value of a front matter property by key, ignoring case;
// a list gives its first item.
func Prop(props []Property, key string) string {
	for _, p := range props {
		if strings.EqualFold(strings.TrimSpace(p.Key), key) {
			if p.List != nil {
				if len(p.List) > 0 {
					return strings.TrimSpace(p.List[0])
				}
				return ""
			}
			return strings.TrimSpace(p.Value)
		}
	}
	return ""
}

// ProjectOf reads which project a note's front matter puts it in: the uid
// from project-id, and the name from project with any [[ ]] around it.
func ProjectOf(props []Property) (uid, name string) {
	name = Prop(props, PropProject)
	name = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(name, "[["), "]]"))
	return Prop(props, PropProjectID), name
}

// InProject reports whether front matter puts a note in the project with
// this uid and name: by project-id, or by name alone when it has no id.
func InProject(props []Property, uid, name string) bool {
	id, n := ProjectOf(props)
	if id != "" {
		return id == uid
	}
	return n != "" && strings.EqualFold(n, strings.TrimSpace(name))
}

// ParsePeltonURL reads a pelton://kind/id link, with an optional second part
// after the id (pelton://project/<uid>/<card task uid>). Both parts are
// percent-decoded.
func ParsePeltonURL(raw string) (kind, id, sub string, ok bool) {
	s := strings.TrimSpace(raw)
	const scheme = "pelton://"
	if len(s) < len(scheme) || !strings.EqualFold(s[:len(scheme)], scheme) {
		return "", "", "", false
	}
	kind, rest, found := strings.Cut(s[len(scheme):], "/")
	if !found || kind == "" || rest == "" {
		return "", "", "", false
	}
	first, second, _ := strings.Cut(rest, "/")
	var err error
	if id, err = url.PathUnescape(first); err != nil || id == "" {
		return "", "", "", false
	}
	if second != "" {
		if sub, err = url.PathUnescape(second); err != nil {
			return "", "", "", false
		}
	}
	return strings.ToLower(kind), id, sub, true
}

var unsafeName = regexp.MustCompile(`[\\/:*?"<>|#^\[\]]`)

// SafeName turns a title into a file or folder name every platform takes,
// without an extension: characters some file system or link syntax refuses
// become spaces, and a leading dot goes, so the name is never hidden.
func SafeName(title string) string {
	s := strings.Join(strings.Fields(unsafeName.ReplaceAllString(title, " ")), " ")
	s = strings.TrimLeft(s, ".")
	if s = strings.TrimSpace(s); s == "" {
		return "Untitled"
	}
	return s
}
