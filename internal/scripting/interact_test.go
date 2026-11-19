package scripting

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAsker answers every question with answer, after wait, and records it.
type fakeAsker struct {
	mu     sync.Mutex
	answer AskAnswer
	err    error
	wait   time.Duration
	asked  []AskRequest
}

func (a *fakeAsker) Ask(ctx context.Context, req AskRequest) (AskAnswer, error) {
	a.mu.Lock()
	a.asked = append(a.asked, req)
	a.mu.Unlock()
	if a.wait > 0 {
		select {
		case <-time.After(a.wait):
		case <-ctx.Done():
			return AskAnswer{}, ctx.Err()
		}
	}
	return a.answer, a.err
}

type fakeResults struct {
	mu    sync.Mutex
	shown []Result
}

func (f *fakeResults) Show(_ context.Context, r Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shown = append(f.shown, r)
	return nil
}

func askEngine(t *testing.T, asker Asker) *Engine {
	t.Helper()
	e := testEngine(t, newFakeMail())
	e.Hosts.Ask = asker
	e.Limits.Timeout = 300 * time.Millisecond
	return e
}

func runAttended(e *Engine, src string, attended bool) *Run {
	return e.Run(context.Background(), RunRequest{Script: luaScript(src), Trigger: TriggerManual, Event: map[string]any{}, Attended: attended})
}

func TestAskReturnsTheAnswerWithTheClockStopped(t *testing.T) {
	asker := &fakeAsker{answer: AskAnswer{Value: "Archive"}, wait: 500 * time.Millisecond}
	e := askEngine(t, asker)
	run := runAttended(e, `local f = ui.choose("Where to?", {"Archive", "Later"}) log.info("picked", f)`, true)
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	if len(asker.asked) != 1 || asker.asked[0].Control != "choice" || !asker.asked[0].Attended || asker.asked[0].Timeout != 0 {
		t.Fatalf("asked %+v", asker.asked)
	}
	if got := asker.asked[0].Params["options"]; len(got.([]any)) != 2 {
		t.Fatalf("options %v", got)
	}
	var ask, logged bool
	for _, s := range run.Steps {
		ask = ask || (s.Kind == StepAsk && strings.Contains(s.Summary, "Archive"))
		logged = logged || (s.Kind == StepLog && s.Summary == "picked Archive")
	}
	if !ask || !logged {
		t.Fatalf("steps %+v", run.Steps)
	}
}

