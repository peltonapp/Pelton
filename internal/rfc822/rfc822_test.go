package rfc822

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseThreadingHeaders(t *testing.T) {
	longIDs := make([]string, 25)
	longTokens := make([]string, 25)
	for i := range longIDs {
		longIDs[i] = fmt.Sprintf("ancestor%d@example.com", i)
		longTokens[i] = "<" + longIDs[i] + ">"
	}
	cases := []struct {
		name       string
		value      string
		inReplyTo  []string
		references []string
	}{
		{"folded comments", "(root (nested)) <a@example.com>\r\n\t<b@example.com> (tail)", []string{"a@example.com", "b@example.com"}, []string{"a@example.com", "b@example.com"}},
		{"escaped comments", `(root \(escaped\) \\ (nested)) <a@example.com>`, []string{"a@example.com"}, []string{"a@example.com"}},
		{"case and duplicates", "<A@Example.COM> <A@Example.COM>", []string{"A@Example.COM", "A@Example.COM"}, []string{"A@Example.COM", "A@Example.COM"}},
		{"duplicate parents", "<a@example.com> <a@example.com>", []string{"a@example.com", "a@example.com"}, []string{"a@example.com", "a@example.com"}},
		{"Normalize domain literals", "<a@[]> <a@[IPv6:2001:db8::1]>", []string{"a@[]", "a@[IPv6:2001:db8::1]"}, []string{"a@[]", "a@[IPv6:2001:db8::1]"}},
		{"invalid IRT not filtered", "<a@example.com> <bad>", []string{}, []string{"a@example.com"}},
		{"References recovers closed token", "<a@x> <bad> <b@x>", []string{}, []string{"a@x", "b@x"}},
		{"junk clears recovery", "<a@x> junk <b@x>", []string{}, []string{}},
		{"unmatched bracket clears recovery", "<a@x> <b@x", []string{}, []string{}},
		{"nested brackets clear recovery", "<a@x> <<b@x>>", []string{}, []string{}},
		{"unterminated comment clears recovery", "<a@x> (tail", []string{}, []string{}},
		{"internal whitespace", "<a@ example.com>", []string{}, []string{}},
		{"internal folding", "<a@\r\n\texample.com>", []string{}, []string{}},
		{"control injection", "<a@x>\x00", []string{}, []string{}},
		{"oversized ID", "<" + strings.Repeat("a", 977) + "@x>", []string{}, []string{}},
		{"non-ASCII not rewritten", "<a@éxample.com>", []string{}, []string{}},
		{"encoded words not decoded", "=?UTF-8?Q?<a@x>?=", []string{}, []string{}},
		{"incoming lists not capped", strings.Join(longTokens, " "), longIDs, longIDs},
		{"empty", "", []string{}, []string{}},
		{"comments only", "(no ancestry)", []string{}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := "In-Reply-To: " + tc.value + "\r\nReferences: " + tc.value + "\r\n\r\nreadable body\r\n"
			assertThreadingParsers(t, raw, tc.inReplyTo, tc.references)
		})
	}
	t.Run("absent", func(t *testing.T) {
		assertThreadingParsers(t, "Subject: Hi\r\n\r\nreadable body\r\n", []string{}, []string{})
	})
}

func TestParseRepeatedThreadingHeaders(t *testing.T) {
	cases := []struct {
		name       string
		headers    string
		references []string
	}{
		{"different IRT IDs", "In-Reply-To: <a@example.com>\r\nIn-Reply-To: <b@example.com>\r\n", []string{}},
		{"same IRT ID", "In-Reply-To: <a@example.com>\r\nIn-Reply-To: <a@example.com>\r\n", []string{}},
		{"empty second IRT", "In-Reply-To: <a@example.com>\r\nIn-Reply-To:\r\n", []string{}},
		{"empty first IRT", "In-Reply-To:\r\nIn-Reply-To: <a@example.com>\r\n", []string{}},
		{"References wire order", "References: <a@example.com>\r\nreferences: <b@example.com>\r\n", []string{"a@example.com", "b@example.com"}},
		{"References duplicates", "References: <a@example.com>\r\nReferences: <a@example.com>\r\n", []string{"a@example.com", "a@example.com"}},
		{"References empty field", "References:\r\nReferences: <b@example.com>\r\n", []string{"b@example.com"}},
		{"References recover per field", "References: <a@x> junk\r\nReferences: <b@x> <bad> <c@x>\r\n", []string{"b@x", "c@x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertThreadingParsers(t, tc.headers+"\r\nreadable body\r\n", []string{}, tc.references)
		})
	}
}

