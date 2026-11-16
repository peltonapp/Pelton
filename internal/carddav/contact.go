package carddav

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav/carddav"
)

// Contact is one address book entry, reduced to the fields Pelton shows and
// edits. Card is the vCard it came from, kept whole: a contact written by
// another client can carry anything from a birthday to a PGP key url, and
// editing a phone number here must not be what deletes it. Every write starts
// from Card and changes only the fields below.
type Contact struct {
	// Path is where the contact lives on the server, and ETag is the version
	// the local copy came from. Both are empty for a contact not yet written.
	Path string
	ETag string
	// UID identifies the contact independently of its path, which is what lets
	// the same person be recognised after a move.
	UID          string
	FullName     string
	Organization string
	Title        string
	Note         string
	Emails       []Labelled
	Phones       []Labelled
	// MatrixID is the person's Matrix user id, kept as a vCard IMPP entry
	// with the matrix: scheme.
	MatrixID string
	Card     vcard.Card
}

// Labelled is one value with the label its type carried ("home", "work"), or
// empty when the card said nothing.
type Labelled struct {
	Value string
	Label string
}

// PrimaryEmail returns the address to show for a contact, or empty for one
// with no address at all, which is a valid contact and simply not one that can
// be written to.
func (c Contact) PrimaryEmail() string {
	if len(c.Emails) == 0 {
		return ""
	}
	return c.Emails[0].Value
}

// fromAddressObject turns a fetched object into a Contact.
func fromAddressObject(obj carddav.AddressObject) (Contact, error) {
	if obj.Card == nil {
		return Contact{}, errors.New("carddav: address object has no card")
	}
	contact := FromCard(obj.Card)
	contact.Path = obj.Path
	contact.ETag = strings.Trim(obj.ETag, `"`)
	return contact, nil
}

// FromCard reads the fields Pelton knows out of a vCard, keeping the card
// itself for the round trip.
func FromCard(card vcard.Card) Contact {
	contact := Contact{
		UID:          card.Value(vcard.FieldUID),
		FullName:     card.PreferredValue(vcard.FieldFormattedName),
		Organization: card.PreferredValue(vcard.FieldOrganization),
		Title:        card.PreferredValue(vcard.FieldTitle),
		Note:         card.PreferredValue(vcard.FieldNote),
		Emails:       labelled(card[vcard.FieldEmail]),
		Phones:       labelled(card[vcard.FieldTelephone]),
		MatrixID:     MatrixIDOf(card),
		Card:         card,
	}
	// a card with no FN is legal in vCard 3.0 and unusable in a list, so the
	// structured name or the first address stands in for it.
	if contact.FullName == "" {
		if name := card.Name(); name != nil {
			contact.FullName = strings.TrimSpace(name.GivenName + " " + name.FamilyName)
		}
	}
	if contact.FullName == "" {
		contact.FullName = contact.PrimaryEmail()
	}
	return contact
}

// ToCard writes the contact's fields back into its card, leaving every other
// property exactly as it was. A contact with no card yet gets a new one.
func (c Contact) ToCard() vcard.Card {
	card := c.Card
	if card == nil {
		card = vcard.Card{}
	}
	setValue(card, vcard.FieldFormattedName, c.FullName)
	setValue(card, vcard.FieldOrganization, c.Organization)
	setValue(card, vcard.FieldTitle, c.Title)
	setValue(card, vcard.FieldNote, c.Note)
	setLabelled(card, vcard.FieldEmail, c.Emails)
	setLabelled(card, vcard.FieldTelephone, c.Phones)
	setMatrixID(card, c.MatrixID)
	if c.UID != "" {
		setValue(card, vcard.FieldUID, c.UID)
	}
	// N is required in vCard 3.0 and some servers reject a card without it.
	if card.Name() == nil {
		card.SetName(&vcard.Name{FamilyName: c.FullName})
	}
	vcard.ToV4(card)
	return card
}

