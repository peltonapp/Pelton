package rfc822

import (
	"strings"
	"testing"
)

// An Outlook invitation carries the event inline as an alternative of the
// body, where the body walk drops it; a Google one also attaches invite.ics.
// Both have to be found.
func TestCalendarParts(t *testing.T) {
	raw := strings.ReplaceAll(`From: Org <org@example.com>
To: me@example.com
Subject: Invitation
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="outer"

--outer
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/plain; charset=utf-8

You are invited.
--alt
Content-Type: text/calendar; charset=utf-8; method=REQUEST
Content-Transfer-Encoding: base64

QkVHSU46VkNBTEVOREFSDQpFTkQ6VkNBTEVOREFSDQo=
--alt--
--outer
Content-Type: application/ics; name="invite.ics"
Content-Disposition: attachment; filename="invite.ics"

BEGIN:VCALENDAR
END:VCALENDAR
--outer--
`, "\n", "\r\n")
	parts, err := CalendarParts([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d calendar parts, want 2", len(parts))
	}
	if !strings.HasPrefix(string(parts[0]), "BEGIN:VCALENDAR") || !strings.HasPrefix(string(parts[1]), "BEGIN:VCALENDAR") {
		t.Errorf("parts = %q", parts)
	}

	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.Text, "VCALENDAR") {
		t.Error("the calendar part was read as the text body")
	}
}

// An inline text/calendar part is kept as an invite.ics attachment, so the
// invitation is stored with the message and never read as its body, even when
// it comes before the text part.
func TestParseKeepsInlineCalendar(t *testing.T) {
	raw := strings.ReplaceAll(`From: Org <org@example.com>
To: me@example.com
Subject: Invitation
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/calendar; charset=utf-8; method=REQUEST

BEGIN:VCALENDAR
END:VCALENDAR
--alt
Content-Type: text/plain; charset=utf-8

You are invited.
--alt--
`, "\n", "\r\n")
	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(msg.Text) != "You are invited." {
		t.Errorf("text = %q", msg.Text)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.Filename != "invite.ics" || att.ContentType != "text/calendar" || !strings.HasPrefix(string(att.Content), "BEGIN:VCALENDAR") {
		t.Errorf("attachment = %q %q %q", att.Filename, att.ContentType, att.Content)
	}
}

func TestIsCalendar(t *testing.T) {
	for _, tt := range []struct {
		ct, name string
		want     bool
	}{
		{"text/calendar", "", true},
		{"application/ics", "", true},
		{"application/octet-stream", "Meeting.ICS", true},
		{"text/plain", "notes.txt", false},
	} {
		if got := IsCalendar(tt.ct, tt.name); got != tt.want {
			t.Errorf("IsCalendar(%q, %q) = %v", tt.ct, tt.name, got)
		}
	}
}
