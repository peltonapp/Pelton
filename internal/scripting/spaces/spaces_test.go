package spaces

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
)

// fake is one in-memory app behind every space host, recording changes.
type fake struct {
	mu       sync.Mutex
	changes  []string
	events   map[int64]Event
	tasks    map[int64]Task
	contacts map[int64]Contact
	cards    map[string]Card
	notes    map[string]string
	items    map[int64]FeedItem
	chatOn   bool
	nextID   int64
}

func newFake() *fake {
	start := time.Date(2026, 11, 18, 10, 0, 0, 0, time.Local)
	return &fake{
		events:   map[int64]Event{1: {ID: 1, Calendar: "Work", Title: "Standup", Start: start, End: start.Add(15 * time.Minute)}},
		tasks:    map[int64]Task{1: {ID: 1, List: "Inbox", Title: "Pay invoice", Due: start}},
		contacts: map[int64]Contact{1: {ID: 1, Name: "Ann", Emails: []string{"ann@example.org"}}},
		cards:    map[string]Card{"c1": {TaskID: 2, UID: "c1", Title: "Launch", ProjectID: 1, Project: "Website", Column: "To do"}},
		notes:    map[string]string{"Ideas.md": "# Ideas\n"},
		items:    map[int64]FeedItem{1: {ID: 1, Feed: "News", Title: "Hello"}},
		nextID:   100,
	}
}

func (f *fake) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, fmt.Sprintf(format, args...))
}

func (f *fake) Changes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.changes...)
}

func (f *fake) id() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	return f.nextID
}

func (f *fake) Events(_ context.Context, from, to time.Time, q string, _ int) ([]Event, error) {
	var out []Event
	for _, e := range f.events {
		if !e.Start.Before(from) && e.Start.Before(to) && strings.Contains(strings.ToLower(e.Title), strings.ToLower(q)) {
			out = append(out, e)
		}
	}
	return out, nil
}
func (f *fake) Event(_ context.Context, id int64) (Event, error) {
	e, ok := f.events[id]
	if !ok {
		return e, sql.ErrNoRows
	}
	return e, nil
}
func (f *fake) CreateEvent(_ context.Context, in EventInput) (Event, error) {
	f.record("event %s %s", in.Title, in.End.Sub(in.Start))
	return Event{ID: f.id(), Title: in.Title, Start: in.Start, End: in.End}, nil
}
func (f *fake) UpdateEvent(_ context.Context, id int64, ch EventChange) (Event, error) {
	f.record("update event %d", id)
	return f.events[id], nil
}
func (f *fake) EventFromMessage(_ context.Context, id int64) (EventInput, error) {
	return EventInput{Title: "From mail", Start: time.Now(), End: time.Now().Add(time.Hour)}, nil
}
func (f *fake) Invitation(context.Context, int64) (Invitation, bool, error) {
	return Invitation{}, false, nil
}

func (f *fake) Tasks(context.Context, TaskFilter) ([]Task, error) { return []Task{f.tasks[1]}, nil }
func (f *fake) Task(_ context.Context, id int64) (Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return t, sql.ErrNoRows
	}
	return t, nil
}
func (f *fake) CreateTask(_ context.Context, in TaskInput) (Task, error) {
	f.record("task %s mail=%d due=%t", in.Title, in.MessageID, !in.Due.IsZero())
	return Task{ID: f.id(), Title: in.Title}, nil
}
func (f *fake) UpdateTask(_ context.Context, id int64, ch TaskChange) (Task, error) {
	f.record("update task %d done=%v", id, ch.Done != nil && *ch.Done)
	return f.tasks[id], nil
}
func (f *fake) TaskFromMessage(_ context.Context, id int64) (TaskInput, error) {
	return TaskInput{Title: "Reply", MessageID: id}, nil
}

func (f *fake) Contacts(context.Context, string, int) ([]Contact, error) {
	return []Contact{f.contacts[1]}, nil
}
func (f *fake) Contact(_ context.Context, id int64) (Contact, error) { return f.contacts[id], nil }
func (f *fake) FindByEmail(_ context.Context, email string) (Contact, bool, error) {
	for _, c := range f.contacts {
		for _, e := range c.Emails {
			if e == email {
				return c, true, nil
			}
		}
	}
	return Contact{}, false, nil
}
func (f *fake) CreateContact(_ context.Context, in ContactInput) (Contact, error) {
	f.record("contact %s <%s>", in.Name, in.Email)
	return Contact{ID: f.id(), Name: in.Name, Emails: []string{in.Email}}, nil
}

