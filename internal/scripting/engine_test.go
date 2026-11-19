package scripting

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSandboxRemovesDangerousGlobals(t *testing.T) {
	e := testEngine(t, newFakeMail())
	for _, name := range []string{"os", "io", "debug", "package", "dofile", "loadfile", "load", "loadstring", "module", "collectgarbage", "getfenv", "setfenv", "newproxy", "coroutine", "channel"} {
		run := runLua(t, e, `if `+name+` ~= nil then error("present") end`)
		if run.Status != StatusOK {
			t.Errorf("%s: status %s, error %q", name, run.Status, run.Error)
		}
	}
}

func TestSandboxEscapeAttempts(t *testing.T) {
	e := testEngine(t, newFakeMail())
	cases := map[string]string{
		"os execute":        `os.execute("touch /tmp/pwned")`,
		"io open":           `io.open("/etc/passwd")`,
		"load bytecode":     `load("\27Lua")()`,
		"loadstring":        `loadstring("return 1")()`,
		"string dump":       `string.dump(print)`,
		"require":           `require("os")`,
		"package loadlib":   `package.loadlib("libc.so", "system")`,
		"debug getregistry": `debug.getregistry()`,
		"getfenv":           `getfenv(0).os.exit()`,
		"string metatable":  `local mt = getmetatable(""); mt.__index.os = nil; ("x"):os()`,
	}
	for name, src := range cases {
		run := runLua(t, e, src)
		if run.Status != StatusError {
			t.Errorf("%s: want an error, got %s", name, run.Status)
		}
	}
}

func TestLuaPatternsAreRefused(t *testing.T) {
	e := testEngine(t, newFakeMail())
	run := runLua(t, e, `string.match(string.rep("a", 30), string.rep("a*", 30) .. "b")`)
	if run.Status != StatusError || !strings.Contains(run.Error, "text.match") {
		t.Fatalf("got %s %q", run.Status, run.Error)
	}
	run = runLua(t, e, `assert(string.find("hello", "ll", 1, true) == 3)`)
	if run.Status != StatusOK {
		t.Fatalf("plain find: %s %q", run.Status, run.Error)
	}
}

