package desktop

import (
	"bytes"
	"net/mail"
	"testing"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/smtp"
)

func TestBuildMessageReplyThreadingHeaders(t *testing.T) {
	app, _, accountID := pgpTestApp(t, nil, nil)
	req := ComposeRequest{
		AccountID:  accountID,
		To:         []AddressDTO{{Email: "recipient@example.com"}},
		Subject:    "Re: threading",
		Text:       "reply body",
		InReplyTo:  "parent@example.com",
		References: []string{"root@example.com", "parent@example.com"},
	}
	msg, err := app.buildMessage(req)
	if err != nil {
		t.Fatalf("build message: %v", err)
	}
	raw, err := smtp.BuildRaw(msg, nil, crypto.ModeNone, crypto.Options{})
	if err != nil {
		t.Fatalf("build MIME: %v", err)
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse MIME: %v", err)
	}
	if got := parsed.Header.Get("In-Reply-To"); got != "<parent@example.com>" {
		t.Errorf("In-Reply-To = %q, want <parent@example.com>", got)
	}
	if got := parsed.Header.Get("References"); got != "<root@example.com> <parent@example.com>" {
		t.Errorf("References = %q, want exact bracketed root/parent chain", got)
	}
}