func (f *fake) Projects(context.Context) ([]Project, error) {
	return []Project{{ID: 1, Name: "Website", Columns: []string{"To do", "Done"}}}, nil
}
func (f *fake) Cards(context.Context, int64) ([]Card, error) { return []Card{f.cards["c1"]}, nil }
func (f *fake) Card(_ context.Context, uid string) (Card, error) {
	return f.cards[uid], nil
}
func (f *fake) CreateCard(_ context.Context, in CardInput) (Card, error) {
	f.record("card %d %s %s", in.ProjectID, in.Column, in.Title)
	return Card{UID: "new", Title: in.Title}, nil
}
func (f *fake) MoveCard(_ context.Context, uid, col string) (Card, error) {
	f.record("move %s %s", uid, col)
	return f.cards[uid], nil
}
func (f *fake) LinkMail(_ context.Context, p, m int64, uid string) error {
	f.record("link %d %d %s", p, m, uid)
	return nil
}

func (f *fake) Note(_ context.Context, p string) (Note, error) {
	return Note{Path: p, Content: f.notes[p]}, nil
}
func (f *fake) SearchNotes(context.Context, string, int) ([]Note, error) { return nil, nil }
func (f *fake) CreateNote(_ context.Context, p, content string) (Note, error) {
	f.record("note %s", p)
	return Note{Path: p}, nil
}
func (f *fake) AppendNote(_ context.Context, p, text string) (Note, error) {
	f.record("append %s %q", p, text)
	return Note{Path: p}, nil
}
func (f *fake) DailyNotePath(_ context.Context, day time.Time) (string, error) {
	return "Daily/" + day.Format("2006-01-02") + ".md", nil
}

func (f *fake) FeedItems(context.Context, FeedFilter) ([]FeedItem, error) {
	return []FeedItem{f.items[1]}, nil
}
func (f *fake) FeedItem(_ context.Context, id int64) (FeedItem, error) { return f.items[id], nil }
func (f *fake) SetFeedItemsRead(_ context.Context, ids []int64, read bool) error {
	f.record("read %v %t", ids, read)
	return nil
}
func (f *fake) SetFeedItemStarred(_ context.Context, id int64, on bool) error {
	f.record("star %d %t", id, on)
	return nil
}

func (f *fake) Enabled() bool { return f.chatOn }
func (f *fake) Rooms(context.Context) ([]ChatRoom, error) {
	return []ChatRoom{{ID: "!a", Name: "Team"}}, nil
}
func (f *fake) Send(_ context.Context, room, text string) (string, error) {
	f.record("chat %s %s", room, text)
	return "$1", nil
}

func setup(t *testing.T) (*scripting.Registry, *fake) {
	t.Helper()
	f := newFake()
	r := scriptingtest.Registry(t, func(r *scripting.Registry) error {
		return Register(r, Hosts{Calendar: f, Tasks: f, Contacts: f, Projects: f, Notes: f, Feeds: f, Chat: f})
	})
	return r, f
}

func run(t *testing.T, r *scripting.Registry, src string, dry bool, perms ...string) *scripting.Run {
	t.Helper()
	return scriptingtest.Run(t, r, scriptingtest.Script(1, src, perms...), dry)
}

func TestLocalized(t *testing.T) {
	r, _ := setup(t)
	scriptingtest.Localized(t, r)
}

func TestRegistersWithoutHosts(t *testing.T) {
	r := scriptingtest.Registry(t, func(r *scripting.Registry) error { return Register(r, Hosts{}) })
	got := run(t, r, `tasks.list()`, false, PermTasksRead)
	if got.Status != scripting.StatusError || !strings.Contains(got.Error, "not available") {
		t.Fatalf("%s %s", got.Status, got.Error)
	}
}

