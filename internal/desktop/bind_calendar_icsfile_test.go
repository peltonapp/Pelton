package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

const testJSCal = `{"@type":"Group","entries":[
{"@type":"Event","uid":"js-1","title":"Standup","start":"2026-05-04T09:15:00","timeZone":"Europe/Berlin","duration":"PT15M","recurrenceRules":[{"@type":"RecurrenceRule","frequency":"weekly","byDay":[{"@type":"NDay","day":"mo"},{"@type":"NDay","day":"tu"}]}],"alerts":{"1":{"@type":"Alert","trigger":{"@type":"OffsetTrigger","offset":"-PT10M"}}}},
{"@type":"Task","uid":"js-2","title":"A task"}]}`

const testICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:ics-1\r\nDTSTART:20260504T090000Z\r\nDTEND:20260504T100000Z\r\nSUMMARY:Planning\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestReadICSFilesByContent(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := &App{}
	files := a.ReadICSFiles([]string{
		write("a.ics", testICS),
		write("events.json", testJSCal),
		write("notes.json", `{"title":"not a calendar"}`),
		write("plain.txt", testICS),
	})
	if files[0].Error != "" || files[0].Events != 1 || !reflect.DeepEqual(files[0].Summaries, []string{"Planning"}) {
		t.Errorf("ics: %+v", files[0])
	}
	if files[1].Error != "" || files[1].Events != 1 || files[1].Summaries[0] != "Standup" {
		t.Errorf("jscal: %+v", files[1])
	}
	if files[2].Error == "" {
		t.Errorf("a json file that is not JSCalendar was accepted")
	}
	if files[3].Error != "" || files[3].Events != 1 {
		t.Errorf("iCalendar content under another name: %+v", files[3])
	}
}

func TestImportJSCalendarData(t *testing.T) {
	a := newCalendarTestApp(t)
	cal := localCalendar(t, a)
	res, err := a.ImportICSData(cal, testJSCal)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Skipped != 0 || res.Tasks != 1 {
		t.Fatalf("result %+v", res)
	}
	tasks, err := a.ListTasks()
	if err != nil || len(tasks) != 1 || tasks[0].Title != "A task" || tasks[0].ListID == cal {
		t.Fatalf("imported task went to %+v, %v", tasks, err)
	}
	occs := occurrences(t, a, "2026-05-04T00:00:00Z", "2026-05-06T00:00:00Z", cal)
	if got := summaries(occs); !reflect.DeepEqual(got, []string{"Standup", "Standup"}) {
		t.Errorf("occurrences %v", got)
	}
}

func TestDecodeJSCalendarDrafts(t *testing.T) {
	a := newCalendarTestApp(t)
	res, err := a.DecodeJSCalendar(testJSCal)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 1 || res.Skipped != 1 {
		t.Fatalf("result %+v", res)
	}
	ev := res.Events[0]
	if ev.Summary != "Standup" || ev.RRule != "FREQ=WEEKLY;BYDAY=MO,TU" || len(ev.Reminders) != 1 || ev.Reminders[0].MinutesBefore != 10 {
		t.Errorf("draft %+v", ev)
	}
	if _, err := a.DecodeJSCalendar(`{"title":"x"}`); err == nil {
		t.Error("plain json decoded")
	}
}

func TestFirstICSArgs(t *testing.T) {
	got := firstICSArgs([]string{"--hidden", "mailto:a@b", "/tmp/x.ics", "file:///home/u/My%20Cal.ICS", "/tmp/y.json"})
	want := []string{"/tmp/x.ics", "/home/u/My Cal.ICS"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestPendingICSCollectedOnce(t *testing.T) {
	a := &App{}
	a.deliverICS([]string{"/tmp/a.ics"})
	a.deliverICS(nil)
	if got := a.ConsumePendingICS(); !reflect.DeepEqual(got, []string{"/tmp/a.ics"}) {
		t.Errorf("first %v", got)
	}
	if got := a.ConsumePendingICS(); len(got) != 0 {
		t.Errorf("second %v", got)
	}
}

func TestMailEventSourceAndLink(t *testing.T) {
	a := newCalendarTestApp(t)
	accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@example.com", DisplayName: "Me", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	folder := storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if folder.ID, err = a.store.CreateFolder(a.ctx, &folder); err != nil {
		t.Fatal(err)
	}
	id, err := a.store.InsertMessageWithAttachments(a.ctx, &storage.Message{
		AccountID: accountID, FolderID: folder.ID, UID: 7, Subject: "Re: Meet up",
		MessageID: "<abc/1@example.com>", FromName: "Ada", FromAddress: "ada@example.com",
		BodyPlain: "Let's meet up on the 3rd December 2026 at 18:00.",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	src, err := a.MailEventSource(id)
	if err != nil {
		t.Fatal(err)
	}
	if src.Subject != "Meet up" || !strings.Contains(src.Text, "3rd December") || src.Link != "mid:abc%2F1@example.com" {
		t.Errorf("source %+v", src)
	}
	ref, err := a.FindMessageByLink(src.Link)
	if err != nil || !ref.Found || ref.ID != id || ref.FolderID != folder.ID {
		t.Errorf("ref %+v err %v", ref, err)
	}
	missing, err := a.FindMessageByLink("mid:gone@example.com")
	if err != nil || missing.Found {
		t.Errorf("missing %+v err %v", missing, err)
	}
}
