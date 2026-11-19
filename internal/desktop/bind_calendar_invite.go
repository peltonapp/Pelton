package desktop

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/rfc822"
	"github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Invitations (iTIP, RFC 5546) arriving by mail. A REQUEST is answered with
// Accept, Tentative or Decline: the event goes into the chosen calendar with
// the user's answer on it, and the REPLY goes to the organizer through the
// outbox like any other mail. A CANCEL or a REPLY from an attendee is applied
// to the copy already stored.

var errInviteNeedsAnswer = errors.New("pelton: this invitation is answered, not applied")

// InviteDTO is the invitation a message carries, for the banner above it.
type InviteDTO struct {
	// Found is false for a message without calendar data; nothing else is set
	// then.
	Found bool `json:"found"`
	// Method is REQUEST, CANCEL, REPLY or PUBLISH, or empty for an event
	// shared without one.
	Method       string        `json:"method"`
	UID          string        `json:"uid"`
	Sequence     int           `json:"sequence"`
	Summary      string        `json:"summary"`
	Description  string        `json:"description"`
	Location     string        `json:"location"`
	Start        string        `json:"start"`
	End          string        `json:"end"`
	AllDay       bool          `json:"allDay"`
	TimeZone     string        `json:"timeZone"`
	Recurring    bool          `json:"recurring"`
	RRule        string        `json:"rrule"`
	RecurrenceID string        `json:"recurrenceId"`
	Organizer    AttendeeDTO   `json:"organizer"`
	Attendees    []AttendeeDTO `json:"attendees"`
	// MyEmail is the mailbox address the invitation is read for, and
	// MyPartStat that attendee's answer so far, as stored or as invited.
	MyEmail    string `json:"myEmail"`
	MyPartStat string `json:"myPartStat"`
	// ExistingEventID and ExistingCalendarID name the copy an earlier answer
	// stored, 0 when there is none.
	ExistingEventID    int64 `json:"existingEventId"`
	ExistingCalendarID int64 `json:"existingCalendarId"`
	// Outdated is true when the stored copy is newer than this invitation, as
	// an older mail in the thread is.
	Outdated bool `json:"outdated"`
}

// InviteAnswerRequest answers an invitation. Response is accepted, tentative
// or declined. CalendarID is where an accepted event goes when no copy is
// stored yet; 0 means the default calendar.
type InviteAnswerRequest struct {
	MessageID  int64  `json:"messageId"`
	CalendarID int64  `json:"calendarId"`
	Response   string `json:"response"`
	SendReply  bool   `json:"sendReply"`
}

// InviteAnswerResultDTO is what answering or applying did.
type InviteAnswerResultDTO struct {
	// EventID is the stored event, 0 when the answer removed it.
	EventID int64 `json:"eventId"`
	// OutboxID is the queued reply, 0 when none was sent.
	OutboxID int64 `json:"outboxId"`
}