func TestAskCancelEndsTheRunCleanly(t *testing.T) {
	e := askEngine(t, &fakeAsker{answer: AskAnswer{Cancelled: true}})
	run := runAttended(e, `local ok = pcall(ui.text, "Name?") log.info("after", ok)`, true)
	if run.Status != StatusCancelled || run.Error != "" {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	for _, s := range run.Steps {
		if s.Kind == StepLog {
			t.Fatalf("the run went on after a cancel: %+v", s)
		}
	}
}

func TestUnattendedAskTimesOut(t *testing.T) {
	e := askEngine(t, &fakeAsker{err: context.DeadlineExceeded})
	run := runAttended(e, `local v = ui.ask("text", "Name?", { default = "anon" }) log.info(v)`, false)
	if run.Status != StatusOK || run.Steps[len(run.Steps)-1].Summary != "anon" {
		t.Fatalf("%s %q %+v", run.Status, run.Error, run.Steps)
	}
	run = runAttended(e, `ui.ask("text", "Name?") log.info("never")`, false)
	if run.Status != StatusStopped {
		t.Fatalf("no default: %s %q", run.Status, run.Error)
	}
}

func TestAskTimeoutComesFromTheSettingWithinBounds(t *testing.T) {
	asker := &fakeAsker{answer: AskAnswer{Value: "x"}}
	e := askEngine(t, asker)
	setting := 5 * time.Minute
	e.AskTimeout = func() time.Duration { return setting }
	cases := []struct {
		src  string
		want time.Duration
	}{
		{`ui.text("Name?")`, 5 * time.Minute},
		{`ui.text("Name?", { timeout = 30 })`, 30 * time.Second},
		{`ui.text("Name?", { timeout = 1 })`, MinAskTimeout},
		{`ui.text("Name?", { timeout = 1e12 })`, MaxAskTimeout},
	}
	for _, tc := range cases {
		asker.asked = nil
		if run := runAttended(e, tc.src, false); run.Status != StatusOK || len(asker.asked) != 1 || asker.asked[0].Timeout != tc.want {
			t.Fatalf("%s: %s %q %+v", tc.src, run.Status, run.Error, asker.asked)
		}
	}
	for setting, want := range map[time.Duration]time.Duration{time.Second: MinAskTimeout, 48 * time.Hour: MaxAskTimeout} {
		e.AskTimeout = func() time.Duration { return setting }
		asker.asked = nil
		if run := runAttended(e, `ui.text("Name?")`, false); run.Status != StatusOK || asker.asked[0].Timeout != want {
			t.Fatalf("setting %s: %s %+v", setting, run.Status, asker.asked)
		}
	}
	asker.asked = nil
	if runAttended(e, `ui.text("Name?", { timeout = 30 })`, true); asker.asked[0].Timeout != 0 {
		t.Fatalf("a question of a run the user started timed out: %+v", asker.asked)
	}
}

func TestAskChecksTheAnswer(t *testing.T) {
	e := askEngine(t, &fakeAsker{answer: AskAnswer{Value: "Elsewhere"}})
	run := runAttended(e, `ui.choose("Where?", {"Archive"})`, true)
	if run.Status != StatusError || !strings.Contains(run.Error, "not one of the options") {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	e = askEngine(t, &fakeAsker{answer: AskAnswer{Value: true}})
	run = runAttended(e, `if ui.confirm("Sure?") then log.info("yes") end`, true)
	if run.Status != StatusOK || run.Steps[len(run.Steps)-1].Summary != "yes" {
		t.Fatalf("%s %+v", run.Status, run.Steps)
	}
}

func TestDryRunStillAsks(t *testing.T) {
	asker := &fakeAsker{answer: AskAnswer{Value: "x"}}
	e := askEngine(t, asker)
	run := e.Run(context.Background(), RunRequest{Script: luaScript(`ui.text("Name?")`), Trigger: TriggerManual, Dry: true, Attended: true})
	if run.Status != StatusOK || len(asker.asked) != 1 || !asker.asked[0].Dry {
		t.Fatalf("%s %+v", run.Status, asker.asked)
	}
}

func TestAskWithoutAskerTakesTheDefault(t *testing.T) {
	e := askEngine(t, nil)
	e.Hosts.Ask = nil
	run := runAttended(e, `log.info(ui.text("Name?", { default = "d" }))`, true)
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	run = runAttended(e, `ui.text("Name?")`, true)
	if run.Status != StatusStopped {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
}

func TestShowResultOffersPermittedActions(t *testing.T) {
	r := testRegistry(t)
	if err := r.RegisterResultAction(ResultAction{Meta: meta("result", "mail.forward_text", "mail"), Icon: "send", Permission: PermMailSend,
		Run: func(context.Context, Result) (string, error) { return "", nil }}); err != nil {
		t.Fatal(err)
	}
	results := &fakeResults{}
	e := testEngine(t, newFakeMail())
	e.Registry = r
	e.Hosts.Result = results
	run := runAttended(e, `ui.show("# Done\n\nall good", { title = "Report", format = "markdown" })`, true)
	if run.Status != StatusOK || len(results.shown) != 1 {
		t.Fatalf("%s %q %+v", run.Status, run.Error, results.shown)
	}
	got := results.shown[0]
	if got.Title != "Report" || got.Format != "markdown" || !slices.Contains(got.Actions, "copy") || slices.Contains(got.Actions, "mail.forward_text") {
		t.Fatalf("result %+v", got)
	}
	if err := r.RegisterResultAction(ResultAction{Meta: meta("result", "bad", "x"), Client: "x", Run: func(context.Context, Result) (string, error) { return "", nil }}); err == nil {
		t.Fatal("an action with both a client and Run was accepted")
	}
}

func TestInputKindRegistersItsAction(t *testing.T) {
	r := testRegistry(t)
	a, ok := r.Action("ui.choice")
	if !ok || len(a.Outputs) != 1 || a.Outputs[0].Name != "value" {
		t.Fatalf("action %+v", a)
	}
	tree := Tree{Blocks: []Block{
		{ID: "b1", Kind: BlockAction, Type: "ui.choice", Args: map[string]Arg{"prompt": {Value: "Where?"}, "options": {Value: []any{"a", "b"}}}},
		{ID: "b2", Kind: BlockAction, Type: "log.write", Args: map[string]Arg{"text": {Ref: "step:b1.value"}}},
	}}
	res := Compile(r, tree, TriggerManual, false)
	if len(res.Errors) > 0 || !strings.Contains(res.Lua, `ui.ask("choice", "Where?", { options = {"a", "b"}`) {
		t.Fatalf("%+v\n%s", res.Errors, res.Lua)
	}
	if err := r.RegisterInputKind(InputKind{Meta: meta("input", "bad", "x"), Control: "text", Value: "string", Params: []Param{NewParam("prompt", TypeString)}}); err == nil {
		t.Fatal("a kind with a reserved parameter was accepted")
	}
}

func TestCancelRunningRun(t *testing.T) {
	s := testService(t, newFakeMail(), nil)
	ctx := context.Background()
	sc, err := s.Create(ctx, newScript("spin", `while true do end`, TriggerManual))
	if err != nil {
		t.Fatal(err)
	}
	enable(t, s, sc)
	done := make(chan *Run, 1)
	go func() {
		run, _ := s.RunNow(ctx, sc.ID, nil)
		done <- run
	}()
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.live) == 1
	})
	s.mu.Lock()
	var id int64
	for k := range s.live {
		id = k
	}
	s.mu.Unlock()
	if !s.Cancel(id) {
		t.Fatal("the run was not found")
	}
	run := <-done
	if run.Status != StatusCancelled || run.Error != "" {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	if s.Cancel(id) {
		t.Fatal("a finished run was cancelled")
	}
}

func TestProgressIsReported(t *testing.T) {
	var mu sync.Mutex
	var seen []LiveRun
	db := testDB(t)
	limits := DefaultLimits()
	limits.Timeout = 2 * time.Second
	s, err := NewService(Config{DB: db.Handle(), Hosts: &Hosts{Mail: newFakeMail(), Notify: &fakeNotify{}}, Limits: limits,
		OnProgress: func(l LiveRun) { mu.Lock(); seen = append(seen, l); mu.Unlock() }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sc, _ := s.Create(ctx, newScript("slow", `local t = time.now() local n = 0 while n < 3e6 do n = n + 1 end log.info("done")`, TriggerManual))
	enable(t, s, sc)
	if _, err := s.RunNow(ctx, sc.ID, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) > 0 && !seen[len(seen)-1].Done {
		t.Fatalf("the last report is not done: %+v", seen)
	}
}

func TestBlockRunMarksBranchesAndUnreachedBlocks(t *testing.T) {
	mail := newFakeMail()
	e := testEngine(t, mail)
	tree, _ := ParseTree([]byte(sampleTree))
	sc := &Script{ID: 1, Name: "rule", Kind: KindBlocks, Blocks: &tree, Trigger: TriggerConfig{Type: TriggerMailArrived},
		Permissions: []string{PermMailRead, PermMailModify, PermMailCompose}, Approved: []string{PermMailRead, PermMailModify, PermMailCompose}}
	run := e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerMailArrived, Event: map[string]any{"message": MessageObject(mail.messages[1])}})
	if run.Status != StatusOK {
		t.Fatalf("%s %q", run.Status, run.Error)
	}
	if !slices.Equal(run.NotReached, []string{"b4", "b5"}) {
		t.Fatalf("not reached %v", run.NotReached)
	}
	for _, s := range run.Steps {
		if s.Kind == StepCheck && (s.Branch == nil || *s.Branch != 0) {
			t.Fatalf("check without its branch: %+v", s)
		}
	}

	run = e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerMailArrived, Event: map[string]any{"message": MessageObject(mail.messages[2])}})
	var branches []int
	for _, s := range run.Steps {
		if s.Kind == StepCheck {
			branches = append(branches, *s.Branch)
		}
	}
	if !slices.Equal(branches, []int{0, 1}) || !slices.Contains(run.NotReached, "b6") || slices.Contains(run.NotReached, "b5") {
		t.Fatalf("branches %v, not reached %v", branches, run.NotReached)
	}
}

