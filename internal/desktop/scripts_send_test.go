package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
)

// answerConfirm gives one answer to every question and keeps what it was
// asked; fail answers with an error, as a closed window or a timeout does.
type answerConfirm struct {
	mu    sync.Mutex
	allow bool
	fail  bool
	asked []scripting.ConfirmRequest
}

func (c *answerConfirm) Confirm(_ context.Context, req scripting.ConfirmRequest) (scripting.ConfirmAnswer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, req)
	if c.fail {
		return scripting.ConfirmAnswer{}, errors.New("no answer")
	}
	return scripting.ConfirmAnswer{Allow: c.allow, Always: true}, nil
}

// beforeSendFile is an imported script that adds a Bcc and claims, in fields
// an import must ignore, to be switched on, approved and allowed everything.
const beforeSendFile = `{
  "format": 1, "name": "copy to me", "kind": "lua",
  "trigger": {"type": "mail.before_send"}, "permissions": ["mail.send"],
  "source": "table.insert(event.draft.bcc, \"spy@example.test\") event.draft.subject = event.draft.subject .. \" [x]\"",
  "enabled": true, "approved": ["mail.send"], "allowUnasked": ["mail.send", "outgoing.recipients"],
  "origin": "user"
}`

func enabledBeforeSend(t *testing.T, a *App) ScriptDTO {
	t.Helper()
	sc, err := a.ImportScript(beforeSendFile)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Enabled || len(sc.Approved) > 0 || len(sc.AllowUnasked) > 0 || sc.Origin != "import" {
		t.Fatalf("the import kept what it claimed: %+v", sc)
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptAllowUnasked(sc.ID, "mail.send", true); err != nil {
		t.Fatal(err)
	}
	return sc
}

// TestRecipientChangesAlwaysAsk checks that a before-send script adding a
// recipient is asked about every time, showing the old and the new
// recipients, whatever the script is allowed, and that a no or no answer
// holds the message rather than sending it either way.
func TestRecipientChangesAlwaysAsk(t *testing.T) {
	a, _ := newScriptTestApp(t)
	enabledBeforeSend(t, a)
	req := ComposeRequest{AccountID: 1, Subject: "Hello", Text: "hi", To: []AddressDTO{{Email: "bo@example.test"}}}

	yes := &answerConfirm{allow: true}
	a.recipientConfirmer = yes
	out, err := a.beforeSendScripts(a.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Bcc) != 1 || out.Subject != "Hello [x]" {
		t.Fatalf("request %+v", out)
	}
	if _, err := a.beforeSendScripts(a.ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(yes.asked) != 2 {
		t.Fatalf("asked %d times, want every send", len(yes.asked))
	}
	q := yes.asked[0]
	if !q.EveryTime || q.ScriptName != "copy to me" || !strings.Contains(q.Summary, "Bcc: (nobody)") || !strings.Contains(q.Summary, "Bcc: spy@example.test") {
		t.Fatalf("question %+v", q)
	}

	for name, c := range map[string]*answerConfirm{"no": {}, "no answer": {fail: true}} {
		a.recipientConfirmer = c
		if _, err := a.beforeSendScripts(a.ctx, req); !errors.Is(err, errRecipientsHeld) {
			t.Fatalf("%s: not held: %v", name, err)
		}
	}
	a.recipientConfirmer = &answerConfirm{fail: true}
	ctx := scripting.WithRunInfo(a.ctx, scripting.RunInfo{RunID: 1, ScriptID: 99})
	if _, err := a.beforeSendScripts(ctx, req); !errors.Is(err, errRecipientsHeld) || !strings.Contains(err.Error(), "Drafts") {
		t.Fatalf("a script's held send is not kept: %v", err)
	}
}

// TestSubjectChangesAreLoggedNotAsked checks that a before-send change to
// the subject goes through without a question and is in the run log with
// what changed, and that a test run lists the change as planned.
func TestSubjectChangesAreLoggedNotAsked(t *testing.T) {
	a, _ := newScriptTestApp(t)
	in := luaInput(`event.draft.subject = "[ext] " .. event.draft.subject`, scripting.PermMailSend)
	in.Trigger.Type = scripting.TriggerBeforeSend
	sc, err := a.CreateScript(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	ask := &answerConfirm{allow: true}
	a.recipientConfirmer = ask
	out, err := a.beforeSendScripts(a.ctx, ComposeRequest{AccountID: 1, Subject: "Hi", To: []AddressDTO{{Email: "bo@example.test"}}})
	if err != nil || out.Subject != "[ext] Hi" || len(ask.asked) != 0 {
		t.Fatalf("%v %+v, asked %d", err, out, len(ask.asked))
	}
	runs, err := a.ScriptRuns(sc.ID, 1)
	if err != nil || len(runs) != 1 {
		t.Fatalf("%v %v", err, runs)
	}
	if !hasStep(runs[0].Steps, scripting.StepCall, "event.draft.subject", `"Hi"`) {
		t.Fatalf("steps %+v", runs[0].Steps)
	}

	add := luaInput(`table.insert(event.draft.cc, "x@example.test")`, scripting.PermMailSend)
	add.Trigger.Type = scripting.TriggerBeforeSend
	test, err := a.TestScript(add, 0)
	if err != nil || !hasStep(test.Steps, scripting.StepPlanned, "event.draft.cc", "x@example.test") {
		t.Fatalf("%v %+v", err, test.Steps)
	}
}

// TestAfterSendCarriesTheChain checks that mail a script sent sets off the
// after-send scripts one level deeper, so scripts that send on every send
// stop instead of mailing for ever.
func TestAfterSendCarriesTheChain(t *testing.T) {
	a, _ := newScriptTestApp(t)
	in := luaInput(`print("sent")`, scripting.PermMailRead)
	in.Trigger.Type = scripting.TriggerAfterSend
	sc, err := a.CreateScript(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	a.sentRequests.Store(int64(500), ComposeRequest{AccountID: 1, Subject: "s"})
	a.scriptState.sentBy.Store(int64(500), scripting.RunInfo{ScriptID: sc.ID + 1, Depth: scripting.MaxDepth})
	a.scriptAfterSend(500)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runs, _ := a.ScriptRuns(sc.ID, 5); len(runs) > 0 {
			if runs[0].Status != string(scripting.StatusSkipped) || runs[0].Depth != scripting.MaxDepth+1 {
				t.Fatalf("run %+v", runs[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the deep after-send event was not held back")
}

func hasStep(steps []scripting.Step, kind scripting.StepKind, function, text string) bool {
	for _, s := range steps {
		if s.Kind == kind && s.Function == function && strings.Contains(s.Summary, text) {
			return true
		}
	}
	return false
}
