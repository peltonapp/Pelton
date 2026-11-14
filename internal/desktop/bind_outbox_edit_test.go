package desktop

import (
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/outbox"
)

func TestEditOutboxMessageTakesAScheduledSendBack(t *testing.T) {
	app, accountID, _ := draftTestApp(t)
	app.queue = outbox.NewQueue(app.store)
	req := ComposeRequest{
		AccountID: accountID,
		To:        []AddressDTO{{Email: "you@example.com"}},
		Subject:   "later",
		Text:      "body",
		SendAt:    time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
	}
	id, err := app.SendMessage(req)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	rows, err := app.ListOutbox()
	if err != nil {
		t.Fatalf("ListOutbox: %v", err)
	}
	if len(rows) != 1 || !rows[0].Editable {
		t.Fatalf("outbox = %+v, want one editable row", rows)
	}

	got, err := app.EditOutboxMessage(id)
	if err != nil {
		t.Fatalf("EditOutboxMessage: %v", err)
	}
	if !got.Taken || got.Request.Subject != "later" || got.Request.To[0].Email != "you@example.com" {
		t.Fatalf("edit = %+v, want the request taken back", got)
	}
	if rows, _ := app.ListOutbox(); len(rows) != 0 {
		t.Fatalf("outbox still holds %+v after taking the message back", rows)
	}

	again, err := app.EditOutboxMessage(id)
	if err != nil {
		t.Fatalf("EditOutboxMessage again: %v", err)
	}
	if again.Taken {
		t.Error("a message taken back once was taken back twice")
	}
}

func TestSendOutboxNowAndForgottenRequests(t *testing.T) {
	app, accountID, _ := draftTestApp(t)
	app.queue = outbox.NewQueue(app.store)
	id, err := app.SendMessage(ComposeRequest{
		AccountID: accountID,
		To:        []AddressDTO{{Email: "you@example.com"}},
		Subject:   "now",
		Text:      "body",
		SendAt:    time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	moved, err := app.SendOutboxNow(id)
	if err != nil || !moved {
		t.Fatalf("SendOutboxNow = %v, %v, want the scheduled send moved", moved, err)
	}

	// a message from before a restart has no request to take back.
	app.sentRequests.Delete(id)
	rows, err := app.ListOutbox()
	if err != nil {
		t.Fatalf("ListOutbox: %v", err)
	}
	if len(rows) != 1 || rows[0].Editable {
		t.Fatalf("outbox = %+v, want one row that cannot be edited", rows)
	}
	got, err := app.EditOutboxMessage(id)
	if err != nil {
		t.Fatalf("EditOutboxMessage: %v", err)
	}
	if got.Taken {
		t.Error("took back a message whose request is not known")
	}
	if rows, _ := app.ListOutbox(); len(rows) != 1 {
		t.Errorf("outbox = %+v, want the message left where it was", rows)
	}
}
