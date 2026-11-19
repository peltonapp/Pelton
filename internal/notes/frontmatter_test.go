package notes

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSetProperties(t *testing.T) {
	project := []Property{{Key: PropProject, Value: "Launch"}, {Key: PropProjectID, Value: "3fa9c0"}}
	for _, tc := range []struct {
		name   string
		in     string
		set    []Property
		remove []string
		want   string
	}{
		{
			name: "no front matter gets a block",
			in:   "# Plan\nbody",
			set:  project,
			want: "---\nproject: Launch\nproject-id: \"3fa9c0\"\n---\n# Plan\nbody",
		},
		{
			name: "other keys, comments and quoting stay",
			in:   "---\n# a comment\ntags:\n  - one\n  - two\ntitle: 'Kept'   \nnested:\n  deep: {a: 1}\n---\nbody\n",
			set:  project,
			want: "---\n# a comment\ntags:\n  - one\n  - two\ntitle: 'Kept'   \nnested:\n  deep: {a: 1}\nproject: Launch\nproject-id: \"3fa9c0\"\n---\nbody\n",
		},
		{
			name: "an existing key is replaced in place, a list value included",
			in:   "---\nproject:\n  - Old\n  - Other\nstatus: open\nproject-id: \"3fa9c0\"\n---\n",
			set:  project,
			want: "---\nproject: Launch\nstatus: open\nproject-id: \"3fa9c0\"\n---\n",
		},
		{
			name: "the same value keeps its bytes",
			in:   "---\nProject:   Launch  \nproject-id: '3fa9c0'\n---\nx",
			set:  project,
			want: "---\nProject:   Launch  \nproject-id: '3fa9c0'\n---\nx",
		},
		{
			name:   "removing keeps everything else",
			in:     "---\nproject: Launch\ntags: [a]\nproject-id: x\n---\nbody",
			remove: []string{PropProject, PropProjectID},
			want:   "---\ntags: [a]\n---\nbody",
		},
		{
			name:   "a block left empty goes",
			in:     "---\nproject: Launch\nproject-id: x\n---\nbody",
			remove: []string{PropProject, PropProjectID},
			want:   "body",
		},
		{
			name: "an empty block that was there stays",
			in:   "---\n---\nbody",
			set:  nil,
			want: "---\n---\nbody",
		},
		{
			name:   "removing from a note without front matter changes nothing",
			in:     "plain",
			remove: []string{PropProject},
			want:   "plain",
		},
		{
			name: "crlf and a byte order mark stay",
			in:   bom + "---\r\ntags: [a]\r\n---\r\nbody\r\n",
			set:  []Property{{Key: PropProject, Value: "Launch"}},
			want: bom + "---\r\ntags: [a]\r\nproject: Launch\r\n---\r\nbody\r\n",
		},
		{
			name: "crlf without front matter",
			in:   "line\r\nmore",
			set:  []Property{{Key: PropProject, Value: "Launch"}},
			want: "---\r\nproject: Launch\r\n---\r\nline\r\nmore",
		},
		{
			name: "values that need quotes get them",
			in:   "x",
			set:  []Property{{Key: PropProject, Value: "Q3: plan #2"}, {Key: "a", Value: "yes"}, {Key: "b", Value: `say "hi"`}},
			want: "---\nproject: \"Q3: plan #2\"\na: \"yes\"\nb: \"say \\\"hi\\\"\"\n---\nx",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SetProperties(tc.in, tc.set, tc.remove)
			if got != tc.want {
				t.Errorf("got\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestSetPropertiesReadsBack(t *testing.T) {
	text := SetProperties("body", []Property{{Key: PropProject, Value: "Q3: plan"}, {Key: PropProjectID, Value: "0e12"}}, nil)
	uid, name := ProjectOf(Parse(text).Props)
	if uid != "0e12" || name != "Q3: plan" {
		t.Errorf("read back %q %q", uid, name)
	}
}

func TestInProject(t *testing.T) {
	byID := []Property{{Key: "Project-ID", Value: "u1"}, {Key: "project", Value: "Old name"}}
	if !InProject(byID, "u1", "New name") || InProject(byID, "u2", "Old name") {
		t.Error("the id did not decide")
	}
	byName := []Property{{Key: "project", Value: "[[Launch]]"}}
	if !InProject(byName, "u1", "launch") || InProject(byName, "u1", "Other") {
		t.Error("a note without an id did not match by name")
	}
	if InProject(nil, "u1", "Launch") {
		t.Error("a note without front matter is in a project")
	}
}

func TestParsePeltonURL(t *testing.T) {
	for _, tc := range []struct {
		in, kind, id, sub string
		ok                bool
	}{
		{"pelton://project/abc", "project", "abc", "", true},
		{"PELTON://Project/abc/task%2F1", "project", "abc", "task/1", true},
		{"pelton://mail/m1%40x", "mail", "m1@x", "", true},
		{"pelton://project/", "", "", "", false},
		{"pelton://event/%E0%A4%A", "", "", "", false},
		{"https://x/y", "", "", "", false},
	} {
		kind, id, sub, ok := ParsePeltonURL(tc.in)
		if kind != tc.kind || id != tc.id || sub != tc.sub || ok != tc.ok {
			t.Errorf("%s: got %q %q %q %v", tc.in, kind, id, sub, ok)
		}
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"Q3: plan / review": "Q3 plan review",
		"..hidden":          "hidden",
		"  ":                "Untitled",
		"[[x]]#^":           "x",
	} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServiceSetPropertiesWritesOnlyWhenChanged(t *testing.T) {
	ctx := context.Background()
	ts := newService(t, map[string]string{"Plan.md": "---\ntags: [a]\n---\nbody"})
	changed, err := ts.SetProperties(ctx, "Plan.md", []Property{{Key: PropProject, Value: "Launch"}}, nil)
	if err != nil || !changed {
		t.Fatalf("set: %v %v", changed, err)
	}
	if got := ts.read(t, "Plan.md"); got != "---\ntags: [a]\nproject: Launch\n---\nbody" {
		t.Errorf("file %q", got)
	}
	snap, _ := ts.Snapshot()
	if _, name := ProjectOf(snap.Notes[0].Props); name != "Launch" {
		t.Errorf("index props %+v", snap.Notes[0].Props)
	}
	info, _ := os.Stat(filepath.Join(ts.root, "Plan.md"))
	changed, err = ts.SetProperties(ctx, "Plan.md", []Property{{Key: PropProject, Value: "Launch"}}, nil)
	if err != nil || changed {
		t.Fatalf("second set: %v %v", changed, err)
	}
	after, _ := os.Stat(filepath.Join(ts.root, "Plan.md"))
	if !after.ModTime().Equal(info.ModTime()) {
		t.Error("an unchanged note was written")
	}
	if _, err := ts.SetProperties(ctx, ".obsidian/app.md", []Property{{Key: "a", Value: "b"}}, nil); err == nil {
		t.Error("a note in .obsidian was written")
	}
	if _, err := ts.SetProperties(ctx, "../out.md", []Property{{Key: "a", Value: "b"}}, nil); err == nil {
		t.Error("a path outside the vault was written")
	}
}

func TestScanDetectsMovesOutsidePelton(t *testing.T) {
	ctx := context.Background()
	ts := newService(t, map[string]string{"a/Plan.md": "unique text", "Same1.md": "same", "Same2.md": "same"})
	if err := os.MkdirAll(filepath.Join(ts.root, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for from, to := range map[string]string{"a/Plan.md": "b/Plan renamed.md", "Same1.md": "Same3.md", "Same2.md": "Same4.md"} {
		if err := os.Rename(filepath.Join(ts.root, filepath.FromSlash(from)), filepath.Join(ts.root, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
	ts.changes = nil
	if err := ts.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ts.changes) != 1 {
		t.Fatalf("changes %+v", ts.changes)
	}
	moved := ts.changes[0].Moved
	if len(moved) != 1 || moved["a/Plan.md"] != "b/Plan renamed.md" {
		t.Errorf("moved %v, want only the note with unique bytes", moved)
	}
}
