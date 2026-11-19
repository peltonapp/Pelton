package ical

import (
	"strings"
	"testing"
	"time"
)

const outlookInvite = "BEGIN:VCALENDAR\r\n" +
	"METHOD:REQUEST\r\n" +
	"PRODID:Microsoft Exchange Server 2010\r\n" +
	"VERSION:2.0\r\n" +
	"BEGIN:VTIMEZONE\r\n" +
	"TZID:W. Europe Standard Time\r\n" +
	"BEGIN:STANDARD\r\nDTSTART:16010101T030000\r\nTZOFFSETFROM:+0200\r\nTZOFFSETTO:+0100\r\n" +
	"RRULE:FREQ=YEARLY;INTERVAL=1;BYDAY=-1SU;BYMONTH=10\r\nEND:STANDARD\r\n" +
	"BEGIN:DAYLIGHT\r\nDTSTART:16010101T020000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0200\r\n" +
	"RRULE:FREQ=YEARLY;INTERVAL=1;BYDAY=-1SU;BYMONTH=3\r\nEND:DAYLIGHT\r\n" +
	"END:VTIMEZONE\r\n" +
	"BEGIN:VEVENT\r\n" +
	"ORGANIZER;CN=Organizer:mailto:org@example.com\r\n" +
	"ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE;CN=Me:mailto:me@example.com\r\n" +
	"ATTENDEE;ROLE=OPT-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE;CN=Other:mailto:other@example.com\r\n" +
	"SUMMARY;LANGUAGE=en-US:Quarterly review\r\n" +
	"DTSTART;TZID=W. Europe Standard Time:20260715T140000\r\n" +
	"DTEND;TZID=W. Europe Standard Time:20260715T150000\r\n" +
	"UID:040000008200E00074C5B7101A82E008\r\n" +
	"SEQUENCE:2\r\n" +
	"DTSTAMP:20260701T100000Z\r\n" +
	"LOCATION;LANGUAGE=en-US:Teams\r\n" +
	"X-MICROSOFT-CDO-BUSYSTATUS:TENTATIVE\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseOutlookInvite(t *testing.T) {
	inv, err := ParseInvite([]byte(outlookInvite), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Method != MethodRequest {
		t.Errorf("method = %q", inv.Method)
	}
	ev := inv.Object.Master
	if ev.Summary != "Quarterly review" || ev.Sequence != 2 {
		t.Errorf("event = %q seq %d", ev.Summary, ev.Sequence)
	}
	if got := inv.Object.Zones.Instant(ev.Start); !got.Equal(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("start = %v, want 12:00 UTC", got)
	}
	if a := ev.Attendee("ME@example.com"); a == nil || a.PartStat != PartStatNeedsAction {
		t.Errorf("attendee = %+v", a)
	}
}

func TestReplyCarriesOnlyTheAnsweringAttendee(t *testing.T) {
	inv, err := ParseInvite([]byte(outlookInvite), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	reply := string(inv.Object.Reply("me@example.com", "", PartStatAccepted).Encode())
	unfolded := unfoldString(reply)
	for _, want := range []string{
		"METHOD:REPLY",
		"UID:040000008200E00074C5B7101A82E008",
		"SEQUENCE:2",
		"ORGANIZER;CN=Organizer:mailto:org@example.com",
		"PARTSTAT=ACCEPTED",
		"CN=Me",
		"BEGIN:VTIMEZONE",
	} {
		if !strings.Contains(unfolded, want) {
			t.Errorf("reply lacks %q:\n%s", want, reply)
		}
	}
	if strings.Contains(reply, "other@example.com") {
		t.Error("reply named another attendee")
	}
}

func TestSetPartStatAddsMissingAttendee(t *testing.T) {
	inv, _ := ParseInvite([]byte(outlookInvite), time.UTC)
	inv.Object.SetPartStat("alias@example.com", "Me", PartStatTentative)
	a := inv.Object.Master.Attendee("alias@example.com")
	if a == nil || a.PartStat != PartStatTentative {
		t.Errorf("attendee = %+v", a)
	}
	if !strings.Contains(string(inv.Object.Encode()), "X-MICROSOFT-CDO-BUSYSTATUS:TENTATIVE") {
		t.Error("lost an unknown property")
	}
}
