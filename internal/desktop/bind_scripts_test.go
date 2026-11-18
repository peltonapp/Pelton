package desktop

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

func newScriptTestApp(t *testing.T) (*App, int64) {
	t.Helper()
	a := newTrashTestApp(t)
	a.startScripts()
	if a.scripts == nil {
		t.Fatal("scripts did not start")
	}
	t.Cleanup(a.stopScripts)
	acct := seedAccount(t, a)
	inbox := seedFolder(t, a, acct, "INBOX", "INBOX", nil, 0)
	id, err := a.store.InsertMessage(a.ctx, &storage.Message{
		AccountID: acct, FolderID: inbox.ID, UID: 1, MessageID: "<one@example.test>",
		Subject: "Invoice 7", FromAddress: "billing@example.test", Date: time.Now().UTC(),
		BodyPlain: "amount due: 12 EUR",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, id
}

func luaInput(src string, perms ...string) ScriptInput {
	return ScriptInput{Name: "t", Kind: "lua", Source: src, Trigger: ScriptTriggerDTO{Type: scripting.TriggerManual}, Permissions: perms}
}

func TestScriptRunChangesMailAndUndoesAsOneGroup(t *testing.T) {
	a, id := newScriptTestApp(t)
	sc, err := a.CreateScript(luaInput(`
local m = mail.get(event.message.id)
assert(text.contains(m.body, "12 EUR"))
mail.flag(m)
mail.mark_read(m)
mail.set_color(m, 4)`, scripting.PermMailRead, scripting.PermMailModify))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Enabled || sc.Origin != "user" {
		t.Fatalf("created %+v", sc)
	}
	if _, err := a.RunScript(sc.ID, id); err == nil {
		t.Fatal("ran while switched off")
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	run, err := a.RunScript(sc.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "ok" || run.Changes != 3 || !run.Undoable || len(run.Messages) != 1 || run.Messages[0] != id {
		t.Fatalf("run %+v", run)
	}
	m, _ := a.store.GetMessage(a.ctx, id)
	if m.Flags&storage.FlagFlagged == 0 || m.Flags&storage.FlagSeen == 0 || m.FlagColor != 4 {
		t.Fatalf("not changed: flags %v color %d", m.Flags, m.FlagColor)
	}
	if err := a.UndoScriptRun(run.ID); err != nil {
		t.Fatal(err)
	}
	m, _ = a.store.GetMessage(a.ctx, id)
	if m.Flags&storage.FlagFlagged != 0 || m.Flags&storage.FlagSeen != 0 || m.FlagColor != 0 {
		t.Fatalf("not undone: flags %v color %d", m.Flags, m.FlagColor)
	}
	if err := a.UndoScriptRun(run.ID); err == nil {
		t.Fatal("undone twice")
	}
	runs, err := a.ScriptRuns(sc.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].ScriptName != "t" {
		t.Fatalf("log %v %+v", err, runs)
	}
}

func TestTestScriptChangesNothing(t *testing.T) {
	a, id := newScriptTestApp(t)
	run, err := a.TestScript(luaInput(`mail.flag(event.message); mail.delete(event.message)`, scripting.PermMailRead, scripting.PermMailModify, scripting.PermMailDelete), id)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "ok" || !run.Dry || run.Changes != 2 || run.Undoable {
		t.Fatalf("run %+v", run)
	}
	m, _ := a.store.GetMessage(a.ctx, id)
	if m.Flags != 0 {
		t.Fatalf("dry run changed flags %v", m.Flags)
	}
	sample, err := a.TestScript(luaInput(`log.info(event.message.subject)`), 0)
	if err != nil || sample.Status != "ok" || !strings.Contains(sample.Steps[0].Summary, "Invoice") {
		t.Fatalf("sample %v %+v", err, sample)
	}
}

func TestBeforeSendScriptsChangeOrStopTheSend(t *testing.T) {
	a, _ := newScriptTestApp(t)
	in := luaInput(`
if text.contains(event.draft.subject, "secret") then outgoing.cancel("no secrets") end
event.draft.subject = "[ext] " .. event.draft.subject
table.insert(event.draft.bcc, "archive@example.test")`, scripting.PermMailSend)
	in.Trigger.Type = scripting.TriggerBeforeSend
	sc, err := a.CreateScript(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, sc.Permissions); err != nil {
		t.Fatal(err)
	}
	a.recipientConfirmer = &answerConfirm{allow: true}
	req := ComposeRequest{AccountID: 1, Subject: "Hello", Text: "hi", To: []AddressDTO{{Name: "Bo", Email: "bo@example.test"}}}
	out, err := a.beforeSendScripts(a.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Subject != "[ext] Hello" || len(out.Bcc) != 1 || out.To[0].Name != "Bo" || out.Text != "hi" {
		t.Fatalf("request %+v", out)
	}
	req.Subject = "the secret"
	if _, err := a.beforeSendScripts(a.ctx, req); err == nil || !strings.Contains(err.Error(), "no secrets") {
		t.Fatalf("not stopped: %v", err)
	}
}

func TestScriptFolderResolution(t *testing.T) {
	a, _ := newScriptTestApp(t)
	acct := int64(1)
	seedFolder(t, a, acct, "Archive", "Archive", []string{"\\Archive"}, 0)
	seedFolder(t, a, acct, "Receipts", "Lists/Receipts", nil, 0)
	m := &scriptMail{app: a}
	for name, want := range map[string]string{"archive": "Archive", "inbox": "INBOX", "Lists/Receipts": "Lists/Receipts", "receipts": "Lists/Receipts"} {
		f, err := m.folder(acct, name)
		if err != nil || f.IMAPPath != want {
			t.Errorf("%s: %v %q", name, err, f.IMAPPath)
		}
	}
	if _, err := m.folder(acct, "Nowhere"); err == nil {
		t.Error("found a folder that does not exist")
	}
}

func TestReplyAndForwardDrafts(t *testing.T) {
	orig := &storage.Message{ID: 3, Subject: "Plans", FromAddress: "ann@example.test", MessageID: "<p@x>",
		ToAddresses: "me@example.test, bo@example.test", CcAddresses: "cy@example.test", References: []string{"<r@x>"}}
	var req ComposeRequest
	fillReply(&req, orig, true, "me@example.test", "line one\nline two")
	if req.Subject != "Re: Plans" || req.To[0].Email != "ann@example.test" || len(req.Cc) != 2 || req.InReplyTo != "<p@x>" ||
		len(req.References) != 2 || !strings.Contains(req.Text, "> line two") {
		t.Fatalf("reply %+v", req)
	}
	var fwd ComposeRequest
	fillForward(&fwd, orig, "body")
	if fwd.Subject != "Fwd: Plans" || fwd.ForwardOf != 3 || !strings.Contains(fwd.Text, "body") {
		t.Fatalf("forward %+v", fwd)
	}
}

func TestScriptExportImportRoundTrip(t *testing.T) {
	a, _ := newScriptTestApp(t)
	sc, err := a.CreateScript(luaInput(`log.info("hi")`))
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := a.scriptService()
	stored, _ := svc.Script(a.ctx, sc.ID)
	data, err := scripting.Export(svc.Registry(), stored)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := a.ImportScript(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if imported.Origin != "import" || imported.Enabled || imported.Source != `log.info("hi")` {
		t.Fatalf("imported %+v", imported)
	}
	if res := a.CheckScriptLua("if then"); res == "" {
		t.Fatal("syntax error not reported")
	}
	comp, err := a.CompileScriptBlocks(`{"version":1,"blocks":[{"id":"a","kind":"action","type":"mail.flag"}]}`, scripting.TriggerMailArrived)
	if err != nil || len(comp.Errors) != 0 || !strings.Contains(comp.Lua, "mail.flag(event.message, true)") {
		t.Fatalf("compile %v %+v", err, comp)
	}
	if comp.Lines["a"].From != 2 {
		t.Fatalf("line map %+v", comp.Lines)
	}
}

func TestPlacedScriptRunsOnTheSelection(t *testing.T) {
	a, id := newScriptTestApp(t)
	in := luaInput(`ui.show(event.message.subject .. " / " .. #event.messages, { title = "Picked" })`)
	in.Trigger.Params = map[string]any{"scope": "message"}
	sc, err := a.CreateScript(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptPlacements(sc.ID, []scripting.PlacementConfig{{ID: "mail.row"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.ScriptPlacements(); len(got) != 0 {
		t.Fatalf("a switched off script is offered: %v", got)
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, nil); err != nil {
		t.Fatal(err)
	}
	got, err := a.ScriptPlacements()
	if err != nil || len(got["mail.row"]) != 1 || got["mail.row"][0].ScriptID != sc.ID {
		t.Fatalf("%v %+v", err, got)
	}
	ref := strconv.FormatInt(id, 10)
	run, err := a.RunPlacedScript(sc.ID, "mail.row", []scripting.PlacedItem{{ID: ref}})
	if err != nil || run.Status != "ok" {
		t.Fatalf("%v %+v", err, run)
	}
	st := &a.scriptState
	st.mu.Lock()
	var resultID string
	var shown scripting.Result
	for k, v := range st.results {
		resultID, shown = k, v
	}
	st.mu.Unlock()
	if shown.Text != "Invoice 7 / 1" || shown.Title != "Picked" {
		t.Fatalf("result %+v", shown)
	}
	if _, err := a.RunScriptResultAction(resultID, "copy"); err == nil {
		t.Fatal("a window only action ran in the backend")
	}
	if _, err := a.RunScriptResultAction("gone", "notes.save"); err == nil {
		t.Fatal("a forgotten result was used")
	}
	if _, err := a.RunPlacedScript(sc.ID, "palette", nil); err == nil {
		t.Fatal("ran from a placement the script is not at")
	}
	fields, err := a.ScriptValue("message", ref)
	if err != nil || fields["subject"] != "Invoice 7" {
		t.Fatalf("%v %v", err, fields)
	}
}

func TestQuestionWithoutWindowTakesTheDefault(t *testing.T) {
	a, _ := newScriptTestApp(t)
	sc, err := a.CreateScript(luaInput(`log.info(ui.text("Name?", { default = "anon" }))`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(sc.ID, true, nil); err != nil {
		t.Fatal(err)
	}
	run, err := a.RunScript(sc.ID, 0)
	if err != nil || run.Status != "ok" || run.Steps[len(run.Steps)-1].Summary != "anon" {
		t.Fatalf("%v %+v", err, run)
	}
	a.AnswerScriptAsk("nobody", "x", false)
	if err := a.CancelScriptRun(12345); err == nil {
		t.Fatal("cancelled a run that is not going")
	}
	if err := a.SetScriptAskSeconds(5); err == nil {
		t.Fatal("accepted a five second wait")
	}
	if err := a.SetScriptAskSeconds(300); err != nil || a.ScriptAskSeconds() != 300 {
		t.Fatalf("%v %d", err, a.ScriptAskSeconds())
	}
}
