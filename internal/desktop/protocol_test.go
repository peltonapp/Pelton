package desktop

import (
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestProtocolForPicksTheAccountsDriver(t *testing.T) {
	a := &App{}
	if _, ok := a.protocolFor(storage.Account{}).(imapProtocol); !ok {
		t.Fatal("an account did not get the IMAP driver")
	}
	for p, want := range map[string]bool{"imap": true, "IMAP": true, "pop3": false, "": true} {
		if knownProtocol(p) != want {
			t.Errorf("knownProtocol(%q) = %v, want %v", p, !want, want)
		}
	}
}
