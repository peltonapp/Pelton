package notes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

type testService struct {
	*Service
	root    string
	changes []Change
}

func newService(t *testing.T, files map[string]string) *testService {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	vault := t.TempDir()
	for p, content := range files {
		full := filepath.Join(vault, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ts := &testService{root: vault}
	ts.Service = NewService(db, filepath.Join(t.TempDir(), "search"), nil, func(c Change) { ts.changes = append(ts.changes, c) })
	if err := ts.Open(ctx, vault); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)
	if err := ts.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	return ts
}

func (ts *testService) read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ts.root, filepath.FromSlash(p)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestServiceIndexesAndFindsBacklinks(t *testing.T) {
	ts := newService(t, map[string]string{
		"Alpha.md":         "---\ntags: [project]\naliases: [First]\n---\n# Alpha\nLinks to [[Beta]] and #idea",
		"Beta.md":          "Back to [[Alpha#Alpha|a]].\nMentions alpha without a link.",
		"folder/Gamma.md":  "[x](../Alpha.md) and [mail](pelton://mail/m1%40x)",
		".obsidian/ws.md":  "[[Alpha]]",
		"img/pic.png":      "png",
		"folder/empty.txt": "",
	})
	snap, err := ts.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Notes) != 3 {
		t.Fatalf("indexed %d notes, want 3", len(snap.Notes))
	}
	if !snap.Ready || ts.changes[0].Reset != true {
		t.Error("the first scan did not report a reset")
	}
	var alpha NoteInfo
	for _, n := range snap.Notes {
		if n.Path == "Alpha.md" {
			alpha = n
		}
	}
	if strings.Join(alpha.Tags, ",") != "project,idea" || strings.Join(alpha.Aliases, ",") != "First" {
		t.Errorf("alpha tags %v aliases %v", alpha.Tags, alpha.Aliases)
	}
	if len(alpha.Links) != 1 || alpha.Links[0].Resolved != "Beta.md" {
		t.Errorf("alpha links %+v", alpha.Links)
	}
	back, err := ts.Backlinks("Alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[0].Source != "Beta.md" || back[1].Source != "folder/Gamma.md" {
		t.Errorf("backlinks %+v", back)
	}
	unlinked, err := ts.UnlinkedMentions("Alpha.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(unlinked) != 1 || unlinked[0].Line != 1 || unlinked[0].Match != "alpha" {
		t.Errorf("unlinked %+v", unlinked)
	}
	if got := ts.NotesLinkingTo("pelton://mail/m1%40x"); len(got) != 1 || got[0] != "folder/Gamma.md" {
		t.Errorf("object notes %v", got)
	}
	hits, err := ts.Search("mentions", 10)
	if err != nil || len(hits) != 1 || hits[0].Path != "Beta.md" || !strings.Contains(hits[0].Context, "Mentions") {
		t.Errorf("search %+v %v", hits, err)
	}
}