// ParseInvite reads the invitation in a message, from an .ics attachment or,
// when the inline text/calendar part was not kept, from the message source on
// the server.
func (a *App) ParseInvite(messageID int64) (InviteDTO, error) {
	if err := a.ready(); err != nil {
		return InviteDTO{}, err
	}
	inv, account, err := a.loadInvite(messageID)
	if errors.Is(err, errInviteNotFound) {
		return InviteDTO{}, nil
	}
	if err != nil {
		return InviteDTO{}, err
	}
	obj := inv.Object
	ev := obj.First()
	start := obj.Zones.Instant(ev.Start)
	dto := InviteDTO{
		Found:       true,
		Method:      inv.Method,
		UID:         ev.UID,
		Sequence:    ev.Sequence,
		Summary:     ev.Summary,
		Description: ev.Description,
		Location:    ev.Location,
		Start:       formatEventTime(start, ev.Start.AllDay),
		End:         formatEventTime(start, ev.Start.AllDay),
		AllDay:      ev.Start.AllDay,
		Recurring:   obj.Master != nil && obj.Master.IsRecurring(),
		Attendees:   []AttendeeDTO{},
		MyEmail:     account.Email,
	}
	for _, occ := range obj.Expand(start, start.Add(time.Second)) {
		if occ.Event == ev {
			dto.End = formatEventTime(occ.End, occ.AllDay)
		}
	}
	if loc, ok := obj.Zones.Location(ev.Start.TZID); ok && !ev.Start.UTC {
		dto.TimeZone = loc.String()
	}
	if obj.Master != nil && obj.Master.RRule != nil {
		dto.RRule = obj.Master.RRule.String()
	}
	if !ev.RecurrenceID.IsZero() {
		dto.RecurrenceID = formatRecurrenceID(obj.Zones.Instant(ev.RecurrenceID))
	}
	if ev.Organizer != nil {
		dto.Organizer = toAttendeeDTO(*ev.Organizer)
	}
	for _, at := range ev.Attendees {
		dto.Attendees = append(dto.Attendees, toAttendeeDTO(at))
	}
	if me := ev.Attendee(account.Email); me != nil {
		dto.MyPartStat = me.PartStat
	}
	existing, err := a.store.FindCalendarEventsByUID(a.ctx, ev.UID)
	if err != nil {
		return InviteDTO{}, err
	}
	if len(existing) > 0 {
		dto.ExistingEventID, dto.ExistingCalendarID = existing[0].ID, existing[0].CalendarID
		if stored, err := parseStoredEvent(existing[0]); err == nil {
			if first := stored.First(); first != nil {
				dto.Outdated = first.Sequence > ev.Sequence
				if me := first.Attendee(account.Email); me != nil {
					dto.MyPartStat = me.PartStat
				}
			}
		}
	}
	return dto, nil
}

// AnswerInvite answers an invitation. Accepting or tentatively accepting
// stores the event, in the calendar holding an earlier copy or else in
// CalendarID; declining removes a stored copy. With SendReply the iTIP REPLY
// is queued to the organizer.
func (a *App) AnswerInvite(req InviteAnswerRequest) (InviteAnswerResultDTO, error) {
	if err := a.ready(); err != nil {
		return InviteAnswerResultDTO{}, err
	}
	partstat, err := partStatFor(req.Response)
	if err != nil {
		return InviteAnswerResultDTO{}, err
	}
	inv, account, err := a.loadInvite(req.MessageID)
	if err != nil {
		return InviteAnswerResultDTO{}, err
	}
	if inv.Method == ical.MethodCancel || inv.Method == ical.MethodReply {
		return InviteAnswerResultDTO{}, errInviteNeedsAnswer
	}
	first := inv.Object.First()
	name := account.DisplayName
	reply := inv.Object.Reply(account.Email, name, partstat)

	answered, err := ical.ParseObject(inv.Object.Encode(), time.Local)
	if err != nil {
		return InviteAnswerResultDTO{}, err
	}
	answered.SetPartStat(account.Email, name, partstat)
	answered.SetMethod("")

	var result InviteAnswerResultDTO
	existing, err := a.store.FindCalendarEventsByUID(a.ctx, first.UID)
	if err != nil {
		return result, err
	}
	if partstat == ical.PartStatDeclined {
		if err := a.declineStored(existing, answered); err != nil {
			return result, err
		}
	} else if result.EventID, err = a.storeAnswered(existing, answered, req.CalendarID); err != nil {
		return result, err
	}

	if req.SendReply && inv.Method == ical.MethodRequest {
		if first.Organizer == nil || first.Organizer.Email == "" {
			return result, errInviteNoOrganizer
		}
		m, err := a.store.GetMessage(a.ctx, req.MessageID)
		if err != nil {
			return result, err
		}
		if result.OutboxID, err = a.sendInviteReply(*account, *first.Organizer, first.Summary, partstat, m.MessageID, reply); err != nil {
			return result, err
		}
	}
	return result, nil
}

