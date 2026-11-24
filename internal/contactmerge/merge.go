package contactmerge

import (
	"fmt"
	"strings"

	"github.com/emersion/go-vcard"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Field names a single-valued field of a contact that a merge picks between.
type Field string

// The single-valued fields. Addresses and numbers are not here: they are
// always combined.
const (
	FieldName         Field = "name"
	FieldOrganization Field = "organization"
	FieldTitle        Field = "title"
	FieldNote         Field = "note"
)

// Both is the choice that keeps the values of every contact. It is only
// honoured for the note, which is joined with a blank line between; the other
// fields hold one value.
const Both int64 = -1

// Choices says which contact each single-valued field comes from, by contact
// id, or Both. A field with no choice, or a choice naming a contact with
// nothing in that field, falls back to the survivor and then to the first
// merged contact that has a value.
type Choices map[Field]int64

// knownProperties are the vCard properties Combine handles itself rather than
// copying across.
var knownProperties = map[string]bool{
	"VERSION": true, "UID": true, "FN": true, "N": true, "EMAIL": true, "TEL": true,
	"ORG": true, "TITLE": true, "NOTE": true, "PRODID": true, "REV": true,
}

// repeatable are the properties a card may hold more than once. A merged
// card's copy is added next to the survivor's unless it is already there; any
// other property the survivor has is a conflict, and the survivor's wins.
var repeatable = map[string]bool{
	"ADR": true, "URL": true, "IMPP": true, "KEY": true, "CATEGORIES": true,
	"NICKNAME": true, "RELATED": true, "ROLE": true, "SOCIALPROFILE": true,
	"FBURL": true, "CALURI": true, "CALADRURI": true, "LABEL": true, "TZ": true,
	"GEO": true, "LANG": true, "SOURCE": true, "MEMBER": true,
}

// Combine builds the card that results from merging others into survivor. The
// survivor's path, etag and UID are kept so the write replaces its card in
// place. Addresses and numbers are the union, deduplicated; the single-valued
// fields follow choices; every other property of the survivor is kept, and
// those of the merged cards are carried over where the survivor has none of
// that property, or where the property may repeat and the value is new.
func Combine(survivor storage.Contact, others []storage.Contact, choices Choices) (pcarddav.Contact, error) {
	if len(others) == 0 {
		return pcarddav.Contact{}, fmt.Errorf("contactmerge: nothing to merge into %q", survivor.FullName)
	}
	out := pcarddav.Contact{}
	if parsed, err := pcarddav.ParseCard(survivor.Card); err == nil {
		out = parsed
	}
	out.Path = survivor.Path
	out.ETag = survivor.ETag
	out.UID = survivor.UID
	if out.Card == nil {
		out.Card = vcard.Card{}
	}
	all := append([]storage.Contact{survivor}, others...)

	out.FullName = pick(FieldName, all, choices, func(c storage.Contact) string { return c.FullName })
	out.Organization = pick(FieldOrganization, all, choices, func(c storage.Contact) string { return c.Organization })
	out.Title = pick(FieldTitle, all, choices, func(c storage.Contact) string { return c.Title })
	out.Note = pickNote(all, choices)

	out.Emails, out.Phones = nil, nil
	for _, c := range all {
		for _, e := range c.Emails {
			out.Emails = addEmail(out.Emails, e)
		}
		for _, p := range c.Phones {
			out.Phones = addPhone(out.Phones, p)
		}
	}

	if from := chosen(FieldName, all, choices, func(c storage.Contact) string { return c.FullName }); from != nil && from.ID != survivor.ID {
		takeStructuredName(out.Card, *from)
	}
	for _, c := range others {
		carryProperties(out.Card, c)
	}
	return out, nil
}

// chosen returns the contact a single-valued field is taken from, or nil.
func chosen(field Field, all []storage.Contact, choices Choices, get func(storage.Contact) string) *storage.Contact {
	if id, ok := choices[field]; ok && id != Both {
		for i := range all {
			if all[i].ID == id && strings.TrimSpace(get(all[i])) != "" {
				return &all[i]
			}
		}
	}
	for i := range all {
		if strings.TrimSpace(get(all[i])) != "" {
			return &all[i]
		}
	}
	return nil
}

func pick(field Field, all []storage.Contact, choices Choices, get func(storage.Contact) string) string {
	if c := chosen(field, all, choices, get); c != nil {
		return strings.TrimSpace(get(*c))
	}
	return ""
}

// pickNote joins every note, without repeating one that two cards share, when
// the choice is Both, and otherwise picks like any other field.
func pickNote(all []storage.Contact, choices Choices) string {
	get := func(c storage.Contact) string { return c.Note }
	if choices[FieldNote] != Both {
		return pick(FieldNote, all, choices, get)
	}
	var notes []string
	seen := map[string]bool{}
	for _, c := range all {
		note := strings.TrimSpace(c.Note)
		if note != "" && !seen[note] {
			seen[note] = true
			notes = append(notes, note)
		}
	}
	return strings.Join(notes, "\n\n")
}

func addEmail(list []pcarddav.Labelled, e storage.ContactValue) []pcarddav.Labelled {
	value := strings.TrimSpace(e.Value)
	if value == "" {
		return list
	}
	for i, have := range list {
		if normalizeEmail(have.Value) == normalizeEmail(value) {
			if list[i].Label == "" {
				list[i].Label = e.Label
			}
			return list
		}
	}
	return append(list, pcarddav.Labelled{Value: value, Label: e.Label})
}

func addPhone(list []pcarddav.Labelled, p storage.ContactValue) []pcarddav.Labelled {
	value := strings.TrimSpace(p.Value)
	if value == "" {
		return list
	}
	for i, have := range list {
		same := PhonesMatch(have.Value, value) || (PhoneKey(have.Value) == "" && strings.EqualFold(have.Value, value))
		if !same {
			continue
		}
		if list[i].Label == "" {
			list[i].Label = p.Label
		}
		// the number with its country code says more than the one without.
		if strings.HasPrefix(value, "+") && !strings.HasPrefix(have.Value, "+") {
			list[i].Value = value
		}
		return list
	}
	return append(list, pcarddav.Labelled{Value: value, Label: p.Label})
}

// takeStructuredName makes the card's N follow a name taken from another
// contact: that card's N when it has one, and none otherwise, which the
// writer rebuilds from the name.
func takeStructuredName(card vcard.Card, from storage.Contact) {
	delete(card, vcard.FieldName)
	parsed, err := pcarddav.ParseCard(from.Card)
	if err != nil || parsed.Card == nil {
		return
	}
	if n := parsed.Card[vcard.FieldName]; len(n) > 0 {
		card[vcard.FieldName] = copyFields(n)
	}
}

// carryProperties copies the properties of a merged contact's card that
// Pelton keeps no field for onto the survivor's card.
func carryProperties(card vcard.Card, from storage.Contact) {
	parsed, err := pcarddav.ParseCard(from.Card)
	if err != nil || parsed.Card == nil {
		return
	}
	for name, fields := range parsed.Card {
		upper := strings.ToUpper(name)
		if knownProperties[upper] {
			continue
		}
		existing := card[name]
		if len(existing) > 0 && !repeatable[upper] {
			continue
		}
		for _, f := range fields {
			if f == nil || strings.TrimSpace(f.Value) == "" || hasField(existing, f) {
				continue
			}
			card[name] = append(card[name], copyField(f))
			existing = card[name]
		}
	}
}

func hasField(list []*vcard.Field, f *vcard.Field) bool {
	for _, e := range list {
		if strings.EqualFold(strings.TrimSpace(e.Value), strings.TrimSpace(f.Value)) {
			return true
		}
	}
	return false
}

func copyField(f *vcard.Field) *vcard.Field {
	out := &vcard.Field{Value: f.Value, Group: f.Group}
	if f.Params != nil {
		out.Params = make(vcard.Params, len(f.Params))
		for k, v := range f.Params {
			out.Params[k] = append([]string(nil), v...)
		}
	}
	return out
}

func copyFields(list []*vcard.Field) []*vcard.Field {
	out := make([]*vcard.Field, 0, len(list))
	for _, f := range list {
		out = append(out, copyField(f))
	}
	return out
}
