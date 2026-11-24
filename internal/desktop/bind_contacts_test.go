package desktop

import (
	"regexp"
	"testing"
)

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
