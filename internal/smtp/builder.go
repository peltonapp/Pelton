package smtp

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime/quotedprintable"
	"strings"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/messageid"
)

const (
	ctTextPlain   = "text/plain; charset=utf-8"
	ctTextHTML    = "text/html; charset=utf-8"
	ctOctetStream = "application/octet-stream"

	mpAlternative = "multipart/alternative"
	mpMixed       = "multipart/mixed"
	mpRelated     = "multipart/related"

	cteQuotedPrintable = "quoted-printable"
	cteBase64          = "base64"

	dispositionAttachment = "attachment"
	dispositionInline     = "inline"

	// base64LineLen is the rfc 2045 recommended max base64 line length.
	base64LineLen = 76

	boundaryBytes = 24
)

// Address is a single mail address with an optional display name.
type Address struct {
	Name  string
	Email string
}

// Attachment is a file to attach. Inline marks a part referenced from the html
// body by its ContentID (a cid: url), as opposed to a regular download.
type Attachment struct {
	Filename    string
	ContentType string // defaults to application/octet-stream
	Content     []byte
	Inline      bool
	ContentID   string // required when Inline, the value a cid: url points at
}

// Message is the input to the builder: addresses, subject, bodies, attachments
// and optional reply threading. Either Text or HTML (or both) should be set.
type Message struct {
	From    Address
	To      []Address
	Cc      []Address
	Bcc     []Address
	Subject string
	Text    string
	HTML    string

	Attachments []Attachment

	// reply threading. InReplyTo is the original message's Message-ID, References
	// is its existing reference chain.
	InReplyTo  string
	References []string

	// MessageID is used as the Message-ID header when set. Left empty, a fresh
	// one is generated. Drafts set it so each save replaces the last copy.
	MessageID string

	// Calendar is an iTIP scheduling part, such as the reply to an
	// invitation. It goes out as one more alternative of the body, which is
	// where calendar clients look for it.
	Calendar *CalendarPart

	// Headers are extra top-level headers, such as Autocrypt, written after
	// the standard ones. Values must already be folded.
	Headers [][2]string

	// Protected applies to an encrypted message only: the headers are
	// repeated inside the encrypted part, as RFC 9788 describes, together
	// with any Autocrypt-Gossip headers.
	Protected *ProtectedHeaders
}

// ProtectedHeaders controls what an encrypted message carries inside its
// encryption.
type ProtectedHeaders struct {
	// HideSubject replaces the visible subject with a placeholder, leaving the
	// real one only inside the encryption.
	HideSubject bool
	// Gossip are Autocrypt-Gossip header values, already folded.
	Gossip []string
}

// SubjectPlaceholder is the visible subject of a message whose subject is
// encrypted, the one most programs that hide the subject use.
const SubjectPlaceholder = "..."

// CalendarPart is iCalendar data with its iTIP method (REQUEST, REPLY,
// CANCEL).
type CalendarPart struct {
	Method string
	Data   []byte
}

// Recipients returns every envelope recipient address, including Bcc. The
// transport needs all of them at RCPT time even though Bcc never appears in the
// transmitted headers.
func (m *Message) Recipients() []string {
	out := make([]string, 0, len(m.To)+len(m.Cc)+len(m.Bcc))
	for _, group := range [][]Address{m.To, m.Cc, m.Bcc} {
		for _, a := range group {
			out = append(out, a.Email)
		}
	}
	return out
}

// part is a node in the MIME tree. A leaf has body and no children; a multipart
// has children and a boundary, and its contentType carries the boundary param.
type part struct {
	contentType string
	headers     [][2]string
	body        []byte
	children    []part
	boundary    string
}

// topHeaders builds the message's top-level rfc 5322 headers, without a trailing
// blank line so the content entity's own headers follow directly. Bcc is
// deliberately omitted from the transmitted headers.
//
// The full message structure, documented once:
//
//	text only                       -> text/plain
//	html only                       -> text/html
//	text + html                     -> multipart/alternative
//	+ inline images (with html)     -> multipart/related  [ alt|html, inline... ]
//	+ regular attachments           -> multipart/mixed    [ body|related, attach... ]
//
// withBcc is only ever true for a draft, which is stored and not transmitted.
func (m *Message) topHeaders(withBcc bool, subject, date, msgID string) string {
	var b strings.Builder
	headers := m.addressHeaders(withBcc, subject, date, msgID)
	headers = append(headers, [2]string{headerMIMEVersion, mimeVersion}, [2]string{headerUserAgent, userAgent})
	for _, h := range append(headers, m.Headers...) {
		writeHeader(&b, h[0], h[1])
	}
	return b.String()
}

