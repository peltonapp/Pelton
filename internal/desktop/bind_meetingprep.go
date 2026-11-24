// bind_meetingprep.go gathers what helps with a meeting from local data only:
// who is invited and what the address book knows of them, the recent mail with
// them, the files exchanged, the open tasks that touch them, the earlier
// meetings with the same people, and how to join. Nothing here reaches the
// network. The ranking is internal/meetingprep's; this layer reads the store.
package desktop

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/ical"
	"github.com/peltonapp/Pelton/internal/meetingprep"
	"github.com/peltonapp/Pelton/internal/storage"
)

// How much of the local data one preparation reads.
const (
	// prepTimeout bounds the whole gathering.
	prepTimeout = 20 * time.Second
	// prepMailDays is how far back mail with the attendees is read.
	prepMailDays = 180
	// prepMailScan caps the messages read for the conversations.
	prepMailScan = 500
	// prepMailAttendees caps how many attendees the mail lookups match.
	prepMailAttendees = 12
	// prepThreads is how many conversations are shown.
	prepThreads = 8
	// prepFileDays is how far back files exchanged with the attendees go.
	prepFileDays = 90
	// prepFileMessages caps the messages whose attachments are listed.
	prepFileMessages = 80
	// prepFiles is how many files are shown.
	prepFiles = 8
	// prepTasks is how many open tasks are shown.
	prepTasks = 10
	// prepPrevious is how many earlier meetings are shown.
	prepPrevious = 5
	// prepEventDays is how far back earlier meetings are looked for.
	prepEventDays = 365
	// prepEventRows caps the stored events read for earlier meetings.
	prepEventRows = 2000
)

// PrepAttendeeDTO is one person invited, with the address book's card for them
// when there is one.
type PrepAttendeeDTO struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	// Role is the iCalendar role and PartStat the reply: NEEDS-ACTION,
	// ACCEPTED, TENTATIVE, DECLINED or DELEGATED.
	Role     string `json:"role"`
	PartStat string `json:"partStat"`
	// Organizer is true for the person who invited, Self for the user's own
	// address.
	Organizer bool `json:"organizer"`
	Self      bool `json:"self"`
	// Contact is the address book card holding the address, null when none does.
	Contact *ContactDTO `json:"contact"`
}

// PrepThreadDTO is one conversation worth a look, with its newest message to
// open in the reader.
type PrepThreadDTO struct {
	ThreadID int64  `json:"threadId"`
	Subject  string `json:"subject"`
	// Last is rfc3339 for the newest message found.
	Last     string `json:"last"`
	Messages int    `json:"messages"`
	// People are the invited who take part, by name or address.
	People []string `json:"people"`
	// TitleMatch is true when the subject matches the event's title.
	TitleMatch bool              `json:"titleMatch"`
	Message    MessageSummaryDTO `json:"message"`
}

// PrepMeetingDTO is an earlier meeting with the same people.
type PrepMeetingDTO struct {
	EventID      int64  `json:"eventId"`
	RecurrenceID string `json:"recurrenceId"`
	Title        string `json:"title"`
	Start        string `json:"start"`
	End          string `json:"end"`
	AllDay       bool   `json:"allDay"`
	// SameSeries is true for an earlier occurrence of the same recurring event.
	SameSeries bool `json:"sameSeries"`
	// Shared are the invited of this meeting who were at that one too.
	Shared []string `json:"shared"`
}

// JoinLinkDTO is how to join the call.
type JoinLinkDTO struct {
	URL string `json:"url"`
	// Provider is the service's name when it is a known one, else empty.
	Provider string `json:"provider"`
}

