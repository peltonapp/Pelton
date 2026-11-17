package scripting

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

// fakeMail is a MailHost over a map of messages that records every change.
type fakeMail struct {
	mu       sync.Mutex
	messages map[int64]Message
	calls    []string
	drafts   []Draft
	fail     error
}

func newFakeMail() *fakeMail {
	return &fakeMail{messages: map[int64]Message{
		1: {ID: 1, AccountID: 7, Account: "me@example.org", Folder: "INBOX", FolderRole: "inbox",
			Subject: "Invoice 42", From: "billing@shop.example", FromName: "Shop Billing",
			To: "me@example.org, other@example.org", Date: time.Now().Add(-48 * time.Hour), Size: 2048,
			HasAttachments: true, Headers: map[string]string{"list-id": "<news.shop.example>"}},
		2: {ID: 2, AccountID: 7, Folder: "INBOX", FolderRole: "inbox", Subject: "Lunch?", From: "friend@example.net",
			Date: time.Now().Add(-time.Hour), Size: 300},
	}}
}

func (f *fakeMail) record(format string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	return nil
}

func (f *fakeMail) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeMail) Message(_ context.Context, id int64) (Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.messages[id]
	if !ok {
		return Message{}, fmt.Errorf("no message %d", id)
	}
	return m, nil
}

func (f *fakeMail) Body(_ context.Context, id int64) (string, error) {
	return fmt.Sprintf("body of %d: total 120 EUR", id), nil
}

func (f *fakeMail) Move(_ context.Context, id int64, folder string) error {
	return f.record("move %d %s", id, folder)
}
func (f *fakeMail) Archive(_ context.Context, id int64) error { return f.record("archive %d", id) }
func (f *fakeMail) Delete(_ context.Context, id int64) error  { return f.record("delete %d", id) }
func (f *fakeMail) SetRead(_ context.Context, id int64, read bool) error {
	return f.record("read %d %v", id, read)
}
func (f *fakeMail) SetFlagged(_ context.Context, id int64, on bool) error {
	return f.record("flag %d %v", id, on)
}
func (f *fakeMail) SetColor(_ context.Context, id int64, c int) error {
	return f.record("color %d %d", id, c)
}
func (f *fakeMail) SetPinned(_ context.Context, id int64, on bool) error {
	return f.record("pin %d %v", id, on)
}
func (f *fakeMail) Snooze(_ context.Context, id int64, until time.Time) error {
	return f.record("snooze %d", id)
}
func (f *fakeMail) Compose(_ context.Context, d Draft, send bool) (int64, error) {
	f.mu.Lock()
	f.drafts = append(f.drafts, d)
	f.mu.Unlock()
	return 99, f.record("compose send=%v to=%v reply=%d fwd=%d", send, d.To, d.ReplyTo, d.Forward)
}

type fakeNotify struct {
	mu     sync.Mutex
	toasts []string
}

func (n *fakeNotify) Toast(_ context.Context, _, text, _ string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.toasts = append(n.toasts, text)
}
func (n *fakeNotify) System(context.Context, string, string) error { return nil }

type fakeConfirm struct {
	answer ConfirmAnswer
	asked  []ConfirmRequest
	mu     sync.Mutex
}

func (c *fakeConfirm) Confirm(_ context.Context, req ConfirmRequest) (ConfirmAnswer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, req)
	return c.answer, nil
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	if err := RegisterBuiltins(r); err != nil {
		t.Fatal(err)
	}
	return r
}

func testEngine(t *testing.T, mail *fakeMail) *Engine {
	t.Helper()
	limits := DefaultLimits()
	limits.Timeout = 2 * time.Second
	return &Engine{Registry: testRegistry(t), Hosts: &Hosts{Mail: mail, Notify: &fakeNotify{}}, Limits: limits}
}

// luaScript is an approved, enabled Lua script with the given permissions.
func luaScript(src string, perms ...string) *Script {
	return &Script{
		ID: 1, Name: "test", Kind: KindLua, Source: src,
		Trigger:     TriggerConfig{Type: TriggerManual},
		Permissions: perms, Approved: perms, Enabled: true, CatchUp: CatchUpLive,
	}
}

func runLua(t *testing.T, e *Engine, src string, perms ...string) *Run {
	t.Helper()
	return e.Run(context.Background(), RunRequest{Script: luaScript(src, perms...), Trigger: TriggerManual, Event: map[string]any{}})
}

func testDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "pelton.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.RunMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func testService(t *testing.T, mail *fakeMail, confirm Confirmer) *Service {
	t.Helper()
	db := testDB(t)
	limits := DefaultLimits()
	limits.Timeout = 2 * time.Second
	s, err := NewService(Config{DB: db.Handle(), Hosts: &Hosts{Mail: mail, Notify: &fakeNotify{}, Confirm: confirm}, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