func TestTimeoutStopsInfiniteLoop(t *testing.T) {
	e := testEngine(t, newFakeMail())
	e.Limits.Timeout = 200 * time.Millisecond
	start := time.Now()
	run := runLua(t, e, `while true do end`)
	if run.Status != StatusTimeout {
		t.Fatalf("status %s %q", run.Status, run.Error)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
}

func TestPcallCannotSwallowTimeoutOrStop(t *testing.T) {
	e := testEngine(t, newFakeMail())
	e.Limits.Timeout = 200 * time.Millisecond
	run := runLua(t, e, `while true do pcall(function() while true do end end) end`)
	if run.Status != StatusTimeout {
		t.Fatalf("timeout: %s %q", run.Status, run.Error)
	}
	run = runLua(t, e, `pcall(stop); error("kept going")`)
	if run.Status != StatusStopped {
		t.Fatalf("stop: %s %q", run.Status, run.Error)
	}
	run = runLua(t, e, `xpcall(function() mail.delete(1) end, function(e) return e end); error("kept going")`)
	if run.Status != StatusDenied {
		t.Fatalf("denied: %s %q", run.Status, run.Error)
	}
}

func TestPcallKeepsOrdinaryErrors(t *testing.T) {
	e := testEngine(t, newFakeMail())
	run := runLua(t, e, `
local ok, err = pcall(function() error("boom") end)
assert(not ok and string.find(err, "boom", 1, true))
local ok2, a, b = pcall(function(x, y) return x + y, x * y end, 2, 3)
assert(ok2 and a == 5 and b == 6)`)
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
}

func TestRunawayMemoryIsStopped(t *testing.T) {
	e := testEngine(t, newFakeMail())
	e.Limits.Memory = 32 << 20
	e.Limits.Timeout = 5 * time.Second
	run := runLua(t, e, `local t = {} local i = 0 while true do i = i + 1 t[i] = {i, i, i, "xxxxxxxxxxxxxxxx" .. i} end`)
	if run.Status != StatusLimit && run.Status != StatusError && run.Status != StatusTimeout {
		t.Fatalf("status %s %q", run.Status, run.Error)
	}
}

func TestStringBuildersAreBounded(t *testing.T) {
	e := testEngine(t, newFakeMail())
	for _, src := range []string{
		`string.rep("x", 1e9)`,
		`string.format("%99999d", 1)`,
		`local s = string.rep("x", 1000000) s = s .. s s = text.replace(s .. s, "x", "yy")`,
	} {
		if run := runLua(t, e, src); run.Status == StatusOK {
			t.Errorf("%s: ran", src)
		}
	}
}

func TestDeepRecursionFails(t *testing.T) {
	e := testEngine(t, newFakeMail())
	run := runLua(t, e, `local function f(n) return f(n + 1) + 1 end f(1)`)
	if run.Status != StatusError {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
}

func TestPermissionDenied(t *testing.T) {
	mail := newFakeMail()
	e := testEngine(t, mail)
	run := runLua(t, e, `mail.move(1, "Archive")`, PermMailRead)
	if run.Status != StatusDenied || !strings.Contains(run.Error, PermMailModify) {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	if len(mail.Calls()) != 0 {
		t.Fatalf("calls %v", mail.Calls())
	}
	declaredNotApproved := luaScript(`mail.archive(1)`, PermMailModify)
	declaredNotApproved.Approved = nil
	run = e.Run(context.Background(), RunRequest{Script: declaredNotApproved, Trigger: TriggerManual})
	if run.Status != StatusDenied {
		t.Fatalf("unapproved: %s", run.Status)
	}
}

func TestHostCallsAndLazyBody(t *testing.T) {
	mail := newFakeMail()
	e := testEngine(t, mail)
	ev := map[string]any{"message": MessageObject(mail.messages[1])}
	run := e.Run(context.Background(), RunRequest{
		Script:  luaScript(`if text.contains(event.message.body, "120 EUR") then mail.move(event.message, "Receipts"); mail.flag(event.message) end`, PermMailRead, PermMailModify),
		Trigger: TriggerManual, Event: ev,
	})
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	got := mail.Calls()
	if len(got) != 2 || got[0] != "move 1 Receipts" || got[1] != "flag 1 true" {
		t.Fatalf("calls %v", got)
	}
	if run.Changes() != 2 {
		t.Fatalf("changes %d", run.Changes())
	}
}

func TestLazyBodyNeedsPermission(t *testing.T) {
	mail := newFakeMail()
	e := testEngine(t, mail)
	run := e.Run(context.Background(), RunRequest{
		Script:  luaScript(`local b = event.message.body`),
		Trigger: TriggerManual, Event: map[string]any{"message": MessageObject(mail.messages[1])},
	})
	if run.Status != StatusDenied {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	mail := newFakeMail()
	e := testEngine(t, mail)
	sc := luaScript(`
local m = mail.get(1)
log.info("subject", m.subject)
mail.archive(m)
local d = mail.send({to = {"x@example.org"}, subject = "hi"})
log.info("draft id", d.id)
store.set("seen", 1)`, PermMailRead, PermMailModify, PermMailSend)
	sc.Approved = nil
	run := e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerManual, Dry: true})
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	if len(mail.Calls()) != 0 {
		t.Fatalf("dry run changed %v", mail.Calls())
	}
	var planned, reads, logs int
	for _, s := range run.Steps {
		switch s.Kind {
		case StepPlanned:
			planned++
		case StepRead:
			reads++
		case StepLog:
			logs++
		}
	}
	if planned != 3 || reads != 1 || logs != 2 {
		t.Fatalf("planned %d reads %d logs %d: %+v", planned, reads, logs, run.Steps)
	}
}

func TestDrySampleMessage(t *testing.T) {
	e := testEngine(t, nil)
	e.Hosts = &Hosts{}
	trig, _ := e.Registry.Trigger(TriggerMailArrived)
	sc := luaScript(`assert(text.contains(event.message.body, "invoice")) mail.archive(event.message)`, PermMailRead, PermMailModify)
	run := e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerMailArrived, Dry: true, Event: SampleEvent(e.Registry, trig, time.Now())})
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
}

func TestRiskyCallAsks(t *testing.T) {
	mail := newFakeMail()
	e := testEngine(t, mail)
	confirm := &fakeConfirm{answer: ConfirmAnswer{Allow: false}}
	e.Hosts.Confirm = confirm
	src := `mail.send({to = {"a@example.org"}, subject = "x"})`
	run := runLua(t, e, src, PermMailSend)
	if run.Status != StatusDenied || len(mail.Calls()) != 0 || len(confirm.asked) != 1 {
		t.Fatalf("declined: %s %v %d", run.Status, mail.Calls(), len(confirm.asked))
	}
	var remembered string
	e.OnAllow = func(_ int64, fn string) { remembered = fn }
	confirm.answer = ConfirmAnswer{Allow: true, Always: true}
	run = runLua(t, e, src+"\n"+src, PermMailSend)
	if run.Status != StatusOK || len(mail.Calls()) != 2 || len(confirm.asked) != 2 {
		t.Fatalf("allowed: %s %v asked %d", run.Status, mail.Calls(), len(confirm.asked))
	}
	if remembered != "mail.send" {
		t.Fatalf("remembered %q", remembered)
	}
	sc := luaScript(src, PermMailSend)
	sc.AllowUnasked = []string{"mail.send"}
	if run := e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerManual}); run.Status != StatusOK || len(confirm.asked) != 2 {
		t.Fatalf("allow unasked: %s asked %d", run.Status, len(confirm.asked))
	}
}