func TestSpacesReadAndWrite(t *testing.T) {
	r, f := setup(t)
	f.chatOn = true
	src := `
local evs = calendar.events("2026-11-18", "2026-11-19", "stand")
print(#evs, evs[1].title)
calendar.create({title = "Review", start = "2026-11-20 14:00", duration = 1800})
local m = {id = 7, subject = "Invoice", from = "Bob <bob@example.org>", from_name = "Bob"}
tasks.from_mail(m, {due = "1d"})
tasks.complete(tasks.get(1))
print(contacts.find("ann@example.org").name, contacts.find("nobody@example.org"))
contacts.add_sender(m)
contacts.add_sender({from = "ann@example.org"})
projects.add_card("website", {title = "Copy", column = "To do"})
projects.move_card(projects.cards("Website")[1], "Done")
projects.link_mail("Website", m)
notes.append_daily("- called Bob")
notes.create("Meeting", "# Meeting")
feeds.mark_read(feeds.items({unread = true}))
chat.send("Team", "hello")`
	perms := []string{PermCalendarRead, PermCalendarWrite, PermTasksRead, PermTasksWrite, PermContactsRead, PermContactsWrite,
		PermProjectsRead, PermProjectsWrite, PermNotesRead, PermNotesWrite, PermFeedsRead, PermFeedsModify, PermChatSend}
	got := run(t, r, src, false, perms...)
	if got.Status != scripting.StatusOK {
		t.Fatalf("%s %s", got.Status, got.Error)
	}
	if s := strings.Join(scriptingtest.Logged(got), "\n"); s != "1 Standup\nAnn nil" {
		t.Fatalf("logged %q", s)
	}
	want := []string{
		"event Review 30m0s", "task Reply mail=7 due=true", "update task 1 done=true",
		"contact Bob <bob@example.org>", "card 1 To do Copy", "move c1 Done", "link 1 7 ",
		`append Daily/` + time.Now().Format("2006-01-02") + `.md "- called Bob"`, "note Meeting.md",
		"read [1] true", "chat Team hello",
	}
	if strings.Join(f.Changes(), "|") != strings.Join(want, "|") {
		t.Fatalf("changes\n%s\nwant\n%s", strings.Join(f.Changes(), "\n"), strings.Join(want, "\n"))
	}
}

func TestPermissionDenied(t *testing.T) {
	r, f := setup(t)
	f.chatOn = true
	cases := map[string][]string{
		`calendar.create({title = "x", start = "1h"})`: {PermCalendarRead},
		`tasks.list()`:                     {PermCalendarRead},
		`contacts.add({email = "a@b.c"})`:  {PermContactsRead},
		`projects.move_card("c1", "Done")`: {PermProjectsRead},
		`notes.append("a", "b")`:           {PermNotesRead},
		`feeds.mark_read(1)`:               {PermFeedsRead},
		`chat.send("Team", "x")`:           {PermChatRead},
	}
	for src, perms := range cases {
		if got := run(t, r, src, false, perms...); got.Status != scripting.StatusDenied {
			t.Errorf("%s: %s %s", src, got.Status, got.Error)
		}
	}
	if len(f.Changes()) != 0 {
		t.Fatalf("changes %v", f.Changes())
	}
}

func TestDryRunPlansChanges(t *testing.T) {
	r, f := setup(t)
	f.chatOn = true
	src := `
local e = calendar.create({title = "x", start = "1h"})
local t = tasks.create({title = "y"})
print(e.id, t.id, #tasks.list())
notes.append_daily("z")
chat.send("Team", "hi")`
	sc := scriptingtest.Script(1, src, PermCalendarWrite, PermTasksWrite, PermTasksRead, PermNotesWrite, PermChatSend)
	sc.Approved = nil
	got := scriptingtest.Run(t, r, sc, true)
	if got.Status != scripting.StatusOK || got.Changes() != 4 || len(f.Changes()) != 0 {
		t.Fatalf("%s %s changes %d %v", got.Status, got.Error, got.Changes(), f.Changes())
	}
	if s := strings.Join(scriptingtest.Logged(got), ""); s != "0 0 1" {
		t.Fatalf("logged %q", s)
	}
}

func TestChatAvailability(t *testing.T) {
	r, f := setup(t)
	SetChatAvailable(r, false)
	if a, _ := r.Action(ActionChatSend); a.Missing != ChatOff {
		t.Fatal("send not marked")
	}
	if tr, _ := r.Trigger(TriggerChatMessage); tr.Missing != ChatOff {
		t.Fatal("trigger not marked")
	}
	if got := run(t, r, `chat.send("Team", "x")`, false, PermChatSend); got.Status != scripting.StatusError {
		t.Fatalf("sent with chat off: %s", got.Status)
	}
	f.chatOn = true
	SetChatAvailable(r, true)
	if a, _ := r.Action(ActionChatSend); a.Missing != "" {
		t.Fatal("still marked")
	}
}