// labelled flattens repeated fields into values plus their first type.
func labelled(fields []*vcard.Field) []Labelled {
	out := make([]Labelled, 0, len(fields))
	for _, f := range fields {
		if f == nil || strings.TrimSpace(f.Value) == "" {
			continue
		}
		out = append(out, Labelled{
			Value: strings.TrimSpace(f.Value),
			Label: strings.ToLower(strings.Join(f.Params[vcard.ParamType], ",")),
		})
	}
	return out
}

// setValue replaces a single-valued field, removing it when the value is empty
// so clearing a note in the editor clears it on the server too.
func setValue(card vcard.Card, name, value string) {
	if strings.TrimSpace(value) == "" {
		delete(card, name)
		return
	}
	card.SetValue(name, value)
}

// setLabelled replaces every instance of a repeated field, keeping the labels
// the caller carried.
func setLabelled(card vcard.Card, name string, values []Labelled) {
	fields := make([]*vcard.Field, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v.Value) == "" {
			continue
		}
		field := &vcard.Field{Value: strings.TrimSpace(v.Value)}
		if v.Label != "" {
			field.Params = vcard.Params{vcard.ParamType: strings.Split(v.Label, ",")}
		}
		fields = append(fields, field)
	}
	if len(fields) == 0 {
		delete(card, name)
		return
	}
	card[name] = fields
}

// matrixScheme starts an IMPP value naming a Matrix account.
const matrixScheme = "matrix:"

// MatrixIDOf returns the Matrix user id on a card, as @user:server, or empty.
// It reads IMPP values in the matrix: URI form (matrix:u/user:server) and the
// older matrix:@user:server some clients wrote.
func MatrixIDOf(card vcard.Card) string {
	for _, f := range card[vcard.FieldIMPP] {
		if f == nil {
			continue
		}
		if id := parseMatrixIMPP(f.Value); id != "" {
			return id
		}
	}
	return ""
}

// OnlyMatrixIMPP reports whether every IMPP entry on a card is a Matrix id.
func OnlyMatrixIMPP(card vcard.Card) bool {
	for _, f := range card[vcard.FieldIMPP] {
		if f != nil && parseMatrixIMPP(f.Value) == "" {
			return false
		}
	}
	return true
}

func parseMatrixIMPP(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), matrixScheme) {
		return ""
	}
	rest := value[len(matrixScheme):]
	rest, _, _ = strings.Cut(rest, "?")
	switch {
	case strings.HasPrefix(rest, "u/"):
		rest = "@" + rest[2:]
	case strings.HasPrefix(rest, "@"):
	default:
		return ""
	}
	if !strings.Contains(rest, ":") {
		return ""
	}
	return rest
}

// setMatrixID replaces the card's Matrix IMPP entry, leaving any other
// instant messaging accounts on it alone.
func setMatrixID(card vcard.Card, matrixID string) {
	var kept []*vcard.Field
	for _, f := range card[vcard.FieldIMPP] {
		if f != nil && parseMatrixIMPP(f.Value) == "" {
			kept = append(kept, f)
		}
	}
	if id := strings.TrimSpace(matrixID); strings.HasPrefix(id, "@") && strings.Contains(id, ":") {
		kept = append(kept, &vcard.Field{Value: matrixScheme + "u/" + id[1:]})
	}
	if len(kept) == 0 {
		delete(card, vcard.FieldIMPP)
		return
	}
	card[vcard.FieldIMPP] = kept
}

// ParseCard reads a stored vCard back into a Contact.
func ParseCard(raw string) (Contact, error) {
	card, err := vcard.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		return Contact{}, fmt.Errorf("carddav: parse card: %w", err)
	}
	return FromCard(card), nil
}

// EncodeCard serialises a card for storage or for a write.
func EncodeCard(card vcard.Card) (string, error) {
	return encodeCard(card)
}

func encodeCard(card vcard.Card) (string, error) {
	var out strings.Builder
	if err := vcard.NewEncoder(&out).Encode(card); err != nil {
		return "", fmt.Errorf("carddav: encode card: %w", err)
	}
	return out.String(), nil
}
