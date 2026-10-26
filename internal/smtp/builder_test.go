package smtp

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/crypto"
)

func baseMessage() *Message {
	return &Message{
		From:    Address{Name: "Ann Sender", Email: "ann@example.com"},
		To:      []Address{{Email: "bob@example.com"}},
		Subject: "hello",
		Text:    "plain body\n",
	}
}

func TestBuildPlainTextOnly(t *testing.T) {
	raw, err := BuildRaw(baseMessage(), nil, crypto.ModeNone, crypto.Options{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, "Content-Type: text/plain; charset=utf-8") {
		t.Fatalf("expected text/plain, got:\n%s", headerOnly(s))
	}
	if !strings.Contains(s, "Message-ID: <") || !strings.Contains(s, "@example.com>") {
		t.Fatal("expected a message id using the sender domain")
	}
	if !strings.Contains(s, "MIME-Version: 1.0") {
		t.Fatal("expected MIME-Version header")
	}
}

func TestBuildAlternativeWithAttachmentAndInline(t *testing.T) {
	msg := baseMessage()
	msg.HTML = `<p>hi <img src="cid:img@x"></p>`
	msg.Attachments = []Attachment{
		{Filename: "doc.txt", ContentType: "text/plain", Content: []byte("file")},
		{Filename: "p.png", ContentType: "image/png", Content: []byte("png"), Inline: true, ContentID: "img@x"},
	}

	raw, err := BuildRaw(msg, nil, crypto.ModeNone, crypto.Options{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	s := string(raw)

	// outermost is mixed (because of the regular attachment), wrapping a related
	// (because of the inline image) wrapping an alternative (text + html).
	for _, want := range []string{
		"Content-Type: multipart/mixed;",
		"Content-Type: multipart/related;",
		"Content-Type: multipart/alternative;",
		"Content-Disposition: attachment; filename=\"doc.txt\"",
		"Content-Disposition: inline; filename=\"p.png\"",
		"Content-ID: <img@x>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, headerOnly(s))
		}
	}
}

func TestBuildReplyThreadingHeaders(t *testing.T) {
	msg := baseMessage()
	msg.InReplyTo = "<parent@example.com>"
	msg.References = []string{"<root@example.com>", "<parent@example.com>"}

	raw, err := BuildRaw(msg, nil, crypto.ModeNone, crypto.Options{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, "In-Reply-To: <parent@example.com>") {
		t.Error("missing In-Reply-To")
	}
	if !strings.Contains(s, "References: <root@example.com>") {
		t.Error("missing References chain")
	}
	// the parent is already in references, so it must not be duplicated.
	if strings.Count(s, "<parent@example.com>") != 2 { // once in In-Reply-To, once in References
		t.Errorf("parent id duplicated in references:\n%s", headerOnly(s))
	}
}

func TestBccNeverInHeaders(t *testing.T) {
	msg := baseMessage()
	msg.Bcc = []Address{{Email: "secret@example.com"}}

	raw, err := BuildRaw(msg, nil, crypto.ModeNone, crypto.Options{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if bytes.Contains(raw, []byte("secret@example.com")) {
		t.Fatal("Bcc address leaked into the transmitted message")
	}
	// but it is still an envelope recipient so it actually receives the mail.
	if !contains(msg.Recipients(), "secret@example.com") {
		t.Fatal("Bcc recipient missing from envelope recipients")
	}
}

// failingEngine stands in for a crypto engine that cannot complete, e.g. a
// missing key, to prove the smtp boundary refuses to emit plaintext.
type failingEngine struct{}

func (failingEngine) Wrap(entity []byte, mode crypto.Mode, opts crypto.Options) (*crypto.Part, error) {
	return nil, errors.New("no key available")
}

func TestBuildRawRefusesPlaintextWhenCryptoFails(t *testing.T) {
	msg := baseMessage()
	msg.Text = "TOP SECRET PLAINTEXT"

	raw, err := BuildRaw(msg, failingEngine{}, crypto.ModeEncrypt, crypto.Options{Recipients: []string{"bob@example.com"}})
	if err == nil {
		t.Fatal("expected an error when crypto fails")
	}
	if raw != nil {
		t.Fatalf("expected nil output on crypto failure, got %d bytes", len(raw))
	}
	if bytes.Contains([]byte(err.Error()), []byte("TOP SECRET PLAINTEXT")) {
		t.Fatal("error leaked the plaintext")
	}
}

func TestBuildRawEncryptModeNeedsEngine(t *testing.T) {
	_, err := BuildRaw(baseMessage(), nil, crypto.ModeEncrypt, crypto.Options{})
	if err == nil {
		t.Fatal("expected an error when an engine is required but nil")
	}
}

func headerOnly(s string) string {
	if before, _, ok := strings.Cut(s, "\r\n\r\n"); ok {
		return before
	}
	return s
}
