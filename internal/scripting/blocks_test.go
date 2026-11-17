package scripting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const sampleTree = `{
  "version": 1,
  "blocks": [
    {"id": "b1", "kind": "if", "branches": [
      {"when": {"kind": "all", "items": [
        {"kind": "condition", "type": "mail.from", "args": {"op": {"value": "contains"}, "value": {"value": "billing"}}},
        {"kind": "not", "items": [{"kind": "condition", "type": "mail.subject", "args": {"op": {"value": "matches"}, "value": {"value": "(?i)reminder"}}}]}
      ]}, "body": [
        {"id": "b2", "kind": "action", "type": "mail.move", "args": {"folder": {"value": "Receipts"}}},
        {"id": "b3", "kind": "action", "type": "mail.color", "args": {"color": {"value": 3}}}
      ]},
      {"when": {"kind": "condition", "type": "mail.has_attachment"}, "body": [
        {"id": "b4", "kind": "action", "type": "mail.flag"}
      ]}
    ], "else": [
      {"id": "b5", "kind": "stop"}
    ]},
    {"id": "b6", "kind": "set", "as": "who", "value": {"ref": "event.message.from"}},
    {"id": "b7", "kind": "action", "type": "mail.create_draft", "args": {"to": {"value": ["a@example.org"]}, "subject": {"ref": "var:who"}}},
    {"id": "b8", "kind": "foreach", "as": "addr", "list": {"value": ["x@example.org", "y@example.org"]}, "body": [
      {"id": "b9", "kind": "action", "type": "log.write", "args": {"text": {"ref": "var:addr"}}}
    ]},
    {"id": "b10", "kind": "lua", "code": "log.info(\"draft\", steps[\"b7\"].id)"}
  ]
}`

func TestCompileTreeRoundTrip(t *testing.T) {
	r := testRegistry(t)
	tree, err := ParseTree([]byte(sampleTree))
	if err != nil {
		t.Fatal(err)
	}
	first := Compile(r, tree, TriggerMailArrived, false)
	if len(first.Errors) > 0 {
		t.Fatalf("errors %v", first.Errors)
	}
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ParseTree(data)
	if err != nil {
		t.Fatal(err)
	}
	second := Compile(r, again, TriggerMailArrived, false)
	if first.Lua != second.Lua {
		t.Fatalf("round trip changed the Lua:\n%s\n---\n%s", first.Lua, second.Lua)
	}
	want := []string{
		`if ((text.test({ event.message.from, event.message.from_name }, "contains", "billing")) and (not (text.test(event.message.subject, "matches", "(?i)reminder")))) then`,
		`  mail.move(event.message, "Receipts")`,
		`  mail.set_color(event.message, 3)`,
		`elseif (event.message.has_attachments == true) then`,
		`  mail.flag(event.message, true)`,
		`else`,
		`  stop()`,
		`vars["who"] = event.message.from`,
		`steps["b7"] = mail.draft({to = {"a@example.org"}, cc = nil, subject = vars["who"], body = nil})`,
		`for _, item in ipairs({"x@example.org", "y@example.org"} or {}) do`,
		`  log["info"](vars["addr"])`,
		`  log.info("draft", steps["b7"].id)`,
	}
	for _, line := range want {
		if !strings.Contains(first.Lua, line+"\n") {
			t.Errorf("missing line %q in:\n%s", line, first.Lua)
		}
	}
	if strings.Contains(first.Lua, "__block") {
		t.Error("plain output has trace calls")
	}
	traced := Compile(r, tree, TriggerMailArrived, true)
	if !strings.Contains(traced.Lua, `__block("b2")`) || !strings.Contains(traced.Lua, `__check("b1", `) {
		t.Errorf("traced output lacks trace calls:\n%s", traced.Lua)
	}
	for _, p := range []string{PermMailModify, PermMailCompose} {
		if !strings.Contains(strings.Join(first.Permissions, ","), p) {
			t.Errorf("permissions %v lack %s", first.Permissions, p)
		}
	}
	if err := CheckLua(first.Lua); err != nil {
		t.Fatal(err)
	}
}

func TestCompiledBlocksRun(t *testing.T) {
	mail := newFakeMail()
	e := testEngine(t, mail)
	tree, _ := ParseTree([]byte(sampleTree))
	sc := &Script{ID: 1, Name: "rule", Kind: KindBlocks, Blocks: &tree, Trigger: TriggerConfig{Type: TriggerMailArrived},
		Permissions: []string{PermMailRead, PermMailModify, PermMailCompose}, Approved: []string{PermMailRead, PermMailModify, PermMailCompose}}
	run := e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerMailArrived, Event: map[string]any{"message": MessageObject(mail.messages[1])}})
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	calls := strings.Join(mail.Calls(), "|")
	if !strings.HasPrefix(calls, "move 1 Receipts|color 1 3|compose send=false") {
		t.Fatalf("calls %s", calls)
	}
	var checked, inBlock bool
	for _, s := range run.Steps {
		if s.Kind == StepCheck && s.Block == "b1" && s.Result {
			checked = true
		}
		if s.Kind == StepCall && s.Function == "mail.move" && s.Block == "b2" {
			inBlock = true
		}
	}
	if !checked || !inBlock {
		t.Fatalf("steps lack block info: %+v", run.Steps)
	}

	run = e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerMailArrived, Event: map[string]any{"message": MessageObject(mail.messages[2])}})
	if run.Status != StatusStopped {
		t.Fatalf("else branch: %s %q", run.Status, run.Error)
	}
}

