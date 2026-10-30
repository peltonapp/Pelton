package smtp

import (
	"bytes"
	"errors"
	"fmt"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/messageid"
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
	type threadingCase struct {
		name   string
		parent string
		refs   []string
		want   []string
	}
	tests := []threadingCase{
		{"bare", "parent@example.com", []string{"root@example.com", "parent@example.com"}, []string{"root@example.com", "parent@example.com"}},
		{"legacy bracketed", "<parent@example.com>", []string{"<root@example.com>", "<parent@example.com>"}, []string{"root@example.com", "parent@example.com"}},
		{"mixed duplicates", "<parent@example.com>", []string{"root@example.com", "<root@example.com>", "middle@example.com", "<middle@example.com>", "parent@example.com"}, []string{"root@example.com", "middle@example.com", "parent@example.com"}},
		{"parent moved last", "parent@example.com", []string{"parent@example.com", "root@example.com", "<parent@example.com>", "newest@example.com"}, []string{"root@example.com", "newest@example.com", "parent@example.com"}},
		{"parent only", "parent@example.com", nil, []string{"parent@example.com"}},
		{"case preserved", "Parent@Example.COM", []string{"Root@Example.COM", "root@example.com"}, []string{"Root@Example.COM", "root@example.com", "Parent@Example.COM"}},
		{"invalid references skipped", "parent@example.com", []string{"root@example.com", "bad", "<<nested@example.com>>", "a..b@example.com", "bad id@example.com", "evil@example.com\r\nX-Injected: yes", "<unclosed@example.com", "", "<parent@example.com>"}, []string{"root@example.com", "parent@example.com"}},
	}
	for _, parent := range []string{"", "bad", "<<parent@example.com>>", "<parent@example.com", "parent@example.com>", "a..b@example.com", "a@b@c", "parent @example.com", "parent@exämple.com", "parent@example.com\r\nX-Injected: yes"} {
		tests = append(tests, threadingCase{"invalid parent " + parent, parent, []string{"root@example.com"}, nil})
	}
	for _, count := range []int{20, 21, 100} {
		refs := make([]string, count)
		for i := range refs {
			refs[i] = fmt.Sprintf("id%d@example.com", i)
		}
		want := []string{refs[0]}
		want = append(want, refs[count-19:]...)
		tests = append(tests, threadingCase{fmt.Sprintf("%d ID chain", count), refs[count-1], refs, want})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forEachReplyBuilder(t, func(t *testing.T, build func(*Message) []byte) {
				msg := baseMessage()
				msg.InReplyTo, msg.References = tt.parent, tt.refs
				parent := ""
				if len(tt.want) > 0 {
					parent = tt.want[len(tt.want)-1]
				}
				first := assertReplyHeaders(t, build(msg), parent, tt.want)
				second := assertReplyHeaders(t, build(msg), parent, tt.want)
				if first.Header.Get("Message-ID") == second.Header.Get("Message-ID") {
					t.Error("successive builds reused their own Message-ID")
				}
			})
		})
	}
}

func TestReferencesLengthGuardDropsMiddle(t *testing.T) {
	refs := make([]string, 20)
	for i := range refs {
		refs[i] = fmt.Sprintf("%02d%s@example.com", i, strings.Repeat("a", 86)) // 100 bare bytes
	}
	// Ten tokens need 1041 bytes unfolded; root + the eight newest need 938.
	want := append([]string{refs[0]}, refs[12:]...)
	forEachReplyBuilder(t, func(t *testing.T, build func(*Message) []byte) {
		msg := baseMessage()
		msg.InReplyTo, msg.References = refs[19], refs
		parsed := assertReplyHeaders(t, build(msg), refs[19], want)
		if got := len("References: ") + len(parsed.Header.Get("References")); got != 938 {
			t.Errorf("unfolded header length = %d, want 938 (<=998)", got)
		}
	})
}

