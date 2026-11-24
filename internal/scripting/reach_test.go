package scripting

import (
	"context"
	"sync"
	"testing"
	"time"
)

// sourceMail feeds a flag change back as an event built with EventFrom, the
// way the desktop hooks do.
type sourceMail struct {
	*fakeMail
	s *Service
}

func (m *sourceMail) SetFlagged(ctx context.Context, id int64, on bool) error {
	if err := m.fakeMail.SetFlagged(ctx, id, on); err != nil {
		return err
	}
	m.s.Emit(EventFrom(ctx, TriggerMailFlagged, map[string]any{"message_id": id, "flagged": on}))
	return nil
}

func TestOwnActionDoesNotRetrigger(t *testing.T) {
	ctx := context.Background()
	mail := newFakeMail()
	s := testService(t, mail, nil)
	var mu sync.Mutex
	runs := map[int64]int{}
	s.cfg.OnRun = func(sc *Script, _ *Run) {
		mu.Lock()
		runs[sc.ID]++
		mu.Unlock()
	}
	echo, _ := s.Create(ctx, newScript("Echo", `mail.flag(event.message, not event.flagged)`, TriggerMailFlagged, PermMailRead, PermMailModify))
	enable(t, s, echo)
	s.cfg.Hosts.Mail = &sourceMail{fakeMail: mail, s: s}
	s.engine.Hosts = s.cfg.Hosts
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.Emit(Event{Trigger: TriggerMailFlagged, Data: map[string]any{"message_id": int64(1), "flagged": true}})
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return runs[echo.ID] == 1
	})
	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if runs[echo.ID] != 1 || len(mail.Calls()) != 1 {
		t.Fatalf("runs %v calls %v", runs, mail.Calls())
	}
}

func TestTwoScriptsStopAtMaxDepth(t *testing.T) {
	ctx := context.Background()
	mail := newFakeMail()
	s := testService(t, mail, nil)
	var mu sync.Mutex
	var statuses []Status
	s.cfg.OnRun = func(_ *Script, r *Run) {
		mu.Lock()
		statuses = append(statuses, r.Status)
		mu.Unlock()
	}
	for _, name := range []string{"Ping", "Pong"} {
		sc, err := s.Create(ctx, newScript(name, `mail.flag(event.message, not event.flagged)`, TriggerMailFlagged, PermMailRead, PermMailModify))
		if err != nil {
			t.Fatal(err)
		}
		enable(t, s, sc)
	}
	s.cfg.Hosts.Mail = &sourceMail{fakeMail: mail, s: s}
	s.engine.Hosts = s.cfg.Hosts
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	s.Emit(Event{Trigger: TriggerMailFlagged, Data: map[string]any{"message_id": int64(1), "flagged": true}})
	time.Sleep(time.Second)
	mu.Lock()
	defer mu.Unlock()
	skipped := 0
	for _, st := range statuses {
		if st == StatusSkipped {
			skipped++
		}
	}
	if skipped == 0 || len(statuses) > 2*(MaxDepth+2) {
		t.Fatalf("statuses %v", statuses)
	}
}

func TestTargetedPermission(t *testing.T) {
	r := testRegistry(t)
	if err := r.RegisterPermission(Permission{Meta: Meta{ID: "web", Label: "Web"}, Risk: "high"}); err != nil {
		t.Fatal(err)
	}
	err := r.RegisterModule(Module{Meta: Meta{ID: "web", Label: "Web"}, Functions: []Function{{
		Name: "get", Permission: "web",
		Handler: func(c *Call) ([]any, error) {
			host, _ := c.String(0)
			if err := c.Require("web:" + host); err != nil {
				return nil, err
			}
			return []any{"ok " + host}, nil
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{Registry: r, Hosts: &Hosts{}, Limits: DefaultLimits()}
	run := func(src string, perms ...string) *Run {
		sc := &Script{Name: "t", Kind: KindLua, Source: src, Trigger: TriggerConfig{Type: TriggerManual}, Permissions: perms, Approved: perms}
		return e.Run(context.Background(), RunRequest{Script: sc, Trigger: TriggerManual})
	}
	if r := run(`print(web.get("a.example"))`, "web:a.example"); r.Status != StatusOK {
		t.Fatalf("declared host: %s %s", r.Status, r.Error)
	}
	if r := run(`print(web.get("b.example"))`, "web:a.example"); r.Status != StatusDenied {
		t.Fatalf("other host: %s", r.Status)
	}
	if r := run(`print(web.get("a.example"))`); r.Status != StatusDenied {
		t.Fatalf("nothing declared: %s", r.Status)
	}
	if r := run(`print(web.get("a.example"))`, "web"); r.Status != StatusDenied {
		t.Fatalf("bare base: %s", r.Status)
	}
}

func TestSetMissing(t *testing.T) {
	r := testRegistry(t)
	if !r.SetMissing("mail.move", "off") {
		t.Fatal("not found")
	}
	tree, err := ParseTree([]byte(`{"version":1,"blocks":[{"id":"a","kind":"action","type":"mail.move","args":{"folder":{"value":"Archive"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if res := Compile(r, tree, TriggerMailArrived, false); len(res.Errors) == 0 {
		t.Fatal("compiled a missing action")
	}
	r.SetMissing("mail.move", "")
	if res := Compile(r, tree, TriggerMailArrived, false); len(res.Errors) != 0 {
		t.Fatalf("errors %v", res.Errors)
	}
	if r.SetMissing("nope", "x") {
		t.Fatal("found an unknown id")
	}
}
