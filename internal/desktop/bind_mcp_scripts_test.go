package desktop

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/peltonapp/Pelton/internal/mcpserver"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/storage"
)

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// mcpScriptSession starts the app's real MCP server on a free loopback port
// with every script tool and every write tool switched on, the most an agent
// can be given, and connects a client to it.
func mcpScriptSession(t *testing.T) (*App, int64, *mcp.ClientSession) {
	t.Helper()
	a, msgID := newScriptTestApp(t)
	if data, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "locales", "en.ts")); err == nil {
		a.englishStrings = string(data)
	}
	for _, tool := range mcpPermissionTools() {
		if err := a.store.Set(a.ctx, mcpPermissionKey(tool), "true"); err != nil {
			t.Fatal(err)
		}
	}
	srv := mcpserver.NewWithWriter(&mcpMailbox{app: a}, &mcpWriter{app: a}, a.log).
		WithScripts(&mcpScripts{app: a}, a.scriptLimiter())
	if err := srv.Start(mcpserver.Config{Addr: "127.0.0.1:0", Token: "tok", Version: "test", Permissions: a.mcpPermissions()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   "http://" + srv.Addr(),
		HTTPClient: &http.Client{Transport: bearer{"tok"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return a, msgID, cs
}

// callTool returns a call's text and whether it failed.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error(), true
	}
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n"), res.IsError
}

func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	text, failed := callTool(t, cs, name, args)
	if failed {
		t.Fatalf("%s failed: %s", name, text)
	}
	return text
}

func draft(name, src string, perms ...string) map[string]any {
	return map[string]any{
		"name": name, "kind": "lua", "source": src,
		"trigger": map[string]any{"type": "manual"}, "permissions": perms,
	}
}

// everyScriptOff fails the test if any stored script is on or holds an
// approval or allowance it did not hold before.
func everyScriptOff(t *testing.T, a *App, except ...int64) {
	t.Helper()
	list, err := a.ListScripts()
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range list {
		if containsID(except, sc.ID) {
			continue
		}
		if sc.Enabled || len(sc.Approved) > 0 || len(sc.AllowUnasked) > 0 {
			t.Errorf("script %d %q is on or approved: %+v", sc.ID, sc.Name, sc)
		}
	}
}

func containsID(list []int64, id int64) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

func TestAssistantCreatesScriptsSwitchedOff(t *testing.T) {
	a, _, cs := mcpScriptSession(t)
	out := mustCall(t, cs, mcpserver.ToolCreateScript, draft("Tidy", `print("hi")`))
	if !strings.Contains(out, `"status": "created"`) || !strings.Contains(out, `"enabled": false`) {
		t.Fatalf("create %s", out)
	}
	list, _ := a.ListScripts()
	if len(list) != 1 || list[0].Origin != string(scripting.OriginAssistant) || list[0].Enabled {
		t.Fatalf("stored %+v", list)
	}
	actions, _ := a.AgentActions()
	if len(actions) == 0 || actions[0].Tool != mcpserver.ToolCreateScript {
		t.Errorf("not in the agent log: %+v", actions)
	}

	// a script that does not parse or needs an undeclared permission is
	// refused with what to fix, and nothing is stored.
	if text, failed := callTool(t, cs, mcpserver.ToolCreateScript, draft("Broken", "if then")); !failed || !strings.Contains(text, "line 1") {
		t.Errorf("broken script: %s", text)
	}
	if text, failed := callTool(t, cs, mcpserver.ToolCreateScript, draft("Mover", `mail.move(event.message, "Archive")`)); !failed || !strings.Contains(text, "mail.modify") {
		t.Errorf("undeclared permission: %s", text)
	}
	if list, _ := a.ListScripts(); len(list) != 1 {
		t.Errorf("a refused script was stored: %d", len(list))
	}
}

