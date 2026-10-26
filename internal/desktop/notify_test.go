package desktop

import (
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

// The two halves of the click argument are the whole of click-to-open (#170):
// Windows hands back exactly what was put on the toast, and nothing else checks
// that the writer and the reader still agree.
func TestNotifyArgsRoundTrip(t *testing.T) {
	for _, id := range []int64{1, 42, 9007199254740993} {
		got, ok := notifyMessageID(notifyArgs(id))
		if !ok {
			t.Errorf("notifyMessageID(%q) refused its own encoding", notifyArgs(id))
			continue
		}
		if got != id {
			t.Errorf("round trip of %d = %d", id, got)
		}
	}
}

// A click Pelton cannot place must be reported as such rather than as message
// zero: the caller opens the window and stops, instead of looking up a message
// that does not exist.
func TestNotifyMessageIDRejectsAnythingElse(t *testing.T) {
	for _, args := range []string{
		"",
		"message:",
		"message:nope",
		"message:0",
		"message:-3",
		"12",
		"compose:12",
		" message:12",
	} {
		if id, ok := notifyMessageID(args); ok {
			t.Errorf("notifyMessageID(%q) = %d, true; want it refused", args, id)
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