func TestCompileErrors(t *testing.T) {
	r := testRegistry(t)
	cases := map[string]string{
		"unknown action":    `[{"id": "a", "kind": "action", "type": "mail.teleport"}]`,
		"missing required":  `[{"id": "a", "kind": "action", "type": "mail.move"}]`,
		"bad literal":       `[{"id": "a", "kind": "action", "type": "mail.color", "args": {"color": {"value": 12}}}]`,
		"bad enum":          `[{"id": "a", "kind": "if", "branches": [{"when": {"kind": "condition", "type": "mail.subject", "args": {"op": {"value": "eats"}, "value": {"value": "x"}}}, "body": []}]}]`,
		"injection ref":     `[{"id": "a", "kind": "set", "as": "x", "value": {"ref": "event.message); os.exit("}}]`,
		"bad var":           `[{"id": "a", "kind": "set", "as": "x y"}]`,
		"duplicate id":      `[{"id": "a", "kind": "stop"}, {"id": "a", "kind": "stop"}]`,
		"step without out":  `[{"id": "a", "kind": "action", "type": "mail.flag"}, {"id": "b", "kind": "set", "as": "x", "value": {"ref": "step:a.id"}}]`,
		"wrong trigger":     `[{"id": "a", "kind": "action", "type": "outgoing.cancel"}]`,
		"unknown parameter": `[{"id": "a", "kind": "action", "type": "mail.flag", "args": {"colour": {"value": 1}}}]`,
		"not with two":      `[{"id": "a", "kind": "if", "branches": [{"when": {"kind": "not", "items": []}, "body": []}]}]`,
	}
	for name, blocks := range cases {
		tree, err := ParseTree([]byte(`{"version": 1, "blocks": ` + blocks + `}`))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		res := Compile(r, tree, TriggerMailArrived, false)
		if len(res.Errors) == 0 || res.Lua != "" {
			t.Errorf("%s: want errors, got Lua:\n%s", name, res.Lua)
		}
	}
}

func TestLiteralsCannotBreakOut(t *testing.T) {
	r := testRegistry(t)
	evil := "\"); os.exit(1); --\n]]\x00\xff"
	tree := Tree{Version: 1, Blocks: []Block{{ID: "a", Kind: BlockAction, Type: "log.write", Args: map[string]Arg{"text": {Value: evil}}}}}
	res := Compile(r, tree, TriggerManual, false)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	e := &Engine{Registry: r, Hosts: &Hosts{}, Limits: DefaultLimits()}
	sc := &Script{Name: "x", Kind: KindLua, Source: res.Lua, Trigger: TriggerConfig{Type: TriggerManual}}
	run := e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerManual})
	if run.Status != StatusOK || len(run.Steps) != 1 || run.Steps[0].Summary != evil {
		t.Fatalf("%s %q %+v", run.Status, run.Error, run.Steps)
	}
}

func TestTypedLiterals(t *testing.T) {
	bad := map[ParamType]any{TypeRegex: "(", TypeTime: "25:00", TypeColor: float64(9), TypeNumber: "x", TypeBool: "yes", TypeDuration: "soon"}
	for typ, v := range bad {
		if _, err := literalLua(Param{Name: "p", Type: typ}, v); err == nil {
			t.Errorf("%s accepted %v", typ, v)
		}
	}
	list := Param{Name: "p", Type: TypeList, Of: TypeAddress}
	if got, err := literalLua(list, []any{"a@b", "c@d"}); err != nil || got != `{"a@b", "c@d"}` {
		t.Errorf("list: %s %v", got, err)
	}
}

func TestDurationAndDefaults(t *testing.T) {
	r := testRegistry(t)
	tree := Tree{Version: 1, Blocks: []Block{{ID: "a", Kind: BlockAction, Type: "mail.snooze", Args: map[string]Arg{"duration": {Value: "2h"}}}}}
	res := Compile(r, tree, TriggerMailArrived, false)
	if len(res.Errors) > 0 || !strings.Contains(res.Lua, "mail.snooze(event.message, time.now() + 7200)") {
		t.Fatalf("%v\n%s", res.Errors, res.Lua)
	}
	for in, want := range map[string]float64{"90s": 90, "15m": 900, "3d": 259200, "1w": 604800, "45": 45} {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
}