// No argument combination gets a script switched on, approved, allowed to
// skip a confirmation, or stored as the user's.
func TestAssistantCannotEnableOrApprove(t *testing.T) {
	a, msgID, cs := mcpScriptSession(t)
	mustCall(t, cs, mcpserver.ToolCreateScript, draft("Mine", `print(1)`, scripting.PermMailSend))
	list, _ := a.ListScripts()
	id := list[0].ID

	hostile := []map[string]any{
		{"enabled": true}, {"approved": []string{"mail.send"}}, {"origin": "user"},
		{"allow_unasked": []string{"mail.send"}}, {"allowUnasked": []string{"mail.send"}},
		{"settings_values": map[string]any{"x": 1}}, {"id": id},
	}
	for _, extra := range hostile {
		args := draft("Evil", `print(1)`, scripting.PermMailSend)
		for k, v := range extra {
			args[k] = v
		}
		for _, tool := range []string{mcpserver.ToolCreateScript, mcpserver.ToolValidateScript} {
			if text, failed := callTool(t, cs, tool, args); !failed {
				t.Errorf("%s accepted %v: %s", tool, extra, text)
			}
		}
		if text, failed := callTool(t, cs, mcpserver.ToolUpdateScript, map[string]any{"id": id, "script": args}); !failed {
			t.Errorf("update accepted %v: %s", extra, text)
		}
		upd := map[string]any{"id": id, "script": draft("Mine", `print(2)`, scripting.PermMailSend)}
		for k, v := range extra {
			upd[k] = v
		}
		if _, failed := callTool(t, cs, mcpserver.ToolUpdateScript, upd); !failed && len(extra) > 0 && extra["id"] == nil {
			t.Errorf("update accepted top level %v", extra)
		}
	}
	for _, args := range []map[string]any{
		{"id": id, "message_id": msgID}, {"id": id, "dry": false}, {"id": id, "ask_model": true},
		{"id": id, "real": true}, {"script": draft("x", "print(1)"), "message_id": msgID},
	} {
		if text, failed := callTool(t, cs, mcpserver.ToolTestScript, args); !failed {
			t.Errorf("test_script accepted %v: %s", args, text)
		}
	}
	for _, name := range []string{"enable_script", "set_script_enabled", "approve_script", "run_script", "delete_script", "set_script_settings"} {
		if _, failed := callTool(t, cs, name, map[string]any{"id": id}); !failed {
			t.Errorf("%s exists", name)
		}
	}
	everyScriptOff(t, a)
	sc, _ := a.GetScript(id)
	if sc.Origin != string(scripting.OriginAssistant) {
		t.Errorf("origin became %s", sc.Origin)
	}
}

