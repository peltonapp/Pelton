package desktop

import (
	"errors"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

func TestResolveFollowUp(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		at        string
		notBefore time.Time
		want      time.Time
		wantErr   error
	}{
		{name: "none set", at: ""},
		{name: "after an immediate send", at: "2026-11-18T09:00:00Z", want: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)},
		{name: "in another zone", at: "2026-11-18T10:00:00+01:00", want: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)},
		{name: "not after now", at: "2026-11-15T12:00:00Z", wantErr: ErrFollowUpTooEarly},
		{name: "before a scheduled send", at: "2026-11-16T09:00:00Z", notBefore: time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC), wantErr: ErrFollowUpTooEarly},
		{name: "after a scheduled send", at: "2026-11-18T09:00:00Z", notBefore: time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC), want: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)},
		{name: "not a time", at: "next week", wantErr: ErrFollowUpInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveFollowUp(tc.at, tc.notBefore, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRecipientSummary(t *testing.T) {
	got := recipientSummary([]AddressDTO{{Name: "Ada Lovelace", Email: "ada@x"}, {Email: "bob@x"}, {Name: " ", Email: " "}})
	if got != "Ada Lovelace, bob@x" {
		t.Fatalf("summary = %q", got)
	}
}

func followUpApp(t *testing.T) (*App, int64) {
	t.Helper()
	app, accountID, _ := draftTestApp(t)
	app.queue = outbox.NewQueue(app.store)
	return app, accountID
}

func onlyFollowUp(t *testing.T, app *App) FollowUpDTO {
	t.Helper()
	list, err := app.ListFollowUps()
	if err != nil {
		t.Fatalf("ListFollowUps: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("follow-ups = %+v, want one", list)
	}
	return list[0]
}

func TestSendMessageSetsAFollowUp(t *testing.T) {
	app, accountID := followUpApp(t)
	due := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	id, err := app.SendMessage(ComposeRequest{
		AccountID:  accountID,
		To:         []AddressDTO{{Name: "Ada", Email: "ada@example.com"}},
		Subject:    "Quote",
		Text:       "body",
		SendAt:     time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		FollowUpAt: due.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	got := onlyFollowUp(t, app)
	if got.Subject != "Quote" || got.Recipients != "Ada" || got.DueAt != due.Format(time.RFC3339) || got.FiredAt != "" || got.MessageID == "" {
		t.Fatalf("follow-up = %+v", got)
	}

	if ok, err := app.CancelSend(id); err != nil || !ok {
		t.Fatalf("CancelSend = %v, %v", ok, err)
	}
	if list, _ := app.ListFollowUps(); len(list) != 0 {
		t.Fatalf("follow-ups after taking the send back = %+v", list)
	}
}

func TestSendMessageRefusesAnEarlyFollowUp(t *testing.T) {
	app, accountID := followUpApp(t)
	_, err := app.SendMessage(ComposeRequest{
		AccountID:  accountID,
		To:         []AddressDTO{{Email: "ada@example.com"}},
		Subject:    "Quote",
		Text:       "body",
		FollowUpAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	})
	if !errors.Is(err, ErrFollowUpTooEarly) {
		t.Fatalf("err = %v, want ErrFollowUpTooEarly", err)
	}
	if rows, _ := app.ListOutbox(); len(rows) != 0 {
		t.Fatalf("queued %+v despite the refused reminder", rows)
	}
}

func TestEditOutboxMessageDropsItsFollowUp(t *testing.T) {
	app, accountID := followUpApp(t)
	id, err := app.SendMessage(ComposeRequest{
		AccountID:  accountID,
		To:         []AddressDTO{{Email: "ada@example.com"}},
		Subject:    "Quote",
		Text:       "body",
		SendAt:     time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		FollowUpAt: time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if got, err := app.EditOutboxMessage(id); err != nil || !got.Taken {
		t.Fatalf("EditOutboxMessage = %+v, %v", got, err)
	}
	if list, _ := app.ListFollowUps(); len(list) != 0 {
		t.Fatalf("follow-ups after editing the send = %+v", list)
	}
}

// followUpFixture is an account with a sent message and a reminder on it.
type followUpFixture struct {
	app     *App
	account int64
	inbox   *storage.Folder
	sent    *storage.Folder
	uid     uint32
	now     time.Time
}

func newFollowUpFixture(t *testing.T) *followUpFixture {
	t.Helper()
	app, accountID := followUpApp(t)
	f := &followUpFixture{
		app:     app,
		account: accountID,
		inbox:   addFolder(t, app, accountID, "INBOX"),
		sent:    addFolder(t, app, accountID, "Sent", `\Sent`),
		now:     time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC),
	}
	return f
}

func (f *followUpFixture) message(t *testing.T, folder *storage.Folder, m storage.Message) int64 {
	t.Helper()
	f.uid++
	m.AccountID = f.account
	m.FolderID = folder.ID
	m.UID = f.uid
	m.Date = f.now.Add(-24 * time.Hour)
	id, err := f.app.store.InsertMessage(f.app.ctx, &m)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	return id
}

func (f *followUpFixture) reminder(t *testing.T, due time.Time, outboxID int64) {
	t.Helper()
	fu := storage.FollowUp{AccountID: f.account, MessageID: "<q@example.com>", OutboxID: outboxID, Subject: "Quote", SentAt: f.now.Add(-48 * time.Hour), DueAt: due}
	if _, err := f.app.store.AddFollowUp(f.app.ctx, &fu); err != nil {
		t.Fatalf("add follow-up: %v", err)
	}
}

func TestCheckFollowUpsFiresWhenNobodyReplied(t *testing.T) {
	f := newFollowUpFixture(t)
	sentID := f.message(t, f.sent, storage.Message{MessageID: "<q@example.com>", Subject: "Quote", FromAddress: "me@example.com"})
	f.message(t, f.inbox, storage.Message{MessageID: "<q@example.com>", Subject: "Quote", FromAddress: "me@example.com"})
	f.reminder(t, f.now.Add(-time.Minute), 0)

	f.app.checkFollowUps(f.now)

	got := onlyFollowUp(t, f.app)
	if got.FiredAt != f.now.Format(time.RFC3339) {
		t.Fatalf("fired at %q, want %v", got.FiredAt, f.now)
	}
	if got.LocalID != sentID || got.FolderID != f.sent.ID {
		t.Fatalf("points at %d in %d, want the Sent copy %d", got.LocalID, got.FolderID, sentID)
	}

	// a second pass leaves a fired reminder as it is.
	f.app.checkFollowUps(f.now.Add(time.Hour))
	if again := onlyFollowUp(t, f.app); again.FiredAt != got.FiredAt {
		t.Fatalf("fired again at %q", again.FiredAt)
	}
}

func TestCheckFollowUpsWaitsUntilDue(t *testing.T) {
	f := newFollowUpFixture(t)
	f.reminder(t, f.now.Add(time.Hour), 0)
	f.app.checkFollowUps(f.now)
	if got := onlyFollowUp(t, f.app); got.FiredAt != "" {
		t.Fatalf("fired early at %q", got.FiredAt)
	}
}

func TestCheckFollowUpsDropsAnAnsweredReminder(t *testing.T) {
	for _, fired := range []bool{false, true} {
		f := newFollowUpFixture(t)
		f.message(t, f.sent, storage.Message{MessageID: "<q@example.com>", Subject: "Quote", FromAddress: "me@example.com"})
		f.reminder(t, f.now.Add(-time.Minute), 0)
		if fired {
			f.app.checkFollowUps(f.now)
		}
		f.message(t, f.inbox, storage.Message{MessageID: "<r@x>", Subject: "Re: Quote", FromAddress: "ada@x", InReplyTo: []string{"<q@example.com>"}})

		f.app.checkFollowUps(f.now.Add(time.Hour))
		if list, _ := f.app.ListFollowUps(); len(list) != 0 {
			t.Fatalf("fired=%v: follow-ups after the reply = %+v", fired, list)
		}
	}
}

func TestCheckFollowUpsWaitsForTheSendToGoOut(t *testing.T) {
	f := newFollowUpFixture(t)
	id, err := f.app.store.InsertOutbox(f.app.ctx, storage.OutboxRow{AccountID: f.account, State: outbox.StateFailed, Raw: []byte("x")})
	if err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
	f.reminder(t, f.now.Add(-time.Minute), id)
	f.app.checkFollowUps(f.now)
	if got := onlyFollowUp(t, f.app); got.FiredAt != "" {
		t.Fatalf("fired at %q while the message is still in the outbox", got.FiredAt)
	}
}

func TestCancelFollowUp(t *testing.T) {
	f := newFollowUpFixture(t)
	f.reminder(t, f.now.Add(time.Hour), 0)
	got := onlyFollowUp(t, f.app)
	if err := f.app.CancelFollowUp(got.ID); err != nil {
		t.Fatalf("CancelFollowUp: %v", err)
	}
	if err := f.app.CancelFollowUp(got.ID); !errors.Is(err, storage.ErrFollowUpNotFound) {
		t.Fatalf("second cancel = %v", err)
	}
}
