package desktop

import (
	"encoding/json"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestCollapseNotificationsSummarisesABurst(t *testing.T) {
	s := notifyStringsFor("en")
	few := []notification{{messageID: 1}, {messageID: 2}, {messageID: 3}}
	if got := collapseNotifications(few, s); len(got) != 3 {
		t.Fatalf("up to the limit each message notifies, got %d", len(got))
	}
	many := append(few, notification{messageID: 4}, notification{messageID: 5})
	got := collapseNotifications(many, s)
	if len(got) != 1 || got[0].body != "5 new messages" || got[0].messageID != 0 {
		t.Fatalf("a burst should become one summary, got %+v", got)
	}
	for lang, l := range notifyLocales {
		if l.summary == "" {
			t.Errorf("%s has no summary text", lang)
		}
	}
}

func notifyTestMessage(name, address, subject string) *storage.Message {
	return &storage.Message{FromName: name, FromAddress: address, Subject: subject}
}

// Click-to-open (#170) rests on reading the id back out of data that has been
// through the OS, and on some platforms through JSON, so every shape a number
// can come back as has to work.
func TestNotifyMessageIDReadsEveryNumberShape(t *testing.T) {
	for _, data := range []map[string]any{
		{notifyDataMessageID: int64(42)},
		{notifyDataMessageID: 42},
		{notifyDataMessageID: float64(42)},
		{notifyDataMessageID: json.Number("42")},
		{notifyDataMessageID: "42"},
	} {
		got, ok := notifyMessageID(data)
		if !ok || got != 42 {
			t.Errorf("notifyMessageID(%v) = %d, %v; want 42, true", data, got, ok)
		}
	}
}

// A click Pelton cannot place must be reported as such rather than as message
// zero: the caller opens the window and stops, instead of looking up a message
// that does not exist.
func TestNotifyMessageIDRejectsAnythingElse(t *testing.T) {
	for _, data := range []map[string]any{
		nil,
		{},
		{notifyDataMessageID: ""},
		{notifyDataMessageID: "nope"},
		{notifyDataMessageID: 0},
		{notifyDataMessageID: -3},
		{notifyDataMessageID: true},
		{"other": 12},
	} {
		if id, ok := notifyMessageID(data); ok {
			t.Errorf("notifyMessageID(%v) = %d, true; want it refused", data, id)
		}
	}
}

// The body is what the user actually reads, and both halves of it come from
// headers a sender controls and can leave empty.
func TestNotifyBodyFallsBackToPlaceholders(t *testing.T) {
	s := notifyStringsFor("en")

	full := notifyBody(notifyTestMessage("Arne Kock", "arne@example.com", "Potato harvest"), s)
	if full != "Arne Kock\nPotato harvest" {
		t.Errorf("body = %q", full)
	}

	noName := notifyBody(notifyTestMessage("", "arne@example.com", "Potato harvest"), s)
	if noName != "arne@example.com\nPotato harvest" {
		t.Errorf("body without a display name = %q, want the address", noName)
	}

	empty := notifyBody(notifyTestMessage("", "", ""), s)
	if empty != s.fromUnknown+"\n"+s.noSubject {
		t.Errorf("body of an empty message = %q, want both placeholders", empty)
	}
}

// An unknown or missing language must not produce a blank notification.
func TestNotifyStringsForFallsBackToEnglish(t *testing.T) {
	for _, lang := range []string{"", "tlh", "de-CH"} {
		if got := notifyStringsFor(lang); got.newMail != notifyLocales["en"].newMail {
			t.Errorf("notifyStringsFor(%q).newMail = %q, want the english fallback", lang, got.newMail)
		}
	}
	if got := notifyStringsFor("de"); got.newMail != notifyLocales["de"].newMail {
		t.Error("a known language did not get its own table")
	}
}
