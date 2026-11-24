package contactmerge

import (
	"strings"
	"testing"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
	"github.com/peltonapp/Pelton/internal/storage"
)

// stored builds a stored contact from a vCard body, the way the sync keeps one.
func stored(t *testing.T, id, bookID int64, path, body string) storage.Contact {
	t.Helper()
	parsed, err := pcarddav.ParseCard("BEGIN:VCARD\r\nVERSION:3.0\r\n" + strings.ReplaceAll(body, "\n", "\r\n") + "\r\nEND:VCARD\r\n")
	if err != nil {
		t.Fatalf("parse card: %v", err)
	}
	card, err := pcarddav.EncodeCard(parsed.Card)
	if err != nil {
		t.Fatalf("encode card: %v", err)
	}
	c := storage.Contact{
		ID: id, BookID: bookID, Path: path, ETag: "e" + path, UID: parsed.UID, FullName: parsed.FullName,
		Organization: parsed.Organization, Title: parsed.Title, Note: parsed.Note, Card: card,
	}
	for _, e := range parsed.Emails {
		c.Emails = append(c.Emails, storage.ContactValue{Value: e.Value, Label: e.Label})
	}
	for _, p := range parsed.Phones {
		c.Phones = append(c.Phones, storage.ContactValue{Value: p.Value, Label: p.Label})
	}
	return c
}

func values(list []pcarddav.Labelled) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, v.Value)
	}
	return out
}

func joined(list []string) string { return strings.Join(list, ",") }