// addressHeaders returns the headers that say who, what and when, the ones an
// encrypted message repeats inside its encryption.
func (m *Message) addressHeaders(withBcc bool, subject, date, msgID string) [][2]string {
	out := [][2]string{{headerFrom, formatAddress(m.From)}}
	if len(m.To) > 0 {
		out = append(out, [2]string{headerTo, formatAddressList(m.To)})
	}
	if len(m.Cc) > 0 {
		out = append(out, [2]string{headerCc, formatAddressList(m.Cc)})
	}
	if withBcc && len(m.Bcc) > 0 {
		out = append(out, [2]string{headerBcc, formatAddressList(m.Bcc)})
	}
	out = append(out,
		[2]string{headerSubject, encodeWord(subject)},
		[2]string{headerDate, date},
		[2]string{headerMessageID, msgID})
	if parent, ok := messageid.Bracket(m.InReplyTo); ok {
		out = append(out,
			[2]string{headerInReplyTo, parent},
			[2]string{headerReferences, referenceChain(m.References, m.InReplyTo)})
	}
	return out
}

// messageID returns the message's Message-ID, making one when it has none.
func (m *Message) messageID() (string, error) {
	if m.MessageID != "" {
		return m.MessageID, nil
	}
	return NewMessageID(m.From.Email)
}

// contentEntity builds the MIME tree for the message body and attachments.
func (m *Message) contentEntity() (*part, error) {
	inline, regular := splitAttachments(m.Attachments)

	body := m.bodyPart()

	// inline images only make sense wrapping an html body.
	if len(inline) > 0 && m.HTML != "" {
		related, err := newMultipart(mpRelated, append([]part{body}, inline...))
		if err != nil {
			return nil, err
		}
		body = *related
	}

	if len(regular) > 0 {
		mixed, err := newMultipart(mpMixed, append([]part{body}, regular...))
		if err != nil {
			return nil, err
		}
		return mixed, nil
	}

	return &body, nil
}

// bodyPart returns the text/html/alternative part for the message body.
func (m *Message) bodyPart() part {
	if m.Calendar != nil {
		alts := []part{textLeaf(m.Text)}
		if m.HTML != "" {
			alts = append(alts, htmlLeaf(m.HTML))
		}
		alts = append(alts, calendarLeaf(*m.Calendar))
		if alt, err := newMultipart(mpAlternative, alts); err == nil {
			return *alt
		}
	}
	switch {
	case m.Text != "" && m.HTML != "":
		alt, err := newMultipart(mpAlternative, []part{textLeaf(m.Text), htmlLeaf(m.HTML)})
		if err == nil {
			return *alt
		}
		// a boundary failure is effectively impossible; fall back to plain text.
		return textLeaf(m.Text)
	case m.HTML != "":
		return htmlLeaf(m.HTML)
	default:
		return textLeaf(m.Text)
	}
}

// splitAttachments separates inline (cid) parts from regular attachments.
func splitAttachments(atts []Attachment) (inline, regular []part) {
	for _, a := range atts {
		if a.Inline {
			inline = append(inline, inlineLeaf(a))
		} else {
			regular = append(regular, attachmentLeaf(a))
		}
	}
	return inline, regular
}

func textLeaf(text string) part {
	return part{
		contentType: ctTextPlain,
		headers:     [][2]string{{headerContentTransferEncoding, cteQuotedPrintable}},
		body:        quotedPrintable(text),
	}
}

func htmlLeaf(html string) part {
	return part{
		contentType: ctTextHTML,
		headers:     [][2]string{{headerContentTransferEncoding, cteQuotedPrintable}},
		body:        quotedPrintable(html),
	}
}

func calendarLeaf(c CalendarPart) part {
	return part{
		contentType: "text/calendar; charset=utf-8; method=" + strings.ToUpper(c.Method),
		headers:     [][2]string{{headerContentTransferEncoding, cteBase64}},
		body:        base64Body(c.Data),
	}
}

func attachmentLeaf(a Attachment) part {
	return part{
		contentType: attachmentContentType(a),
		headers: [][2]string{
			{headerContentTransferEncoding, cteBase64},
			{headerContentDisposition, dispositionAttachment + "; filename=\"" + a.Filename + "\""},
		},
		body: base64Body(a.Content),
	}
}

func inlineLeaf(a Attachment) part {
	return part{
		contentType: attachmentContentType(a),
		headers: [][2]string{
			{headerContentTransferEncoding, cteBase64},
			{headerContentDisposition, dispositionInline + "; filename=\"" + a.Filename + "\""},
			{headerContentID, "<" + a.ContentID + ">"},
		},
		body: base64Body(a.Content),
	}
}

func attachmentContentType(a Attachment) string {
	if a.ContentType != "" {
		return a.ContentType
	}
	return ctOctetStream
}

