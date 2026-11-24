package scripting

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryValidation(t *testing.T) {
	r := testRegistry(t)
	bad := map[string]error{
		"duplicate trigger": r.RegisterTrigger(Trigger{Meta: Meta{ID: TriggerManual, Label: "x"}}),
		"bad id":            r.RegisterAction(Action{Meta: Meta{ID: "Bad Id", Label: "x"}, Lua: "x()"}),
		"no label":          r.RegisterAction(Action{Meta: Meta{ID: "a.b"}, Lua: "x()"}),
		"unknown hole":      r.RegisterAction(Action{Meta: Meta{ID: "a.c", Label: "x"}, Lua: "x({{nope}})"}),
		"unknown perm":      r.RegisterAction(Action{Meta: Meta{ID: "a.d", Label: "x"}, Lua: "x()", Permission: "nuke"}),
		"enum without opts": r.RegisterCondition(Condition{Meta: Meta{ID: "a.e", Label: "x"}, Params: []Param{{Name: "k", Type: TypeEnum}}, Lua: "true"}),
		"keyword module":    r.RegisterModule(Module{Meta: Meta{ID: "end", Label: "x"}}),
		"sandbox module":    r.RegisterModule(Module{Meta: Meta{ID: "string", Label: "x"}}),
		"reserved module":   r.RegisterModule(Module{Meta: Meta{ID: "event", Label: "x"}}),
		"no handler":        r.RegisterModule(Module{Meta: Meta{ID: "ext", Label: "x"}, Functions: []Function{{Name: "f"}}}),
		"bad risk":          r.RegisterPermission(Permission{Meta: Meta{ID: "web", Label: "x"}, Risk: "extreme"}),
	}
	for name, err := range bad {
		if err == nil {
			t.Errorf("%s: registered", name)
		}
	}
}

// TestExtensionRegistersThroughPublicAPI is what another package does at
// startup: a permission, a module with a Go handler, an action and a
// condition built on it, and a trigger, all usable by scripts and blocks.
func TestExtensionRegistersThroughPublicAPI(t *testing.T) {
	r := testRegistry(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.RegisterPermission(Permission{Meta: Meta{ID: "weather", Label: "Read the weather"}, Risk: "low"}))
	must(r.RegisterModule(Module{Meta: Meta{ID: "weather", Label: "Weather"}, Functions: []Function{{
		Name: "today", Permission: "weather",
		Handler: func(c *Call) ([]any, error) {
			return []any{map[string]any{"rain": true, "city": c.Arg(0)}}, nil
		},
	}}}))
	must(r.RegisterCondition(Condition{Meta: Meta{ID: "weather.rain", Label: "It rains"}, Permission: "weather",
		Params: []Param{{Name: "city", Type: TypeString, Label: "City", Required: true}},
		Lua:    "weather.today({{city}}).rain"}))
	must(r.RegisterAction(Action{Meta: Meta{ID: "weather.say", Label: "Say the weather"},
		Params: []Param{{Name: "city", Type: TypeString, Label: "City"}}, Lua: `log.info("weather in", {{city}})`}))
	must(r.RegisterTrigger(Trigger{Meta: Meta{ID: "weather.changed", Label: "Weather changes"}, Event: []Field{{Name: "city", Type: "string", Label: "City"}}}))

	cat := r.Catalog()
	if _, err := json.Marshal(cat); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range cat.Actions {
		found = found || a.ID == "weather.say"
	}
	if !found {
		t.Fatal("action missing from the catalog")
	}

	tree, err := ParseTree([]byte(`{"version":1,"blocks":[{"id":"w","kind":"if","branches":[{"when":{"kind":"condition","type":"weather.rain","args":{"city":{"ref":"event.city"}}},"body":[{"id":"s","kind":"action","type":"weather.say","args":{"city":{"ref":"event.city"}}}]}]}]}`))
	must(err)
	sc := &Script{ID: 3, Name: "w", Kind: KindBlocks, Blocks: &tree, Trigger: TriggerConfig{Type: "weather.changed"},
		Permissions: []string{"weather"}, Approved: []string{"weather"}, CatchUp: CatchUpLive}
	must(sc.Validate(r))
	e := &Engine{Registry: r, Hosts: &Hosts{}, Limits: DefaultLimits()}
	run := e.Run(context.Background(), RunRequest{Script: sc, Trigger: "weather.changed", Event: map[string]any{"city": "Oslo"}})
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	var said bool
	for _, s := range run.Steps {
		said = said || (s.Kind == StepLog && s.Summary == "weather in Oslo")
	}
	if !said {
		t.Fatalf("steps %+v", run.Steps)
	}
}

func TestCatalogHasEveryBuiltin(t *testing.T) {
	cat := testRegistry(t).Catalog()
	triggers := map[string]bool{}
	for _, tr := range cat.Triggers {
		triggers[tr.ID] = true
	}
	for _, id := range []string{TriggerManual, TriggerAppStart, TriggerEvery, TriggerDaily, TriggerWeekdays, TriggerMailArrived, TriggerBeforeSend, TriggerAfterSend, TriggerMailMoved, TriggerMailFlagged, TriggerMailReadMark} {
		if !triggers[id] {
			t.Errorf("trigger %s missing", id)
		}
	}
	if len(cat.Conditions) < 10 || len(cat.Actions) < 15 || len(cat.Modules) < 8 || len(cat.ParamTypes) != 15 {
		t.Fatalf("catalog too small: %d conditions, %d actions, %d modules", len(cat.Conditions), len(cat.Actions), len(cat.Modules))
	}
}