func TestErrorNamesTheLineOrBlock(t *testing.T) {
	e := testEngine(t, newFakeMail())
	run := runLua(t, e, "local a = 1\nlocal b = nil\nlog.info(b.x)\n")
	if run.Status != StatusError || run.ErrorLine != 3 {
		t.Fatalf("%s %q line %d", run.Status, run.Error, run.ErrorLine)
	}
	tree := Tree{Blocks: []Block{
		{ID: "b1", Kind: BlockAction, Type: "log.write", Args: map[string]Arg{"text": {Value: "hi"}}},
		{ID: "b2", Kind: BlockLua, Code: "error('boom')"},
		{ID: "b3", Kind: BlockStop},
	}}
	sc := &Script{ID: 1, Name: "b", Kind: KindBlocks, Blocks: &tree, Trigger: TriggerConfig{Type: TriggerManual}}
	run = e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerManual})
	if run.Status != StatusError || run.ErrorBlock != "b2" || !slices.Equal(run.NotReached, []string{"b3"}) {
		t.Fatalf("%s %q block %q not reached %v", run.Status, run.Error, run.ErrorBlock, run.NotReached)
	}
}

func TestCompileLineMap(t *testing.T) {
	tree, _ := ParseTree([]byte(sampleTree))
	res := Compile(testRegistry(t), tree, TriggerMailArrived, false)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	lines := strings.Split(strings.TrimSuffix(res.Lua, "\n"), "\n")
	check := func(id, want string) {
		t.Helper()
		r, ok := res.Lines[id]
		if !ok || r.From < 1 || r.To > len(lines) || r.From > r.To {
			t.Fatalf("%s: range %+v of %d lines", id, r, len(lines))
		}
		if !strings.Contains(lines[r.From-1], want) {
			t.Fatalf("%s starts at %q, want %q", id, lines[r.From-1], want)
		}
	}
	check("b1", "if ")
	check("b2", "mail.move")
	check("b6", `vars["who"]`)
	check("b8", "for _, item")
	check("b10", "do")
	if b1, b2 := res.Lines["b1"], res.Lines["b2"]; b2.From <= b1.From || b2.To >= b1.To {
		t.Fatalf("b2 %+v not inside b1 %+v", b2, b1)
	}
	if res.Lines["b1"].To != res.Lines["b6"].From-1 {
		t.Fatalf("b1 %+v does not end before b6 %+v", res.Lines["b1"], res.Lines["b6"])
	}
}