// ApplyInvite applies a CANCEL from the organizer or a REPLY from an attendee
// to the stored copy of the event.
func (a *App) ApplyInvite(messageID int64) (InviteAnswerResultDTO, error) {
	if err := a.ready(); err != nil {
		return InviteAnswerResultDTO{}, err
	}
	inv, _, err := a.loadInvite(messageID)
	if err != nil {
		return InviteAnswerResultDTO{}, err
	}
	if inv.Method != ical.MethodCancel && inv.Method != ical.MethodReply {
		return InviteAnswerResultDTO{}, errInviteNeedsAnswer
	}
	existing, err := a.store.FindCalendarEventsByUID(a.ctx, inv.Object.UID())
	if err != nil {
		return InviteAnswerResultDTO{}, err
	}
	var result InviteAnswerResultDTO
	for _, row := range existing {
		cal, err := a.store.GetCalendar(a.ctx, row.CalendarID)
		if err != nil {
			return result, err
		}
		if cal.ReadOnly {
			continue
		}
		stored, err := parseStoredEvent(row)
		if err != nil {
			return result, err
		}
		for _, ev := range inv.Object.Events() {
			target := stored.First()
			if !ev.RecurrenceID.IsZero() {
				rid := inv.Object.Zones.Instant(ev.RecurrenceID)
				if stored.Master != nil {
					target = stored.Override(rid)
				} else if ov := stored.FindOverride(rid); ov != nil {
					target = ov
				}
			}
			if target == nil {
				continue
			}
			if inv.Method == ical.MethodCancel {
				target.Status = ical.StatusCancelled
			} else {
				for _, answer := range ev.Attendees {
					if mine := target.Attendee(answer.Email); mine != nil {
						mine.PartStat = answer.PartStat
					}
				}
			}
			target.Apply()
		}
		saved, err := a.commitEvent(a.ctx, *cal, row, stored, time.Time{}, false)
		if err != nil {
			return result, err
		}
		result.EventID = saved.Event.ID
	}
	return result, nil
}

// loadInvite finds and parses the calendar data of a message, and the mailbox
// it arrived in.
func (a *App) loadInvite(messageID int64) (*ical.Invite, *storage.Account, error) {
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return nil, nil, err
	}
	account, err := a.store.GetAccount(a.ctx, m.AccountID)
	if err != nil {
		return nil, nil, err
	}
	for _, data := range a.inviteData(*m, account.Local) {
		if inv, err := ical.ParseInvite(data, time.Local); err == nil {
			return inv, account, nil
		}
	}
	return nil, nil, errInviteNotFound
}

// inviteData collects the iCalendar parts of a message: stored .ics
// attachments first, then the parts in the message source, which is where an
// inline text/calendar alternative is. The source is only fetched for mail on
// a server.
func (a *App) inviteData(m storage.Message, local bool) [][]byte {
	var out [][]byte
	atts, err := a.store.ListAttachments(a.ctx, m.ID)
	if err != nil {
		a.log.Warn("list attachments for an invitation", "message", m.ID, "err", err)
	}
	for _, att := range atts {
		if !rfc822.IsCalendar(att.ContentType, att.Filename) {
			continue
		}
		f, err := a.store.OpenAttachment(att.DiskPath)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, 8<<20))
		f.Close()
		if err == nil {
			out = append(out, data)
		}
	}
	if len(out) > 0 || local {
		return out
	}
	raw, err := a.GetMessageSource(m.ID)
	if err != nil {
		a.log.Warn("fetch a message for its invitation", "message", m.ID, "err", err)
		return nil
	}
	parts, err := rfc822.CalendarParts([]byte(raw))
	if err != nil {
		a.log.Warn("read the invitation in a message", "message", m.ID, "err", err)
	}
	return append(out, parts...)
}

