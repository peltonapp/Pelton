package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// recordingConfirm says yes, always, to everything and keeps what it was
// asked.
type recordingConfirm struct {
	mu    sync.Mutex
	asked []scripting.ConfirmRequest
}

func (c *recordingConfirm) Confirm(_ context.Context, req scripting.ConfirmRequest) (scripting.ConfirmAnswer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, req)
	return scripting.ConfirmAnswer{Allow: true, Always: true}, nil
}

// TestModelChosenRiskyCallsAlwaysAsk checks that a risky call a model makes
// asks the user each time even when the script may make it without asking,
// and that answering "always" does not widen the script's allowance.
func TestModelChosenRiskyCallsAlwaysAsk(t *testing.T) {
	f := newFake(t,
		toolAnswer("risky_go", map[string]any{"to": "a@example.org"}),
		toolAnswer("risky_go", map[string]any{"to": "b@example.org"}),
		reply(oaAnswer("sent both")))
	e := setup(t, Network{}, local(f))
	if err := e.reg.Register(scripting.Entries{
		Permissions: []scripting.Permission{{Meta: scripting.Meta{ID: "risky", Label: "risky"}, Risk: "high"}},
		Modules: []scripting.Module{{Meta: scripting.Meta{ID: "risky", Label: "Risky"}, Functions: []scripting.Function{{
			Name: "go", Description: "Sends something.", Permission: "risky", Risky: true, Effect: true,
			Params:  []scripting.Param{{Name: "to", Type: scripting.TypeString, Label: "to"}},
			Handler: func(*scripting.Call) ([]any, error) { return []any{"ok"}, nil },
		}}}},
	}); err != nil {
		t.Fatal(err)
	}
	confirm := &recordingConfirm{}
	var allowed []string
	sc := &scripting.Script{
		ID: 7, Name: "agent", Kind: scripting.KindLua, Trigger: scripting.TriggerConfig{Type: scripting.TriggerManual},
		Source:       `local r = ai.agent("go", {tools = {"risky"}}) print(r.ok, r.tool_calls)`,
		Permissions:  []string{"ai_local:local", "risky"},
		Approved:     []string{"ai_local:local", "risky"},
		AllowUnasked: []string{"risky.go"},
	}
	eng := &scripting.Engine{Registry: e.reg, Hosts: &scripting.Hosts{Confirm: confirm}, Limits: scripting.DefaultLimits(),
		OnAllow: func(_ int64, fn string) { allowed = append(allowed, fn) }}
	run := eng.Run(context.Background(), scripting.RunRequest{Script: sc, Trigger: scripting.TriggerManual})
	if run.Status != scripting.StatusOK {
		t.Fatalf("%s %s", run.Status, run.Error)
	}
	if len(confirm.asked) != 2 {
		t.Fatalf("asked %d times, want once per model call", len(confirm.asked))
	}
	for _, q := range confirm.asked {
		if !q.EveryTime {
			t.Fatalf("a model's call offered allow without asking: %+v", q)
		}
	}
	if len(allowed) != 0 {
		t.Fatalf("an always answer to a model's call was remembered: %v", allowed)
	}
}

// TestModelInTestNeedsApproval checks that a test run asks a model only for
// a provider the user approved, not one the script merely declares.
func TestModelInTestNeedsApproval(t *testing.T) {
	f := newFake(t, reply(oaAnswer("hi")))
	e := setup(t, Network{}, local(f))
	sc := &scripting.Script{
		ID: 7, Name: "t", Kind: scripting.KindLua, Trigger: scripting.TriggerConfig{Type: scripting.TriggerManual},
		Source: `local r = ai.ask("x") assert(r.ok, r.error)`, Permissions: []string{"ai_local:local"},
	}
	eng := &scripting.Engine{Registry: e.reg, Limits: scripting.DefaultLimits()}
	ctx := WithModelInTest(context.Background())
	run := eng.Run(ctx, scripting.RunRequest{Script: sc, Trigger: scripting.TriggerManual, Dry: true})
	if run.Status != scripting.StatusError || !strings.Contains(run.Error, "approved") || len(f.requests()) != 0 {
		t.Fatalf("declared only: %s %q, %d requests", run.Status, run.Error, len(f.requests()))
	}
	sc.Approved = sc.Permissions
	run = eng.Run(ctx, scripting.RunRequest{Script: sc, Trigger: scripting.TriggerManual, Dry: true})
	if run.Status != scripting.StatusOK || len(f.requests()) != 1 {
		t.Fatalf("approved: %s %q, %d requests", run.Status, run.Error, len(f.requests()))
	}
}

// TestRequestsPerRunAreCapped checks that a loop cannot keep a run asking a
// model for ever while the run's own clock is stopped.
func TestRequestsPerRunAreCapped(t *testing.T) {
	answers := make([]func(recorded) (int, any), MaxRequestsPerRun)
	for i := range answers {
		answers[i] = reply(oaAnswer("again"))
	}
	f := newFake(t, answers...)
	e := setup(t, Network{}, local(f))
	run := e.run(t, `for i = 1, 1000 do local r = ai.ask("x") if not r.ok then print(i, r.error) break end end`, false, "ai_local:local")
	if run.Status != scripting.StatusOK || len(f.requests()) != MaxRequestsPerRun {
		t.Fatalf("%s %q, %d requests", run.Status, run.Error, len(f.requests()))
	}
}