func assertThreadingParsers(t *testing.T, raw string, inReplyTo, references []string) {
	t.Helper()
	for _, parser := range []struct {
		name  string
		parse func([]byte) (*Message, error)
	}{{"Parse", Parse}, {"ParseBody", ParseBody}} {
		t.Run(parser.name, func(t *testing.T) {
			msg, err := parser.parse([]byte(raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(msg.InReplyTo, inReplyTo) {
				t.Errorf("InReplyTo = %#v, want %#v", msg.InReplyTo, inReplyTo)
			}
			if !reflect.DeepEqual(msg.References, references) {
				t.Errorf("References = %#v, want %#v", msg.References, references)
			}
			if strings.TrimSpace(msg.Text) != "readable body" {
				t.Errorf("body = %q, want readable body", msg.Text)
			}
		})
	}
}

// multipart/related inside multipart/mixed is the shape a real message with an
// inline image and a real attachment arrives in, and it is what the imap path
// depends on this parser getting right.
const multipartMessage = "Message-ID: <root@example.com>\r\n" +
	"Subject: =?UTF-8?B?R3LDvMOfZQ==?=\r\n" +
	"From: \"Doe, Jane\" <jane@example.com>\r\n" +
	"To: a@example.com, Bob <b@example.com>\r\n" +
	"Cc: c@example.com\r\n" +
	"Date: Mon, 06 Jan 2020 10:00:00 +0000\r\n" +
	"List-Unsubscribe: <https://example.com/u>\r\n" +
	"List-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=outer\r\n" +
	"\r\n" +
	"--outer\r\n" +
	"Content-Type: multipart/alternative; boundary=inner\r\n" +
	"\r\n" +
	"--inner\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"plain body\r\n" +
	"--inner\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"\r\n" +
	"<p>html body</p>\r\n" +
	"--inner--\r\n" +
	"--outer\r\n" +
	"Content-Type: image/png\r\n" +
	"Content-Disposition: attachment; filename=\"logo.png\"\r\n" +
	"Content-Id: <logo@example.com>\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"aGVsbG8=\r\n" +
	"--outer\r\n" +
	"Content-Type: text/plain\r\n" +
	"Content-Disposition: attachment; filename=\"notes.txt\"\r\n" +
	"\r\n" +
	"attached text\r\n" +
	"--outer--\r\n"

func TestParseMultipart(t *testing.T) {
	msg, err := Parse([]byte(multipartMessage))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if msg.Subject != "Grüße" {
		t.Errorf("subject = %q, want Grüße", msg.Subject)
	}
	if msg.MessageID != "root@example.com" {
		t.Errorf("message id = %q", msg.MessageID)
	}
	// a display name containing a comma must stay one address, not split into
	// two, which is exactly what naive comma-splitting gets wrong.
	if msg.From != "Doe, Jane <jane@example.com>" {
		t.Errorf("from = %q", msg.From)
	}
	if msg.To != "a@example.com, Bob <b@example.com>" {
		t.Errorf("to = %q", msg.To)
	}
	if msg.Cc != "c@example.com" {
		t.Errorf("cc = %q", msg.Cc)
	}
	if msg.Date.IsZero() {
		t.Error("date was not parsed")
	}
	if strings.TrimSpace(msg.Text) != "plain body" {
		t.Errorf("text = %q", msg.Text)
	}
	if !strings.Contains(msg.HTML, "<p>html body</p>") {
		t.Errorf("html = %q", msg.HTML)
	}
	if !msg.ListUnsubscribePost {
		t.Error("one-click unsubscribe was not detected")
	}
	if msg.Size != int64(len(multipartMessage)) {
		t.Errorf("size = %d, want %d", msg.Size, len(multipartMessage))
	}

	if len(msg.Attachments) != 2 {
		t.Fatalf("got %d attachments, want 2", len(msg.Attachments))
	}
	inline := msg.Attachments[0]
	if inline.ContentID != "logo@example.com" {
		t.Errorf("content id = %q, want the angle brackets stripped", inline.ContentID)
	}
	if string(inline.Content) != "hello" {
		t.Errorf("inline content = %q, want the base64 decoded", inline.Content)
	}
	if strings.TrimSpace(string(msg.Attachments[1].Content)) != "attached text" {
		t.Errorf("attachment content = %q", msg.Attachments[1].Content)
	}
}

// ParseBody is what the imap path uses: the server supplies the envelope, so
// only the bodies and attachments are read here.
func TestParseBodySkipsEnvelope(t *testing.T) {
	msg, err := ParseBody([]byte(multipartMessage))
	if err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if msg.Subject != "" || msg.From != "" {
		t.Errorf("ParseBody filled envelope fields: subject=%q from=%q", msg.Subject, msg.From)
	}
	if strings.TrimSpace(msg.Text) != "plain body" || len(msg.Attachments) != 2 {
		t.Errorf("ParseBody did not read the body: %+v", msg)
	}
}

// a message with a charset Go has no decoder for still has readable headers and
// structure, so it must not fail the whole parse.
func TestParseUnknownCharsetIsNotFatal(t *testing.T) {
	const raw = "Subject: hi\r\n" +
		"From: a@example.com\r\n" +
		"Content-Type: text/plain; charset=x-made-up\r\n" +
		"\r\n" +
		"body\r\n"
	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Subject != "hi" {
		t.Errorf("subject = %q", msg.Subject)
	}
}

// a broken address header must not cost the whole message: the raw value is
// better than nothing in the sender column.
func TestParseFallsBackOnUnparseableAddress(t *testing.T) {
	const raw = "Subject: hi\r\nFrom: not an address at all\r\n\r\nbody\r\n"
	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.From != "not an address at all" {
		t.Errorf("from = %q, want the raw header value", msg.From)
	}
}

// TestParseBodyReadsAuthenticationHeaders: the headers are dropped once a
// message is stored, so anything not read here is gone for good.
func TestParseBodyReadsAuthenticationHeaders(t *testing.T) {
	raw := []byte("From: Someone <someone@example.com>\r\n" +
		"Reply-To: Other <other@elsewhere.test>\r\n" +
		"Authentication-Results: mine.example.org; spf=fail smtp.mailfrom=evil.test\r\n" +
		"Authentication-Results: relay.example.net; dkim=pass header.d=example.com\r\n" +
		"Subject: Hi\r\n" +
		"\r\n" +
		"body\r\n")

	msg, err := ParseBody(raw)
	if err != nil {
		t.Fatalf("ParseBody: %v", err)
	}
	if msg.ReplyTo != "Other <other@elsewhere.test>" {
		t.Errorf("ReplyTo = %q", msg.ReplyTo)
	}
	if len(msg.AuthResults) != 2 {
		t.Fatalf("AuthResults = %v, want both headers in order", msg.AuthResults)
	}
	if !strings.Contains(msg.AuthResults[0], "spf=fail") {
		t.Errorf("first header = %q, want the receiving server's one first", msg.AuthResults[0])
	}
}

// TestParseBodyWithoutAuthenticationHeaders: most mail has none, and that must
// come out empty rather than as anything a check could read as a failure.
func TestParseBodyWithoutAuthenticationHeaders(t *testing.T) {
	msg, err := ParseBody([]byte("From: a@example.com\r\nSubject: Hi\r\n\r\nbody\r\n"))
	if err != nil {
		t.Fatalf("ParseBody: %v", err)
	}
	if len(msg.AuthResults) != 0 || msg.ReplyTo != "" {
		t.Errorf("AuthResults = %v, ReplyTo = %q, want both empty", msg.AuthResults, msg.ReplyTo)
	}
}

// the mojibake bug (#311). Mail that declares no charset, or one nothing knows,
// used to reach storage as the sender's raw bytes and be rendered as utf-8
// later. Every case here has to come out as valid utf-8, and the ones where the
// original text is recoverable have to come out as the original text.
func TestParseDecodesTextThatDeclaresNoUsableCharset(t *testing.T) {
	// "Grüße aus Köln" in latin-1: long enough for the detector to have
	// something to work with, which short strings do not give it.
	const latin1Body = "Gr\xfc\xdfe aus K\xf6ln. Der Kaffee war gr\xf6\xdfer als \xfcblich und die T\xfcr stand offen."
	const want = "Grüße aus Köln. Der Kaffee war größer als üblich und die Tür stand offen."

	cases := []struct {
		name        string
		contentType string
		body        string
		want        string
		wantGuess   bool
	}{
		{"declared latin-1", "text/plain; charset=iso-8859-1", latin1Body, want, false},
		{"declared windows-1252", "text/plain; charset=windows-1252", latin1Body, want, false},
		{"no charset at all", "text/plain", latin1Body, want, true},
		{"charset nothing knows", "text/plain; charset=x-nonsense", latin1Body, want, true},
		{"us-ascii with high bytes", "text/plain; charset=us-ascii", latin1Body, want, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := "Subject: hi\r\nFrom: a@example.com\r\n" +
				"Content-Type: " + c.contentType + "\r\n\r\n" + c.body + "\r\n"
			msg, err := Parse([]byte(raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !utf8.ValidString(msg.Text) {
				t.Fatalf("stored body is not valid utf-8: %q", msg.Text)
			}
			if got := strings.TrimRight(msg.Text, "\r\n"); got != c.want {
				t.Errorf("body = %q, want %q", got, c.want)
			}
			if guessed := msg.CharsetGuess != ""; guessed != c.wantGuess {
				t.Errorf("CharsetGuess = %q, guessed = %v, want %v", msg.CharsetGuess, guessed, c.wantGuess)
			}
		})
	}
}

// an encoded-word naming a charset go's own decoder does not know used to be
// left in the subject as its raw =?...?= source.
func TestParseDecodesEncodedWordInAnyCharset(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		{"=?koi8-r?B?8NLJ18XU?=", "Привет"},
		{"=?UTF-8?B?R3LDvMOfZQ==?=", "Grüße"},
		{"=?iso-8859-1?Q?Gr=FC=DFe?=", "Grüße"},
	}
	for _, c := range cases {
		raw := "Subject: " + c.subject + "\r\nFrom: a@example.com\r\n\r\nbody\r\n"
		msg, err := Parse([]byte(raw))
		if err != nil {
			t.Fatalf("parse %q: %v", c.subject, err)
		}
		if msg.Subject != c.want {
			t.Errorf("subject %q decoded to %q, want %q", c.subject, msg.Subject, c.want)
		}
	}
}

// a display name in raw 8-bit bytes has no charset of its own to convert by, so
// it goes the same way as a body rather than reaching the sender column broken.
func TestParseAddressWithRawHighBytesIsValidUTF8(t *testing.T) {
	const raw = "Subject: hi\r\nFrom: J\xfcrgen M\xfcller <j@example.com>\r\n\r\nbody\r\n"
	msg, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !utf8.ValidString(msg.From) {
		t.Fatalf("from is not valid utf-8: %q", msg.From)
	}
}
