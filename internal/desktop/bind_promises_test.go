package desktop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/promises"
	"github.com/peltonapp/Pelton/internal/scripting"
	"github.com/peltonapp/Pelton/internal/scripting/scriptingtest"
	"github.com/peltonapp/Pelton/internal/storage"
)

type promiseFixture struct {
	app     *App
	account int64
	inbox   *storage.Folder
	sent    *storage.Folder
}

func newPromiseFixture(t *testing.T) promiseFixture {
	t.Helper()
	app := newCalendarTestApp(t)
	id, err := app.store.CreateAccount(app.ctx, &storage.Account{DisplayName: "Me", Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return promiseFixture{
		app:     app,
		account: id,
		inbox:   addFolder(t, app, id, "INBOX"),
		sent:    addFolder(t, app, id, "Sent", `\Sent`),
	}
}

func (f promiseFixture) mail(t *testing.T, folder *storage.Folder, m storage.Message) int64 {
	t.Helper()
	m.AccountID, m.FolderID = f.account, folder.ID
	if m.Date.IsZero() {
		m.Date = time.Now().Add(-time.Hour)
	}
	if m.ToAddresses == "" {
		m.ToAddresses = "Me <me@example.com>"
	}
	id, err := f.app.store.InsertMessage(f.app.ctx, &m)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f promiseFixture) promisesOf(t *testing.T, id int64) []PromiseDTO {
	t.Helper()
	list, err := f.app.PromisesForMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestPromisesFromReceivedMailAreSuggestedOnce(t *testing.T) {
	f := newPromiseFixture(t)
	id := f.mail(t, f.inbox, storage.Message{
		UID: 1, MessageID: "<m1@example.com>", FromAddress: "Anna <anna@example.com>", FromName: "Anna",
		BodyPlain: "Hi,\n\nI'll send the contract by Friday. Can you confirm the delivery address?\n\nBest,\nAnna\n\nOn Mon, Tom wrote:\n> Please pay the invoice today.\n",
	})
	f.app.readPromisesIn([]int64{id}, false)
	got := f.promisesOf(t, id)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Direction != "theirs" || got[0].PersonEmail != "anna@example.com" || got[0].State != "suggested" || got[0].Due == "" {
		t.Errorf("commitment: %+v", got[0])
	}
	if got[1].Direction != "mine" || !strings.HasPrefix(got[1].Quote, "Can you confirm") {
		t.Errorf("request: %+v", got[1])
	}
	if err := f.app.DismissPromise(got[0].ID); err != nil {
		t.Fatal(err)
	}
	f.app.readPromisesIn([]int64{id}, false)
	if _, err := f.app.FindPromises(id); err != nil {
		t.Fatal(err)
	}
	again := f.promisesOf(t, id)
	if len(again) != 1 || again[0].ID != got[1].ID {
		t.Fatalf("a resync changed the suggestions: %+v", again)
	}
}

func TestPromisesSkipListsAutomatedAndOtherMail(t *testing.T) {
	f := newPromiseFixture(t)
	body := "I'll send you the report by Friday."
	ids := []int64{
		f.mail(t, f.inbox, storage.Message{UID: 1, MessageID: "<l@x>", FromAddress: "news@shop.example", ListID: "<news.shop.example>", ListUnsubscribe: "<mailto:u@shop.example>", BodyPlain: body}),
		f.mail(t, f.inbox, storage.Message{UID: 2, MessageID: "<n@x>", FromAddress: "noreply@shop.example", BodyPlain: body}),
		f.mail(t, f.inbox, storage.Message{UID: 3, MessageID: "<o@x>", FromAddress: "anna@example.com", BodyPlain: body, Date: time.Now().AddDate(0, 0, -40)}),
		f.mail(t, f.inbox, storage.Message{UID: 4, MessageID: "<c@x>", FromAddress: "anna@example.com", ToAddresses: "bob@example.com", CcAddresses: "me@example.com", BodyPlain: "Can you send me the report by Friday?"}),
	}
	f.app.readPromisesIn(ids, false)
	for _, id := range ids {
		if got := f.promisesOf(t, id); len(got) != 0 {
			t.Errorf("message %d: %+v", id, got)
		}
	}
	if err := f.app.store.Set(f.app.ctx, settingDesign, "classic"); err != nil {
		t.Fatal(err)
	}
	if f.app.promisesActive() {
		t.Fatal("promises run in the classic design")
	}
}

func TestOwedToMeClosesOnReplyWithUndo(t *testing.T) {
	f := newPromiseFixture(t)
	sentAt := time.Now().Add(-3 * time.Hour)
	ask := f.mail(t, f.sent, storage.Message{
		UID: 1, MessageID: "<ask@example.com>", FromAddress: "me@example.com", ToAddresses: "Anna <anna@example.com>",
		BodyPlain: "Hi Anna,\n\nCould you send me the signed contract by Friday?\n", Date: sentAt,
	})
	f.app.readPromisesIn([]int64{ask}, true)
	got := f.promisesOf(t, ask)
	if len(got) != 1 || got[0].Direction != "theirs" || got[0].PersonEmail != "anna@example.com" {
		t.Fatalf("got %+v", got)
	}
	accepted, err := f.app.AcceptPromise(PromiseEditDTO{ID: got[0].ID, Title: "Signed contract from Anna", Due: got[0].Due})
	if err != nil {
		t.Fatal(err)
	}
	taskID := f.app.promiseTaskID(accepted.TaskUID)
	task, err := f.app.GetTask(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "Signed contract from Anna" || len(task.MailLinks) != 1 || task.MailLinks[0] != "mid:ask@example.com" || task.Due == "" {
		t.Fatalf("task %+v", task)
	}
	if lists, _ := f.app.ListTaskLists(); !lists[0].Local || task.ListID != lists[0].ID {
		t.Fatalf("task in list %d, want the local one", task.ListID)
	}

	other := f.mail(t, f.inbox, storage.Message{UID: 2, MessageID: "<other@x>", FromAddress: "bob@example.com", InReplyTo: []string{"ask@example.com"}, BodyPlain: "+1"})
	f.app.readPromisesIn([]int64{other}, false)
	if f.app.taskDone(taskID) {
		t.Fatal("a reply from someone else closed it")
	}
	reply := f.mail(t, f.inbox, storage.Message{
		UID: 3, MessageID: "<reply@example.com>", FromAddress: "Anna <anna@example.com>", InReplyTo: []string{"ask@example.com"},
		References: []string{"ask@example.com"}, BodyPlain: "Here it is.",
	})
	f.app.readPromisesIn([]int64{reply}, false)
	closed := f.promisesOf(t, ask)[0]
	if closed.State != "closed" || closed.ReplyID != "reply@example.com" || closed.ReplyMessageID != reply || !f.app.taskDone(taskID) {
		t.Fatalf("not closed by the reply: %+v", closed)
	}
	if err := f.app.ReopenPromise(closed.ID); err != nil {
		t.Fatal(err)
	}
	reopened := f.promisesOf(t, ask)[0]
	if reopened.State != "accepted" || reopened.ReplyID != "" || f.app.taskDone(taskID) {
		t.Fatalf("undo: %+v", reopened)
	}
	f.app.settleByReply(*mustMessage(t, f.app, reply), false)
	if f.promisesOf(t, ask)[0].State != "accepted" {
		t.Fatal("the reply an undo kept out closed it again")
	}
}

func TestIOweIsOfferedWhenIReply(t *testing.T) {
	f := newPromiseFixture(t)
	req := f.mail(t, f.inbox, storage.Message{
		UID: 1, MessageID: "<req@example.com>", FromAddress: "Anna <anna@example.com>", Date: time.Now().Add(-2 * time.Hour),
		BodyPlain: "Can you review the draft by Monday?",
	})
	f.app.readPromisesIn([]int64{req}, false)
	p := f.promisesOf(t, req)[0]
	if p.Direction != "mine" {
		t.Fatalf("%+v", p)
	}
	if _, err := f.app.AcceptPromise(PromiseEditDTO{ID: p.ID, Title: p.Title, Due: p.Due}); err != nil {
		t.Fatal(err)
	}
	answer := f.mail(t, f.sent, storage.Message{
		UID: 2, MessageID: "<ans@example.com>", FromAddress: "me@example.com", ToAddresses: "anna@example.com",
		InReplyTo: []string{"req@example.com"}, BodyPlain: "Done, see my comments.",
	})
	f.app.readPromisesIn([]int64{answer}, true)
	got := f.promisesOf(t, req)[0]
	if got.State != "accepted" || got.ReplyID != "ans@example.com" {
		t.Fatalf("no offer to complete: %+v", got)
	}
	if f.app.taskDone(f.app.promiseTaskID(got.TaskUID)) {
		t.Fatal("the user's reply completed the task without asking")
	}
}

func TestAutoAcceptMineInSentMail(t *testing.T) {
	f := newPromiseFixture(t)
	if err := f.app.store.SetBool(f.app.ctx, settingPromisesAutoMine, true); err != nil {
		t.Fatal(err)
	}
	id := f.mail(t, f.sent, storage.Message{UID: 1, MessageID: "<s@x>", FromAddress: "me@example.com", ToAddresses: "anna@example.com",
		BodyPlain: "I'll send you the slides tomorrow. Could you book the room?"})
	f.app.readPromisesIn([]int64{id}, true)
	got := f.promisesOf(t, id)
	if len(got) != 2 || got[0].State != "accepted" || got[0].TaskUID == "" || got[1].State != "suggested" {
		t.Fatalf("got %+v", got)
	}
}

func TestPromiseAccountSwitch(t *testing.T) {
	f := newPromiseFixture(t)
	s, err := f.app.GetPromiseSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enabled || s.AutoAcceptMine || !s.Nudge || s.TaskList != "local" || len(s.Accounts) != 1 || !s.Accounts[0].On {
		t.Fatalf("defaults %+v", s)
	}
	s.Accounts[0].On = false
	if err := f.app.SetPromiseSettings(s); err != nil {
		t.Fatal(err)
	}
	s.Accounts[0].AI, s.Accounts[0].Provider = true, "nobody"
	if err := f.app.SetPromiseSettings(s); err == nil {
		t.Fatal("an AI check without a provider was accepted")
	}
	id := f.mail(t, f.inbox, storage.Message{UID: 1, MessageID: "<a@x>", FromAddress: "anna@example.com", BodyPlain: "I'll call you tomorrow."})
	f.app.readPromisesIn([]int64{id}, false)
	if got := f.promisesOf(t, id); len(got) != 0 {
		t.Fatalf("read mail of an account with promises off: %+v", got)
	}
}

func TestPromiseScriptEntriesAreLocalized(t *testing.T) {
	r := scriptingtest.Registry(t, func(r *scripting.Registry) error {
		return r.Register(promiseScriptEntries(fakePromiseHost{}))
	})
	scriptingtest.Localized(t, r)
}

type fakePromiseHost struct{}

func (fakePromiseHost) Promise(_ context.Context, id int64) (promises.Promise, error) {
	return promises.Promise{ID: id}, nil
}
func (fakePromiseHost) Accept(context.Context, int64) error  { return nil }
func (fakePromiseHost) Dismiss(context.Context, int64) error { return nil }

func mustMessage(t *testing.T, app *App, id int64) *storage.Message {
	t.Helper()
	m, err := app.store.GetMessage(app.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}
