package desktop

import (
	"regexp"
	"strings"
	"testing"

	pcarddav "github.com/peltonapp/Pelton/internal/carddav"
)

// A new contact is stored after the server takes it, so the contact has to
// carry the card that was sent.
func TestNewContactCarriesItsCard(t *testing.T) {
	a := newAccountTestApp(t)
	contact := a.contactFromRequest(ContactRequest{
		FullName: "Rosa Erdapfel",
		Emails:   []ContactValueDTO{{Value: "rosa@example.org"}},
	}, nil)
	card, err := pcarddav.EncodeCard(contact.Card)
	if err != nil {
		t.Fatalf("encode the new contact's card: %v", err)
	}
	for _, want := range []string{"VERSION:4.0", "FN:Rosa Erdapfel", "rosa@example.org", "UID:" + contact.UID} {
		if !strings.Contains(card, want) {
			t.Errorf("card is missing %q:\n%s", want, card)
		}
	}
}

// Stored and synced contacts keep their UID forever, so the text form must stay
// a 36-char lowercase hyphenated random v4.
func TestNewContactUIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := newContactUID(), newContactUID()
	if a == b {
		t.Fatalf("two UIDs are equal: %s", a)
	}
	for _, uid := range []string{a, b} {
		if !re.MatchString(uid) {
			t.Errorf("uid %q does not match v4 format", uid)
		}
	}
}