// storeAnswered stores an answered invitation: over the copy an earlier answer
// left, or new in calendarID. An invitation to one occurrence of a series
// already stored becomes that occurrence's override.
func (a *App) storeAnswered(existing []storage.CalendarEvent, answered *ical.Object, calendarID int64) (int64, error) {
	if len(existing) > 0 {
		row := existing[0]
		cal, err := a.store.GetCalendar(a.ctx, row.CalendarID)
		if err != nil {
			return 0, err
		}
		obj := answered
		if answered.Master == nil {
			stored, err := parseStoredEvent(row)
			if err != nil {
				return 0, err
			}
			mergeOverrides(stored, answered)
			obj = stored
		}
		saved, err := a.commitEvent(a.ctx, *cal, row, obj, time.Time{}, false)
		return saved.Event.ID, err
	}
	if calendarID == 0 {
		cals, err := a.store.ListCalendars(a.ctx)
		if err != nil {
			return 0, err
		}
		calendarID = a.defaultCalendarID(cals)
	}
	if calendarID == 0 {
		return 0, errEventNeedsCalendar
	}
	cal, err := a.store.GetCalendar(a.ctx, calendarID)
	if err != nil {
		return 0, err
	}
	if cal.ReadOnly {
		return 0, errCalendarReadOnly
	}
	answered.EnsureTimezones()
	saved, err := a.commitEvent(a.ctx, *cal, newEventRow(*cal, answered), answered, time.Time{}, false)
	return saved.Event.ID, err
}

// declineStored removes what declining takes away: the one occurrence for an
// invitation to an occurrence, else the stored event.
func (a *App) declineStored(existing []storage.CalendarEvent, answered *ical.Object) error {
	for _, row := range existing {
		cal, err := a.store.GetCalendar(a.ctx, row.CalendarID)
		if err != nil {
			return err
		}
		stored, err := parseStoredEvent(row)
		if err != nil {
			return err
		}
		if answered.Master == nil && stored.Master != nil {
			for _, ov := range answered.Overrides {
				stored.ExcludeOccurrence(answered.Zones.Instant(ov.RecurrenceID))
			}
			if _, err := a.commitEvent(a.ctx, *cal, row, stored, time.Time{}, false); err != nil {
				return err
			}
			continue
		}
		if _, err := a.deleteEventRow(a.ctx, *cal, row, false); err != nil {
			return err
		}
	}
	return nil
}

// mergeOverrides puts the overrides of an invitation into a stored series,
// replacing the ones for the same occurrences.
func mergeOverrides(stored, invite *ical.Object) {
	for _, ov := range invite.Overrides {
		rid := invite.Zones.Instant(ov.RecurrenceID)
		replaced := false
		for i, mine := range stored.Overrides {
			if stored.Zones.Instant(mine.RecurrenceID).Equal(rid) {
				stored.Overrides[i] = ov
				replaced = true
			}
		}
		if !replaced {
			stored.Overrides = append(stored.Overrides, ov)
		}
	}
}

// sendInviteReply queues the iTIP REPLY to the organizer, as a reply in the
// invitation's thread.
func (a *App) sendInviteReply(account storage.Account, organizer ical.Attendee, summary, partstat, inReplyTo string, reply *ical.Object) (int64, error) {
	text := calendarTextFor(a.stringSetting(settingLanguage, "en"))
	format := text.accepted
	switch partstat {
	case ical.PartStatTentative:
		format = text.tentative
	case ical.PartStatDeclined:
		format = text.declined
	}
	if strings.TrimSpace(summary) == "" {
		summary = text.untitled
	}
	subject := fmt.Sprintf(format, summary)
	msg := &smtp.Message{
		From:      smtp.Address{Name: account.DisplayName, Email: account.Email},
		To:        []smtp.Address{{Name: organizer.Name, Email: organizer.Email}},
		Subject:   subject,
		Text:      subject + "\n",
		InReplyTo: inReplyTo,
		Calendar:  &smtp.CalendarPart{Method: ical.MethodReply, Data: reply.Encode()},
	}
	id, err := smtp.Enqueue(a.ctx, a.queue, account.ID, msg, nil, crypto.ModeNone, crypto.Options{}, time.Time{})
	if err != nil {
		return 0, err
	}
	a.emit(EventOutboxChanged, nil)
	return id, nil
}

func partStatFor(response string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(response)) {
	case "accepted", "accept":
		return ical.PartStatAccepted, nil
	case "tentative":
		return ical.PartStatTentative, nil
	case "declined", "decline":
		return ical.PartStatDeclined, nil
	}
	return "", errInviteUnknownResponse
}