func TestCombineUnionsAndDedupesValues(t *testing.T) {
	survivor := stored(t, 1, 1, "/a.vcf", "UID:u1\nFN:Ada Lovelace\nEMAIL;TYPE=work:ada@work.example\nTEL:+31 6 1234 5678")
	other := stored(t, 2, 1, "/b.vcf", "UID:u2\nFN:Ada L\nEMAIL;TYPE=home:ADA@work.example\nEMAIL;TYPE=home:ada@home.example\nTEL:06 1234 5678\nTEL:+44 20 7946 0958")
	got, err := Combine(survivor, []storage.Contact{other}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e := joined(values(got.Emails)); e != "ada@work.example,ada@home.example" {
		t.Errorf("emails = %s", e)
	}
	if got.Emails[0].Label != "work" {
		t.Errorf("the survivor's label was replaced: %q", got.Emails[0].Label)
	}
	if p := joined(values(got.Phones)); p != "+31 6 1234 5678,+44 20 7946 0958" {
		t.Errorf("phones = %s", p)
	}
	if got.Path != "/a.vcf" || got.UID != "u1" || got.ETag != "e/a.vcf" {
		t.Errorf("identity not the survivor's: %+v", got)
	}
}

func TestCombineUpgradesANationalNumberToTheInternationalForm(t *testing.T) {
	survivor := stored(t, 1, 1, "/a.vcf", "UID:u1\nFN:Ada\nTEL:06 1234 5678")
	other := stored(t, 2, 1, "/b.vcf", "UID:u2\nFN:Ada\nTEL:+31 6 1234 5678")
	got, _ := Combine(survivor, []storage.Contact{other}, nil)
	if p := joined(values(got.Phones)); p != "+31 6 1234 5678" {
		t.Errorf("phones = %s", p)
	}
}

func TestCombineFollowsChoices(t *testing.T) {
	survivor := stored(t, 1, 1, "/a.vcf", "UID:u1\nFN:Ada Lovelace\nN:Lovelace;Ada;;;\nORG:Analytical\nTITLE:Countess\nNOTE:first")
	other := stored(t, 2, 1, "/b.vcf", "UID:u2\nFN:Augusta Ada King\nN:King;Augusta;;;\nORG:Engine Co\nNOTE:second")

	tests := []struct {
		name    string
		choices Choices
		check   func(t *testing.T, c pcarddav.Contact)
	}{
		{"default keeps the survivor's", nil, func(t *testing.T, c pcarddav.Contact) {
			if c.FullName != "Ada Lovelace" || c.Organization != "Analytical" || c.Note != "first" {
				t.Errorf("got %+v", c)
			}
		}},
		{"a field can come from the other", Choices{FieldName: 2, FieldOrganization: 2}, func(t *testing.T, c pcarddav.Contact) {
			if c.FullName != "Augusta Ada King" || c.Organization != "Engine Co" || c.Note != "first" {
				t.Errorf("got %+v", c)
			}
			if n := c.Card.Name(); n == nil || n.FamilyName != "King" {
				t.Errorf("N did not follow the name: %+v", n)
			}
		}},
		{"a field the chosen contact lacks falls back", Choices{FieldTitle: 2}, func(t *testing.T, c pcarddav.Contact) {
			if c.Title != "Countess" {
				t.Errorf("title = %q", c.Title)
			}
		}},
		{"both notes are kept", Choices{FieldNote: Both}, func(t *testing.T, c pcarddav.Contact) {
			if c.Note != "first\n\nsecond" {
				t.Errorf("note = %q", c.Note)
			}
		}},
		{"both is ignored for a name", Choices{FieldName: Both}, func(t *testing.T, c pcarddav.Contact) {
			if c.FullName != "Ada Lovelace" {
				t.Errorf("name = %q", c.FullName)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Combine(survivor, []storage.Contact{other}, tt.choices)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, got)
		})
	}
}

func TestCombineFillsEmptyFieldsFromTheOthers(t *testing.T) {
	survivor := stored(t, 1, 1, "/a.vcf", "UID:u1\nFN:Ada")
	other := stored(t, 2, 1, "/b.vcf", "UID:u2\nFN:Ada\nORG:Engine Co\nTITLE:Engineer")
	got, _ := Combine(survivor, []storage.Contact{other}, nil)
	if got.Organization != "Engine Co" || got.Title != "Engineer" {
		t.Errorf("got %+v", got)
	}
}

func TestCombineKeepsUnknownProperties(t *testing.T) {
	survivor := stored(t, 1, 1, "/a.vcf", "UID:u1\nFN:Ada\nBDAY:1815-12-10\nURL:https://ada.example\nX-SURVIVOR:keep")
	other := stored(t, 2, 1, "/b.vcf", strings.Join([]string{
		"UID:u2", "FN:Ada",
		"BDAY:1900-01-01",
		"URL:https://ada.example",
		"URL:https://notes.example",
		"NICKNAME:Countess",
		"X-SURVIVOR:conflict",
		"X-OTHER:carry",
		"ADR;TYPE=home:;;1 Analytical St;London;;;UK",
	}, "\n"))
	got, err := Combine(survivor, []storage.Contact{other}, nil)
	if err != nil {
		t.Fatal(err)
	}
	card := got.Card
	if v := card.Value("BDAY"); v != "1815-12-10" {
		t.Errorf("BDAY = %q, the survivor's must win a conflict", v)
	}
	if urls := card.Values("URL"); len(urls) != 2 || urls[0] != "https://ada.example" || urls[1] != "https://notes.example" {
		t.Errorf("URL = %v, want the survivor's then the new one", urls)
	}
	if v := card.Value("NICKNAME"); v != "Countess" {
		t.Errorf("NICKNAME = %q, a property only the merged card had should carry over", v)
	}
	if v := card.Value("X-SURVIVOR"); v != "keep" {
		t.Errorf("X-SURVIVOR = %q", v)
	}
	if v := card.Value("X-OTHER"); v != "carry" {
		t.Errorf("X-OTHER = %q", v)
	}
	if card.Get("ADR") == nil {
		t.Error("ADR did not carry over")
	}
	// the written card still round-trips.
	if _, err := pcarddav.EncodeCard(got.ToCard()); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func TestCombineSurvivesAnUnreadableSurvivorCard(t *testing.T) {
	survivor := storage.Contact{ID: 1, BookID: 1, Path: "/a.vcf", UID: "u1", FullName: "Ada", Card: "not a card"}
	other := stored(t, 2, 1, "/b.vcf", "UID:u2\nFN:Ada\nEMAIL:ada@x.example")
	got, err := Combine(survivor, []storage.Contact{other}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Card == nil || joined(values(got.Emails)) != "ada@x.example" {
		t.Errorf("got %+v", got)
	}
}

func TestCombineNeedsSomethingToMerge(t *testing.T) {
	if _, err := Combine(storage.Contact{FullName: "Ada"}, nil, nil); err == nil {
		t.Error("merging nothing should fail")
	}
}
