package scripting

import (
	"context"
	"strings"
	"testing"
)

// enabledScript stores a Lua script and switches it on with its permissions
// approved and mail.send allowed without asking.
func enabledScript(t *testing.T, s *Service, name, src string, perms ...string) *Script {
	t.Helper()
	ctx := context.Background()
	sc, err := s.Create(ctx, &Script{Name: name, Kind: KindLua, Source: src, Trigger: TriggerConfig{Type: TriggerManual}, Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	if sc, err = s.SetEnabled(ctx, sc.ID, true, perms); err != nil {
		t.Fatal(err)
	}
	if len(perms) > 0 {
		if sc, err = s.SetAllowUnasked(ctx, sc.ID, "mail.send", true); err != nil {
			t.Fatal(err)
		}
	}
	return sc
}

func assertRevoked(t *testing.T, s *Service, id int64, want bool) {
	t.Helper()
	sc, err := s.Script(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	revoked := !sc.Enabled && len(sc.Approved) == 0 && len(sc.AllowUnasked) == 0
	if revoked != want {
		t.Fatalf("%q: enabled %v, approved %v, allowed %v; want revoked %v", sc.Name, sc.Enabled, sc.Approved, sc.AllowUnasked, want)
	}
}

// TestApprovalIsBoundToTheCode checks that changing what a run does takes
// back the approval, and that changing only its name or description keeps it.
func TestApprovalIsBoundToTheCode(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	for name, change := range map[string]func(*Script){
		"name only":   func(sc *Script) { sc.Name, sc.Description = "renamed", "new words" },
		"code":        func(sc *Script) { sc.Source += "\nprint(2)" },
		"permissions": func(sc *Script) { sc.Permissions = append(sc.Permissions, "mail.delete") },
		"trigger":     func(sc *Script) { sc.Trigger = TriggerConfig{Type: TriggerAppStart} },
	} {
		t.Run(name, func(t *testing.T) {
			sc := enabledScript(t, s, "s "+name, "print(1)", "mail.read", "mail.send")
			change(sc)
			if _, err := s.Update(ctx, sc); err != nil {
				t.Fatal(err)
			}
			assertRevoked(t, s, sc.ID, name != "name only")
		})
	}
}

// TestLibraryChangeRevokesItsScripts checks that changing, adding or removing
// a library takes back the approval of every script that loads it, directly
// or through another library, and of no other.
func TestLibraryChangeRevokesItsScripts(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	util, err := s.CreateLibrary(ctx, &Library{Name: "util", Source: "return {}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLibrary(ctx, &Library{Name: "wrap", Source: `return require("util")`}); err != nil {
		t.Fatal(err)
	}
	direct := enabledScript(t, s, "direct", `local u = require("util")`, "mail.send")
	through := enabledScript(t, s, "through", `local w = require "wrap"`, "mail.send")
	other := enabledScript(t, s, "other", `print(1)`, "mail.send")
	later := enabledScript(t, s, "later", `local x = require("later")`, "mail.send")

	util.Description = "only the words"
	if _, err := s.UpdateLibrary(ctx, util); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, direct.ID, false)

	util.Source = "return { evil = true }"
	if _, err := s.UpdateLibrary(ctx, util); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, direct.ID, true)
	assertRevoked(t, s, through.ID, true)
	assertRevoked(t, s, other.ID, false)

	if _, err := s.CreateLibrary(ctx, &Library{Name: "later", Source: "return 1"}); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, s, later.ID, true)
	assertRevoked(t, s, other.ID, false)
}

// TestTargetedPermissionsMatchExactly checks that a targeted permission
// covers its own target only, and that a wildcard cannot be declared.
func TestTargetedPermissionsMatchExactly(t *testing.T) {
	r := testRegistry(t)
	if err := r.RegisterPermission(Permission{Meta: Meta{ID: "net", Label: "net"}, Risk: "high"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"net:*", "net:*.example.org", "net:a:b", "net:"} {
		if r.ValidPermission(p) {
			t.Errorf("%q can be declared", p)
		}
	}
	sc := &Script{Permissions: []string{"net:example.com"}, Approved: []string{"net:example.com"}}
	for perm, want := range map[string]bool{
		"net:example.com": true, "net:EXAMPLE.com": true, "net": true,
		"net:example.com.evil.org": false, "net:evil-example.com": false, "net:example.com.": false,
		"net:sub.example.com": false, "net:example.co": false,
	} {
		if got := sc.granted(perm, false); got != want {
			t.Errorf("granted(%q) = %v, want %v", perm, got, want)
		}
	}
}

// TestRequireOnlyLoadsNamedLibraries checks that a script cannot load a
// library whose name it builds at run time, which its approval never named.
func TestRequireOnlyLoadsNamedLibraries(t *testing.T) {
	e := libEngine(t, newFakeMail(), libMap{"evil": "return 1", "good": `return require("evil")`})
	run := runLua(t, e, `local n = "ev" .. "il" require(n)`)
	if run.Status != StatusError || !strings.Contains(run.Error, "not named") {
		t.Fatalf("built name: %s %q", run.Status, run.Error)
	}
	if run := runLua(t, e, `assert(require("good") == 1)`); run.Status != StatusOK {
		t.Fatalf("named through a library: %s %q", run.Status, run.Error)
	}
}

// TestStringBuildingIsBounded checks that the string builders a script can
// reach refuse a result past the cap before they allocate it, including the
// cases where the size check could overflow or be skipped.
func TestStringBuildingIsBounded(t *testing.T) {
	e := testEngine(t, newFakeMail())
	for name, src := range map[string]string{
		"rep overflowing the check": `string.rep("abcd", 2^62)`,
		"rep with a separator":      `string.rep("", 2^30, "x")`,
		"format with many widths": `local t = {} for i = 1, 300 do t[i] = 1 end
			string.format(string.rep("%9999d", 300), unpack(t))`,
		"replace of every position": `text.replace(string.rep("a", 100000), "", string.rep("b", 1000))`,
		"replace with a group":      `text.replace(string.rep("a", 600000), "(a)", "$1$1")`,
	} {
		t.Run(name, func(t *testing.T) {
			run := runLua(t, e, src)
			if run.Status != StatusError {
				t.Fatalf("status %s (%s), want an error", run.Status, run.Error)
			}
			if !strings.Contains(run.Error, "more than") {
				t.Fatalf("error %q, want the size cap", run.Error)
			}
		})
	}
	ok := runLua(t, e, `
		assert(string.rep("", 2^40) == "")
		assert(string.rep("ab", 3, "-") == "ab-ab-ab")
		assert(string.format("%5d|%s", 42, "x") == "   42|x")
		assert(text.replace("a1b22", "[0-9]+", "<$0>") == "a<1>b<22>")
		assert(text.replace("abc", "b", "") == "ac")`)
	if ok.Status != StatusOK {
		t.Fatalf("plain use failed: %s %s", ok.Status, ok.Error)
	}
}
