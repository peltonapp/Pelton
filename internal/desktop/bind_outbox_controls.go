package desktop

import (
	"errors"

	"github.com/peltonapp/Pelton/internal/outbox"
)

// Controls over the send queue beyond retry and discard: cancelling a send,
// sending one first, and the live activity log behind the send activity
// window.

// activityLogSize is how many steps the activity log keeps.
const activityLogSize = 400

// Outcomes of CancelOutboxSend.
const (
	// cancelDraft: the message is back in the Drafts folder.
	cancelDraft = "draft"
	// cancelCompose: the message could not be put in Drafts, so its request
	// comes back to be opened in a composer.
	cancelCompose = "compose"
	// cancelHeld: the message was stopped and kept as a failed send, because
	// what it was written from is not known in this run.
	cancelHeld = "held"
	// cancelTooLate: the server had already accepted the message.
	cancelTooLate = "tooLate"
	// cancelGone: the message was no longer waiting or sending.
	cancelGone = "gone"
)

// OutboxCancelDTO is the result of cancelling a send.
type OutboxCancelDTO struct {
	// Outcome is one of draft, compose, held, tooLate or gone.
	Outcome string `json:"outcome"`
	// Request is what the message was written from, when Outcome is compose.
	Request ComposeRequest `json:"request"`
}

// CancelOutboxSend stops a message on its way out. A waiting message is taken
// out of the queue; one being transmitted is aborted when the server has not
// yet accepted it. Either way it goes back to the Drafts folder when the
// request it was written from is known. When the draft cannot be put there
// (an encrypted message, a mailbox without a server, a failed upload) the
// request is returned for a composer instead, and a message sent before a
// restart stays in the outbox as a failed send. Nothing is dropped.
func (a *App) CancelOutboxSend(id int64) (OutboxCancelDTO, error) {
	if err := a.ready(); err != nil {
		return OutboxCancelDTO{}, err
	}
	result, err := a.withdraw(id)
	if err != nil {
		return OutboxCancelDTO{}, err
	}
	switch result {
	case outbox.WithdrawTooLate:
		return OutboxCancelDTO{Outcome: cancelTooLate}, nil
	case outbox.WithdrawNone:
		return OutboxCancelDTO{Outcome: cancelGone}, nil
	}
	a.emit(EventOutboxChanged, nil)

	stored, ok := a.sentRequests.Load(id)
	if !ok {
		return OutboxCancelDTO{Outcome: cancelHeld}, nil
	}
	req := stored.(ComposeRequest)
	req.SendAt = ""
	// the held message leaves the outbox first, so it can never go out as
	// well as come back; the request in hand covers a draft that fails.
	discarded, err := a.queue.Discard(a.ctx, id)
	if err != nil {
		return OutboxCancelDTO{}, err
	}
	if !discarded {
		return OutboxCancelDTO{Outcome: cancelGone}, nil
	}
	a.sentRequests.Delete(id)
	a.emit(EventOutboxChanged, nil)
	if !encrypts(req.Protection) && a.draftOnServer(req) {
		return OutboxCancelDTO{Outcome: cancelDraft}, nil
	}
	return OutboxCancelDTO{Outcome: cancelCompose, Request: req}, nil
}

// withdraw stops message id through the running worker, or straight in the
// queue before the worker has started.
func (a *App) withdraw(id int64) (outbox.Withdrawal, error) {
	if w := a.sendWorker.Load(); w != nil {
		return w.Withdraw(a.ctx, id)
	}
	held, err := a.queue.Hold(a.ctx, id)
	if err != nil || !held {
		return outbox.WithdrawNone, err
	}
	a.activity.Add(outbox.Entry{MessageID: id, Step: outbox.StepCancelled, Recipients: a.outboxRecipients(id)})
	return outbox.WithdrawHeld, nil
}

// draftOnServer saves req as a draft and puts it in its account's Drafts
// folder, reporting whether it got there. A draft that did not is removed
// again, since nothing could open it.
func (a *App) draftOnServer(req ComposeRequest) bool {
	draftID, err := a.SaveDraft(0, req)
	if err != nil {
		a.log.Error("save a cancelled send as a draft", "err", err)
		return false
	}
	messageID, err := a.UploadDraft(draftID)
	if err == nil && messageID != "" {
		return true
	}
	if err != nil && !errors.Is(err, ErrProtectedDraftStaysLocal) {
		a.log.Error("upload a cancelled send as a draft", "err", err)
	}
	if err := a.DeleteDraft(draftID); err != nil {
		a.log.Error("remove a cancelled send's local draft", "err", err)
	}
	return false
}

// PrioritiseOutboxSend moves a waiting message to the front of the queue, so
// it is the next one sent once due, and reports whether it was still waiting.
func (a *App) PrioritiseOutboxSend(id int64) (bool, error) {
	if err := a.ready(); err != nil {
		return false, err
	}
	moved, err := a.queue.Prioritise(a.ctx, id)
	if err != nil || !moved {
		return false, err
	}
	a.activity.Add(outbox.Entry{MessageID: id, Step: outbox.StepPrioritised, Recipients: a.outboxRecipients(id)})
	a.emit(EventOutboxChanged, nil)
	return true, nil
}

// outboxRecipients returns the recipients of message id, or nil.
func (a *App) outboxRecipients(id int64) []string {
	rows, err := a.queue.List(a.ctx)
	if err != nil {
		return nil
	}
	for _, r := range rows {
		if r.ID == id {
			return r.Recipients
		}
	}
	return nil
}

// OutboxLogEntryDTO is one line of the send activity log.
type OutboxLogEntryDTO struct {
	Seq        uint64   `json:"seq"`
	At         string   `json:"at"`
	MessageID  int64    `json:"messageId"`
	Step       string   `json:"step"`
	Recipients []string `json:"recipients"`
	// Detail is the error of a failed step.
	Detail string `json:"detail"`
	// RetryAt is when a retry step's message is tried again.
	RetryAt string `json:"retryAt"`
}

// ReadOutboxLog returns the send activity of this run, oldest first. It is
// kept in memory only and is empty after a restart.
func (a *App) ReadOutboxLog() []OutboxLogEntryDTO {
	entries := a.activity.Entries()
	out := make([]OutboxLogEntryDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, toLogEntryDTO(e))
	}
	return out
}

// emitActivity pushes a new activity log entry to the ui.
func (a *App) emitActivity(e outbox.Entry) {
	a.emit(EventOutboxLog, toLogEntryDTO(e))
	if e.Step == outbox.StepSent {
		a.scriptAfterSend(e.MessageID)
	}
}

func toLogEntryDTO(e outbox.Entry) OutboxLogEntryDTO {
	recipients := e.Recipients
	if recipients == nil {
		recipients = []string{}
	}
	return OutboxLogEntryDTO{
		Seq:        e.Seq,
		At:         e.At.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		MessageID:  e.MessageID,
		Step:       e.Step,
		Recipients: recipients,
		Detail:     e.Detail,
		RetryAt:    formatDate(e.RetryAt),
	}
}
