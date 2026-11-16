package desktop

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

type prepFixture struct {
	a       *App
	inbox   int64
	account int64
	uid     uint32
}

func newPrepFixture(t *testing.T) *prepFixture {
	t.Helper()
	a := newCalendarTestApp(t)
	acc, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@x.example", DisplayName: "Me"})
	if err != nil {
		t.Fatal(err)
	}
	folder := storage.Folder{AccountID: acc, Name: "INBOX", IMAPPath: "INBOX", Delimiter: "/"}
	inbox, err := a.store.CreateFolder(a.ctx, &folder)
	if err != nil {
		t.Fatal(err)
	}
	return &prepFixture{a: a, inbox: inbox, account: acc}
}

func (f *prepFixture) mail(t *testing.T, messageID, subject, from, to string, date time.Time, attachments ...string) int64 {
	t.Helper()
	f.uid++
	m := &storage.Message{
		AccountID: f.account, FolderID: f.inbox, UID: f.uid, MessageID: "<" + messageID + ">",
		Subject: subject, FromAddress: from, ToAddresses: to, Date: date,
	}
	var atts []storage.IncomingAttachment
	for _, name := range attachments {
		atts = append(atts, storage.IncomingAttachment{Filename: name, Content: bytes.NewReader(make([]byte, 5000))})
	}
	id, err := f.a.store.InsertMessageWithAttachments(f.a.ctx, m, atts)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *prepFixture) event(t *testing.T, summary string, start time.Time, rrule string, people ...string) EventDTO {
	t.Helper()
	req := EventRequest{
		CalendarID: localCalendar(t, f.a), Summary: summary,
		Start: start.Format(time.RFC3339), End: start.Add(time.Hour).Format(time.RFC3339),
		Organizer: AttendeeDTO{Email: "me@x.example"}, RRule: rrule,
	}
	for _, p := range people {
		req.Attendees = append(req.Attendees, AttendeeDTO{Email: p, PartStat: "NEEDS-ACTION"})
	}
	res, err := f.a.CreateEvent(req)
	if err != nil {
		t.Fatal(err)
	}
	return res.Event
}

func threadSubjects(p MeetingPrepDTO) []string {
	var out []string
	for _, th := range p.Threads {
		out = append(out, th.Subject)
	}
	return out
}

