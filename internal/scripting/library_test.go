package scripting

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

type libMap map[string]string

func (m libMap) LibraryByName(_ context.Context, name string) (*Library, error) {
	src, ok := m[name]
	if !ok {
		return nil, ErrNotFound
	}
	return &Library{Name: name, Source: src}, nil
}

func libEngine(t *testing.T, mail *fakeMail, libs libMap) *Engine {
	t.Helper()
	e := testEngine(t, mail)
	e.Libraries = libs
	return e
}

func TestRequireLoadsOncePerRun(t *testing.T) {
	e := libEngine(t, newFakeMail(), libMap{
		"util": "print('loading util')\nlocal M = {}\nfunction M.twice(x) return x * 2 end\nreturn M",
		"flag": "flagged = true",
	})
	run := runLua(t, e, `
local a = require("util")
local b = require("util")
print(a == b, a.twice(21), require("flag"), flagged)`)
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	var lines []string
	for _, s := range run.Steps {
		if s.Kind == StepLog {
			lines = append(lines, s.Summary)
		}
	}
	if strings.Join(lines, "\n") != "loading util\ntrue 42 true true" {
		t.Fatalf("logged %q", lines)
	}
}

func TestLibraryRunsWithTheScriptsPermissions(t *testing.T) {
	mail := newFakeMail()
	e := libEngine(t, mail, libMap{"tidy": "return { archive = function(m) mail.archive(m) end }"})
	src := `require("tidy").archive(event.message)`
	event := map[string]any{"message": MessageObject(mail.messages[1])}
	run := e.Run(context.Background(), RunRequest{Script: luaScript(src, PermMailRead), Trigger: TriggerManual, Event: event})
	if run.Status != StatusDenied || len(mail.Calls()) != 0 {
		t.Fatalf("without mail.modify: %s %q, calls %v", run.Status, run.Error, mail.Calls())
	}
	run = e.Run(context.Background(), RunRequest{Script: luaScript(src, PermMailRead, PermMailModify), Trigger: TriggerManual, Event: event, Dry: true})
	if run.Status != StatusOK || len(mail.Calls()) != 0 || run.Changes() != 1 {
		t.Fatalf("a dry run: %s %q, calls %v, %d changes", run.Status, run.Error, mail.Calls(), run.Changes())
	}
	run = e.Run(context.Background(), RunRequest{Script: luaScript(src, PermMailRead, PermMailModify), Trigger: TriggerManual, Event: event})
	if run.Status != StatusOK || !slices.Equal(mail.Calls(), []string{"archive 1"}) {
		t.Fatalf("with it: %s %q, calls %v", run.Status, run.Error, mail.Calls())
	}
}

func TestRequireFailures(t *testing.T) {
	e := libEngine(t, newFakeMail(), libMap{
		"a":      `require("b")`,
		"b":      `require("a")`,
		"broken": "local x = 1\nerror('boom')",
		"syntax": "function (",
		"spin":   "while true do end",
	})
	cases := map[string]string{
		`require("a")`:        "a -> b -> a",
		`require("nothing")`:  `no library "nothing"`,
		`require("../etc")`:   `no library "../etc"`,
		`require("broken")`:   "broken.lua:2: boom",
		`require("syntax")`:   `require "syntax"`,
		`pcall(require, "a")`: "",
	}
	for src, want := range cases {
		run := runLua(t, e, src)
		if want == "" {
			if run.Status != StatusOK {
				t.Errorf("%s: a caught require error ended the run: %s %q", src, run.Status, run.Error)
			}
			continue
		}
		if run.Status != StatusError || !strings.Contains(run.Error, want) {
			t.Errorf("%s: %s %q, want %q", src, run.Status, run.Error, want)
		}
	}
	run := runLua(t, e, `require("broken")`)
	if run.ErrorLibrary != "broken" || run.ErrorLine != 2 {
		t.Fatalf("error place: library %q line %d", run.ErrorLibrary, run.ErrorLine)
	}
	e.Limits.Timeout = 200 * time.Millisecond
	run = runLua(t, e, `require("spin")`)
	if run.Status != StatusTimeout {
		t.Fatalf("a library that never ends: %s %q", run.Status, run.Error)
	}
}

func TestLibraryStoreAndFiles(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	lib, err := s.CreateLibrary(ctx, &Library{Name: "dates", Description: "Date helpers", Source: "return { today = function() return 1 end }"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLibrary(ctx, &Library{Name: "dates"}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("a second library of the same name: %v", err)
	}
	if _, err := s.CreateLibrary(ctx, &Library{Name: "Bad Name"}); err == nil {
		t.Fatal("a name require cannot take was accepted")
	}

	sc, err := s.Create(ctx, &Script{Name: "uses", Kind: KindLua, Source: `local d = require("dates") print(d.today()) require 'other'`, Trigger: TriggerConfig{Type: TriggerManual}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.TestRun(ctx, sc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusError || !strings.Contains(run.Error, `no library "other"`) {
		t.Fatalf("test run: %s %q", run.Status, run.Error)
	}

	data, err := Export(s.Registry(), sc)
	if err != nil {
		t.Fatal(err)
	}
	kind, requires, err := ReadFileInfo(data)
	if err != nil || kind != KindLua || !slices.Equal(requires, []string{"dates", "other"}) {
		t.Fatalf("file info %q %v %v", kind, requires, err)
	}
	missing, err := s.MissingLibraries(ctx, requires)
	if err != nil || !slices.Equal(missing, []string{"other"}) {
		t.Fatalf("missing %v %v", missing, err)
	}

	libData, err := ExportLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(s.Registry(), libData); err == nil {
		t.Fatal("a library file was read as a script")
	}
	if _, err := s.ImportLibrary(ctx, libData); err == nil {
		t.Fatal("an import replaced a library of the same name")
	}
	if err := s.DeleteLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	back, err := s.ImportLibrary(ctx, libData)
	if err != nil {
		t.Fatal(err)
	}
	if back.Name != "dates" || back.Source != lib.Source || back.Origin != OriginImport {
		t.Fatalf("imported %+v", back)
	}
	list, err := s.Libraries(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("libraries %v %v", list, err)
	}
}

func TestBlockScriptRequiresFromLuaBlocks(t *testing.T) {
	tree, err := ParseTree([]byte(`{"version":1,"blocks":[{"id":"l","kind":"lua","code":"local x = require(\"helpers\")"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := scriptRequires(&Script{Kind: KindBlocks, Blocks: &tree})
	if !slices.Equal(got, []string{"helpers"}) {
		t.Fatalf("requires %v", got)
	}
}
