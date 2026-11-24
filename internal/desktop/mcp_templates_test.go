package desktop

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/mcpserver"
)

// TestTemplatesThroughTheServer checks that the template tools are offered
// behind their switches, list the gallery and hand out a draft that
// create_script stores switched off.
func TestTemplatesThroughTheServer(t *testing.T) {
	if tools := mcpPermissionTools(); !slices.Contains(tools, mcpserver.ToolListTemplates) || !slices.Contains(tools, mcpserver.ToolGetScriptTemplate) {
		t.Fatalf("template tools have no switch: %v", tools)
	}
	a, _, cs := mcpScriptSession(t)
	list := mustCall(t, cs, mcpserver.ToolListTemplates, map[string]any{})
	if !strings.Contains(list, `"newsletters"`) {
		t.Fatalf("list %s", list)
	}
	var tpl mcpserver.ScriptTemplateDetail
	if err := json.Unmarshal([]byte(mustCall(t, cs, mcpserver.ToolGetScriptTemplate, map[string]any{"id": "newsletters"})), &tpl); err != nil {
		t.Fatal(err)
	}
	if tpl.ID != "newsletters" || tpl.Script.Kind == "" || tpl.Script.Trigger.Type == "" || len(tpl.Script.Permissions) == 0 {
		t.Fatalf("template %+v", tpl)
	}
	var args map[string]any
	data, _ := json.Marshal(tpl.Script)
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatal(err)
	}
	if out := mustCall(t, cs, mcpserver.ToolCreateScript, args); !strings.Contains(out, `"created"`) {
		t.Fatalf("create from template %s", out)
	}
	everyScriptOff(t, a)
	if text, failed := callTool(t, cs, mcpserver.ToolGetScriptTemplate, map[string]any{"id": "nope"}); !failed || !strings.Contains(text, "no template") {
		t.Fatalf("unknown template: %s", text)
	}
}

// TestAssistantBeforeSendScriptStillAsks checks that a before-send script an
// assistant wrote, once the user switches it on, cannot change recipients
// without the question either.
func TestAssistantBeforeSendScriptStillAsks(t *testing.T) {
	a, _, cs := mcpScriptSession(t)
	d := draft("Copy", `table.insert(event.draft.to, "spy@example.test")`, "mail.send")
	d["trigger"] = map[string]any{"type": "mail.before_send"}
	mustCall(t, cs, mcpserver.ToolCreateScript, d)
	list, _ := a.ListScripts()
	if _, err := a.SetScriptEnabled(list[0].ID, true, list[0].Permissions); err != nil {
		t.Fatal(err)
	}
	ask := &answerConfirm{}
	a.recipientConfirmer = ask
	if _, err := a.beforeSendScripts(a.ctx, ComposeRequest{AccountID: 1, Subject: "s", To: []AddressDTO{{Email: "bo@example.test"}}}); !errors.Is(err, errRecipientsHeld) || len(ask.asked) != 1 {
		t.Fatalf("%v, asked %d", err, len(ask.asked))
	}
}