func TestTriggersPrepareAndMatch(t *testing.T) {
	r, _ := setup(t)
	check := func(id string, params, data map[string]any, want bool) {
		t.Helper()
		tr, ok := r.Trigger(id)
		if !ok {
			t.Fatalf("%s not registered", id)
		}
		ev := data
		if tr.Prepare != nil {
			var err error
			if ev, err = tr.Prepare(&scripting.PrepareContext{Ctx: context.Background()}, data); err != nil {
				t.Fatal(err)
			}
		}
		if got := tr.Match == nil || tr.Match(params, ev); got != want {
			t.Fatalf("%s %v: match %t", id, params, got)
		}
	}
	check(TriggerEventStarts, map[string]any{"minutes": float64(15)}, map[string]any{"event_id": int64(1), "minutes": 15}, true)
	check(TriggerEventStarts, map[string]any{"minutes": float64(5)}, map[string]any{"event_id": int64(1), "minutes": 15}, false)
	check(TriggerEventCreated, map[string]any{"calendar": "work"}, map[string]any{"event_id": int64(1)}, true)
	check(TriggerTaskOverdue, map[string]any{"list": "Errands"}, map[string]any{"task_id": int64(1)}, false)
	check(TriggerCardMoved, map[string]any{"column": "to do"}, map[string]any{"card_uid": "c1", "from_column": "Doing"}, true)
	check(TriggerNoteSaved, map[string]any{"folder": "Daily"}, map[string]any{"note_path": "Daily/x.md"}, true)
	check(TriggerNoteSaved, map[string]any{"folder": "Daily"}, map[string]any{"note_path": "Ideas.md"}, false)
	check(TriggerFeedItem, map[string]any{"feed": "news"}, map[string]any{"item_id": int64(1)}, true)
	chat := map[string]any{"chat": ChatMessageObject(ChatMessage{Room: "Team", Direct: true})}
	check(TriggerChatMessage, map[string]any{"only": "mention"}, chat, false)
	check(TriggerChatMessage, map[string]any{"only": "direct", "room": "team"}, chat, true)

	ev := scripting.SampleEvent(r, mustTrigger(t, r, TriggerTaskOverdue), time.Now())
	if task, ok := ev["task"].(scripting.Object); !ok || task.Fields["title"] == "" {
		t.Fatalf("sample %v", ev)
	}
}

func mustTrigger(t *testing.T, r *scripting.Registry, id string) scripting.Trigger {
	tr, ok := r.Trigger(id)
	if !ok {
		t.Fatal(id)
	}
	return tr
}

func TestPlacementsAndResultActions(t *testing.T) {
	r, f := setup(t)
	want := map[string]string{
		"contacts.contact": "contact", "calendar.event": "event", "tasks.task": "task", "notes.note": "note",
		"projects.card": "card", "feeds.item": "feed_item", "chat.message": "chat_message",
	}
	for id, vt := range want {
		p, ok := r.Placement(id)
		if !ok || p.ValueType != vt || p.Surface != scripting.SurfaceMenu || p.Permission == "" {
			t.Fatalf("%s: %+v", id, p)
		}
	}
	manual := mustTrigger(t, r, scripting.TriggerManual)
	for _, p := range manual.Params {
		if p.Name == "scope" && len(p.Options) < 2+len(want) {
			t.Fatalf("scope options %+v", p.Options)
		}
	}
	p, _ := r.Placement("contacts.contact")
	obj, err := p.Load(&scripting.PrepareContext{Ctx: context.Background()}, "1")
	if err != nil || obj.Type != "contact" {
		t.Fatalf("%v %+v", err, obj)
	}
	if _, err := p.Load(&scripting.PrepareContext{Ctx: context.Background()}, "x"); err == nil {
		t.Fatal("a bad id was accepted")
	}
	for _, id := range []string{"notes.save", "tasks.create"} {
		a, ok := r.ResultAction(id)
		if !ok || a.Run == nil {
			t.Fatalf("%s missing", id)
		}
		if _, err := a.Run(context.Background(), scripting.Result{ScriptName: "Digest", Title: "a/b", Text: "line one\nline two"}); err != nil {
			t.Fatal(err)
		}
	}
	changes := strings.Join(f.Changes(), "|")
	if !strings.Contains(changes, "note a-b.md") || !strings.Contains(changes, "task a/b") {
		t.Fatalf("changes %s", changes)
	}
	if _, ok := r.InputKind("contact"); !ok {
		t.Fatal("the contact question is missing")
	}
	if _, ok := r.Action("ui.contact"); !ok {
		t.Fatal("the contact question has no block action")
	}
}