// MeetingPrepDTO is everything the Prepare panel shows for one meeting.
type MeetingPrepDTO struct {
	EventID      int64  `json:"eventId"`
	RecurrenceID string `json:"recurrenceId"`
	UID          string `json:"uid"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Location     string `json:"location"`
	URL          string `json:"url"`
	Start        string `json:"start"`
	End          string `json:"end"`
	AllDay       bool   `json:"allDay"`
	Recurring    bool   `json:"recurring"`
	// Join is null when the event names no call to join.
	Join      *JoinLinkDTO      `json:"join"`
	Attendees []PrepAttendeeDTO `json:"attendees"`
	Threads   []PrepThreadDTO   `json:"threads"`
	// Files are attachments from mail with the invited, newest first, each
	// listed once.
	Files    []AttachmentEntryDTO `json:"files"`
	Tasks    []TaskDTO            `json:"tasks"`
	Previous []PrepMeetingDTO     `json:"previous"`
}

// PrepareMeeting gathers what helps with a meeting from local data: the
// invited with their address book cards and replies, recent conversations with
// them (those with several of them or with a subject matching the title
// first), files exchanged, open tasks linked to them or to those mails,
// earlier meetings with the same people, and the call's join link. A
// recurrenceID names one occurrence of a series. It reads only the store, with
// the limits above.
func (a *App) PrepareMeeting(eventID int64, recurrenceID string) (MeetingPrepDTO, error) {
	if err := a.ready(); err != nil {
		return MeetingPrepDTO{}, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, prepTimeout)
	defer cancel()

	ev, err := a.GetEvent(eventID, recurrenceID)
	if err != nil {
		return MeetingPrepDTO{}, err
	}
	start, err := parseEventTimeText(ev.Start, ev.AllDay)
	if err != nil {
		return MeetingPrepDTO{}, err
	}
	out := MeetingPrepDTO{
		EventID:      ev.ID,
		RecurrenceID: ev.RecurrenceID,
		UID:          ev.UID,
		Title:        ev.Summary,
		Description:  ev.Description,
		Location:     ev.Location,
		URL:          ev.URL,
		Start:        ev.Start,
		End:          ev.End,
		AllDay:       ev.AllDay,
		Recurring:    ev.Recurring,
		Attendees:    []PrepAttendeeDTO{},
		Threads:      []PrepThreadDTO{},
		Files:        []AttachmentEntryDTO{},
		Tasks:        []TaskDTO{},
		Previous:     []PrepMeetingDTO{},
	}
	if link, ok := meetingprep.ConferenceLink(ev.URL, ev.Location, ev.Description); ok {
		out.Join = &JoinLinkDTO{URL: link.URL, Provider: link.Provider}
	}

	self, err := a.ownAddresses(ctx)
	if err != nil {
		return MeetingPrepDTO{}, err
	}
	if out.Attendees, err = a.prepAttendees(ctx, ev, self); err != nil {
		return MeetingPrepDTO{}, err
	}
	var others []string
	for _, p := range out.Attendees {
		if !p.Self {
			others = append(others, p.Email)
		}
	}

	messages, err := a.prepMessages(ctx, others, ev.Summary)
	if err != nil {
		return MeetingPrepDTO{}, err
	}
	var threadMessageIDs map[string]bool
	if out.Threads, out.Files, threadMessageIDs, err = a.prepMail(ctx, messages, others, ev.Summary, nameIndex(out.Attendees)); err != nil {
		return MeetingPrepDTO{}, err
	}
	if out.Tasks, err = a.prepTasks(ctx, others, threadMessageIDs, ev.UID); err != nil {
		return MeetingPrepDTO{}, err
	}
	if out.Previous, err = a.prepPrevious(ctx, ev, start, others); err != nil {
		return MeetingPrepDTO{}, err
	}
	return out, nil
}

// parseEventTimeText reads an event time as the editor DTO writes it: a date
// for an all-day event, else rfc3339.
func parseEventTimeText(s string, allDay bool) (time.Time, error) {
	if allDay {
		return time.ParseInLocation(dateLayout, s, time.Local)
	}
	return time.Parse(time.RFC3339, s)
}

// prepAttendees lists the organizer and the invited, once each, with their
// address book cards.
func (a *App) prepAttendees(ctx context.Context, ev EventDTO, self map[string]bool) ([]PrepAttendeeDTO, error) {
	contacts, err := a.store.ListContacts(ctx, 0)
	if err != nil {
		return nil, err
	}
	books, err := a.bookIndex()
	if err != nil {
		return nil, err
	}
	cards := make(map[string]ContactDTO)
	for _, c := range contacts {
		dto := toContactDTO(c, books)
		for _, e := range c.Emails {
			key := strings.ToLower(strings.TrimSpace(e.Value))
			if _, taken := cards[key]; !taken {
				cards[key] = dto
			}
		}
	}

	var out []PrepAttendeeDTO
	seen := map[string]bool{}
	add := func(p AttendeeDTO, organizer bool) {
		email := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(p.Email), "mailto:")))
		if email == "" || seen[email] {
			return
		}
		seen[email] = true
		entry := PrepAttendeeDTO{
			Email: email, Name: p.Name, Role: p.Role, PartStat: p.PartStat,
			Organizer: organizer, Self: self[email],
		}
		if card, ok := cards[email]; ok {
			entry.Contact = &card
		}
		out = append(out, entry)
	}
	add(ev.Organizer, true)
	for _, p := range ev.Attendees {
		add(p, false)
	}
	return out, nil
}

// nameIndex maps each invited address to the name to show for it: the
// address book's, the invitation's, or the address.
func nameIndex(attendees []PrepAttendeeDTO) map[string]string {
	out := make(map[string]string, len(attendees))
	for _, p := range attendees {
		name := p.Name
		if p.Contact != nil && p.Contact.FullName != "" {
			name = p.Contact.FullName
		}
		if name == "" {
			name = p.Email
		}
		out[p.Email] = name
	}
	return out
}

// prepMessages reads the recent mail in which the invited send or receive, plus
// mail whose subject contains the title.
func (a *App) prepMessages(ctx context.Context, others []string, title string) ([]storage.Message, error) {
	folders, err := a.browsableFolders(ctx)
	if err != nil {
		return nil, err
	}
	since := time.Now().AddDate(0, 0, -prepMailDays)
	if len(others) > prepMailAttendees {
		others = others[:prepMailAttendees]
	}
	involving, err := a.store.MessagesInvolving(ctx, others, folders, since, prepMailScan)
	if err != nil {
		return nil, err
	}
	bySubject, err := a.store.MessagesWithSubject(ctx, title, folders, since, prepMailScan/4)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(involving)+len(bySubject))
	var all []storage.Message
	for _, m := range slices.Concat(involving, bySubject) {
		if !seen[m.ID] {
			seen[m.ID] = true
			all = append(all, m)
		}
	}
	slices.SortStableFunc(all, func(x, y storage.Message) int {
		if c := y.Date.Compare(x.Date); c != 0 {
			return c
		}
		return int(y.ID - x.ID)
	})
	return all, nil
}

// prepMail ranks the conversations among the messages and lists the files they
// carry. It also returns the Message-IDs of the conversations shown, which the
// tasks are matched against.
func (a *App) prepMail(ctx context.Context, messages []storage.Message, others []string, title string, names map[string]string) ([]PrepThreadDTO, []AttachmentEntryDTO, map[string]bool, error) {
	type group struct {
		thread   meetingprep.Thread
		newest   storage.Message
		messages []storage.Message
	}
	groups := map[string]*group{}
	var order []string
	seenCopies := map[string]bool{}
	for _, m := range messages {
		if m.MessageID != "" {
			copyKey := fmt.Sprintf("%d|%s", m.AccountID, m.MessageID)
			if seenCopies[copyKey] {
				continue
			}
			seenCopies[copyKey] = true
		}
		key := fmt.Sprintf("m%d", m.ID)
		if m.ThreadID != 0 {
			key = fmt.Sprintf("t%d|%d", m.AccountID, m.ThreadID)
		}
		g := groups[key]
		if g == nil {
			g = &group{thread: meetingprep.Thread{Key: key, Subject: m.Subject, Last: m.Date}, newest: m}
			groups[key] = g
			order = append(order, key)
		}
		g.messages = append(g.messages, m)
		g.thread.Messages++
		g.thread.People = append(g.thread.People, storageAddresses(m)...)
	}
	threads := make([]meetingprep.Thread, 0, len(order))
	for _, key := range order {
		threads = append(threads, groups[key].thread)
	}
	ranked := meetingprep.RankThreads(threads, others, title, time.Now(), prepThreads)

	newest := make([]storage.Message, 0, len(ranked))
	for _, r := range ranked {
		newest = append(newest, groups[r.Key].newest)
	}
	summaries, err := a.buildSummaries(ctx, newest)
	if err != nil {
		return nil, nil, nil, err
	}
	out := make([]PrepThreadDTO, 0, len(ranked))
	messageIDs := map[string]bool{}
	for i, r := range ranked {
		g := groups[r.Key]
		people := make([]string, 0, len(r.Involved))
		for _, addr := range r.Involved {
			people = append(people, names[strings.ToLower(addr)])
		}
		out = append(out, PrepThreadDTO{
			ThreadID: g.newest.ThreadID, Subject: g.newest.Subject, Last: formatDate(g.newest.Date),
			Messages: g.thread.Messages, People: people, TitleMatch: r.TitleMatch, Message: summaries[i],
		})
		for _, m := range g.messages {
			if id := strings.Trim(strings.TrimSpace(m.MessageID), "<>"); id != "" {
				messageIDs[id] = true
			}
		}
	}

	files, err := a.prepFiles(ctx, messages, others)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, files, messageIDs, nil
}

// storageAddresses is the sender and recipients of a message, bare and
// lowercase.
func storageAddresses(m storage.Message) []string {
	out := []string{strings.ToLower(strings.TrimSpace(bareAddress(m.FromAddress)))}
	out = append(out, storage.AddressList(m.ToAddresses)...)
	return append(out, storage.AddressList(m.CcAddresses)...)
}

// prepFiles lists the attachments of the recent mail with the invited, newest
// first, each file once: the same name and size sent again is one entry.
func (a *App) prepFiles(ctx context.Context, messages []storage.Message, others []string) ([]AttachmentEntryDTO, error) {
	if len(others) == 0 {
		return []AttachmentEntryDTO{}, nil
	}
	want := make(map[string]bool, len(others))
	for _, o := range others {
		want[strings.ToLower(o)] = true
	}
	since := time.Now().AddDate(0, 0, -prepFileDays)
	var ids []int64
	for _, m := range messages {
		if !m.HasAttachments || m.Date.Before(since) || len(ids) >= prepFileMessages {
			continue
		}
		if slices.ContainsFunc(storageAddresses(m), func(addr string) bool { return want[addr] }) {
			ids = append(ids, m.ID)
		}
	}
	entries, err := a.store.AttachmentsOfMessages(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := []AttachmentEntryDTO{}
	seen := map[string]bool{}
	for _, e := range entries {
		key := fmt.Sprintf("%s|%d", strings.ToLower(e.Filename), e.SizeBytes)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a.attachmentEntryDTO(e))
		if len(out) == prepFiles {
			break
		}
	}
	return out, nil
}

// prepTasks lists the open tasks that name one of the invited, link to one of
// the conversations shown, or relate to this event, the soonest due first.
func (a *App) prepTasks(ctx context.Context, others []string, messageIDs map[string]bool, uid string) ([]TaskDTO, error) {
	rows, err := a.store.QueryTasks(ctx, storage.TaskQuery{OpenOnly: true})
	if err != nil {
		return nil, err
	}
	cals, err := a.calendarIndex(ctx)
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(others))
	for _, o := range others {
		want[strings.ToLower(o)] = true
	}
	var out []TaskDTO
	for _, row := range rows {
		obj, err := ical.ParseTodoObject([]byte(row.Data), time.Local)
		if err != nil {
			continue
		}
		dto := taskDTO(row, cals[row.CalendarID], obj)
		if dto.Done || !taskTouches(dto, want, messageIDs, uid) {
			continue
		}
		out = append(out, dto)
	}
	slices.SortStableFunc(out, func(x, y TaskDTO) int {
		switch {
		case x.Due == y.Due:
			return 0
		case x.Due == "":
			return 1
		case y.Due == "":
			return -1
		}
		return strings.Compare(x.Due, y.Due)
	})
	if len(out) > prepTasks {
		out = out[:prepTasks]
	}
	return out, nil
}

// taskTouches says whether a task is about one of the people, one of the mails,
// or the event.
func taskTouches(t TaskDTO, people, messageIDs map[string]bool, uid string) bool {
	for _, c := range t.Contacts {
		if people[strings.ToLower(strings.TrimSpace(c.Email))] {
			return true
		}
	}
	for _, link := range t.MailLinks {
		if messageIDs[midHeader(link)] {
			return true
		}
	}
	return uid != "" && slices.Contains(t.EventUIDs, uid)
}

// prepPrevious finds earlier occurrences of the same series and earlier events
// with the same people, from the stored events of the past year.
func (a *App) prepPrevious(ctx context.Context, ev EventDTO, start time.Time, others []string) ([]PrepMeetingDTO, error) {
	from := start.AddDate(0, 0, -prepEventDays)
	rows, err := a.store.QueryCalendarEvents(ctx, storage.EventQuery{From: from, To: start, VisibleOnly: true})
	if err != nil {
		return nil, err
	}
	if len(rows) > prepEventRows {
		rows = rows[:prepEventRows]
	}
	var candidates []meetingprep.Meeting
	type shown struct {
		end    time.Time
		allDay bool
	}
	detail := map[string]shown{}
	for _, row := range rows {
		obj, err := parseStoredEvent(row)
		if err != nil {
			continue
		}
		for _, occ := range obj.Expand(from, start) {
			if occ.Event.Status == ical.StatusCancelled {
				continue
			}
			rid := formatRecurrenceID(occ.RecurrenceID)
			if row.ID == ev.ID && rid == ev.RecurrenceID {
				continue
			}
			people := make([]string, 0, len(occ.Event.Attendees)+1)
			if occ.Event.Organizer != nil {
				people = append(people, occ.Event.Organizer.Email)
			}
			for _, p := range occ.Event.Attendees {
				people = append(people, p.Email)
			}
			for i, p := range people {
				people[i] = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(strings.ToLower(p)), "mailto:"))
			}
			candidates = append(candidates, meetingprep.Meeting{
				EventID: row.ID, RecurrenceID: rid, UID: row.UID, Title: occ.Event.Summary,
				Start: occ.Start, People: people,
			})
			detail[fmt.Sprintf("%d|%s", row.ID, rid)] = shown{end: occ.End, allDay: occ.AllDay}
		}
	}
	ranked := meetingprep.RankMeetings(candidates, ev.UID, others, start, prepPrevious)
	out := make([]PrepMeetingDTO, 0, len(ranked))
	for _, r := range ranked {
		d := detail[fmt.Sprintf("%d|%s", r.EventID, r.RecurrenceID)]
		out = append(out, PrepMeetingDTO{
			EventID: r.EventID, RecurrenceID: r.RecurrenceID, Title: r.Title,
			Start: formatEventTime(r.Start, d.allDay), End: formatEventTime(d.end, d.allDay), AllDay: d.allDay,
			SameSeries: r.SameSeries, Shared: append([]string{}, r.Shared...),
		})
	}
	return out, nil
}