type slowConfirm struct{ wait time.Duration }

func (c slowConfirm) Confirm(context.Context, ConfirmRequest) (ConfirmAnswer, error) {
	time.Sleep(c.wait)
	return ConfirmAnswer{Allow: true}, nil
}

func TestConfirmationDoesNotUseTheTimeBudget(t *testing.T) {
	mail := newFakeMail()
	e := testEngine(t, mail)
	e.Limits.Timeout = 150 * time.Millisecond
	e.Hosts.Confirm = slowConfirm{wait: 400 * time.Millisecond}
	run := runLua(t, e, `mail.send({to = {"a@example.org"}})`, PermMailSend)
	if run.Status != StatusOK || len(mail.Calls()) != 1 {
		t.Fatalf("%s %q %v", run.Status, run.Error, mail.Calls())
	}
}

func TestRiskyWithoutConfirmerIsDenied(t *testing.T) {
	e := testEngine(t, newFakeMail())
	run := runLua(t, e, `mail.send({to = {"a@example.org"}})`, PermMailSend)
	if run.Status != StatusDenied {
		t.Fatalf("%s", run.Status)
	}
}

func TestLogIsBounded(t *testing.T) {
	e := testEngine(t, newFakeMail())
	e.Limits.LogBytes = 100
	run := runLua(t, e, `for i = 1, 50 do print("0123456789") end`)
	if run.Status != StatusOK || !run.LogTruncated || len(run.Steps) > 10 {
		t.Fatalf("%s truncated=%v steps=%d", run.Status, run.LogTruncated, len(run.Steps))
	}
}

func TestStepLimit(t *testing.T) {
	e := testEngine(t, newFakeMail())
	e.Limits.Steps = 5
	run := runLua(t, e, `for i = 1, 100 do mail.flag(1) end`, PermMailModify)
	if run.Status != StatusLimit {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
}

func TestMutableEventIsReadBack(t *testing.T) {
	e := testEngine(t, newFakeMail())
	sc := luaScript(`event.draft.subject = "[ext] " .. event.draft.subject; outgoing.cancel("no")`, PermMailSend)
	sc.Trigger.Type = TriggerBeforeSend
	run := e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerBeforeSend,
		Event: map[string]any{"draft": map[string]any{"subject": "Hi", "to": []any{"a@b.c"}}}})
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	d, _ := run.Event["draft"].(map[string]any)
	if d["subject"] != "[ext] Hi" || !run.Cancelled || run.CancelReason != "no" {
		t.Fatalf("event %v cancelled %v", run.Event, run.Cancelled)
	}
}

func TestTextHelpers(t *testing.T) {
	e := testEngine(t, newFakeMail())
	run := runLua(t, e, `
assert(text.test("Invoice 42", "contains", "invoice"))
assert(text.test({"a@x", "Shop"}, "equals", "shop"))
assert(text.test("Invoice 42", "matches", "\\d+$"))
assert(not text.test("Invoice", "starts", "voice"))
assert(text.test("Invoice", "ends", "VOICE"))
local y, m = text.match("2026-11", "(\\d+)-(\\d+)")
assert(y == "2026" and m == "11")
assert(#text.find_all("a1b22c333", "\\d+") == 3)
assert(text.replace("a-b-c", "-", "+") == "a+b+c")
assert(#text.split("a, b,,c") == 3)
assert(json.decode(json.encode({a = 1})).a == 1)
assert(time.duration("2h") == 7200)
assert(time.format(0, "iso") ~= "")
local ok = pcall(text.matches, "x", "(")
assert(not ok)`)
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
}

func TestHandlerPanicBecomesError(t *testing.T) {
	r := testRegistry(t)
	err := r.RegisterModule(Module{Meta: Meta{ID: "boom", Label: "Boom"}, Functions: []Function{
		{Name: "now", Handler: func(*Call) ([]any, error) { panic("kaboom") }},
	}})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Registry: r, Hosts: &Hosts{}, Limits: DefaultLimits()}
	run := e.Run(context.Background(), RunRequest{Script: luaScript(`boom.now()`), Trigger: TriggerManual})
	if run.Status != StatusError || !strings.Contains(run.Error, "kaboom") {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
}

func TestSyntaxError(t *testing.T) {
	e := testEngine(t, newFakeMail())
	run := runLua(t, e, `if then`)
	if run.Status != StatusError {
		t.Fatalf("%s", run.Status)
	}
	if CheckLua(`if then`) == nil || CheckLua(`local x = 1`) != nil {
		t.Fatal("CheckLua")
	}
}