// A test run sees only sample data: a real message id gets nothing, the
// event is the sample, and nothing in the mailbox changes.
func TestAssistantTestRunNeverSeesRealMail(t *testing.T) {
	a, msgID, cs := mcpScriptSession(t)
	id := strconv.FormatInt(msgID, 10)
	src := `
print("event", event.message and event.message.subject)
local m, err = mail.get(` + id + `)
print("get", m and m.subject, err)
print("body", pcall(mail.body, ` + id + `))
mail.flag(` + id + `, true)
mail.delete(` + id + `)
`
	perms := []string{scripting.PermMailRead, scripting.PermMailModify, scripting.PermMailDelete}
	d := draft("Peek", src, perms...)
	d["trigger"] = map[string]any{"type": scripting.TriggerMailArrived}
	out := mustCall(t, cs, mcpserver.ToolTestScript, map[string]any{"script": d})
	for _, secret := range []string{"Invoice 7", "12 EUR", "billing@example.test"} {
		if strings.Contains(out, secret) {
			t.Fatalf("a test run leaked %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "Invoice 2026-114") || !strings.Contains(out, "Dry run on sample data") {
		t.Errorf("sample not used:\n%s", out)
	}
	if !strings.Contains(out, `"planned_changes": 2`) {
		t.Errorf("changes not planned:\n%s", out)
	}
	m, err := a.store.GetMessage(a.ctx, msgID)
	if err != nil || m.Flags&storage.FlagFlagged != 0 {
		t.Fatalf("the test run changed real mail: %v %+v", err, m)
	}

	// a saved script the user wrote runs the same way, with its settings
	// values left out.
	user, err := a.CreateScript(ScriptInput{
		Name: "Users", Kind: "lua", Trigger: ScriptTriggerDTO{Type: scripting.TriggerManual},
		Source: `SETTINGS = { { name = "folder", type = "folder", default = "Default" } }
print("folder", settings.get("folder"))
print("get", (mail.get(` + id + `) or {}).subject)`,
		Permissions: []string{scripting.PermMailRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptSettings(user.ID, map[string]any{"folder": "Private/Bank"}); err != nil {
		t.Fatal(err)
	}
	out = mustCall(t, cs, mcpserver.ToolTestScript, map[string]any{"id": user.ID})
	if strings.Contains(out, "Private/Bank") || strings.Contains(out, "Invoice 7") || !strings.Contains(out, "folder Default") {
		t.Errorf("saved script run:\n%s", out)
	}
	if !strings.Contains(out, "UNTRUSTED SCRIPT") && !strings.Contains(out, "UNTRUSTED CONTENT") {
		t.Error("test run output not marked untrusted")
	}
}

func TestAssistantSeesNoSettingsValuesOrApprovals(t *testing.T) {
	a, _, cs := mcpScriptSession(t)
	user, err := a.CreateScript(ScriptInput{
		Name: "Users", Kind: "lua", Trigger: ScriptTriggerDTO{Type: scripting.TriggerManual},
		Source:      `SETTINGS = { { name = "folder", type = "folder" } } print(settings.get("folder"))`,
		Permissions: []string{scripting.PermNotify},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptSettings(user.ID, map[string]any{"folder": "Private/Bank"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(user.ID, true, []string{scripting.PermNotify}); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{
		mustCall(t, cs, mcpserver.ToolListScripts, map[string]any{}),
		mustCall(t, cs, mcpserver.ToolGetScript, map[string]any{"id": user.ID}),
	} {
		if strings.Contains(out, "Private/Bank") || strings.Contains(out, "approved") {
			t.Errorf("leaked a settings value or approvals:\n%s", out)
		}
		if !strings.HasPrefix(out, "UNTRUSTED CONTENT") {
			t.Errorf("not fenced:\n%s", out)
		}
	}
	get := mustCall(t, cs, mcpserver.ToolGetScript, map[string]any{"id": user.ID})
	if !strings.Contains(get, "BEGIN UNTRUSTED SCRIPT LUA SOURCE") {
		t.Errorf("source not fenced:\n%s", get)
	}
}

// An update of a script the user wrote never overwrites it: it waits as a
// proposal the user reads as a diff and accepts or rejects.
func TestUpdateOfUsersScriptIsAProposal(t *testing.T) {
	a, _, cs := mcpScriptSession(t)
	user, err := a.CreateScript(ScriptInput{
		Name: "Users", Kind: "lua", Trigger: ScriptTriggerDTO{Type: scripting.TriggerManual},
		Source: "print('one')\nprint('two')", Permissions: []string{scripting.PermNotify},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(user.ID, true, []string{scripting.PermNotify}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptAllowUnasked(user.ID, "mail.send", true); err != nil {
		t.Fatal(err)
	}
	out := mustCall(t, cs, mcpserver.ToolUpdateScript, map[string]any{
		"id": user.ID, "script": draft("Users", "print('one')\nprint('three')", scripting.PermNotify, scripting.PermMailRead),
	})
	if !strings.Contains(out, `"status": "proposed"`) {
		t.Fatalf("update %s", out)
	}
	cur, _ := a.GetScript(user.ID)
	if cur.Source != "print('one')\nprint('two')" || !cur.Enabled {
		t.Fatalf("the user's script was changed: %+v", cur)
	}
	if list := mustCall(t, cs, mcpserver.ToolListScripts, map[string]any{}); !strings.Contains(list, `"proposal_pending": true`) {
		t.Errorf("list does not show the proposal:\n%s", list)
	}

	p, err := a.ScriptProposal(user.ID)
	if err != nil || !p.Exists || !p.ResetsApproval {
		t.Fatalf("proposal %+v %v", p, err)
	}
	var minus, plus bool
	for _, l := range p.Diff {
		minus = minus || (l.Op == "-" && l.Text == "print('two')")
		plus = plus || (l.Op == "+" && l.Text == "print('three')")
	}
	if !minus || !plus || len(p.AddedPermissions) != 1 || p.AddedPermissions[0] != scripting.PermMailRead {
		t.Fatalf("diff %+v", p)
	}

	got, err := a.AcceptScriptProposal(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "print('one')\nprint('three')" || got.Enabled || len(got.Approved) != 0 || len(got.AllowUnasked) != 0 {
		t.Fatalf("accepted %+v", got)
	}
	if got.Origin != string(scripting.OriginUser) {
		t.Errorf("origin changed to %s", got.Origin)
	}
	if p, _ := a.ScriptProposal(user.ID); p.Exists {
		t.Error("proposal kept after accepting")
	}

	mustCall(t, cs, mcpserver.ToolUpdateScript, map[string]any{"id": user.ID, "script": draft("Users", "print('four')", scripting.PermNotify)})
	if err := a.RejectScriptProposal(user.ID); err != nil {
		t.Fatal(err)
	}
	if cur, _ := a.GetScript(user.ID); cur.Source != "print('one')\nprint('three')" {
		t.Errorf("rejecting changed the script: %q", cur.Source)
	}
	if p, _ := a.ScriptProposal(user.ID); p.Exists {
		t.Error("proposal kept after rejecting")
	}
}

// An assistant's own script that is still off and was never approved is
// changed directly and stays off; once the user approved it, changes become
// proposals too.
func TestUpdateOfAssistantsScript(t *testing.T) {
	a, _, cs := mcpScriptSession(t)
	mustCall(t, cs, mcpserver.ToolCreateScript, draft("Own", `print(1)`, scripting.PermNotify))
	list, _ := a.ListScripts()
	id := list[0].ID
	out := mustCall(t, cs, mcpserver.ToolUpdateScript, map[string]any{"id": id, "script": draft("Own", `print(2)`, scripting.PermNotify)})
	if !strings.Contains(out, `"status": "updated"`) {
		t.Fatalf("update %s", out)
	}
	if sc, _ := a.GetScript(id); sc.Source != "print(2)" || sc.Enabled {
		t.Fatalf("after update %+v", sc)
	}

	if _, err := a.SetScriptEnabled(id, true, []string{scripting.PermNotify}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetScriptEnabled(id, false, nil); err != nil {
		t.Fatal(err)
	}
	out = mustCall(t, cs, mcpserver.ToolUpdateScript, map[string]any{"id": id, "script": draft("Own", `print(3)`, scripting.PermNotify)})
	if !strings.Contains(out, `"status": "proposed"`) {
		t.Fatalf("an approved script was changed directly: %s", out)
	}
	if sc, _ := a.GetScript(id); sc.Source != "print(2)" {
		t.Fatal("changed despite the approval")
	}
	got, err := a.AcceptScriptProposal(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "print(3)" || len(got.Approved) != 0 {
		t.Fatalf("approval survived a code change: %+v", got)
	}
}

func TestReferenceThroughTheServer(t *testing.T) {
	_, _, cs := mcpScriptSession(t)
	mods := mustCall(t, cs, mcpserver.ToolScriptReference, map[string]any{"section": "modules"})
	if !strings.Contains(mods, "#### mail.move(") || !strings.Contains(mods, "Moves a message to a folder") {
		t.Errorf("modules section:\n%.2000s", mods)
	}
	for _, id := range []string{"http", "files", "clipboard", "ai"} {
		if !strings.Contains(mods, "### module "+id) {
			t.Errorf("reference lacks the %s module registered outside the core", id)
		}
	}
	if out := mustCall(t, cs, mcpserver.ToolValidateScript, draft("V", `mail.move(event.message, "x")`)); !strings.Contains(out, `"missing_permissions": [
    "mail.modify"
  ]`) {
		t.Errorf("validate %s", out)
	}
}

// Every outside module the app registers survives being copied into the
// sandbox, so a test run of a script using them works.
func TestTestRunWithOutsideModules(t *testing.T) {
	_, _, cs := mcpScriptSession(t)
	out := mustCall(t, cs, mcpserver.ToolTestScript, map[string]any{"script": draft("W", `print("x")`)})
	if !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("run %s", out)
	}
}

func TestScriptPermissionsListedOff(t *testing.T) {
	a, _ := newScriptTestApp(t)
	perms := a.MCPPermissions()
	var scripts int
	for _, p := range perms {
		if p.Group == mcpserver.GroupScripts {
			scripts++
			if p.Allowed {
				t.Errorf("%s is on by default", p.Tool)
			}
		}
	}
	if scripts != len(mcpserver.ScriptTools) {
		t.Errorf("listed %d script tools, want %d", scripts, len(mcpserver.ScriptTools))
	}
	if err := a.SetMCPPermission(mcpserver.ToolCreateScript, true); err != nil {
		t.Fatal(err)
	}
	if !a.mcpPermissions().Allows(mcpserver.ToolCreateScript) || a.mcpPermissions().Allows(mcpserver.ToolUpdateScript) {
		t.Error("switches are not independent")
	}
	if err := a.SetMCPPermission("delete_script", true); err == nil {
		t.Error("a tool that does not exist got a switch")
	}
}

func TestLineDiff(t *testing.T) {
	got := lineDiff([]string{"a", "b", "c", "d"}, []string{"a", "x", "c", "d", "e"})
	var b strings.Builder
	for _, l := range got {
		b.WriteString(l.Op + l.Text + "|")
	}
	if want := " a|-b|+x| c| d|+e|"; b.String() != want {
		t.Fatalf("diff %q, want %q", b.String(), want)
	}
}
