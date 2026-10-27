package smtp

import (
	"bytes"
	"errors"
	"mime"
	"mime/multipart"
	"net/mail"
	"slices"
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
	if i := strings.Index(s, "\r\n\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// A draft is only stored, never transmitted, so it keeps its Bcc header for
// reopening, and it keeps a Message-ID it was given so every saved copy of it
// carries the same one.
func TestBuildDraft(t *testing.T) {
	for _, given := range []string{"<draft-1@example.com>", ""} {
		msg := baseMessage()
		msg.Bcc = []Address{{Email: "secret@example.com"}}
		msg.MessageID = given

		raw, id, err := BuildDraft(msg)
		if err != nil {
			t.Fatalf("build draft %q: %v", given, err)
		}
		if given != "" && id != given {
			t.Errorf("message id = %q, want the given %q", id, given)
		}
		if !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@example.com>") {
			t.Errorf("message id = %q, want <...@example.com>", id)
		}
		head := headerOnly(string(raw))
		if !strings.Contains(head, "Message-ID: "+id) || !strings.Contains(head, "Bcc: secret@example.com") {
			t.Errorf("draft headers lack Message-ID %s or the Bcc:\n%s", id, head)
		}
	}
}

// The Message-ID borrows the sender's domain, so stray spaces or a line break
// in the account address must not reach the header.
func TestNewMessageIDKeepsTheDomainClean(t *testing.T) {
	for email, domain := range map[string]string{
		"me@example.com":     "example.com",
		" me@example.com ":   "example.com",
		"me@exa mple.com":    fallbackDomain,
		"me@exa\r\nmple.com": fallbackDomain,
		"me":                 fallbackDomain,
	} {
		id, err := NewMessageID(email)
		if err != nil {
			t.Fatalf("NewMessageID(%q): %v", email, err)
		}
		if !strings.HasSuffix(id, "@"+domain+">") {
			t.Errorf("NewMessageID(%q) = %q, want the domain %q", email, id, domain)
		}
	}
}

// recordingEngine stands a fixed ciphertext in for the entity and remembers
// what it was asked to do.
type recordingEngine struct {
	mode   crypto.Mode
	opts   crypto.Options
	entity []byte
}

func (e *recordingEngine) Wrap(entity []byte, mode crypto.Mode, opts crypto.Options) (*crypto.Part, error) {
	e.mode, e.opts, e.entity = mode, opts, entity
	return &crypto.Part{ContentType: "multipart/encrypted", Body: []byte("CIPHERTEXT")}, nil
}

// An encrypted draft is readable by its author alone: it is encrypted to the
// owner's key and nobody else's, even when sent from another address, never
// signed, and keeps the Message-ID
// it was given. Its clear headers name neither the subject, the recipients
// nor the thread it answers.
func TestBuildEncryptedDraft(t *testing.T) {
	msg := baseMessage()
	msg.Subject, msg.Text = "quarterly numbers", "TOP SECRET PLAINTEXT"
	msg.Cc = []Address{{Email: "carol@example.com"}}
	msg.Bcc = []Address{{Email: "secret@example.com"}}
	msg.InReplyTo, msg.MessageID = "<thread-1@example.com>", "<draft-1@example.com>"
	msg.From = Address{Email: "alias@example.com"}
	engine := &recordingEngine{}

	raw, id, err := BuildEncryptedDraft(msg, "ann@example.com", engine)
	if err != nil {
		t.Fatalf("build encrypted draft: %v", err)
	}
	if engine.mode != crypto.ModeEncrypt || engine.opts.Passphrase != nil || engine.opts.SenderEmail != "ann@example.com" || !slices.Equal(engine.opts.Recipients, []string{"ann@example.com"}) {
		t.Errorf("wrapped with mode %v and %+v, want encryption to ann@example.com alone", engine.mode, engine.opts)
	}
	s, head := string(raw), headerOnly(string(raw))
	if id != msg.MessageID || !strings.Contains(head, "Message-ID: "+id) || !strings.Contains(head, "Subject: ...\r\n") {
		t.Errorf("message id = %q, want %q and the subject hidden in the headers:\n%s", id, msg.MessageID, head)
	}
	for _, leak := range []string{"TOP SECRET PLAINTEXT", "quarterly numbers", "bob@example.com", "carol@example.com", "secret@example.com", "thread-1"} {
		if strings.Contains(s, leak) {
			t.Errorf("the encrypted draft carries %q in the clear:\n%s", leak, s)
		}
	}
	if !strings.HasSuffix(s, "Content-Type: multipart/encrypted\r\n\r\nCIPHERTEXT") {
		t.Errorf("the draft is not the wrapped entity:\n%s", s)
	}

	if raw, _, err := BuildEncryptedDraft(msg, "ann@example.com", failingEngine{}); err == nil || raw != nil {
		t.Errorf("a failed wrap gave %d bytes and %v, want nothing and an error", len(raw), err)
	}
}

// encryptedDraftEntity builds msg as an encrypted draft and returns the
// entity that was encrypted, parsed, and the outer headers.
func encryptedDraftEntity(t *testing.T, msg *Message) (*mail.Message, string) {
	t.Helper()
	engine := &recordingEngine{}
	raw, _, err := BuildEncryptedDraft(msg, "ann@example.com", engine)
	if err != nil {
		t.Fatalf("build encrypted draft: %v", err)
	}
	inner, err := mail.ReadMessage(bytes.NewReader(engine.entity))
	if err != nil {
		t.Fatalf("parse the encrypted entity: %v\n%s", err, engine.entity)
	}
	return inner, headerOnly(string(raw))
}

// The encrypted part of a draft carries its real headers as protected headers,
// so another client that decrypts it (Thunderbird, or Pelton elsewhere) gets
// the subject, the recipients, Bcc included, and the thread back. The clear
// headers still say none of it.
func TestEncryptedDraftCarriesProtectedHeaders(t *testing.T) {
	msg := baseMessage()
	msg.Subject = "Quartalszahlen für Ärger"
	msg.Cc = []Address{{Name: "Kock, Arne", Email: "carol@example.com"}}
	msg.Bcc = []Address{{Email: "secret@example.com"}}
	msg.InReplyTo, msg.MessageID = "<thread-2@example.com>", "<draft-1@example.com>"
	msg.References = []string{"<thread-1@example.com>"}
	msg.Attachments = []Attachment{{Filename: "a.txt", ContentType: "text/plain", Content: []byte("x")}}

	inner, outer := encryptedDraftEntity(t, msg)
	mediaType, params, err := mime.ParseMediaType(inner.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" || params["protected-headers"] != "v1" || params["boundary"] == "" {
		t.Fatalf("root part Content-Type %q (%v), want multipart/mixed with its boundary and protected-headers=v1", inner.Header.Get("Content-Type"), err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(inner.Header.Get("Subject"))
	if err != nil || subject != msg.Subject {
		t.Errorf("protected Subject = %q, %v; want %q", subject, err, msg.Subject)
	}
	for header, want := range map[string]string{
		"From":        "\"Ann Sender\" <ann@example.com>",
		"To":          "bob@example.com",
		"Cc":          "\"Kock, Arne\" <carol@example.com>",
		"Bcc":         "secret@example.com",
		"Message-ID":  "<draft-1@example.com>",
		"In-Reply-To": "<thread-2@example.com>",
		"References":  "<thread-1@example.com> <thread-2@example.com>",
	} {
		if got := inner.Header.Get(header); got != want {
			t.Errorf("protected %s = %q, want %q", header, got, want)
		}
	}
	if inner.Header.Get("Date") == "" || !strings.Contains(outer, "Date: "+inner.Header.Get("Date")+"\r\n") {
		t.Errorf("protected Date %q is not the clear one:\n%s", inner.Header.Get("Date"), outer)
	}
	if _, err := multipart.NewReader(inner.Body, params["boundary"]).NextPart(); err != nil {
		t.Errorf("the protected root part no longer reads as multipart: %v", err)
	}
	for _, leak := range []string{"Quartalszahlen", "Arne", "bob@example.com", "secret@example.com", "thread-"} {
		if strings.Contains(outer, leak) {
			t.Errorf("the clear headers carry %q:\n%s", leak, outer)
		}
	}
	if !strings.Contains(outer, "Subject: ...\r\n") {
		t.Errorf("the clear subject is not hidden:\n%s", outer)
	}
}

// A line break smuggled into a protected header value must not start a header
// of its own: a subject is encoded, and anything else is refused.
func TestProtectedHeadersCannotBeInjected(t *testing.T) {
	msg := baseMessage()
	msg.Subject = "hi\r\nBcc: evil@example.com"
	inner, _ := encryptedDraftEntity(t, msg)
	if got := inner.Header.Get("Bcc"); got != "" {
		t.Fatalf("an injected Bcc %q got in", got)
	}
	if subject, _ := new(mime.WordDecoder).DecodeHeader(inner.Header.Get("Subject")); subject != msg.Subject {
		t.Errorf("protected Subject = %q, want %q", subject, msg.Subject)
	}

	for name, spoil := range map[string]func(*Message){
		"In-Reply-To": func(m *Message) { m.InReplyTo = "<a@example.com>\r\nX-Evil: 1" },
		"References":  func(m *Message) { m.References = []string{"<a@example.com>\nX-Evil: 1"} },
		"To":          func(m *Message) { m.To = []Address{{Email: "bob@example.com\r\nX-Evil: 1"}} },
	} {
		msg := baseMessage()
		spoil(msg)
		if raw, _, err := BuildEncryptedDraft(msg, "ann@example.com", &recordingEngine{}); err == nil {
			t.Errorf("%s with a line break built:\n%s", name, raw)
		}
	}
}

// An encrypted send is not a draft: it keeps the shape it had, with no
// protected headers added to what the recipients decrypt.
func TestEncryptedSendHasNoProtectedHeaders(t *testing.T) {
	engine := &recordingEngine{}
	if _, err := BuildRaw(baseMessage(), engine, crypto.ModeEncrypt, crypto.Options{SenderEmail: "ann@example.com", Recipients: []string{"bob@example.com"}}); err != nil {
		t.Fatalf("BuildRaw: %v", err)
	}
	if strings.Contains(string(engine.entity), "protected-headers") || strings.Contains(string(engine.entity), "Subject:") {
		t.Fatalf("the sent entity changed:\n%s", engine.entity)
	}
}

// A draft with no subject still carries a protected Subject, empty: a client
// replaces the clear "..." only when there is one to replace it with.
func TestEmptySubjectIsStillProtected(t *testing.T) {
	msg := baseMessage()
	msg.Subject = ""
	inner, _ := encryptedDraftEntity(t, msg)
	if values, ok := inner.Header["Subject"]; !ok || len(values) != 1 || values[0] != "" {
		t.Fatalf("protected Subject %q (present %v), want one empty", values, ok)
	}
}
