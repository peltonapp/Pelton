package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
)

// TestAppRegistryIsLocalized checks the registry as the app builds it, every
// package's entries included: each key is in en.ts and each entry has its
// English text.
func TestAppRegistryIsLocalized(t *testing.T) {
	a, _ := newScriptTestApp(t)
	scriptingtest.Localized(t, a.scripts.Registry())
}

func TestScriptsListAFolderAndTestMatches(t *testing.T) {
	a, id := newScriptTestApp(t)
	run, err := a.TestScript(luaInput(`local list = mail.list("inbox", {limit = 5}) print(#list, list[1].subject)`, scripting.PermMailRead), 0)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "ok" || run.Steps[len(run.Steps)-1].Summary != "1 Invoice 7" {
		t.Fatalf("run %+v", run)
	}
	rep, err := a.TestScriptMatch(scripting.MatchRequest{Folder: "inbox", Pattern: `\d+ EUR`, Field: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 1 || rep.Matched != 1 || rep.Matches[0].MessageID != id || rep.Matches[0].Text != "amount due: 12 EUR" {
		t.Fatalf("report %+v", rep)
	}
	if _, err := a.TestScriptMatch(scripting.MatchRequest{Folder: "no such folder", Pattern: "x", Field: "subject"}); err == nil {
		t.Fatal("a folder that does not exist was tested")
	}
}

func TestTemplatesAndLibrariesThroughTheBindings(t *testing.T) {
	a, _ := newScriptTestApp(t)
	cat, err := a.ScriptRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Templates) == 0 {
		t.Fatal("no templates in the catalog")
	}
	sc, err := a.UseScriptTemplate("newsletters")
	if err != nil {
		t.Fatal(err)
	}
	if sc.Enabled || sc.Origin != "user" || sc.Kind != "blocks" {
		t.Fatalf("copy %+v", sc)
	}

	lib, err := a.CreateScriptLibrary(ScriptLibraryInput{Name: "greet", Source: `return { hi = function(n) return "hi " .. n end }`})
	if err != nil {
		t.Fatal(err)
	}
	user, err := a.CreateScript(luaInput(`print(require("greet").hi("you")) require("absent_lib")`))
	if err != nil {
		t.Fatal(err)
	}
	run, err := a.TestScript(luaInput(`print(require("greet").hi("you"))`), 0)
	if err != nil || run.Status != "ok" || run.Steps[0].Summary != "hi you" {
		t.Fatalf("run %+v %v", run, err)
	}
	if list, err := a.ListScriptLibraries(); err != nil || len(list) != 1 || list[0].Name != "greet" {
		t.Fatalf("libraries %v %v", list, err)
	}

	sc2, err := a.scripts.Script(a.ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := scripting.Export(a.scripts.Registry(), sc2)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "uses"+scripting.FileExtension)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := a.ReadScriptFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if file.Kind != "lua" || !slices.Equal(file.MissingLibraries, []string{"absent_lib"}) {
		t.Fatalf("file %+v", file)
	}
	libData, err := scripting.ExportLibrary(&scripting.Library{Name: "other", Source: "return 1"})
	if err != nil {
		t.Fatal(err)
	}
	libPath := filepath.Join(t.TempDir(), "other"+scripting.FileExtension)
	if err := os.WriteFile(libPath, libData, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err = a.ReadScriptFile(libPath)
	if err != nil || file.Kind != "library" || file.Library == nil || file.Library.Name != "other" {
		t.Fatalf("library file %+v %v", file, err)
	}
	if _, err := a.ImportScriptLibrary(file.Text); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteScriptLibrary(lib.Library.ID); err != nil {
		t.Fatal(err)
	}
	run, err = a.TestScript(luaInput(`require("greet")`), 0)
	if err != nil || run.Status != "error" || !strings.Contains(run.Error, `no library "greet"`) {
		t.Fatalf("after delete: %+v %v", run, err)
	}
}

// TestLibraryChangeTellsWhichScriptsItSwitchedOff checks that saving,
// creating and deleting a library list the scripts they switch off, that the
// impact is known before saving, and that each script keeps a note with the
// change since its approval until it is switched on again.
func TestLibraryChangeTellsWhichScriptsItSwitchedOff(t *testing.T) {
	a, _ := newScriptTestApp(t)
	lib, err := a.CreateScriptLibrary(ScriptLibraryInput{Name: "util", Source: "return { a = 1 }"})
	if err != nil || len(lib.SwitchedOff) != 0 {
		t.Fatalf("%+v %v", lib, err)
	}
	enabled := func(src string) ScriptDTO {
		t.Helper()
		sc, err := a.CreateScript(luaInput(src, scripting.PermNotify))
		if err != nil {
			t.Fatal(err)
		}
		if sc, err = a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
			t.Fatal(err)
		}
		return sc
	}
	uses := enabled(`local u = require("util")`)
	other := enabled(`print(1)`)

	impact, err := a.ScriptLibraryImpact(lib.Library.ID, "util")
	if err != nil || len(impact) != 1 || impact[0].ID != uses.ID || !impact[0].Enabled {
		t.Fatalf("impact %+v %v", impact, err)
	}
	if note, _ := a.ScriptLibraryNote(uses.ID); note.Exists {
		t.Fatalf("a note before any change: %+v", note)
	}

	saved, err := a.UpdateScriptLibrary(lib.Library.ID, ScriptLibraryInput{Name: "util", Source: "return { a = 2 }"})
	if err != nil || len(saved.SwitchedOff) != 1 || saved.SwitchedOff[0].ID != uses.ID || saved.SwitchedOff[0].Name != uses.Name {
		t.Fatalf("saved %+v %v", saved, err)
	}
	if _, err := a.UpdateScriptLibrary(lib.Library.ID, ScriptLibraryInput{Name: "util", Source: "return { a = 3 }"}); err != nil {
		t.Fatal(err)
	}
	note, err := a.ScriptLibraryNote(uses.ID)
	if err != nil || !note.Exists || note.Library != "util" || note.Deleted {
		t.Fatalf("note %+v %v", note, err)
	}
	want := []ScriptDiffLineDTO{{Op: "-", Text: "return { a = 1 }"}, {Op: "+", Text: "return { a = 3 }"}}
	if !slices.Equal(note.Diff, want) {
		t.Fatalf("the note shows %+v, want the change since the approval", note.Diff)
	}
	if n, _ := a.ScriptLibraryNote(other.ID); n.Exists {
		t.Fatal("a script that does not load the library got a note")
	}

	if _, err := a.SetScriptEnabled(uses.ID, true, uses.Permissions); err != nil {
		t.Fatal(err)
	}
	if n, _ := a.ScriptLibraryNote(uses.ID); n.Exists {
		t.Fatal("the note outlived the new approval")
	}

	gone, err := a.DeleteScriptLibrary(lib.Library.ID)
	if err != nil || len(gone) != 1 || gone[0].ID != uses.ID {
		t.Fatalf("deleted %+v %v", gone, err)
	}
	if n, _ := a.ScriptLibraryNote(uses.ID); !n.Exists || !n.Deleted {
		t.Fatalf("note after delete %+v", n)
	}
	if _, err := a.SetScriptEnabled(uses.ID, true, uses.Permissions); err != nil {
		t.Fatal(err)
	}
	back, err := a.CreateScriptLibrary(ScriptLibraryInput{Name: "util", Source: "return {}"})
	if err != nil || len(back.SwitchedOff) != 1 {
		t.Fatalf("recreated %+v %v", back, err)
	}
	if err := a.DeleteScript(uses.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := a.ScriptLibraryNote(uses.ID); n.Exists {
		t.Fatal("the note outlived its script")
	}
}

func TestDeletingAScriptDropsItsAIOptions(t *testing.T) {
	a, _ := newScriptTestApp(t)
	sc, err := a.CreateScript(luaInput(`print(1)`))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetAIScriptOptions(sc.ID, AIScriptOptionsDTO{LogText: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteScript(sc.ID); err != nil {
		t.Fatal(err)
	}
	if len(a.aiScriptOptions()) != 0 {
		t.Fatalf("options left behind: %v", a.aiScriptOptions())
	}
}

func TestScriptReference(t *testing.T) {
	a, _ := newScriptTestApp(t)
	md, err := a.ScriptReference()
	if err != nil || !strings.Contains(md, "## Lua modules and functions") || !strings.Contains(md, "### module calendar") || !strings.Contains(md, "ai.draft_reply") {
		t.Fatalf("markdown reference: %v\n%.400s", err, md)
	}
}
