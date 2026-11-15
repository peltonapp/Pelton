package desktop

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Follow-up reminders: "remind me if nobody replies". A reminder is set with
// a send and stored against the sent message's Message-ID. The snooze poller
// checks them off the ui thread: a reply that names the message in its
// In-Reply-To or References drops the reminder without a word, and a reminder
// that comes due without one fires once, with a notification, and stays until
// the user dismisses it.

// ErrFollowUpInvalid is returned when a ComposeRequest.FollowUpAt string does
// not parse as RFC3339.
var ErrFollowUpInvalid = errors.New("pelton: followUpAt is not a valid RFC3339 timestamp")

// ErrFollowUpTooEarly is returned when a reminder would come due before the
// message it is about has gone out.
var ErrFollowUpTooEarly = errors.New("pelton: the reminder must come after the message is sent")

// FollowUpDTO is one reminder for the frontend. Times are RFC3339; FiredAt is
// empty while the reminder is waiting. LocalID, FolderID and ThreadID point
// at the cached copy of the sent message, the one in Sent when there is one,
// and are 0 until a copy has synced.
type FollowUpDTO struct {
	ID         int64  `json:"id"`
	AccountID  int64  `json:"accountId"`
	MessageID  string `json:"messageId"`
	Subject    string `json:"subject"`
	Recipients string `json:"recipients"`
	SentAt     string `json:"sentAt"`
	DueAt      string `json:"dueAt"`
	FiredAt    string `json:"firedAt"`
	LocalID    int64  `json:"localId"`
	FolderID   int64  `json:"folderId"`
	ThreadID   int64  `json:"threadId"`
}

// resolveFollowUp parses a reminder time and checks it comes after the send,
// which goes out at notBefore or, when that is zero, now. Empty means no
// reminder and returns the zero time.
func resolveFollowUp(at string, notBefore, now time.Time) (time.Time, error) {
	if at == "" {
		return time.Time{}, nil
	}
	when, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return time.Time{}, ErrFollowUpInvalid
	}
	sends := now
	if notBefore.After(now) {
		sends = notBefore
	}
	if !when.After(sends) {
		return time.Time{}, ErrFollowUpTooEarly
	}
	return when.UTC(), nil
}

// recipientSummary is the To line of a reminder: names where given, else
// addresses, comma separated.
func recipientSummary(to []AddressDTO) string {
	parts := make([]string, 0, len(to))
	for _, a := range to {
		if name := strings.TrimSpace(a.Name); name != "" {
			parts = append(parts, name)
		} else if email := strings.TrimSpace(a.Email); email != "" {
			parts = append(parts, email)
		}
	}
	return strings.Join(parts, ", ")
}

// recordFollowUp stores the reminder of a send just queued. A failure is
// logged and does not fail the send: the message is already in the outbox.
func (a *App) recordFollowUp(req ComposeRequest, outboxID int64, messageID string, due, sends time.Time) {
	if due.IsZero() {
		return
	}
	if sends.IsZero() {
		sends = time.Now().UTC()
	}
	f := storage.FollowUp{
		AccountID:  req.AccountID,
		MessageID:  messageID,
		OutboxID:   outboxID,
		Subject:    req.Subject,
		Recipients: recipientSummary(req.To),
		SentAt:     sends,
		DueAt:      due,
	}
	if _, err := a.store.AddFollowUp(a.ctx, &f); err != nil {
		a.log.Error("record follow-up reminder", "outbox", outboxID, "err", err)
		return
	}
	a.emit(EventFollowUpsChanged, nil)
}

// dropFollowUpsForOutbox forgets the reminders of a send taken back out of
// the outbox.
func (a *App) dropFollowUpsForOutbox(outboxID int64) {
	gone, err := a.store.DeleteFollowUpsForOutbox(a.ctx, outboxID)
	if err != nil {
		a.log.Error("drop follow-up reminders", "outbox", outboxID, "err", err)
		return
	}
	if gone {
		a.emit(EventFollowUpsChanged, nil)
	}
}

