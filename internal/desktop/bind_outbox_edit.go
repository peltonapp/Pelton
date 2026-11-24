package desktop

import (
	"github.com/peltonapp/Pelton/internal/outbox"
)

// Taking a message back out of the outbox. The queue holds finished, possibly
// encrypted MIME, which cannot be turned back into what the composer edits, so
// the request each send was built from is kept in memory for the life of the
// run. A message sent before a restart can still be cancelled, retried or
// discarded, just not edited.

// OutboxEditDTO is the result of taking a message back out of the outbox.
type OutboxEditDTO struct {
	// Taken is false when the message could not be taken back: it was already
	// on its way, or it was not sent in this run.
	Taken bool `json:"taken"`
	// Request is the compose request the message was sent with, when Taken.
	Request ComposeRequest `json:"request"`
}

// EditOutboxMessage takes a scheduled or failed message out of the outbox and
// returns the compose request it was sent with, so it can be opened in a
// composer again. A message that is sending, or was sent before a restart, is
// left where it is and Taken is false.
func (a *App) EditOutboxMessage(id int64) (OutboxEditDTO, error) {
	if err := a.ready(); err != nil {
		return OutboxEditDTO{}, err
	}
	stored, ok := a.sentRequests.Load(id)
	if !ok {
		return OutboxEditDTO{}, nil
	}
	taken, err := a.queue.Cancel(a.ctx, id)
	if err != nil {
		return OutboxEditDTO{}, err
	}
	if !taken {
		if taken, err = a.queue.Discard(a.ctx, id); err != nil {
			return OutboxEditDTO{}, err
		}
	}
	if !taken {
		return OutboxEditDTO{}, nil
	}
	a.sentRequests.Delete(id)
	a.dropFollowUpsForOutbox(id)
	a.emit(EventOutboxChanged, nil)
	return OutboxEditDTO{Taken: true, Request: stored.(ComposeRequest)}, nil
}

// SendOutboxNow makes a scheduled message due immediately and reports whether
// it was still waiting. False means it is already on its way.
func (a *App) SendOutboxNow(id int64) (bool, error) {
	if err := a.ready(); err != nil {
		return false, err
	}
	moved, err := a.queue.SendNow(a.ctx, id)
	if err != nil {
		return false, err
	}
	if moved {
		a.emit(EventOutboxChanged, nil)
	}
	return moved, nil
}

// pruneSentRequests forgets the requests of messages that have left the
// outbox or been sent.
func (a *App) pruneSentRequests(rows []outbox.Message) {
	live := make(map[int64]bool, len(rows))
	for _, r := range rows {
		live[r.ID] = r.State != outbox.StateSent
	}
	a.sentRequests.Range(func(key, _ any) bool {
		if !live[key.(int64)] {
			a.sentRequests.Delete(key)
		}
		return true
	})
}
