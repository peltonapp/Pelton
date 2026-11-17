package templates

import (
	"context"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/ai"
	"github.com/peltonapp/Pelton/internal/scripting/files"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
	"github.com/peltonapp/Pelton/internal/scripting/spaces"
	"github.com/peltonapp/Pelton/internal/scripting/system"
	"github.com/peltonapp/Pelton/internal/scripting/web"
	"github.com/peltonapp/Pelton/internal/storage"
)

// sampleMail is a mailbox to run the templates against: an old read
// notification, unread mail, and sent mail with and without a reply.
type sampleMail struct{ now time.Time }

func (m sampleMail) messages() []scripting.Message {
	day := 24 * time.Hour
	return []scripting.Message{
		{ID: 1, AccountID: 1, Folder: "INBOX", FolderRole: "inbox", Subject: "Your weekly report", From: "no-reply@service.example",
			Date: m.now.Add(-20 * day), Read: true, Headers: map[string]string{"list-id": "<weekly.service.example>"}},
		{ID: 2, AccountID: 1, Folder: "INBOX", FolderRole: "inbox", Subject: "Lunch on Friday?", From: "ana@example.org",
			Date: m.now.Add(-time.Hour), Headers: map[string]string{"in-reply-to": "<answered@me.example>"}},
		{ID: 3, AccountID: 1, Folder: "Sent", FolderRole: "sent", Subject: "Offer", To: "client@example.com",
			Date: m.now.Add(-3*day - time.Hour), Read: true, Headers: map[string]string{"message-id": "<waiting@me.example>"}},
		{ID: 4, AccountID: 1, Folder: "Sent", FolderRole: "sent", Subject: "Question", To: "ana@example.org",
			Date: m.now.Add(-3*day - 2*time.Hour), Read: true, Headers: map[string]string{"message-id": "<answered@me.example>"}},
	}
}