func TestTreeWritesEmptyLists(t *testing.T) {
	b, err := json.Marshal(Tree{Version: 1, Blocks: []Block{{ID: "b1", Kind: BlockIf, Branches: []Branch{{When: Cond{Kind: CondAll}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") || !strings.Contains(string(b), `"body":[]`) {
		t.Fatalf("%s", b)
	}
	b, _ = json.Marshal(Tree{Version: 1})
	if string(b) != `{"version":1,"blocks":[]}` {
		t.Fatalf("%s", b)
	}
}

func TestLuaSettings(t *testing.T) {
	src := `
-- a script with settings
SETTINGS = {
  { name = "folder", type = "folder", label = "Target folder", default = "Archive" },
  { name = "mode", type = "enum", options = { "fast", { value = "slow", label = "Slowly" } }, default = "fast" },
  { name = "days", type = "number", default = -7, required = true },
}
log.info(settings.get("folder"))
`
	schema, err := LuaSettings(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(schema) != 3 || schema[0].Label != "Target folder" || schema[1].Options[1].Label != "Slowly" || schema[2].Default != float64(-7) {
		t.Fatalf("%+v", schema)
	}
	for _, bad := range []string{
		`SETTINGS = { { name = "x", type = "text", default = os.getenv("HOME") } }`,
		`SETTINGS = { { name = "x", type = "password" } }`,
		`SETTINGS = { { name = "x", type = "regex" } }`,
		`SETTINGS = { { name = "x" } }`,
		`SETTINGS = { { name = "x", type = "number", default = "seven" } }`,
		`SETTINGS = { { name = "x", type = "text", colour = "red" } }`,
		`SETTINGS = load("return {}")()`,
	} {
		if _, err := LuaSettings(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if schema, err := LuaSettings(`log.info("no settings")`); err != nil || schema != nil {
		t.Fatalf("%v %v", schema, err)
	}
}

func TestSettingsFromLuaAreStoredValidatedAndNotExported(t *testing.T) {
	s := testService(t, newFakeMail(), nil)
	ctx := context.Background()
	src := `SETTINGS = {
  { name = "folder", type = "folder", default = "Archive" },
  { name = "who", type = "account", default = 3 },
}
log.info(settings.get("folder"))`
	sc, err := s.Create(ctx, newScript("set", src, TriggerManual))
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.SettingsSchema) != 2 {
		t.Fatalf("schema %+v", sc.SettingsSchema)
	}
	if _, err := s.SetSettings(ctx, sc.ID, map[string]any{"folder": 3.0}); err == nil {
		t.Fatal("a number was accepted for a folder")
	}
	if _, err := s.SetSettings(ctx, sc.ID, map[string]any{"nope": "x"}); err == nil {
		t.Fatal("an undeclared setting was accepted")
	}
	sc, err = s.SetSettings(ctx, sc.ID, map[string]any{"folder": "Receipts", "who": 5.0})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Export(s.Registry(), sc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "Receipts") || !strings.Contains(text, `"default": "Archive"`) || strings.Contains(text, `"default": 3`) {
		t.Fatalf("export leaks values or keeps the account default:\n%s", text)
	}
	enable(t, s, sc)
	run, err := s.RunNow(ctx, sc.ID, nil)
	if err != nil || run.Steps[0].Summary != "Receipts" {
		t.Fatalf("%v %+v", err, run)
	}

	sc.Source = `SETTINGS = { { name = "folder", type = "folder" } } log.info("x")`
	sc, err = s.Update(ctx, sc)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sc.Settings["who"]; ok || sc.Settings["folder"] != "Receipts" {
		t.Fatalf("stale values kept: %v", sc.Settings)
	}
	if _, err := s.Create(ctx, newScript("bad", `SETTINGS = { { name = "p", type = "password" } }`, TriggerManual)); err == nil {
		t.Fatal("a password setting was accepted")
	}
}

func TestPlacements(t *testing.T) {
	s := testService(t, newFakeMail(), nil)
	ctx := context.Background()
	manual, _ := s.Registry().Trigger(TriggerManual)
	if !slices.ContainsFunc(manual.Event, func(f Field) bool { return f.Name == "messages" && f.Of == "message" }) {
		t.Fatalf("manual event %+v", manual.Event)
	}

	none, _ := s.Create(ctx, newScript("plain", `log.info("plain")`, TriggerManual))
	msgSrc := `log.info(event.message.subject, #event.messages)`
	msg := newScript("on mail", msgSrc, TriggerManual)
	msg.Trigger.Params = map[string]any{"scope": "message"}
	msg, _ = s.Create(ctx, msg)

	if _, err := s.SetPlacements(ctx, none.ID, []PlacementConfig{{ID: "mail.row"}}); err == nil {
		t.Fatal("a script without a message took the mail menu")
	}
	if _, err := s.SetPlacements(ctx, msg.ID, []PlacementConfig{{ID: "toolbar.list", Options: map[string]any{"icon": 4.0}}}); err == nil {
		t.Fatal("a bad option was accepted")
	}
	if _, err := s.SetPlacements(ctx, none.ID, []PlacementConfig{{ID: "palette"}}); err != nil {
		t.Fatal(err)
	}
	msg, err := s.SetPlacements(ctx, msg.ID, []PlacementConfig{{ID: "mail.row"}, {ID: "toolbar.list", Options: map[string]any{"icon": "star", "label": "Go"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Placed(); len(got) != 0 {
		t.Fatalf("switched off scripts offered: %v", got)
	}
	none = enable(t, s, none)
	enable(t, s, msg)
	got := s.Placed()
	if len(got["palette"]) != 1 || len(got["mail.row"]) != 1 || got["toolbar.list"][0].Options["label"] != "Go" {
		t.Fatalf("placed %+v", got)
	}

	run, err := s.RunPlaced(ctx, msg.ID, "mail.row", []PlacedItem{{ID: "1"}, {ID: "2"}})
	if err != nil || run.Status != StatusOK || run.Steps[0].Summary != "Invoice 42 2" {
		t.Fatalf("%v %+v", err, run)
	}
	if _, err := s.RunPlaced(ctx, msg.ID, "palette", nil); err == nil {
		t.Fatal("ran from a place the script is not at")
	}

	msg.Trigger.Params = map[string]any{"scope": "none"}
	msg, err = s.Update(ctx, msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Placements) != 0 {
		t.Fatalf("placements kept after the scope changed: %+v", msg.Placements)
	}

	data, _ := Export(s.Registry(), none)
	if !strings.Contains(string(data), `"placements"`) {
		t.Fatalf("export lacks placements:\n%s", data)
	}
}

func TestPlacementValueFromFields(t *testing.T) {
	r := testRegistry(t)
	if err := r.RegisterValueType(ValueType{Meta: meta("type", "thing", "x"), Fields: []Field{field("name", "string")}}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterPlacement(Placement{Meta: meta("place", "thing.menu", "x"), Surface: SurfaceMenu, ValueType: "thing"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterPlacement(Placement{Meta: meta("place", "bad", "x"), Surface: SurfaceMenu, ValueType: "nothing"}); err == nil {
		t.Fatal("a placement of an unknown value type was accepted")
	}
	s := &Service{reg: r, cfg: Config{Hosts: &Hosts{}}}
	p, _ := r.Placement("thing.menu")
	data, err := s.placedEvent(context.Background(), p, []PlacedItem{{Fields: map[string]any{"name": "a", "secret": "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	obj := data["thing"].(Object)
	if obj.Fields["name"] != "a" || obj.Fields["secret"] != nil {
		t.Fatalf("%+v", obj)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