// newMultipart builds a multipart node with a fresh boundary baked into its
// content type.
func newMultipart(subtype string, children []part) (*part, error) {
	boundary, err := newBoundary()
	if err != nil {
		return nil, err
	}
	return &part{
		contentType: fmt.Sprintf("%s; boundary=%q", subtype, boundary),
		children:    children,
		boundary:    boundary,
	}, nil
}

// serializePart renders a MIME entity to bytes with crlf line endings: its
// headers, a blank line, then either the leaf body or the child parts wrapped in
// boundary delimiters.
func serializePart(p *part) []byte {
	var b bytes.Buffer
	b.WriteString(headerContentType + ": " + p.contentType + crlf)
	for _, h := range p.headers {
		b.WriteString(h[0] + ": " + h[1] + crlf)
	}
	b.WriteString(crlf)

	if len(p.children) == 0 {
		b.Write(p.body)
		return b.Bytes()
	}

	for i := range p.children {
		b.WriteString("--" + p.boundary + crlf)
		b.Write(serializePart(&p.children[i]))
		b.WriteString(crlf)
	}
	b.WriteString("--" + p.boundary + "--" + crlf)
	return b.Bytes()
}

// quotedPrintable encodes text with crlf line endings for a text part body.
func quotedPrintable(s string) []byte {
	normalized := strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	// the writer only fails if the underlying buffer fails, which it cannot.
	_, _ = w.Write([]byte(normalized))
	_ = w.Close()
	return b.Bytes()
}

// base64Body encodes content as base64 split into crlf-separated 76-char lines.
func base64Body(content []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(content)
	var b bytes.Buffer
	for len(encoded) > base64LineLen {
		b.WriteString(encoded[:base64LineLen])
		b.WriteString(crlf)
		encoded = encoded[base64LineLen:]
	}
	b.WriteString(encoded)
	return b.Bytes()
}

// BuildRaw builds the full transmittable message. When mode is ModeNone the
// content entity is emitted directly; otherwise the engine wraps it in a signed
// or encrypted structure.
//
// This is the security boundary: if crypto is requested and fails, BuildRaw
// returns an error and no bytes. The plaintext content entity is a local
// variable that is never returned on the failure path, so a message meant to be
// protected can never be transmitted in the clear.
func BuildRaw(msg *Message, engine crypto.Engine, mode crypto.Mode, opts crypto.Options) ([]byte, error) {
	msgID, err := msg.messageID()
	if err != nil {
		return nil, err
	}
	date := nowDate()
	encrypting := mode == crypto.ModeEncrypt || mode == crypto.ModeSignEncrypt
	subject := msg.Subject
	if encrypting && msg.Protected != nil && msg.Protected.HideSubject {
		subject = SubjectPlaceholder
	}
	top := msg.topHeaders(false, subject, date, msgID)
	root, err := msg.contentEntity()
	if err != nil {
		return nil, err
	}
	if encrypting && msg.Protected != nil {
		inner := msg.addressHeaders(false, msg.Subject, date, msgID)
		for _, g := range msg.Protected.Gossip {
			inner = append(inner, [2]string{headerAutocryptGossip, g})
		}
		root.contentType += `; protected-headers="v1"`
		root.headers = append(inner, root.headers...)
	}
	content := serializePart(root)

	if mode == crypto.ModeNone {
		return append([]byte(top), content...), nil
	}

	if engine == nil {
		return nil, fmt.Errorf("smtp: crypto mode %s requested but no engine is configured", mode)
	}
	wrapped, err := engine.Wrap(content, mode, opts)
	if err != nil {
		return nil, fmt.Errorf("smtp: crypto wrap failed, refusing to send in plaintext: %w", err)
	}

	var b bytes.Buffer
	b.WriteString(top)
	b.WriteString(headerContentType + ": " + wrapped.ContentType + crlf)
	b.WriteString(crlf)
	b.Write(wrapped.Body)
	return b.Bytes(), nil
}

// newBoundary returns a random multipart boundary.
func newBoundary() (string, error) {
	buf := make([]byte, boundaryBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("smtp: generate boundary: %w", err)
	}
	return fmt.Sprintf("pelton-%x", buf), nil
}

// BuildDraft builds a message for the server Drafts folder and returns it with
// its Message-ID. Unlike BuildRaw it keeps the Bcc header, so a reopened draft
// still has those recipients, and it never signs or encrypts: a draft of a
// message meant to be encrypted is the caller's to keep off the server.
func BuildDraft(msg *Message) ([]byte, string, error) {
	msgID, err := msg.messageID()
	if err != nil {
		return nil, "", err
	}
	top := msg.topHeaders(true, msg.Subject, nowDate(), msgID)
	root, err := msg.contentEntity()
	if err != nil {
		return nil, "", err
	}
	return append([]byte(top), serializePart(root)...), msgID, nil
}