func (m sampleMail) Recent(_ context.Context, folder string, _ int64, limit int) ([]scripting.Message, error) {
	var out []scripting.Message
	for _, msg := range m.messages() {
		if strings.EqualFold(msg.FolderRole, folder) && len(out) < limit {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (m sampleMail) Message(_ context.Context, id int64) (scripting.Message, error) {
	return m.messages()[id-1], nil
}
func (sampleMail) Body(context.Context, int64) (string, error)                   { return "Hello", nil }
func (sampleMail) Move(context.Context, int64, string) error                     { return nil }
func (sampleMail) Copy(context.Context, int64, string) error                     { return nil }
func (sampleMail) Archive(context.Context, int64) error                          { return nil }
func (sampleMail) Delete(context.Context, int64) error                           { return nil }
func (sampleMail) SetRead(context.Context, int64, bool) error                    { return nil }
func (sampleMail) SetFlagged(context.Context, int64, bool) error                 { return nil }
func (sampleMail) SetColor(context.Context, int64, int) error                    { return nil }
func (sampleMail) SetPinned(context.Context, int64, bool) error                  { return nil }
func (sampleMail) Snooze(context.Context, int64, time.Time) error                { return nil }
func (sampleMail) Compose(context.Context, scripting.Draft, bool) (int64, error) { return 1, nil }

// shown takes the results a run shows.
type shown struct{}

func (shown) Show(context.Context, scripting.Result) error { return nil }

// testProvider is the AI provider the AI templates are dry run with.
var testProvider = ai.Provider{ID: "test", Name: "Test", Kind: "ollama", BaseURL: "http://127.0.0.1:11434", Model: "small"}

// register adds everything the app registers, so the templates meet the
// registry they meet in the app.
func register(r *scripting.Registry) error {
	steps := []func() error{
		func() error { return spaces.Register(r, spaces.Hosts{}) },
		func() error { return web.Register(r, web.Config{}) },
		func() error { return files.Register(r, nil) },
		func() error { return system.Register(r, nil) },
		func() error {
			return ai.Register(r, ai.Config{Providers: func(context.Context) ([]ai.Provider, error) { return []ai.Provider{testProvider}, nil }})
		},
		func() error { return Register(r) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func service(t *testing.T) *scripting.Service {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "pelton.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	limits := scripting.DefaultLimits()
	limits.Timeout = 5 * time.Second
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	svc, err := scripting.NewService(scripting.Config{DB: db.Handle(), Limits: limits, Hosts: &scripting.Hosts{
		Mail: sampleMail{now: now}, Confirm: scriptingtest.AllowAll{}, Result: shown{}, Now: func() time.Time { return now },
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := register(svc.Registry()); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestEveryFileIsRegistered(t *testing.T) {
	entries, err := content.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	r := scriptingtest.Registry(t, register)
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), path.Ext(e.Name()))
		if _, ok := r.Template(id); !ok {
			t.Errorf("%s is embedded but not registered", e.Name())
		}
	}
	scriptingtest.Localized(t, r)
}

// planned lists the functions a run planned, in order.
func planned(run *scripting.Run) []string {
	var out []string
	for _, s := range run.Steps {
		if s.Kind == scripting.StepPlanned {
			out = append(out, s.Function)
		}
	}
	return out
}

func TestTemplatesCompileAndDryRun(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	list := svc.Registry().Templates()
	if len(list) != len(builtin) {
		t.Fatalf("%d templates listed, want %d", len(list), len(builtin))
	}
	want := map[string][]string{
		"old_notifications": {"mail.archive"},
		"unread_digest":     {"notes.append_daily", "notes.append_daily"},
		"no_reply":          {"tasks.create"},
		"flagged_task":      {"tasks.from_mail"},
		"ai_summary":        {"ai.summarise"},
		"ai_reply":          {"ai.draft_reply", "mail.reply"},
		"ai_sort":           {"ai.classify", "mail.set_color"},
	}
	for _, tmpl := range list {
		t.Run(tmpl.ID, func(t *testing.T) {
			if tmpl.Missing != "" {
				t.Fatalf("does not compile: %s", tmpl.Missing)
			}
			if tmpl.AI != strings.HasPrefix(tmpl.ID, "ai_") {
				t.Fatalf("AI is %v", tmpl.AI)
			}
			if tmpl.Label == "" || tmpl.Description == "" {
				t.Fatal("no name or description")
			}
			sc, err := svc.FromTemplate(ctx, tmpl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if sc.Enabled || sc.Origin != scripting.OriginUser || sc.Name != tmpl.Label {
				t.Fatalf("the copy is enabled %v, origin %s, named %q", sc.Enabled, sc.Origin, sc.Name)
			}
			for _, p := range tmpl.Permissions {
				if !slices.Contains(sc.Permissions, p) {
					t.Fatalf("the copy does not declare %s: %v", p, sc.Permissions)
				}
			}
			if tmpl.AI {
				sc.Permissions = append(sc.Permissions, "ai_local:test")
			}
			run, err := svc.TestRun(ctx, sc, nil)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status != scripting.StatusOK {
				t.Fatalf("dry run: %s %q", run.Status, run.Error)
			}
			if w, ok := want[tmpl.ID]; ok && !slices.Equal(planned(run), w) {
				t.Fatalf("planned %v, want %v", planned(run), w)
			}
		})
	}
}

func TestReplyContactsOnlyForReplies(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	sc, err := svc.FromTemplate(ctx, "reply_contacts")
	if err != nil {
		t.Fatal(err)
	}
	for subject, adds := range map[string]int{"Re: Offer": 2, "Offer": 0} {
		draft := map[string]any{"subject": subject, "to": []any{"ana@example.org", "bo@example.org"}, "cc": []any{}, "bcc": []any{}}
		run, err := svc.TestRun(ctx, sc, map[string]any{"draft": draft})
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != scripting.StatusOK || len(planned(run)) != adds {
			t.Fatalf("%q: %s %q, planned %v", subject, run.Status, run.Error, planned(run))
		}
	}
}

func TestOwnTemplatesRegisterThroughTheRegistry(t *testing.T) {
	r := scriptingtest.Registry(t)
	own := []byte(`{"format":1,"name":"Mine","kind":"lua","trigger":{"type":"manual"},"permissions":[],"source":"print(1)"}`)
	if err := r.RegisterTemplate("mine", "utility", own); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterTemplate("mine", "utility", own); err == nil {
		t.Fatal("a template id was registered twice")
	}
	if err := r.RegisterTemplate("lib", "utility", []byte(`{"format":1,"name":"x","kind":"library","source":""}`)); err == nil {
		t.Fatal("a library was registered as a template")
	}
	got, ok := r.Template("mine")
	if !ok || got.Label != "Mine" || got.Missing != "" {
		t.Fatalf("template %+v", got)
	}
}
