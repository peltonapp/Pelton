package desktop

import (
	"context"
	"slices"
	"testing"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
)

type fakeTrustHost struct {
	unsubscribed []int64
}

func (fakeTrustHost) Passport(_ context.Context, id int64) (map[string]any, error) {
	return map[string]any{"state": "verified", "verified": id == 1, "first": id == 2, "lookalike": id == 3, "domain": "example.com"}, nil
}

func (fakeTrustHost) Shield(_ context.Context, id int64) (map[string]any, error) {
	return map[string]any{"trackers": int(id), "links": 0, "still_sending": id == 4}, nil
}

func (f *fakeTrustHost) Unsubscribe(_ context.Context, id int64) error {
	f.unsubscribed = append(f.unsubscribed, id)
	return nil
}

func TestTrustScriptEntriesAreLocalized(t *testing.T) {
	r := scriptingtest.Registry(t, func(r *scripting.Registry) error {
		return r.Register(trustScriptEntries(&fakeTrustHost{}))
	})
	scriptingtest.Localized(t, r)
}

func TestTrustScriptFunctions(t *testing.T) {
	host := &fakeTrustHost{}
	r := scriptingtest.Registry(t, func(r *scripting.Registry) error {
		return r.Register(trustScriptEntries(host))
	})
	src := `print(tostring(trust.passport(1).verified))
print(tostring(trust.passport(2).first))
print(tostring(trust.shield(4).still_sending))
print(tostring(trust.shield(3).trackers))
trust.unsubscribe(9)`
	run := scriptingtest.Run(t, r, scriptingtest.Script(1, src, permTrustRead, permTrustUnsubscribe), false)
	if run.Error != "" {
		t.Fatalf("run failed: %s", run.Error)
	}
	if got := scriptingtest.Logged(run); !slices.Equal(got, []string{"true", "true", "true", "3"}) {
		t.Errorf("printed %v", got)
	}
	if !slices.Equal(host.unsubscribed, []int64{9}) {
		t.Errorf("unsubscribed %v, want the one message, after asking", host.unsubscribed)
	}

	// without the permission the unsubscribe never reaches the host.
	host.unsubscribed = nil
	run = scriptingtest.Run(t, r, scriptingtest.Script(2, `trust.unsubscribe(9)`, permTrustRead), false)
	if run.Error == "" || len(host.unsubscribed) != 0 {
		t.Errorf("an unsubscribe without the permission ran: %q, %v", run.Error, host.unsubscribed)
	}
	var risky bool
	for _, m := range r.Catalog().Modules {
		for _, f := range m.Functions {
			if m.ID == "trust" && f.Name == "unsubscribe" {
				risky = f.Risky
			}
		}
	}
	if !risky {
		t.Error("unsubscribe is not marked risky, so it would not ask first")
	}
}
