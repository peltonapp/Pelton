package desktop

import (
	"errors"
	"testing"
)

func TestDiscoverConfigWaitsForConsent(t *testing.T) {
	a := newChatTestApp(t)

	prefs, err := a.GetUIPrefs()
	if err != nil {
		t.Fatal(err)
	}
	if prefs.AutoconfigLookup != "ask" {
		t.Fatalf("lookup default = %q, want ask", prefs.AutoconfigLookup)
	}

	for _, value := range []string{"", "ask", "manual", "yes"} {
		if value != "" {
			if err := a.SetSetting(settingAutoconfigLookup, value); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := a.DiscoverConfig("someone@example.invalid"); !errors.Is(err, errLookupNotAllowed) {
			t.Fatalf("with %q: err = %v, want errLookupNotAllowed", value, err)
		}
	}
}
