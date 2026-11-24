package promises

import (
	"strings"
	"testing"
	"time"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skip("zone data missing:", err)
	}
	return loc
}

func TestDueDates(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	ny := mustZone(t, "America/New_York")
	wed := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	sun := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	sat := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	thu := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		lang  string
		text  string
		ref   time.Time
		loc   *time.Location
		first time.Weekday
		want  string
	}{
		{"tomorrow in utc", "en", "I'll send it tomorrow.", time.Date(2026, 11, 19, 23, 30, 0, 0, time.UTC), time.UTC, time.Monday, "2026-11-20"},
		{"tomorrow in berlin is a day later", "en", "I'll send it tomorrow.", time.Date(2026, 11, 19, 23, 30, 0, 0, time.UTC), berlin, time.Monday, "2026-11-21"},
		{"tomorrow in new york is a day earlier", "en", "I'll send it tomorrow.", time.Date(2026, 11, 20, 2, 0, 0, 0, time.UTC), ny, time.Monday, "2026-11-20"},
		{"weekday ahead", "en", "I'll send it by Friday.", wed, time.UTC, time.Monday, "2026-11-20"},
		{"same weekday means next week", "en", "I'll send it by Friday.", time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), time.UTC, time.Monday, "2026-11-27"},
		{"next friday from wednesday", "en", "I'll send it next Friday.", wed, time.UTC, time.Monday, "2026-11-27"},
		{"next friday from sunday, monday weeks", "en", "I'll send it next Friday.", sun, time.UTC, time.Monday, "2026-11-27"},
		{"next friday from sunday, sunday weeks", "en", "I'll send it next Friday.", sun, time.UTC, time.Sunday, "2026-12-04"},
		{"end of next week, monday weeks", "en", "We'll ship it by the end of next week.", sun, time.UTC, time.Monday, "2026-11-27"},
		{"end of next week, sunday weeks", "en", "We'll ship it by the end of next week.", sun, time.UTC, time.Sunday, "2026-12-04"},
		{"end of week on saturday rolls on", "en", "I'll finish it by the end of the week.", sat, time.UTC, time.Monday, "2026-11-27"},
		{"end of week, saturday weeks", "en", "I'll finish it by the end of the week.", sat, time.UTC, time.Saturday, "2026-11-27"},
		{"bis monatsende", "de", "Ich werde die Rechnung bis Monatsende bezahlen.", wed, berlin, time.Monday, "2026-11-30"},
		{"end of next month", "en", "I'll pay it by the end of next month.", wed, time.UTC, time.Monday, "2026-12-31"},
		{"in two days", "en", "I'll call you in two days.", wed, time.UTC, time.Monday, "2026-11-20"},
		{"business days skip the weekend", "en", "We will refund it within 3 business days.", thu, time.UTC, time.Monday, "2026-11-24"},
		{"in zwei tagen", "de", "Wir melden uns in zwei Tagen.", wed, berlin, time.Monday, "2026-11-20"},
		{"dotted date without year is the next one", "de", "Sie erhalten die Unterlagen bis 15.03.", wed, berlin, time.Monday, "2027-03-15"},
		{"dotted date with year", "de", "Ich werde den Vertrag bis 30.11.2026 unterschreiben.", wed, berlin, time.Monday, "2026-11-30"},
		{"month first in english", "en", "I will submit it by 11/30.", wed, time.UTC, time.Monday, "2026-11-30"},
		{"a day over twelve decides the order", "en", "I will submit it by 30/11.", wed, time.UTC, time.Monday, "2026-11-30"},
		{"iso date", "en", "We plan to release it on 2026-12-01.", wed, time.UTC, time.Monday, "2026-12-01"},
		{"day and month name", "en", "I'll book the room for the 12th of May.", wed, time.UTC, time.Monday, "2027-05-12"},
		{"german day and month", "de", "Ich werde den Termin für den 12. Mai buchen.", wed, berlin, time.Monday, "2027-05-12"},
		{"time tomorrow in zone", "en", "I'll call you at 3pm tomorrow.", wed, berlin, time.Monday, "2026-11-19T15:00:00+01:00"},
		{"german time", "de", "Ich rufe dich morgen um 15 Uhr an.", wed, berlin, time.Monday, "2026-11-19T15:00:00+01:00"},
		{"time alone later today", "en", "Please reply to the customer before 5pm.", wed, time.UTC, time.Monday, "2026-11-18T17:00:00Z"},
		{"time alone already past", "en", "Please reply to the customer before 5pm.", time.Date(2026, 11, 18, 18, 0, 0, 0, time.UTC), time.UTC, time.Monday, "2026-11-19T17:00:00Z"},
		{"hours from now", "en", "I'll send it in 2 hours.", wed, time.UTC, time.Monday, "2026-11-18T11:00:00Z"},
		{"guten morgen is not a date", "de", "Guten Morgen, ich kümmere mich darum.", wed, berlin, time.Monday, ""},
		{"no date", "en", "I'll take care of the booking.", wed, time.UTC, time.Monday, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := Detect(Input{Text: tt.text, Clock: Clock{Ref: tt.ref, Location: tt.loc, WeekStart: tt.first}, LangHint: tt.lang})
			if len(found) != 1 {
				t.Fatalf("found %d promises in %q", len(found), tt.text)
			}
			if got := found[0].Due.String(); got != tt.want {
				t.Errorf("due %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthorTextCutsQuotesAndSignatures(t *testing.T) {
	tests := []struct {
		name string
		text string
		keep string
		gone []string
	}{
		{
			name: "english reply header and quote",
			text: "Hi Tom,\n\nSounds good.\n\nOn Mon, 16 Nov 2026 at 10:00, Anna <anna@example.com> wrote:\n> I'll send the slides tomorrow.\n",
			keep: "Sounds good.",
			gone: []string{"slides"},
		},
		{
			name: "header broken over two lines",
			text: "Thanks!\n\nOn Mon, 16 Nov 2026 at 10:00, Anna Example\n<anna@example.com> wrote:\nI'll send the slides tomorrow.\n",
			keep: "Thanks!",
			gone: []string{"slides"},
		},
		{
			name: "german reply header",
			text: "Passt.\n\nAm 16.11.2026 um 10:00 schrieb Anna <anna@example.com>:\nIch schicke dir morgen die Folien.\n",
			keep: "Passt.",
			gone: []string{"Folien"},
		},
		{
			name: "outlook block",
			text: "See below.\n\nFrom: Anna Example\nSent: Monday, November 16, 2026 10:00\nTo: Tom\nI'll send the slides tomorrow.\n",
			keep: "See below.",
			gone: []string{"slides"},
		},
		{
			name: "forwarded message",
			text: "FYI\n\n---------- Forwarded message ---------\nI'll send the slides tomorrow.\n",
			keep: "FYI",
			gone: []string{"slides"},
		},
		{
			name: "signature separator",
			text: "I'll call you later.\n-- \nAnna Example\nI'll send you the brochure.\n",
			keep: "I'll call you later.",
			gone: []string{"brochure"},
		},
		{
			name: "sign-off near the end",
			text: "I'll call you later.\n\nBest regards,\nAnna Example\nHead of Sales\n",
			keep: "I'll call you later.",
			gone: []string{"Head of Sales"},
		},
		{
			name: "inline quoted lines are blanked",
			text: "> Can you send the report by Friday?\nYes, I'll send it by Friday.\n> And the invoice?\nThat one is already paid.\n",
			keep: "Yes, I'll send it by Friday.",
			gone: []string{"Can you", "invoice"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := AuthorText(tt.text, "")
			if len(got) > len(tt.text) {
				t.Fatalf("author text is longer than the message: %q", got)
			}
			if !strings.Contains(got, tt.keep) {
				t.Errorf("lost %q: %q", tt.keep, got)
			}
			for _, g := range tt.gone {
				if strings.Contains(got, g) {
					t.Errorf("kept %q: %q", g, got)
				}
			}
		})
	}
}

func TestDetectReadsOnlyTheAuthorsText(t *testing.T) {
	clock := Clock{Ref: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Location: time.UTC, WeekStart: time.Monday}
	text := "Hi Anna,\n\nThanks, that works for me.\n\nOn Mon, 16 Nov 2026, Anna <a@example.com> wrote:\n> Can you confirm by Friday?\n> I'll send the contract tomorrow.\n"
	if got := Detect(Input{Text: text, Clock: clock}); len(got) != 0 {
		t.Fatalf("found %+v in quoted text", got)
	}
	text = "Hi Anna,\n\nThanks for the call. I'll send you the contract by Friday.\nCan you confirm the delivery address?\n\nBest,\nTom"
	got := Detect(Input{Text: text, Clock: clock})
	if len(got) != 2 {
		t.Fatalf("found %d, want 2: %+v", len(got), got)
	}
	if got[0].Kind != Commitment || got[0].Owner != OwnerAuthor || got[0].Due.String() != "2026-11-20" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Kind != Request || got[1].Owner != OwnerRecipient {
		t.Errorf("second: %+v", got[1])
	}
	for _, c := range got {
		if text[c.Start:c.End] != c.Quote {
			t.Errorf("span %d:%d is %q, quote %q", c.Start, c.End, text[c.Start:c.End], c.Quote)
		}
		if strings.Contains(c.Quote, "Hi Anna") || strings.Contains(c.Quote, "Thanks for the call") {
			t.Errorf("quote holds more than its sentence: %q", c.Quote)
		}
	}
}

func TestDetectPicksTheLanguageFromTheText(t *testing.T) {
	clock := Clock{Ref: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Location: time.UTC, WeekStart: time.Monday}
	got := Detect(Input{Text: "Hallo Tom,\n\nich schicke dir die Unterlagen bis Freitag. Danke und bis bald!", Clock: clock, LangHint: "en"})
	if len(got) != 1 || got[0].Lang != "de" || got[0].Due.String() != "2026-11-20" {
		t.Fatalf("got %+v", got)
	}
}

func TestHasCue(t *testing.T) {
	if !HasCue("If you want, I'll send it tomorrow.", "en") {
		t.Error("a conditional promise is still a cue")
	}
	if HasCue("Thanks for the update, see you soon.", "en") {
		t.Error("no cue expected")
	}
}
