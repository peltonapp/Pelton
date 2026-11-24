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
}