// ListFollowUps returns every reminder, waiting and fired, the soonest due
// first.
func (a *App) ListFollowUps() ([]FollowUpDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	list, err := a.store.ListFollowUps(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FollowUpDTO, 0, len(list))
	for _, f := range list {
		dto := FollowUpDTO{
			ID:         f.ID,
			AccountID:  f.AccountID,
			MessageID:  f.MessageID,
			Subject:    f.Subject,
			Recipients: f.Recipients,
			SentAt:     formatRFC3339(f.SentAt),
			DueAt:      formatRFC3339(f.DueAt),
			FiredAt:    formatRFC3339(f.FiredAt),
		}
		if c, ok := a.sentCopy(f); ok {
			dto.LocalID, dto.FolderID, dto.ThreadID = c.ID, c.FolderID, c.ThreadID
		}
		out = append(out, dto)
	}
	return out, nil
}

// sentCopy picks the cached copy of a reminder's message to open: the one in
// Sent when there is one, else the first one cached.
func (a *App) sentCopy(f storage.FollowUp) (storage.MessageCopy, bool) {
	copies, err := a.store.CopiesOf(a.ctx, f.AccountID, f.MessageID)
	if err != nil || len(copies) == 0 {
		return storage.MessageCopy{}, false
	}
	for _, c := range copies {
		if folder, err := a.store.GetFolder(a.ctx, c.FolderID); err == nil && folderRole(*folder) == roleSent {
			return c, true
		}
	}
	return copies[0], true
}

// CancelFollowUp drops a reminder, waiting or fired.
func (a *App) CancelFollowUp(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteFollowUp(a.ctx, id); err != nil {
		return err
	}
	a.emit(EventFollowUpsChanged, nil)
	return nil
}

// checkFollowUps settles every reminder against now: one answered is dropped,
// one due without an answer fires and notifies. A reminder whose send is
// still in the outbox waits for it to go out. It runs on the snooze poller's
// goroutine.
func (a *App) checkFollowUps(now time.Time) {
	list, err := a.store.ListFollowUps(a.ctx)
	if err != nil {
		a.log.Error("follow-ups: list", "err", err)
		return
	}
	if len(list) == 0 {
		return
	}
	waiting, err := a.unsentOutbox()
	if err != nil {
		a.log.Error("follow-ups: read outbox", "err", err)
		return
	}
	changed := false
	for _, f := range list {
		if a.ctx.Err() != nil {
			return
		}
		replied, err := a.store.FollowUpReplied(a.ctx, f)
		if err != nil {
			a.log.Error("follow-ups: look for a reply", "id", f.ID, "err", err)
			continue
		}
		if replied {
			if err := a.store.DeleteFollowUp(a.ctx, f.ID); err != nil && !errors.Is(err, storage.ErrFollowUpNotFound) {
				a.log.Error("follow-ups: drop answered", "id", f.ID, "err", err)
				continue
			}
			changed = true
			continue
		}
		if !f.FiredAt.IsZero() || f.DueAt.After(now) || waiting[f.OutboxID] {
			continue
		}
		if err := a.store.MarkFollowUpFired(a.ctx, f.ID, now); err != nil {
			a.log.Error("follow-ups: fire", "id", f.ID, "err", err)
			continue
		}
		changed = true
		a.notifyFollowUp(f)
	}
	if changed {
		a.emit(EventFollowUpsChanged, nil)
	}
}

// unsentOutbox is the set of outbox ids still waiting to go out or failed.
func (a *App) unsentOutbox() (map[int64]bool, error) {
	rows, err := a.store.ListOutbox(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(rows))
	for _, r := range rows {
		if r.State != outbox.StateSent {
			out[r.ID] = true
		}
	}
	return out, nil
}

// notifyFollowUp raises the notification of a reminder that came due. A
// click opens the sent message when a copy of it is cached.
func (a *App) notifyFollowUp(f storage.FollowUp) {
	s := notifyStringsFor(a.stringSetting(settingLanguage, "en"))
	subject := strings.TrimSpace(f.Subject)
	if subject == "" {
		subject = s.noSubject
	}
	body := subject
	if f.Recipients != "" {
		body = fmt.Sprintf(s.followUpTo, f.Recipients) + "\n" + subject
	}
	n := notification{title: s.followUp, body: body}
	if c, ok := a.sentCopy(f); ok {
		n.messageID = c.ID
	}
	a.sendNotification(n)
}

// formatRFC3339 renders a time for the frontend, the zero time as "".
func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
