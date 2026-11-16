package notes

import (
	"path"
	"sort"
	"strings"
)

// Resolver turns what a link names into a file in the vault, the way
// Obsidian does: case does not matter, ".md" may be left off a note, a bare
// name finds the file of that name wherever it is, and when several share it
// the one beside the linking note wins, then the one with the shortest path.
type Resolver struct {
	byKey  map[string][]string
	byPath map[string]string
}

// NewResolver builds a resolver over every file of a vault, notes and
// attachments alike.
func NewResolver(files []string) *Resolver {
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	r := &Resolver{byKey: map[string][]string{}, byPath: map[string]string{}}
	for _, f := range sorted {
		k := fileKey(f)
		r.byPath[k] = f
		base := path.Base(k)
		r.byKey[base] = append(r.byKey[base], f)
	}
	return r
}

// fileKey is the lower-case path a file is looked up by: a note without its
// .md, any other file with its extension.
func fileKey(p string) string {
	lower := strings.ToLower(p)
	if IsNote(p) {
		lower = strings.TrimSuffix(lower, ".md")
	}
	return lower
}

// targetKey normalises what a link names the same way.
func targetKey(target string) string {
	t := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(target, "\\", "/")))
	t = strings.TrimPrefix(t, "/")
	t = strings.TrimPrefix(t, "./")
	return strings.TrimSuffix(t, ".md")
}

// Resolve returns the vault path a link written in note from points at, or
// "" when nothing matches. An empty target is the note itself.
func (r *Resolver) Resolve(target, from string) string {
	key := targetKey(target)
	if key == "" {
		return from
	}
	if strings.Contains(key, "/") {
		if f, ok := r.byPath[path.Clean(key)]; ok {
			return f
		}
		if dir := path.Dir(from); dir != "." {
			if f, ok := r.byPath[path.Clean(strings.ToLower(dir)+"/"+key)]; ok {
				return f
			}
		}
		// a partial path matches any file whose path ends with it.
		var hits []string
		for k, f := range r.byPath {
			if strings.HasSuffix(k, "/"+key) {
				hits = append(hits, f)
			}
		}
		return pick(hits, from)
	}
	return pick(r.byKey[key], from)
}

// ResolveMarkdown resolves the target of a Markdown link, which is a path
// relative to the linking note's folder before it is anything else.
func (r *Resolver) ResolveMarkdown(target, from string) string {
	key := targetKey(target)
	if key == "" {
		return from
	}
	if !strings.HasPrefix(strings.TrimSpace(target), "/") {
		joined := path.Clean(path.Join(strings.ToLower(path.Dir(from)), key))
		if f, ok := r.byPath[joined]; ok {
			return f
		}
	}
	return r.Resolve(target, from)
}

// pick chooses among files sharing a name: the one in the linking note's
// folder, then the shortest path, then the first in order.
func pick(hits []string, from string) string {
	switch len(hits) {
	case 0:
		return ""
	case 1:
		return hits[0]
	}
	dir := path.Dir(from)
	best := ""
	for _, h := range hits {
		if path.Dir(h) == dir {
			return h
		}
		if best == "" || len(h) < len(best) || len(h) == len(best) && h < best {
			best = h
		}
	}
	return best
}

// LinkText is the shortest way to name a file in a wiki link: its bare name
// when no other file shares it, else its full path. A note loses its .md.
func (r *Resolver) LinkText(p string) string {
	name := path.Base(p)
	if IsNote(p) {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	if len(r.byKey[path.Base(fileKey(p))]) <= 1 {
		return name
	}
	if IsNote(p) {
		return strings.TrimSuffix(p, path.Ext(p))
	}
	return p
}