func TestPrepareMeetingGathersEverythingLocally(t *testing.T) {
	f := newPrepFixture(t)
	a := f.a
	now := time.Now()
	start := now.Add(26 * time.Hour).Truncate(time.Minute)

	meeting := f.event(t, "Roadmap review", start, "", "ada@x.example", "bob@x.example", "me@x.example")
	meeting, _ = a.GetEvent(meeting.ID, "")

	// the address book knows Ada.
	book := storage.AddressBook{Name: "personal", URL: "https://cloud.example.com", CollectionPath: "/b"}
	if _, err := a.store.CreateAddressBook(a.ctx, &book); err != nil {
		t.Fatal(err)
	}
	card := storage.Contact{BookID: book.ID, Path: "/b/ada.vcf", UID: "ada", FullName: "Ada Lovelace", Organization: "Analytical", Emails: []storage.ContactValue{{Value: "ada@x.example"}}}
	if err := a.store.SaveContact(a.ctx, &card); err != nil {
		t.Fatal(err)
	}

	// mail: a thread with both, one with a matching subject and nobody invited,
	// one with a single attendee, one that is unrelated.
	f.mail(t, "both-1", "Budget", "Ada <ada@x.example>", "me@x.example, bob@x.example", now.AddDate(0, 0, -20), "plan.pdf")
	f.mail(t, "both-2", "Re: Budget", "me@x.example", "ada@x.example", now.AddDate(0, 0, -19), "plan.pdf")
	f.mail(t, "title", "Roadmap review agenda", "carol@y.example", "me@x.example", now.AddDate(0, 0, -30))
	f.mail(t, "single", "Lunch", "bob@x.example", "me@x.example", now.AddDate(0, 0, -2))
	f.mail(t, "unrelated", "Newsletter", "news@y.example", "me@x.example", now.AddDate(0, 0, -1))
	f.mail(t, "ancient", "Ancient", "ada@x.example", "me@x.example", now.AddDate(0, 0, -400))

	// tasks: one for Ada, one linked to the title thread's mail, one for the
	// event, one unrelated, one already done.
	list := localTaskList(t, a)
	mk := func(title string, req TaskRequest) {
		req.ListID, req.Title = list, title
		if _, err := a.CreateTask(req); err != nil {
			t.Fatal(err)
		}
	}
	mk("Send Ada the numbers", TaskRequest{Due: "2026-12-02", Contacts: []TaskContactDTO{{Email: "ADA@x.example"}}})
	mk("Reply about the agenda", TaskRequest{MailLinks: []string{"mid:title"}})
	mk("Book the room", TaskRequest{EventUIDs: []string{meeting.UID}, Due: "2026-11-30"})
	mk("Buy milk", TaskRequest{})
	mk("Old news for Ada", TaskRequest{Status: "COMPLETED", Contacts: []TaskContactDTO{{Email: "ada@x.example"}}})

	// earlier meetings: a previous occurrence is not available for a single
	// event, but an event with the same people is.
	f.event(t, "Budget talk", now.AddDate(0, 0, -10), "", "ada@x.example", "bob@x.example")
	f.event(t, "Dentist", now.AddDate(0, 0, -9), "")
	f.event(t, "Lunch with Ada", now.AddDate(0, 0, -8), "", "ada@x.example")

	prep, err := a.PrepareMeeting(meeting.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	if prep.Title != "Roadmap review" || prep.Join != nil {
		t.Errorf("basics = %q, join %v", prep.Title, prep.Join)
	}

	var self, ada *PrepAttendeeDTO
	for i := range prep.Attendees {
		switch prep.Attendees[i].Email {
		case "me@x.example":
			self = &prep.Attendees[i]
		case "ada@x.example":
			ada = &prep.Attendees[i]
		}
	}
	if len(prep.Attendees) != 3 || self == nil || !self.Self || !self.Organizer {
		t.Fatalf("attendees = %+v", prep.Attendees)
	}
	if ada == nil || ada.Contact == nil || ada.Contact.FullName != "Ada Lovelace" || ada.PartStat != "NEEDS-ACTION" {
		t.Errorf("ada = %+v", ada)
	}

	// both attendees, then the title match, then the single attendee.
	if got, want := strings.Join(threadSubjects(prep), "|"), "Re: Budget|Roadmap review agenda|Lunch"; got != want {
		t.Errorf("threads = %q, want %q", got, want)
	}
	if first := prep.Threads[0]; len(first.People) != 2 || first.Messages != 2 || first.Message.ID == 0 {
		t.Errorf("first thread = %+v", first)
	}
	if !prep.Threads[1].TitleMatch {
		t.Errorf("title thread = %+v", prep.Threads[1])
	}

	if len(prep.Files) != 1 || prep.Files[0].Filename != "plan.pdf" {
		t.Errorf("files = %+v, want plan.pdf once", prep.Files)
	}

	var tasks []string
	for _, task := range prep.Tasks {
		tasks = append(tasks, task.Title)
	}
	if want := "Book the room|Send Ada the numbers|Reply about the agenda"; strings.Join(tasks, "|") != want {
		t.Errorf("tasks = %v, want %q (open ones about the people, the mails or the event, soonest due first)", tasks, want)
	}

	var previous []string
	for _, m := range prep.Previous {
		previous = append(previous, m.Title)
	}
	// the one with both attendees comes before the one with Ada alone, and the
	// dentist, with nobody invited, is left out.
	if want := "Budget talk|Lunch with Ada"; strings.Join(previous, "|") != want {
		t.Errorf("previous = %v, want %q", previous, want)
	}
}

func TestPrepareMeetingFindsEarlierOccurrencesOfASeries(t *testing.T) {
	f := newPrepFixture(t)
	start := time.Now().AddDate(0, 0, 1).Truncate(time.Hour)
	// a weekly series that began three weeks ago; the next occurrence is the
	// one being prepared.
	first := start.AddDate(0, 0, -21)
	series := f.event(t, "Weekly planning", first, "FREQ=WEEKLY;COUNT=6", "ada@x.example")

	occs, err := f.a.ListOccurrences(start.Add(-time.Hour).Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339), nil)
	if err != nil || len(occs) != 1 {
		t.Fatalf("occurrences = %+v, %v", occs, err)
	}
	prep, err := f.a.PrepareMeeting(series.ID, occs[0].RecurrenceID)
	if err != nil {
		t.Fatal(err)
	}
	if !prep.Recurring || len(prep.Previous) != 3 {
		t.Fatalf("previous = %+v", prep.Previous)
	}
	for _, m := range prep.Previous {
		if !m.SameSeries || m.RecurrenceID == prep.RecurrenceID {
			t.Errorf("previous entry = %+v", m)
		}
	}
	if prep.Previous[0].Start <= prep.Previous[1].Start {
		t.Errorf("most recent first expected: %v", []string{prep.Previous[0].Start, prep.Previous[1].Start})
	}
}

func TestPrepareMeetingFindsTheJoinLink(t *testing.T) {
	f := newPrepFixture(t)
	res, err := f.a.CreateEvent(EventRequest{
		CalendarID: localCalendar(t, f.a), Summary: "Call", Description: "Join at https://acme.zoom.us/j/123456789.",
		Start: time.Now().Add(time.Hour).Format(time.RFC3339), End: time.Now().Add(2 * time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	prep, err := f.a.PrepareMeeting(res.Event.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if prep.Join == nil || prep.Join.Provider != "Zoom" || prep.Join.URL != "https://acme.zoom.us/j/123456789" {
		t.Errorf("join = %+v", prep.Join)
	}
	if len(prep.Attendees) != 0 || len(prep.Threads) != 0 {
		t.Errorf("an event with nobody invited should gather no people or mail: %+v", prep)
	}
}

func TestPrepareMeetingUnknownEvent(t *testing.T) {
	f := newPrepFixture(t)
	if _, err := f.a.PrepareMeeting(9999, ""); err == nil {
		t.Error("an unknown event should fail")
	}
}
