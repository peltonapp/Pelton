package scripting

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func newScript(name, src string, trigger string, perms ...string) *Script {
	return &Script{Name: name, Kind: KindLua, Source: src, Trigger: TriggerConfig{Type: trigger}, Permissions: perms}
}

func enable(t *testing.T, s *Service, sc *Script) *Script {
	t.Helper()
	out, err := s.SetEnabled(context.Background(), sc.ID, true, sc.Permissions)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestStoreCRUDAndApproval(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	created, err := s.Create(ctx, newScript("Rule", `mail.archive(event.message)`, TriggerMailArrived, PermMailRead, PermMailModify))
	if err != nil {
		t.Fatal(err)
	}
	if created.Enabled || created.Origin != OriginUser || created.CatchUp != CatchUpLive || created.Trigger.Params["folder"] != "inbox" {
		t.Fatalf("created %+v", created)
	}
	if _, err := s.SetEnabled(ctx, created.ID, true, []string{PermMailRead}); err == nil {
		t.Fatal("enabled with a permission unapproved")
	}
	on := enable(t, s, created)
	if !on.Enabled || len(on.Approved) != 2 || on.EnabledAt.IsZero() {
		t.Fatalf("enabled %+v", on)
	}
	on.Permissions = append(on.Permissions, PermMailSend)
	updated, err := s.Update(ctx, on)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("a new permission left the script on")
	}
	if _, err := s.Create(ctx, newScript("Bad", `x`, TriggerMailArrived)); err == nil {
		t.Fatal("created without the trigger permission")
	}
	if _, err := s.Create(ctx, newScript("Bad", `x`, "nope")); err == nil {
		t.Fatal("created with an unknown trigger")
	}
	broken, _ := s.Create(ctx, newScript("Broken", `if then`, TriggerManual))
	if _, err := s.SetEnabled(ctx, broken.ID, true, nil); err == nil {
		t.Fatal("enabled a script that does not compile")
	}
	if err := s.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Script(ctx, created.ID); err != ErrNotFound {
		t.Fatalf("after delete: %v", err)
	}
}

func TestKVStoreThroughScripts(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	sc, _ := s.Create(ctx, newScript("Counter", `local n = store.get("n") or 0; store.set("n", n + 1); log.info(n + 1)`, TriggerManual))
	enable(t, s, sc)
	for i := 0; i < 3; i++ {
		run, err := s.RunNow(ctx, sc.ID, nil)
		if err != nil || run.Status != StatusOK {
			t.Fatalf("%v %+v", err, run)
		}
	}
	v, ok, err := s.Store().KVGet(ctx, sc.ID, "n")
	if err != nil || !ok || v != "3" {
		t.Fatalf("%q %v %v", v, ok, err)
	}
	runs, err := s.Store().Runs(ctx, sc.ID, 10)
	if err != nil || len(runs) != 3 || runs[0].Status != StatusOK || len(runs[0].Steps) == 0 {
		t.Fatalf("runs %v %+v", err, runs)
	}
	dry, err := s.TestRun(ctx, sc, nil)
	if err != nil || dry.Status != StatusOK {
		t.Fatalf("%v %+v", err, dry)
	}
	if v, _, _ := s.Store().KVGet(ctx, sc.ID, "n"); v != "3" {
		t.Fatalf("dry run wrote storage: %s", v)
	}
	if runs, _ := s.Store().Runs(ctx, sc.ID, 10); len(runs) != 3 {
		t.Fatal("dry run was logged")
	}
}

func TestDispatchMailArrived(t *testing.T) {
	ctx := context.Background()
	mail := newFakeMail()
	s := testService(t, mail, nil)
	var mu sync.Mutex
	var runs []*Run
	s.cfg.OnRun = func(_ *Script, r *Run) {
		mu.Lock()
		runs = append(runs, r)
		mu.Unlock()
	}
	sc, err := s.Create(ctx, newScript("Archive invoices",
		`if text.test(event.message.subject, "contains", "invoice") then mail.archive(event.message) end`,
		TriggerMailArrived, PermMailRead, PermMailModify))
	if err != nil {
		t.Fatal(err)
	}
	enable(t, s, sc)
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.Emit(Event{Trigger: TriggerMailArrived, Data: map[string]any{"message_id": int64(1)}})
	s.Emit(Event{Trigger: TriggerMailArrived, Data: map[string]any{"message_id": int64(2)}})
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(runs) == 2
	})
	if got := mail.Calls(); len(got) != 1 || got[0] != "archive 1" {
		t.Fatalf("calls %v", got)
	}
}