func TestReferencesLengthGuardKeepsMinimum(t *testing.T) {
	refs := make([]string, 4)
	for i := range refs {
		refs[i] = fmt.Sprintf("%02d%s@example.com", i, strings.Repeat("a", 286)) // 300 bare bytes
	}
	forEachReplyBuilder(t, func(t *testing.T, build func(*Message) []byte) {
		msg := baseMessage()
		msg.InReplyTo, msg.References = refs[3], refs
		parsed := assertReplyHeaders(t, build(msg), refs[3], refs)
		if got := len("References: ") + len(parsed.Header.Get("References")); got != 1223 {
			t.Errorf("unfolded minimum length = %d, want 1223 (>998)", got)
		}
	})
}

func TestReplyThreadingHeaderLineLimits(t *testing.T) {
	accepted := strings.Repeat("a", 966) + "@example.com" // bare 978, bracketed 980
	rejected := strings.Repeat("a", 967) + "@example.com" // bare 979, bracketed 981
	for _, value := range []string{accepted, "<" + accepted + ">"} {
		token, ok := messageid.Bracket(value)
		if !ok || len(token) != len(accepted)+2 {
			t.Fatalf("Bracket accepted boundary: ok=%v, length=%d, want %d", ok, len(token), len(accepted)+2)
		}
		forEachReplyBuilder(t, func(t *testing.T, build func(*Message) []byte) {
			msg := baseMessage()
			msg.InReplyTo = value
			msg.References = []string{value, "middle@example.com"}
			assertReplyHeaders(t, build(msg), accepted, []string{"middle@example.com", accepted})
			// A maximum-sized root exercises the References field-name prefix.
			msg.InReplyTo = "parent@example.com"
			msg.References = []string{value}
			assertReplyHeaders(t, build(msg), "parent@example.com", []string{accepted, "parent@example.com"})
		})
	}
	for _, value := range []string{rejected, "<" + rejected + ">", "parent@example.com\r\nX-Injected: yes"} {
		forEachReplyBuilder(t, func(t *testing.T, build func(*Message) []byte) {
			msg := baseMessage()
			msg.InReplyTo, msg.References = value, []string{"root@example.com"}
			assertReplyHeaders(t, build(msg), "", nil)
			msg.InReplyTo, msg.References = "parent@example.com", []string{"root@example.com", value}
			assertReplyHeaders(t, build(msg), "parent@example.com", []string{"root@example.com", "parent@example.com"})
		})
	}
}

func forEachReplyBuilder(t *testing.T, check func(*testing.T, func(*Message) []byte)) {
	t.Helper()
	for _, name := range []string{"raw", "draft"} {
		t.Run(name, func(t *testing.T) {
			check(t, func(msg *Message) []byte {
				t.Helper()
				var raw []byte
				var err error
				if name == "draft" {
					raw, _, err = BuildDraft(msg)
				} else {
					raw, err = BuildRaw(msg, nil, crypto.ModeNone, crypto.Options{})
				}
				if err != nil {
					t.Fatalf("build %s: %v", name, err)
				}
				return raw
			})
		})
	}
}

