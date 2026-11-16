package notes

import (
	"net/url"
	"path"
	"sort"
	"strings"
)

// RewriteLinks updates the links in one note after files moved. moves maps
// each old vault path to its new one. before resolves links as they were
// written, after knows the files at their new places; noteBefore and
// noteAfter are where this note itself was and is. Only the bytes naming a
// moved target change: aliases, headings, the rest of the line and the rest
// of the file stay exactly as they were. It returns the new text and how many
// links changed.
func RewriteLinks(text, noteBefore, noteAfter string, before, after *Resolver, moves map[string]string) (string, int) {
	p := Parse(text)
	type edit struct {
		start, end int
		with       string
	}
	var edits []edit
	noteMoved := noteBefore != noteAfter
	for _, l := range p.Links {
		raw := text[l.TStart:l.TEnd]
		switch l.Kind {
		case LinkWiki:
			if strings.TrimSpace(l.Target) == "" {
				continue
			}
			old := before.Resolve(l.Target, noteBefore)
			moved, ok := moves[old]
			if old == "" || !ok {
				continue
			}
			trimmed := strings.TrimSpace(raw)
			withExt := strings.HasSuffix(strings.ToLower(trimmed), ".md")
			var next string
			if strings.Contains(trimmed, "/") {
				next = moved
				if IsNote(moved) && !withExt {
					next = strings.TrimSuffix(moved, path.Ext(moved))
				}
			} else {
				next = after.LinkText(moved)
				if withExt && IsNote(moved) && !strings.Contains(next, "/") {
					next += path.Ext(moved)
				}
			}
			if next != trimmed {
				edits = append(edits, edit{l.TStart, l.TEnd, next})
			}
		case LinkMarkdown:
			old := before.ResolveMarkdown(l.Target, noteBefore)
			if old == "" {
				continue
			}
			target := old
			if moved, ok := moves[old]; ok {
				target = moved
			} else if !noteMoved {
				continue
			}
			next := relPath(path.Dir(noteAfter), target)
			decoded := raw
			if d, err := url.PathUnescape(raw); err == nil {
				decoded = d
			}
			if decoded == next {
				continue
			}
			if !l.Angle {
				next = escapePath(next)
			}
			edits = append(edits, edit{l.TStart, l.TEnd, next})
		}
	}
	if len(edits) == 0 {
		return text, 0
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := text
	for _, e := range edits {
		out = out[:e.start] + e.with + out[e.end:]
	}
	return out, len(edits)
}

// relPath is the relative path from folder dir to the vault path to.
func relPath(dir, to string) string {
	if dir == "." || dir == "" {
		return to
	}
	from := strings.Split(dir, "/")
	parts := strings.Split(to, "/")
	i := 0
	for i < len(from) && i < len(parts)-1 && from[i] == parts[i] {
		i++
	}
	up := strings.Repeat("../", len(from)-i)
	return up + strings.Join(parts[i:], "/")
}

// escapePath percent-encodes each segment of a path the way a Markdown link
// needs it, leaving the slashes.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = (&url.URL{Path: s}).EscapedPath()
	}
	return strings.Join(segs, "/")
}