func TestServicePicksUpOutsideEdits(t *testing.T) {
	ts := newService(t, map[string]string{"a.md": "one"})
	ctx := context.Background()
	ts.changes = nil
	later := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(filepath.Join(ts.root, "a.md"), []byte("two #new"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(filepath.Join(ts.root, "a.md"), later, later)
	if err := os.WriteFile(filepath.Join(ts.root, "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ts.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ts.changes) != 1 || strings.Join(ts.changes[0].Changed, ",") != "a.md,b.md" {
		t.Errorf("changes %+v", ts.changes)
	}
	os.Remove(filepath.Join(ts.root, "b.md"))
	ts.changes = nil
	_ = ts.Scan(ctx)
	if len(ts.changes) != 1 || strings.Join(ts.changes[0].Removed, ",") != "b.md" {
		t.Errorf("removal %+v", ts.changes)
	}
	ts.changes = nil
	_ = ts.Scan(ctx)
	if len(ts.changes) != 0 {
		t.Errorf("a scan with nothing new reported %+v", ts.changes)
	}
}

func TestServiceReloadsFromStoredIndex(t *testing.T) {
	ts := newService(t, map[string]string{"a.md": "[[b]]", "b.md": "x"})
	ctx := context.Background()
	if err := ts.Open(ctx, ts.root); err != nil {
		t.Fatal(err)
	}
	snap, _ := ts.Snapshot()
	if len(snap.Notes) != 2 || snap.Ready {
		t.Errorf("reopened snapshot has %d notes, ready %v", len(snap.Notes), snap.Ready)
	}
}

func TestServiceSaveDetectsConflict(t *testing.T) {
	ts := newService(t, map[string]string{"a.md": "one"})
	ctx := context.Background()
	f, err := ts.Read("a.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ts.root, "a.md"), []byte("theirs"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ts.Save(ctx, "a.md", "mine", f.Hash, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Conflict || res.Disk != "theirs" {
		t.Errorf("save over an outside edit: %+v", res)
	}
	if ts.read(t, "a.md") != "theirs" {
		t.Error("a conflicting save wrote anyway")
	}
	res, err = ts.Save(ctx, "a.md", "mine", res.DiskHash, false)
	if err != nil || res.Conflict || ts.read(t, "a.md") != "mine" {
		t.Errorf("save after resolving: %+v %v", res, err)
	}
}

func TestServiceMoveRewritesLinksAndUndoes(t *testing.T) {
	gamma := "---\r\ntitle: g\r\n---\r\nSee [[Old]] and [m](Old.md#top).\r\nUnrelated [[Other]] stays.\r\n"
	ts := newService(t, map[string]string{
		"Old.md":   "# Old",
		"Other.md": "[[Old|alias]]",
		"Gamma.md": gamma,
		"Lone.md":  "nothing here",
	})
	ctx := context.Background()
	loneBefore, _ := os.Stat(filepath.Join(ts.root, "Lone.md"))
	res, err := ts.Move(ctx, "Old.md", "dir/New.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Rewritten, ",") != "Gamma.md,Other.md" && strings.Join(res.Rewritten, ",") != "Other.md,Gamma.md" {
		t.Errorf("rewritten %v", res.Rewritten)
	}
	if got := ts.read(t, "Other.md"); got != "[[New|alias]]" {
		t.Errorf("Other.md = %q", got)
	}
	want := "---\r\ntitle: g\r\n---\r\nSee [[New]] and [m](dir/New.md#top).\r\nUnrelated [[Other]] stays.\r\n"
	if got := ts.read(t, "Gamma.md"); got != want {
		t.Errorf("Gamma.md = %q", got)
	}
	loneAfter, _ := os.Stat(filepath.Join(ts.root, "Lone.md"))
	if !loneAfter.ModTime().Equal(loneBefore.ModTime()) {
		t.Error("a note without links to the moved one was rewritten")
	}
	if err := ts.Undo(ctx, res.UndoID); err != nil {
		t.Fatal(err)
	}
	if got := ts.read(t, "Gamma.md"); got != gamma {
		t.Errorf("after undo Gamma.md = %q", got)
	}
	if _, err := os.Stat(filepath.Join(ts.root, "Old.md")); err != nil {
		t.Error("undo did not move the note back")
	}
}

func TestServiceMoveFolder(t *testing.T) {
	ts := newService(t, map[string]string{
		"a/One.md":  "[two](Two.md)",
		"a/Two.md":  "x",
		"Index.md":  "[[a/One]] [o](a/One.md)",
		"img/p.png": "p",
	})
	ctx := context.Background()
	if _, err := ts.Move(ctx, "a", "b/c"); err != nil {
		t.Fatal(err)
	}
	if got := ts.read(t, "Index.md"); got != "[[b/c/One]] [o](b/c/One.md)" {
		t.Errorf("Index.md = %q", got)
	}
	if got := ts.read(t, "b/c/One.md"); got != "[two](Two.md)" {
		t.Errorf("a link inside the moved folder changed: %q", got)
	}
}

func TestServiceDeleteToTrashAndUndo(t *testing.T) {
	ts := newService(t, map[string]string{"a.md": "x"})
	ctx := context.Background()
	res, err := ts.Delete(ctx, "a.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ts.root, ".trash", "a.md")); err != nil {
		t.Error("the note is not in .trash")
	}
	snap, _ := ts.Snapshot()
	if len(snap.Notes) != 0 {
		t.Error("the deleted note is still indexed")
	}
	if err := ts.Undo(ctx, res.UndoID); err != nil {
		t.Fatal(err)
	}
	if ts.read(t, "a.md") != "x" {
		t.Error("undo did not restore the note")
	}
}

func TestServiceLinkMention(t *testing.T) {
	ts := newService(t, map[string]string{
		"Project Alpha.md": "x",
		"b.md":             "Talked about project alpha today, see [[Project Alpha]].\nproject alpha again",
	})
	ctx := context.Background()
	if err := ts.LinkMention(ctx, "b.md", 1, "project alpha", "Project Alpha.md"); err != nil {
		t.Fatal(err)
	}
	want := "Talked about project alpha today, see [[Project Alpha]].\n[[Project Alpha|project alpha]] again"
	if got := ts.read(t, "b.md"); got != want {
		t.Errorf("b.md = %q", got)
	}
}

func TestServiceRefusesInvalidUTF8Writes(t *testing.T) {
	ts := newService(t, map[string]string{"bin.md": "ok \xff\xfe bytes"})
	f, err := ts.Read("bin.md")
	if err != nil {
		t.Fatal(err)
	}
	if !f.ReadOnly {
		t.Error("a note that is not UTF-8 was not read-only")
	}
	if _, err := ts.Save(context.Background(), "bin.md", "replaced", f.Hash, true); err == nil {
		t.Error("saved over a note that is not UTF-8")
	}
}
