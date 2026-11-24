package scriptsandbox

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
)

var now = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

// outsideCalls counts calls that reached a handler registered outside the
// scripting core. A sandboxed run must never make one.
var outsideCalls atomic.Int64

func registerOutside(r *scripting.Registry) error {
	if err := r.RegisterPermission(scripting.Permission{Meta: scripting.Meta{ID: "vault", Label: "Vault"}, Risk: "high"}); err != nil {
		return err
	}
	read := scripting.Function{
		Name: "read", Description: "Reads a secret.", Permission: "vault",
		Handler: func(*scripting.Call) ([]any, error) {
			outsideCalls.Add(1)
			return []any{"the user's secret"}, nil
		},
	}
	write := read
	write.Name, write.Effect = "write", true
	write.Dry = func(*scripting.Call) ([]any, error) {
		outsideCalls.Add(1)
		return []any{"dry secret"}, nil
	}
	if err := r.RegisterModule(scripting.Module{Meta: scripting.Meta{ID: "vault", Label: "Vault"}, Functions: []scripting.Function{read, write}}); err != nil {
		return err
	}
	return r.RegisterAction(scripting.Action{
		Meta: scripting.Meta{ID: "vault.read", Label: "Read the vault"}, Permission: "vault",
		Lua: "print(vault.read())",
	})
}

func lua(src string, perms ...string) *scripting.Script {
	sc := scriptingtest.Script(7, src, perms...)
	sc.Approved = nil
	return sc
}

func run(t *testing.T, sc *scripting.Script) *Result {
	t.Helper()
	res, err := Run(context.Background(), scriptingtest.Registry(t, registerOutside), sc, now)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func logged(res *Result) string {
	return strings.Join(scriptingtest.Logged(res.Run), "\n")
}

func TestOnlyTheSampleMessage(t *testing.T) {
	sc := lua(`
local m = event.message
print("subject", m.subject)
print("body", mail.body(m))
local other, err = mail.get(5)
print("other", other, err)
mail.move(m, "Archive")
`, scripting.PermMailRead, scripting.PermMailModify)
	sc.Trigger.Type = scripting.TriggerMailArrived
	res := run(t, sc)
	if res.Run.Status != scripting.StatusOK {
		t.Fatalf("%s %s", res.Run.Status, res.Run.Error)
	}
	log := logged(res)
	if !strings.Contains(log, "Invoice 2026-114") || !strings.Contains(log, "120.00 EUR") {
		t.Errorf("sample message not used:\n%s", log)
	}
	if !strings.Contains(log, "other nil "+errSampleOnly.Error()) {
		t.Errorf("a real message id was not refused:\n%s", log)
	}
	if res.Run.Changes() != 1 || !res.Run.Dry {
		t.Errorf("move was not a planned step: %+v", res.Run.Steps)
	}
}

// A function another package registered, which could read anything, is
// never called: not as a read, not as a dry run answer, not from a block.
func TestOutsideFunctionsAreStandIns(t *testing.T) {
	outsideCalls.Store(0)
	res := run(t, lua(`print("got", vault.read()); print("dry", vault.write())`, "vault"))
	if res.Run.Status != scripting.StatusOK {
		t.Fatalf("%s %s", res.Run.Status, res.Run.Error)
	}
	if outsideCalls.Load() != 0 {
		t.Fatal("an outside handler ran in the sandbox")
	}
	log := logged(res)
	if strings.Contains(log, "secret") || !strings.Contains(log, "vault.read does not run in a test") {
		t.Errorf("log:\n%s", log)
	}

	tree := scripting.Tree{Version: 1, Blocks: []scripting.Block{{ID: "b1", Kind: scripting.BlockAction, Type: "vault.read"}}}
	sc := &scripting.Script{ID: 8, Name: "blocks", Kind: scripting.KindBlocks, Blocks: &tree,
		Trigger: scripting.TriggerConfig{Type: scripting.TriggerManual}, Permissions: []string{"vault"}}
	res = run(t, sc)
	if res.Run.Status != scripting.StatusOK || outsideCalls.Load() != 0 {
		t.Fatalf("block run: %s %s, outside calls %d", res.Run.Status, res.Run.Error, outsideCalls.Load())
	}
}

// The user's setting values, stored values, approvals and allowances stay out
// of a sandboxed run.
func TestNothingPersonalReachesTheRun(t *testing.T) {
	sc := lua(`
SETTINGS = { { name = "folder", type = "folder", default = "Default" } }
print("folder", settings.get("folder"))
print("stored", store.get("k"))
`)
	sc.SettingsSchema = []scripting.Param{scripting.WithDefault(scripting.NewParam("folder", scripting.TypeFolder), "Default")}
	sc.Settings = map[string]any{"folder": "Private/Bank"}
	sc.Approved = []string{scripting.PermMailSend}
	sc.AllowUnasked = []string{"mail.send"}
	res := run(t, sc)
	log := logged(res)
	if strings.Contains(log, "Private/Bank") || !strings.Contains(log, "folder Default") || !strings.Contains(log, "stored nil") {
		t.Errorf("log:\n%s", log)
	}
	if sc.Settings["folder"] != "Private/Bank" || len(sc.AllowUnasked) != 1 {
		t.Error("the caller's script was changed")
	}
}

// Nobody is asked: a question takes its default or stops the run, and a risky
// call that is not a change is refused rather than put to the user.
func TestNobodyIsAsked(t *testing.T) {
	res := run(t, lua(`print("answer", ui.text("Name?", { default = "Ann" })); ui.show("done", { title = "T" })`))
	if res.Run.Status != scripting.StatusOK || !strings.Contains(logged(res), "answer Ann") {
		t.Fatalf("%s %s %s", res.Run.Status, res.Run.Error, logged(res))
	}
	if len(res.Shown) != 1 || res.Shown[0] != "T\ndone" {
		t.Errorf("shown %q", res.Shown)
	}
	res = run(t, lua(`ui.text("Name?")`))
	if res.Run.Status != scripting.StatusStopped {
		t.Fatalf("a question without a default: %s", res.Run.Status)
	}
}

func TestUnknownTrigger(t *testing.T) {
	sc := lua(`print(1)`)
	sc.Trigger.Type = "nope"
	if _, err := Run(context.Background(), scriptingtest.Registry(t), sc, now); err == nil {
		t.Fatal("ran with an unknown trigger")
	}
}

func TestSandboxRegistryCopiesOutsideEntries(t *testing.T) {
	live := scriptingtest.Registry(t, registerOutside)
	r, err := sandboxRegistry(live)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Action("vault.read"); !ok {
		t.Error("outside action missing")
	}
	if _, ok := r.Permission("vault"); !ok {
		t.Error("outside permission missing")
	}
	a, _ := json.Marshal(live.Catalog().InputKinds)
	b, _ := json.Marshal(r.Catalog().InputKinds)
	if string(a) != string(b) {
		t.Error("input kinds differ")
	}
}