func TestLoopGuard(t *testing.T) {
	ctx := context.Background()
	mail := newFakeMail()
	s := testService(t, mail, nil)
	var mu sync.Mutex
	statuses := []Status{}
	s.cfg.OnRun = func(_ *Script, r *Run) {
		mu.Lock()
		statuses = append(statuses, r.Status)
		mu.Unlock()
	}
	// a script that reacts to flags by flagging, and the host feeding each
	// flag back as an event from inside the run, the way the desktop hooks do.
	sc, _ := s.Create(ctx, newScript("Echo", `mail.flag(event.message, not event.flagged)`, TriggerMailFlagged, PermMailRead, PermMailModify))
	enable(t, s, sc)
	s.cfg.Hosts.Mail = &loopMail{fakeMail: mail, s: s}
	s.engine.Hosts = s.cfg.Hosts
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.Emit(Event{Trigger: TriggerMailFlagged, Data: map[string]any{"message_id": int64(1), "flagged": true}})
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) == MaxDepth+2
	})
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	skipped := 0
	for _, st := range statuses {
		if st == StatusSkipped {
			skipped++
		}
	}
	if len(statuses) != MaxDepth+2 || skipped != 1 {
		t.Fatalf("statuses %v", statuses)
	}
	if len(mail.Calls()) != MaxDepth+1 {
		t.Fatalf("calls %v", mail.Calls())
	}
}

type loopMail struct {
	*fakeMail
	s *Service
}

func (l *loopMail) SetFlagged(ctx context.Context, id int64, on bool) error {
	if err := l.fakeMail.SetFlagged(ctx, id, on); err != nil {
		return err
	}
	l.s.Emit(Event{Trigger: TriggerMailFlagged, Data: map[string]any{"message_id": id, "flagged": on}, Depth: EventDepth(ctx)})
	return nil
}

func TestRateLimit(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	sc, _ := s.Create(ctx, newScript("Busy", `log.info("x")`, TriggerAppStart))
	sc = enable(t, s, sc)
	if err := s.load(ctx); err != nil {
		t.Fatal(err)
	}
	allowed := 0
	for i := 0; i < maxRunsPerMinute+10; i++ {
		if s.guard(sc, Event{Trigger: TriggerMailArrived}) {
			allowed++
		}
	}
	if allowed != maxRunsPerMinute {
		t.Fatalf("allowed %d", allowed)
	}
	runs, _ := s.Store().Runs(ctx, sc.ID, 100)
	if len(runs) != 1 || runs[0].Status != StatusSkipped {
		t.Fatalf("skipped runs logged: %+v", runs)
	}
}

func TestSchedules(t *testing.T) {
	r := testRegistry(t)
	loc := time.UTC
	at := func(h, m int) time.Time { return time.Date(2026, 11, 17, h, m, 0, 0, loc) } // a tuesday
	daily, _ := r.Trigger(TriggerDaily)
	params := map[string]any{"at": "09:00"}
	since := at(7, 0)
	if daily.Due(params, time.Time{}, since, at(8, 59)) {
		t.Error("due before the time")
	}
	if !daily.Due(params, time.Time{}, since, at(9, 0)) {
		t.Error("not due at the time")
	}
	if daily.Due(params, at(9, 0), since, at(15, 0)) {
		t.Error("due twice a day")
	}
	if daily.Due(params, time.Time{}, at(12, 0), at(15, 0)) {
		t.Error("due straight after enabling past the time")
	}
	weekdays, _ := r.Trigger(TriggerWeekdays)
	if !weekdays.Due(map[string]any{"at": "09:00", "days": []any{"tue"}}, time.Time{}, since, at(9, 1)) {
		t.Error("weekday not due")
	}
	if weekdays.Due(map[string]any{"at": "09:00", "days": []any{"mon"}}, time.Time{}, since, at(9, 1)) {
		t.Error("due on the wrong day")
	}
	every, _ := r.Trigger(TriggerEvery)
	if every.Due(map[string]any{"minutes": float64(15)}, at(9, 0), since, at(9, 10)) ||
		!every.Due(map[string]any{"minutes": float64(15)}, at(9, 0), since, at(9, 15)) {
		t.Error("every")
	}
}