func assertReplyHeaders(t *testing.T, raw []byte, parent string, refs []string) *mail.Message {
	t.Helper()
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse MIME: %v", err)
	}
	wantParent := ""
	if parent != "" {
		wantParent = "<" + parent + ">"
	}
	if got := parsed.Header.Get("In-Reply-To"); got != wantParent {
		t.Errorf("In-Reply-To = %q, want %q", got, wantParent)
	}
	tokens := make([]string, len(refs))
	for i, id := range refs {
		tokens[i] = "<" + id + ">"
	}
	if got, want := parsed.Header.Get("References"), strings.Join(tokens, " "); got != want {
		t.Errorf("unfolded References = %q, want %q", got, want)
	}
	for _, field := range []string{"In-Reply-To", "References"} {
		wantCount := 0
		if parent != "" {
			wantCount = 1
		}
		if got := len(parsed.Header[textproto.CanonicalMIMEHeaderKey(field)]); got != wantCount {
			t.Errorf("%s field count = %d, want %d", field, got, wantCount)
		}
	}
	if got := parsed.Header.Get("X-Injected"); got != "" {
		t.Errorf("injected header escaped validation: %q", got)
	}
	own, ok := messageid.Normalize(parsed.Header.Get("Message-ID"))
	if !ok || own == parent {
		t.Errorf("own Message-ID = %q, want fresh valid ID distinct from parent", parsed.Header.Get("Message-ID"))
	}
	threading := false
	for _, line := range strings.Split(headerOnly(string(raw)), "\r\n") {
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			threading = strings.HasPrefix(line, "In-Reply-To:") || strings.HasPrefix(line, "References:")
		}
		if threading && len(line) > 998 {
			t.Errorf("physical threading line has %d bytes, exceeds 998", len(line))
		}
		if threading && strings.HasPrefix(line, " ") {
			// Continuations contain a whole bracketed token, with exactly one leading space.
			if !strings.HasPrefix(line, " <") || !strings.HasSuffix(line, ">") || strings.ContainsAny(line[1:], " \t") {
				t.Errorf("invalid threading continuation: %q", line)
			}
		}
	}
	return parsed
}

type recordingThreadingEngine struct {
	entity []byte
	mode   crypto.Mode
}

func (e *recordingThreadingEngine) Wrap(entity []byte, mode crypto.Mode, _ crypto.Options) (*crypto.Part, error) {
	e.entity, e.mode = bytes.Clone(entity), mode
	return &crypto.Part{ContentType: "application/octet-stream", Body: []byte("wrapped content")}, nil
}

func TestReplyThreadingOutsidePGP(t *testing.T) {
	for _, mode := range []crypto.Mode{crypto.ModeSign, crypto.ModeEncrypt, crypto.ModeSignEncrypt} {
		t.Run(mode.String(), func(t *testing.T) {
			msg := baseMessage()
			msg.InReplyTo = "parent@example.com"
			msg.References = []string{"root@example.com", "parent@example.com"}
			engine := &recordingThreadingEngine{}
			raw, err := BuildRaw(msg, engine, mode, crypto.Options{})
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			assertReplyHeaders(t, raw, "parent@example.com", []string{"root@example.com", "parent@example.com"})
			inner, err := mail.ReadMessage(bytes.NewReader(engine.entity))
			if err != nil {
				t.Fatalf("parse wrapped input: %v", err)
			}
			if engine.mode != mode || len(engine.entity) == 0 {
				t.Fatalf("crypto input missing or wrong mode: %v", engine.mode)
			}
			for _, field := range []string{"In-Reply-To", "References"} {
				if len(inner.Header[textproto.CanonicalMIMEHeaderKey(field)]) != 0 || bytes.Contains(engine.entity, []byte(field+":")) {
					t.Errorf("%s leaked into crypto input", field)
				}
			}
		})
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
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
		"me@bücher.de":                            "xn--bcher-kva.de",
		"me@exämple.com":                          "xn--exmple-cua.com",
		"me@example.com.":                         fallbackDomain,
		"me@.example.com":                         fallbackDomain,
		"me@example..com":                         fallbackDomain,
		"me@" + strings.Repeat("a", 980) + ".com": fallbackDomain,
		"me@\xff.com":                             fallbackDomain,
		"me@example.com":                          "example.com",
		" me@example.com ":                        "example.com",
		"me@exa mple.com":                         fallbackDomain,
		"me@exa\r\nmple.com":                      fallbackDomain,
		"me":                                      fallbackDomain,
	} {
		id, err := NewMessageID(email)
		if err != nil {
			t.Fatalf("NewMessageID(%q): %v", email, err)
		}
		if _, ok := messageid.Normalize(id); !ok {
			t.Errorf("NewMessageID(%q) = %q, want a strict reply token", email, id)
		}
		if !strings.HasSuffix(id, "@"+domain+">") {
			t.Errorf("NewMessageID(%q) = %q, want the domain %q", email, id, domain)
		}
	}
}
