package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
)

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
	if err := a.DeleteScriptLibrary(lib.ID); err != nil {
		t.Fatal(err)
	}
	run, err = a.TestScript(luaInput(`require("greet")`), 0)
	if err != nil || run.Status != "error" || !strings.Contains(run.Error, `no library "greet"`) {
		t.Fatalf("after delete: %+v %v", run, err)
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
	md, err := a.ScriptReference(true)
	if err != nil || !strings.Contains(md, "## Modules") || !strings.Contains(md, "### calendar") || !strings.Contains(md, "ai.draft_reply") {
		t.Fatalf("markdown reference: %v\n%.400s", err, md)
	}
}
