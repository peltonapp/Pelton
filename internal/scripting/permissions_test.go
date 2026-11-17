package scripting

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// twoPerms registers a function and an action that each need two
// permissions, the way an action calling two modules does.
func twoPerms(t *testing.T) *Registry {
	t.Helper()
	r := testRegistry(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.RegisterModule(Module{Meta: Meta{ID: "pair", Label: "Pair"}, Functions: []Function{{
		Name: "both", Permission: PermMailRead, Permissions: []string{PermMailCompose},
		Handler: func(*Call) ([]any, error) { return []any{"done"}, nil },
	}}}))
	must(r.RegisterAction(Action{Meta: Meta{ID: "pair.both", Label: "Both"}, Permission: PermMailRead,
		Permissions: []string{PermMailCompose, PermMailRead}, Lua: "pair.both()"}))
	return r
}

func TestPermissionListsAreNormalized(t *testing.T) {
	r := twoPerms(t)
	a, _ := r.Action("pair.both")
	if want := []string{PermMailRead, PermMailCompose}; !slices.Equal(a.Permissions, want) {
		t.Fatalf("action permissions %v, want %v", a.Permissions, want)
	}
	f, _ := r.Function("pair.both")
	if want := []string{PermMailRead, PermMailCompose}; !slices.Equal(f.Permissions, want) {
		t.Fatalf("function permissions %v, want %v", f.Permissions, want)
	}
	move, _ := r.Action("mail.move")
	if !slices.Equal(move.Permissions, []string{PermMailModify}) {
		t.Fatalf("a single permission is listed too, got %v", move.Permissions)
	}
	if err := r.RegisterAction(Action{Meta: Meta{ID: "pair.bad", Label: "x"}, Permissions: []string{"nuke"}, Lua: "x()"}); err == nil {
		t.Fatal("an unknown permission in the list was accepted")
	}
}

func TestCompileNeedsEveryListedPermission(t *testing.T) {
	r := twoPerms(t)
	tree, err := ParseTree([]byte(`{"version":1,"blocks":[{"id":"a","kind":"action","type":"pair.both"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	res := Compile(r, tree, TriggerManual, false)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	for _, p := range []string{PermMailRead, PermMailCompose} {
		if !slices.Contains(res.Permissions, p) {
			t.Errorf("compiled permissions %v lack %s", res.Permissions, p)
		}
	}
}

func TestCallNeedsEveryListedPermission(t *testing.T) {
	r := twoPerms(t)
	e := &Engine{Registry: r, Hosts: &Hosts{}, Limits: DefaultLimits()}
	run := e.Run(context.Background(), RunRequest{Script: luaScript(`print(pair.both())`, PermMailRead), Trigger: TriggerManual})
	if run.Status != StatusDenied || !strings.Contains(run.Error, PermMailCompose) {
		t.Fatalf("with one of two permissions: %s %q", run.Status, run.Error)
	}
	run = e.Run(context.Background(), RunRequest{Script: luaScript(`print(pair.both())`, PermMailRead, PermMailCompose), Trigger: TriggerManual})
	if run.Status != StatusOK {
		t.Fatalf("with both: %s %q", run.Status, run.Error)
	}
}

func TestBuiltinsCarryEnglish(t *testing.T) {
	r := testRegistry(t)
	move, _ := r.Action("mail.move")
	if move.Label == "" || move.Description == "" {
		t.Fatalf("mail.move has no English label or description: %+v", move.Meta)
	}
	if move.Params[1].Label == "" {
		t.Fatal("the folder parameter has no English label")
	}
	f, _ := r.Function("mail.move")
	if f.Description == "" {
		t.Fatal("mail.move has no English description")
	}
	own := Meta{ID: "own.thing", Label: "Mine", LabelKey: "scripts.action.mail.move"}
	englishMeta(&own)
	if own.Label != "Mine" {
		t.Fatalf("a label of its own was replaced: %q", own.Label)
	}
}

var enEntry = regexp.MustCompile(`(?m)^\s*'([^']+)':\s*'((?:[^'\\]|\\.)*)',?\s*$`)

// TestEnglishMatchesFrontend keeps english.json in step with en.ts: every key
// it holds has the same text there.
func TestEnglishMatchesFrontend(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "locales", "en.ts"))
	if err != nil {
		t.Skip("frontend catalog not found:", err)
	}
	en := map[string]string{}
	for _, m := range enEntry.FindAllStringSubmatch(string(data), -1) {
		en[m[1]] = strings.NewReplacer(`\'`, `'`, `\\`, `\`, `\n`, "\n").Replace(m[2])
	}
	for k, v := range englishText() {
		got, ok := en[k]
		if !ok {
			t.Errorf("%s is in english.json but not in en.ts", k)
			continue
		}
		if got != v {
			t.Errorf("%s: english.json has %q, en.ts has %q", k, v, got)
		}
	}
	for _, k := range testRegistry(t).Catalog().LocaleKeys() {
		if _, ok := englishText()[k]; !ok {
			t.Errorf("%s has no English text in english.json", k)
		}
	}
}

func TestReference(t *testing.T) {
	c := testRegistry(t).Catalog()
	md := Reference(c, ReferenceMarkdown)
	for _, want := range []string{
		"# Pelton scripting reference", "## Modules", "### mail",
		"`mail.move(message: message?, folder: folder?)`", "needs mail.modify",
		"`mail.send(draft: draft?) -> {id: number}`", "asks the user first",
		"### mail.arrived", "`event.message` message", "## Value types", "`subject` string",
		"`body` text, read on first use through mail.body", "## Permissions", "`mail.delete` ", "high risk",
		"`mail.move` Move", "Needs mail.modify.", `require("name")`,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown reference lacks %q", want)
		}
	}
	text := Reference(c, ReferenceText)
	if strings.Contains(text, "## ") || strings.Contains(text, "`") {
		t.Error("the text reference has markdown in it")
	}
	if !strings.Contains(text, "Modules\n-------") || !strings.Contains(text, "mail.move(message: message?, folder: folder?)") {
		t.Errorf("text reference:\n%s", text[:min(len(text), 2000)])
	}
}
