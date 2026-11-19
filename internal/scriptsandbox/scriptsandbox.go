// Package scriptsandbox test-runs a script with nothing of the user's within
// reach: sample data in place of mail, no stored values, no settings values,
// nobody to ask, and stand-ins for every function a package outside the
// scripting core registered. It is the test run an assistant gets, whose
// output goes back to a model the user does not control.
//
// The ordinary test run in the editor reads real mail on purpose; this one
// must not, even for a script whose code asks for a real message id, so it
// runs on an engine of its own rather than the service's.
package scriptsandbox

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// Timeout bounds one sandboxed run.
const Timeout = 5 * time.Second

// maxShown caps the text kept of one result the script shows.
const maxShown = 4 << 10

// errSampleOnly answers any mail access beyond the sample message.
var errSampleOnly = errors.New("a test run only has the sample message, id 0")

// Result is a finished sandboxed run.
type Result struct {
	Run *scripting.Run
	// Shown is what the script put in the result panel, clipped.
	Shown []string
}

// Run dry-runs sc on a sample event for its trigger. live is the app's
// registry, read for the trigger and the entries other packages registered;
// none of its handlers outside the core is called. now dates the sample.
func Run(ctx context.Context, live *scripting.Registry, sc *scripting.Script, now time.Time) (*Result, error) {
	t, ok := live.Trigger(sc.Trigger.Type)
	if !ok {
		return nil, fmt.Errorf("unknown trigger %q", sc.Trigger.Type)
	}
	reg, err := sandboxRegistry(live)
	if err != nil {
		return nil, err
	}
	run := *sc
	run.Approved, run.AllowUnasked, run.Settings, run.Placements = nil, nil, nil, nil
	shown := &results{}
	limits := scripting.DefaultLimits()
	limits.Timeout = Timeout
	clock := func() time.Time { return now }
	e := &scripting.Engine{
		Registry: reg,
		Hosts:    &scripting.Hosts{Mail: sampleMail{now: now}, Result: shown, Now: clock},
		KV:       emptyKV{},
		Limits:   limits,
	}
	out := e.Run(ctx, scripting.RunRequest{
		Script:  &run,
		Trigger: sc.Trigger.Type,
		Event:   scripting.SampleEvent(live, t, now),
		Dry:     true,
	})
	return &Result{Run: out, Shown: shown.list()}, nil
}

// sandboxRegistry builds the registry a sandboxed run uses: the built-ins,
// which reach the app only through the hosts the sandbox controls, and a copy
// of every entry other packages added to live, with their functions replaced
// by stand-ins that log and return nothing.
func sandboxRegistry(live *scripting.Registry) (*scripting.Registry, error) {
	r := scripting.NewRegistry()
	if err := scripting.RegisterBuiltins(r); err != nil {
		return nil, err
	}
	cat := live.Catalog()
	for _, p := range cat.Permissions {
		if _, ok := r.Permission(p.ID); !ok {
			if err := r.RegisterPermission(p); err != nil {
				return nil, err
			}
		}
	}
	for _, v := range cat.ValueTypes {
		if _, ok := r.ValueType(v.ID); !ok {
			if err := r.RegisterValueType(v); err != nil {
				return nil, err
			}
		}
	}
	builtin := map[string]bool{}
	for _, m := range r.Modules() {
		builtin[m.ID] = true
	}
	for _, m := range cat.Modules {
		if builtin[m.ID] {
			continue
		}
		if err := r.RegisterModule(standIn(m)); err != nil {
			return nil, err
		}
	}
	for _, k := range cat.InputKinds {
		if _, ok := r.InputKind(k.ID); !ok {
			k.Check = nil
			if err := r.RegisterInputKind(k); err != nil {
				return nil, err
			}
		}
	}
	for _, c := range cat.Conditions {
		if _, ok := r.Condition(c.ID); !ok {
			if err := r.RegisterCondition(c); err != nil {
				return nil, err
			}
		}
	}
	for _, a := range cat.Actions {
		if _, ok := r.Action(a.ID); !ok {
			if err := r.RegisterAction(a); err != nil {
				return nil, err
			}
		}
	}
	return r, nil
}

// standIn copies a module with every function replaced by one that logs that
// it was not run and returns nothing.
func standIn(m scripting.Module) scripting.Module {
	fns := make([]scripting.Function, 0, len(m.Functions))
	for _, f := range m.Functions {
		full := m.ID + "." + f.Name
		f.Handler = func(c *scripting.Call) ([]any, error) {
			c.Log("info", full+" does not run in a test; it returned nothing")
			return nil, nil
		}
		f.Dry = nil
		fns = append(fns, f)
	}
	m.Functions = fns
	return m
}

// sampleMail is the only mail a sandboxed run sees: the sample message.
type sampleMail struct{ now time.Time }

func (s sampleMail) Message(_ context.Context, id int64) (scripting.Message, error) {
	if id != 0 {
		return scripting.Message{}, errSampleOnly
	}
	return scripting.SampleMessage(s.now), nil
}

func (sampleMail) Body(_ context.Context, id int64) (string, error) {
	return "", errSampleOnly
}

func (sampleMail) Move(context.Context, int64, string) error      { return errSampleOnly }
func (sampleMail) Copy(context.Context, int64, string) error      { return errSampleOnly }
func (sampleMail) Archive(context.Context, int64) error           { return errSampleOnly }
func (sampleMail) Delete(context.Context, int64) error            { return errSampleOnly }
func (sampleMail) SetRead(context.Context, int64, bool) error     { return errSampleOnly }
func (sampleMail) SetFlagged(context.Context, int64, bool) error  { return errSampleOnly }
func (sampleMail) SetColor(context.Context, int64, int) error     { return errSampleOnly }
func (sampleMail) SetPinned(context.Context, int64, bool) error   { return errSampleOnly }
func (sampleMail) Snooze(context.Context, int64, time.Time) error { return errSampleOnly }
func (sampleMail) Compose(context.Context, scripting.Draft, bool) (int64, error) {
	return 0, errSampleOnly
}

// emptyKV is storage with nothing in it that keeps nothing.
type emptyKV struct{}

func (emptyKV) KVGet(context.Context, int64, string) (string, bool, error) { return "", false, nil }
func (emptyKV) KVSet(context.Context, int64, string, string) error         { return nil }
func (emptyKV) KVDelete(context.Context, int64, string) error              { return nil }
func (emptyKV) KVKeys(context.Context, int64) ([]string, error)            { return nil, nil }

// results keeps what a script shows. A run that overstays its timeout may
// still be showing something while the caller reads.
type results struct {
	mu    sync.Mutex
	texts []string
}

func (r *results) Show(_ context.Context, res scripting.Result) error {
	text := res.Text
	if len(text) > maxShown {
		text = text[:maxShown]
	}
	if res.Title != "" {
		text = res.Title + "\n" + text
	}
	r.mu.Lock()
	r.texts = append(r.texts, text)
	r.mu.Unlock()
	return nil
}

func (r *results) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.texts)
}
