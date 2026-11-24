package desktop

import (
	"testing"

	"github.com/peltonapp/Pelton/internal/outbox"
)

func TestCancelOutboxSendPutsAWaitingMessageBackInDrafts(t *testing.T) {
	app, accountID, client := draftTestApp(t)
	app.queue = outbox.NewQueue(app.store)
	app.activity = outbox.NewActivityLog(10, nil)
	id, err := app.SendMessage(ComposeRequest{
		AccountID: accountID,
		To:        []AddressDTO{{Email: "you@example.com"}},
		Subject:   "second thoughts",
		Text:      "body",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	got, err := app.CancelOutboxSend(id)
	if err != nil {
		t.Fatalf("CancelOutboxSend: %v", err)
	}
	settle(t, app)
	if got.Outcome != cancelDraft {
		t.Fatalf("outcome = %q, want %q", got.Outcome, cancelDraft)
	}
	if rows, _ := app.ListOutbox(); len(rows) != 0 {
		t.Fatalf("outbox still holds %+v", rows)
	}
	drafts := listDrafts(t, app)
	if len(drafts) != 1 || drafts[0].Request.Subject != "second thoughts" {
		t.Fatalf("drafts = %+v, want the cancelled message", drafts)
	}
	if len(client.drafts) != 1 || client.drafts[0].mailbox != "Drafts" {
		t.Fatalf("server drafts = %+v, want one copy in Drafts", client.drafts)
	}

	var steps []string
	for _, e := range app.ReadOutboxLog() {
		steps = append(steps, e.Step)
	}
	if len(steps) != 2 || steps[0] != outbox.StepQueued || steps[1] != outbox.StepCancelled {
		t.Fatalf("activity = %v, want queued then cancelled", steps)
	}

	again, err := app.CancelOutboxSend(id)
	if err != nil || again.Outcome != cancelGone {
		t.Fatalf("second cancel = %+v, %v; want gone", again, err)
	}
}

func TestCancelOutboxSendHoldsAMessageFromAnEarlierRun(t *testing.T) {
	app, accountID, _ := draftTestApp(t)
	app.queue = outbox.NewQueue(app.store)
	id, err := app.queue.Enqueue(app.ctx, outbox.Message{
		AccountID:    accountID,
		EnvelopeFrom: "me@example.com",
		Recipients:   []string{"you@example.com"},
		Raw:          []byte("Subject: hi\r\n\r\nbody\r\n"),
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	got, err := app.CancelOutboxSend(id)
	if err != nil || got.Outcome != cancelHeld {
		t.Fatalf("cancel = %+v, %v; want held", got, err)
	}
	rows, _ := app.ListOutbox()
	if len(rows) != 1 || rows[0].State != outbox.StateFailed || rows[0].LastError != outbox.CancelledReason {
		t.Fatalf("outbox = %+v, want the message kept as a cancelled failure", rows)
	}
}

func TestPrioritiseOutboxSend(t *testing.T) {
	app, accountID, _ := draftTestApp(t)
	app.queue = outbox.NewQueue(app.store)
	app.activity = outbox.NewActivityLog(10, nil)
	var ids []int64
	for range 3 {
		id, err := app.queue.Enqueue(app.ctx, outbox.Message{
			AccountID:    accountID,
			EnvelopeFrom: "me@example.com",
			Recipients:   []string{"you@example.com"},
			Raw:          []byte("x"),
		})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		ids = append(ids, id)
	}

	moved, err := app.PrioritiseOutboxSend(ids[2])
	if err != nil || !moved {
		t.Fatalf("prioritise = %v, %v; want moved", moved, err)
	}
	rows, _ := app.ListOutbox()
	for _, r := range rows {
		if (r.ID == ids[2]) != (r.Priority > 0) {
			t.Fatalf("row %d has priority %d", r.ID, r.Priority)
		}
	}
	log := app.ReadOutboxLog()
	if len(log) != 1 || log[0].Step != outbox.StepPrioritised || log[0].MessageID != ids[2] || log[0].Recipients[0] != "you@example.com" {
		t.Fatalf("activity = %+v, want one prioritised entry", log)
	}
}
