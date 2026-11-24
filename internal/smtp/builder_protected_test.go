package smtp

import (
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/crypto"
)

func protectedMessage() *Message {
	msg := baseMessage()
	msg.Subject = "Quarterly numbers"
	msg.Headers = [][2]string{{"Autocrypt", "addr=me@example.com; keydata=\r\n AAAA"}}
	msg.Protected = &ProtectedHeaders{HideSubject: true, Gossip: []string{"addr=b@example.com; keydata=\r\n BBBB"}}
	return msg
}

func headerBlock(raw string) string {
	head, _, _ := strings.Cut(raw, "\r\n\r\n")
	return head
}

func TestProtectedHeadersInsideEncryption(t *testing.T) {
	engine := &recordingThreadingEngine{}
	raw, err := BuildRaw(protectedMessage(), engine, crypto.ModeSignEncrypt, crypto.Options{})
	if err != nil {
		t.Fatal(err)
	}
	outer := headerBlock(string(raw))
	if !strings.Contains(outer, "Subject: "+SubjectPlaceholder+"\r\n") || strings.Contains(outer, "Quarterly") {
		t.Errorf("outer subject not hidden:\n%s", outer)
	}
	if !strings.Contains(outer, "Autocrypt: addr=me@example.com; keydata=\r\n AAAA") {
		t.Errorf("Autocrypt header missing from the outer headers:\n%s", outer)
	}
	if strings.Contains(outer, "Autocrypt-Gossip") {
		t.Error("gossip is outside the encryption")
	}

	inner := headerBlock(string(engine.entity))
	for _, want := range []string{
		`protected-headers="v1"`,
		"Subject: Quarterly numbers\r\n",
		"From: ",
		"Message-ID: ",
		"Autocrypt-Gossip: addr=b@example.com; keydata=\r\n BBBB",
	} {
		if !strings.Contains(inner, want) {
			t.Errorf("inner headers lack %q:\n%s", want, inner)
		}
	}
	// the Message-ID and Date inside are the ones outside.
	outerID := between(outer, "Message-ID: ", "\r\n")
	if outerID == "" || between(inner, "Message-ID: ", "\r\n") != outerID {
		t.Error("inner Message-ID differs from the outer one")
	}
	if between(inner, "Date: ", "\r\n") != between(outer, "Date: ", "\r\n") {
		t.Error("inner Date differs from the outer one")
	}
}

func TestProtectedHeadersIgnoredWhenNotEncrypting(t *testing.T) {
	for _, mode := range []crypto.Mode{crypto.ModeNone, crypto.ModeSign} {
		engine := &recordingThreadingEngine{}
		raw, err := BuildRaw(protectedMessage(), engine, mode, crypto.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(headerBlock(string(raw)), "Subject: Quarterly numbers") {
			t.Errorf("%s: subject hidden on a message that is not encrypted", mode)
		}
		if strings.Contains(string(raw), "Autocrypt-Gossip") || strings.Contains(string(engine.entity), "Autocrypt-Gossip") {
			t.Errorf("%s: gossip on a message that is not encrypted", mode)
		}
	}
}

func between(s, start, end string) string {
	_, rest, ok := strings.Cut(s, start)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, end)
	return v
}