func TestScheduleTickRunsScript(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	ran := make(chan struct{}, 4)
	s.cfg.OnRun = func(_ *Script, r *Run) {
		if r.Trigger == TriggerEvery {
			ran <- struct{}{}
		}
	}
	sc := newScript("Tick", `log.info("tick")`, TriggerEvery)
	sc.Trigger.Params = map[string]any{"minutes": float64(5)}
	sc, _ = s.Create(ctx, sc)
	enable(t, s, sc)
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.tick(ctx, time.Now().Add(2*time.Minute))
	s.tick(ctx, time.Now().Add(6*time.Minute))
	s.tick(ctx, time.Now().Add(7*time.Minute))
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("no run")
	}
	select {
	case <-ran:
		t.Fatal("ran twice")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestBeforeSendRunSync(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	a, _ := s.Create(ctx, newScript("Tag", `event.draft.subject = "[x] " .. event.draft.subject`, TriggerBeforeSend, PermMailSend))
	enable(t, s, a)
	b, _ := s.Create(ctx, newScript("Guard", `if text.contains(event.draft.subject, "secret") then outgoing.cancel("secret") end`, TriggerBeforeSend, PermMailSend))
	enable(t, s, b)
	if err := s.load(ctx); err != nil {
		t.Fatal(err)
	}
	runs := s.RunSync(ctx, Event{Trigger: TriggerBeforeSend, Data: map[string]any{"draft": map[string]any{"subject": "hello", "to": []any{}}}})
	if len(runs) != 2 || runs[1].Cancelled {
		t.Fatalf("runs %+v", runs)
	}
	if d := runs[1].Event["draft"].(map[string]any); d["subject"] != "[x] hello" {
		t.Fatalf("draft %v", d)
	}
	runs = s.RunSync(ctx, Event{Trigger: TriggerBeforeSend, Data: map[string]any{"draft": map[string]any{"subject": "the secret plan"}}})
	if len(runs) != 2 || !runs[1].Cancelled || runs[1].CancelReason != "secret" {
		t.Fatalf("runs %+v", runs)
	}
}

func TestCatchUp(t *testing.T) {
	ctx := context.Background()
	mail := newFakeMail()
	s := testService(t, mail, nil)
	asked := make(chan int, 1)
	s.cfg.OnCatchUp = func(_ *Script, n int) { asked <- n }
	mk := func(name string, mode CatchUpMode) *Script {
		sc := newScript(name, `mail.flag(event.message)`, TriggerMailArrived, PermMailRead, PermMailModify)
		sc.CatchUp = mode
		sc.CatchUpCap = 1
		sc, _ = s.Create(ctx, sc)
		return enable(t, s, sc)
	}
	live := mk("live", CatchUpLive)
	ask := mk("ask", CatchUpAsk)
	_ = live
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	old := time.Now().Add(-365 * 24 * time.Hour)
	s.Emit(Event{Trigger: TriggerMailArrived, CatchUp: true, Date: old, Data: map[string]any{"message_id": int64(1)}})
	s.Emit(Event{Trigger: TriggerMailArrived, CatchUp: true, Date: time.Now().Add(time.Minute), Data: map[string]any{"message_id": int64(1)}})
	s.Emit(Event{Trigger: TriggerMailArrived, CatchUp: true, Date: time.Now().Add(2 * time.Minute), Data: map[string]any{"message_id": int64(2)}})
	select {
	case n := <-asked:
		if n != 1 {
			t.Fatalf("asked about %d", n)
		}
	case <-time.After(catchUpSettle + 3*time.Second):
		t.Fatal("not asked")
	}
	if len(mail.Calls()) != 0 {
		t.Fatalf("ran before the answer: %v", mail.Calls())
	}
	if got := s.PendingCatchUps(); got[ask.ID] != 1 {
		t.Fatalf("pending %v", got)
	}
	s.ResolveCatchUp(ask.ID, true)
	waitFor(t, func() bool { return len(mail.Calls()) == 1 })
	if got := mail.Calls()[0]; got != "flag 2 true" {
		t.Fatalf("caught up on %s, want the newest within the cap", got)
	}
}

func TestExportImport(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	tree, _ := ParseTree([]byte(`{"version":1,"blocks":[{"id":"a","kind":"if","branches":[{"when":{"kind":"condition","type":"mail.account","args":{"account":{"value":7}}},"body":[{"id":"b","kind":"action","type":"mail.archive"}]}]}]}`))
	sc := &Script{Name: "Mine", Description: "d", Kind: KindBlocks, Blocks: &tree,
		Trigger:     TriggerConfig{Type: TriggerMailArrived, Params: map[string]any{"account": float64(7), "folder": "INBOX"}},
		Permissions: []string{PermMailRead, PermMailModify}, Settings: map[string]any{"secret": "x"}}
	sc, err := s.Create(ctx, sc)
	if err != nil {
		t.Fatal(err)
	}
	enable(t, s, sc)
	data, err := Export(s.Registry(), sc)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"account"`) || strings.Contains(text, "secret") || strings.Contains(text, "approved") || !strings.Contains(text, `"format": 1`) {
		t.Fatalf("export leaks or lacks:\n%s", text)
	}
	imported, err := s.Import(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Enabled || imported.Origin != OriginImport || len(imported.Approved) != 0 || imported.Name != "Mine" {
		t.Fatalf("imported %+v", imported)
	}
	for name, bad := range map[string]string{
		"not json":      "hello",
		"no format":     `{"name":"x"}`,
		"future format": `{"format":99,"name":"x"}`,
		"unknown perm":  `{"format":1,"name":"x","kind":"lua","source":"","trigger":{"type":"manual"},"permissions":["root"]}`,
		"no name":       `{"format":1,"kind":"lua","trigger":{"type":"manual"},"permissions":[]}`,
	} {
		if _, err := ParseFile(s.Registry(), []byte(bad)); err == nil {
			t.Errorf("%s: imported", name)
		}
	}
}

func TestConvertToLua(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	tree, _ := ParseTree([]byte(`{"version":1,"blocks":[{"id":"a","kind":"action","type":"mail.flag"}]}`))
	sc, err := s.Create(ctx, &Script{Name: "B", Kind: KindBlocks, Blocks: &tree, Trigger: TriggerConfig{Type: TriggerMailArrived}, Permissions: []string{PermMailRead, PermMailModify}})
	if err != nil {
		t.Fatal(err)
	}
	lua, err := s.ConvertToLua(ctx, sc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lua.Kind != KindLua || lua.Blocks != nil || !strings.Contains(lua.Source, "mail.flag(event.message, true)") || strings.Contains(lua.Source, "__block") {
		t.Fatalf("%+v", lua)
	}
	if _, err := s.ConvertToLua(ctx, sc.ID); err == nil {
		t.Fatal("converted twice")
	}
}

func TestPruneRuns(t *testing.T) {
	ctx := context.Background()
	s := testService(t, newFakeMail(), nil)
	sc, _ := s.Create(ctx, newScript("P", `x = 1`, TriggerManual))
	for i := 0; i < 5; i++ {
		started := time.Now().Add(-time.Duration(i) * 24 * time.Hour)
		if err := s.Store().FinishRun(ctx, &Run{ScriptID: sc.ID, Trigger: TriggerManual, Started: started, Status: StatusOK}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Store().PruneRuns(ctx, time.Now().Add(-36*time.Hour), 2); err != nil {
		t.Fatal(err)
	}
	runs, _ := s.Store().Runs(ctx, sc.ID, 10)
	if len(runs) != 2 {
		t.Fatalf("kept %d", len(runs))
	}
}
