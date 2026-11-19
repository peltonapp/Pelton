package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// VaultHeading is one heading of an indexed note. Line counts from 0.
type VaultHeading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

// VaultLink is one link an indexed note makes. Kind is wiki, markdown,
// external or pelton; Target is the link as written without its subpath.
type VaultLink struct {
	Kind    string
	Embed   bool
	Target  string
	Subpath string
	Display string
	Line    int
}

// VaultProp is one front matter property of an indexed note: its value, or
// its items when the value is a list.
type VaultProp struct {
	Key   string   `json:"key"`
	Value string   `json:"value"`
	List  []string `json:"list,omitempty"`
}

// VaultNote is the index row of one Markdown note in a vault. Path is
// relative to the vault with forward slashes; MTime is unix nanoseconds.
type VaultNote struct {
	Path     string
	Title    string
	MTime    int64
	Size     int64
	Hash     string
	Headings []VaultHeading
	Aliases  []string
	Tags     []string
	Links    []VaultLink
	Props    []VaultProp
	// Snippet is the opening words of the note's text.
	Snippet string
}

// LoadVaultNotes returns every indexed note of a vault with its tags and
// links. A vault that was never indexed returns no rows.
func (d *DB) LoadVaultNotes(ctx context.Context, vault string) ([]VaultNote, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, path, title, mtime, size, hash, headings, aliases, props, snippet FROM vault_notes WHERE vault = ? ORDER BY id`, vault)
	if err != nil {
		return nil, fmt.Errorf("storage: load vault notes: %w", err)
	}
	byID := map[int64]int{}
	var out []VaultNote
	for rows.Next() {
		var (
			id                       int64
			n                        VaultNote
			headings, aliases, props string
		)
		if err := rows.Scan(&id, &n.Path, &n.Title, &n.MTime, &n.Size, &n.Hash, &headings, &aliases, &props, &n.Snippet); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan vault note: %w", err)
		}
		_ = json.Unmarshal([]byte(headings), &n.Headings)
		_ = json.Unmarshal([]byte(aliases), &n.Aliases)
		_ = json.Unmarshal([]byte(props), &n.Props)
		byID[id] = len(out)
		out = append(out, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate vault notes: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}

	tagRows, err := d.sql.QueryContext(ctx,
		`SELECT t.note_id, t.tag FROM vault_note_tags t JOIN vault_notes n ON n.id = t.note_id WHERE n.vault = ? ORDER BY t.rowid`, vault)
	if err != nil {
		return nil, fmt.Errorf("storage: load vault tags: %w", err)
	}
	for tagRows.Next() {
		var (
			id  int64
			tag string
		)
		if err := tagRows.Scan(&id, &tag); err != nil {
			tagRows.Close()
			return nil, fmt.Errorf("storage: scan vault tag: %w", err)
		}
		if i, ok := byID[id]; ok {
			out[i].Tags = append(out[i].Tags, tag)
		}
	}
	tagRows.Close()

	linkRows, err := d.sql.QueryContext(ctx,
		`SELECT l.note_id, l.kind, l.embed, l.target, l.subpath, l.display, l.line
		 FROM vault_note_links l JOIN vault_notes n ON n.id = l.note_id WHERE n.vault = ? ORDER BY l.rowid`, vault)
	if err != nil {
		return nil, fmt.Errorf("storage: load vault links: %w", err)
	}
	defer linkRows.Close()
	for linkRows.Next() {
		var (
			id    int64
			l     VaultLink
			embed int
		)
		if err := linkRows.Scan(&id, &l.Kind, &embed, &l.Target, &l.Subpath, &l.Display, &l.Line); err != nil {
			return nil, fmt.Errorf("storage: scan vault link: %w", err)
		}
		l.Embed = embed != 0
		if i, ok := byID[id]; ok {
			out[i].Links = append(out[i].Links, l)
		}
	}
	return out, linkRows.Err()
}

// SaveVaultNotes writes the index rows of notes in one transaction, replacing
// any earlier row for the same path along with its tags and links.
func (d *DB) SaveVaultNotes(ctx context.Context, vault string, notes []VaultNote) error {
	if len(notes) == 0 {
		return nil
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin vault save: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, n := range notes {
		headings, _ := json.Marshal(nonNilHeadings(n.Headings))
		aliases, _ := json.Marshal(nonNilStrings(n.Aliases))
		props, _ := json.Marshal(nonNilProps(n.Props))
		var id int64
		err := tx.QueryRowContext(ctx, `
INSERT INTO vault_notes (vault, path, title, mtime, size, hash, headings, aliases, props, snippet)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (vault, path) DO UPDATE SET title = excluded.title, mtime = excluded.mtime,
    size = excluded.size, hash = excluded.hash, headings = excluded.headings, aliases = excluded.aliases,
    props = excluded.props, snippet = excluded.snippet
RETURNING id`, vault, n.Path, n.Title, n.MTime, n.Size, n.Hash, string(headings), string(aliases), string(props), n.Snippet).Scan(&id)
		if err != nil {
			return fmt.Errorf("storage: save vault note %q: %w", n.Path, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault_note_tags WHERE note_id = ?`, id); err != nil {
			return fmt.Errorf("storage: clear vault tags: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM vault_note_links WHERE note_id = ?`, id); err != nil {
			return fmt.Errorf("storage: clear vault links: %w", err)
		}
		for _, tag := range n.Tags {
			if _, err := tx.ExecContext(ctx, `INSERT INTO vault_note_tags (note_id, tag) VALUES (?, ?)`, id, tag); err != nil {
				return fmt.Errorf("storage: save vault tag: %w", err)
			}
		}
		for _, l := range n.Links {
			embed := 0
			if l.Embed {
				embed = 1
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO vault_note_links (note_id, kind, embed, target, subpath, display, line) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id, l.Kind, embed, l.Target, l.Subpath, l.Display, l.Line); err != nil {
				return fmt.Errorf("storage: save vault link: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit vault save: %w", err)
	}
	return nil
}

// DeleteVaultNotes removes the index rows of the given paths; their tags and
// links cascade away. Paths that were never indexed are ignored.
func (d *DB) DeleteVaultNotes(ctx context.Context, vault string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	const chunk = 400
	for start := 0; start < len(paths); start += chunk {
		end := min(start+chunk, len(paths))
		part := paths[start:end]
		args := make([]any, 0, len(part)+1)
		args = append(args, vault)
		for _, p := range part {
			args = append(args, p)
		}
		marks := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
		if _, err := d.sql.ExecContext(ctx, `DELETE FROM vault_notes WHERE vault = ? AND path IN (`+marks+`)`, args...); err != nil {
			return fmt.Errorf("storage: delete vault notes: %w", err)
		}
	}
	return nil
}

func nonNilHeadings(h []VaultHeading) []VaultHeading {
	if h == nil {
		return []VaultHeading{}
	}
	return h
}

func nonNilProps(p []VaultProp) []VaultProp {
	if p == nil {
		return []VaultProp{}
	}
	return p
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
