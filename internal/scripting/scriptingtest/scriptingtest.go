// Package scriptingtest helps the tests of packages that register scripting
// entries: a registry with the built-ins, a quick run of a Lua source, and a
// check that every key the entries use has an English string.
package scriptingtest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// Registry returns a registry with the built-ins and whatever register adds.
func Registry(t testing.TB, register ...func(*scripting.Registry) error) *scripting.Registry {
	t.Helper()
	r := scripting.NewRegistry()
	if err := scripting.RegisterBuiltins(r); err != nil {
		t.Fatal(err)
	}
	for _, reg := range register {
		if err := reg(r); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// AllowAll answers every confirmation with yes.
type AllowAll struct{}

// Confirm says yes.
func (AllowAll) Confirm(context.Context, scripting.ConfirmRequest) (scripting.ConfirmAnswer, error) {
	return scripting.ConfirmAnswer{Allow: true}, nil
}

// Script returns a manual Lua script with perms declared and approved.
func Script(id int64, src string, perms ...string) *scripting.Script {
	return &scripting.Script{
		ID: id, Name: "test", Kind: scripting.KindLua, Source: src,
		Trigger:     scripting.TriggerConfig{Type: scripting.TriggerManual},
		Permissions: perms, Approved: perms, CatchUp: scripting.CatchUpLive,
	}
}

// Run runs a script in r, a dry run when dry is set, with every risky call
// confirmed.
func Run(t testing.TB, r *scripting.Registry, sc *scripting.Script, dry bool) *scripting.Run {
	t.Helper()
	limits := scripting.DefaultLimits()
	limits.Timeout = 5 * time.Second
	e := &scripting.Engine{Registry: r, Hosts: &scripting.Hosts{Confirm: AllowAll{}}, Limits: limits}
	return e.Run(context.Background(), scripting.RunRequest{Script: sc, Trigger: sc.Trigger.Type, Dry: dry})
}

// Logged returns the lines a run printed, in order.
func Logged(run *scripting.Run) []string {
	var out []string
	for _, s := range run.Steps {
		if s.Kind == scripting.StepLog {
			out = append(out, s.Summary)
		}
	}
	return out
}

// Localized fails the test for every key r uses that the English catalog
// lacks.
func Localized(t testing.TB, r *scripting.Registry) {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "frontend", "src", "lib", "locales", "en.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skip("frontend catalog not found:", err)
	}
	text := string(data)
	var missing []string
	for _, k := range r.Catalog().LocaleKeys() {
		if !strings.Contains(text, "'"+k+"':") {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d keys missing from en.ts:\n%s", len(missing), strings.Join(missing, "\n"))
	}
	English(t, r)
}

// English fails the test for every entry of r without an English label, and
// every module function without an English description, which the API
// reference and the AI tool list show.
func English(t testing.TB, r *scripting.Registry) {
	t.Helper()
	c := r.Catalog()
	var bare []string
	metas := []scripting.Meta{}
	for _, x := range c.Triggers {
		metas = append(metas, x.Meta)
	}
	for _, x := range c.Conditions {
		metas = append(metas, x.Meta)
	}
	for _, x := range c.Actions {
		metas = append(metas, x.Meta)
	}
	for _, x := range c.ValueTypes {
		metas = append(metas, x.Meta)
	}
	for _, x := range c.Permissions {
		metas = append(metas, x.Meta)
	}
	for _, m := range c.Modules {
		metas = append(metas, m.Meta)
		for _, f := range m.Functions {
			if f.Description == "" {
				bare = append(bare, m.ID+"."+f.Name)
			}
		}
	}
	for _, m := range metas {
		if m.Label == "" {
			bare = append(bare, m.ID)
		}
	}
	if len(bare) > 0 {
		t.Fatalf("%d entries without English text (add their keys to internal/scripting/english.json):\n%s", len(bare), strings.Join(bare, "\n"))
	}
}
