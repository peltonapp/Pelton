package smtp

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime/quotedprintable"
	"strings"

	"github.com/peltonapp/Pelton/internal/crypto"
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

// headerSet is which top-level headers a built message carries.
type headerSet int

const (
	// headersSend is everything but Bcc, for a message that is transmitted.
	headersSend headerSet = iota
	// headersDraft adds Bcc, for a draft that is only stored.
	headersDraft
	// headersHidden leaves out every header that says what the message is
	// about: recipients, thread, and the subject, which becomes hiddenSubject.
	headersHidden
)

// hiddenSubject stands in for the subject of an encrypted draft, the same
// placeholder Thunderbird shows for an encrypted subject.
const hiddenSubject = "..."

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
func (m *Message) topHeaders(set headerSet, date string) (string, string, error) {
	var b strings.Builder
	writeHeader(&b, headerFrom, formatAddress(m.From))
	hidden := set == headersHidden
	if !hidden && len(m.To) > 0 {
		writeHeader(&b, headerTo, formatAddressList(m.To))
	}
	if !hidden && len(m.Cc) > 0 {
		writeHeader(&b, headerCc, formatAddressList(m.Cc))
	}
	if set == headersDraft && len(m.Bcc) > 0 {
		writeHeader(&b, headerBcc, formatAddressList(m.Bcc))
	}
	if hidden {
		writeHeader(&b, headerSubject, hiddenSubject)
	} else {
		writeHeader(&b, headerSubject, encodeWord(m.Subject))
	}
	writeHeader(&b, headerDate, date)

	msgID := m.MessageID
	if msgID == "" {
		var err error
		if msgID, err = NewMessageID(m.From.Email); err != nil {
			return "", "", err
		}
	}
	writeHeader(&b, headerMessageID, msgID)

	if !hidden && m.InReplyTo != "" {
		writeHeader(&b, headerInReplyTo, m.InReplyTo)
	}
	if refs := referenceChain(m.References, m.InReplyTo); !hidden && refs != "" {
		writeHeader(&b, headerReferences, refs)
	}

	writeHeader(&b, headerMIMEVersion, mimeVersion)
	writeHeader(&b, headerUserAgent, userAgent)
	return b.String(), msgID, nil
}

// protectHeaders puts the message's real headers on root, the part that gets
// encrypted, marked protected-headers="v1" (draft-autocrypt-lamps-protected-
// headers), which is where Thunderbird and other clients that decrypt the
// message read its subject, recipients and thread from. Bcc is among them,
// since only a draft, encrypted to its author alone, carries protected
// headers. A value with a line break that is not a header fold is refused
// rather than written: inside the encryption it would start a header of its
// own.
func (m *Message) protectHeaders(root *part, date, msgID string) error {
	var headers [][2]string
	add := func(key, value string) {
		if value != "" {
			headers = append(headers, [2]string{key, value})
		}
	}
	add(headerFrom, formatAddress(m.From))
	add(headerTo, formatAddressList(m.To))
	add(headerCc, formatAddressList(m.Cc))
	add(headerBcc, formatAddressList(m.Bcc))
	// Subject goes in even when empty: a client replaces the clear "..." only
	// with a protected Subject it finds.
	headers = append(headers, [2]string{headerSubject, encodeWord(m.Subject)})
	add(headerDate, date)
	add(headerMessageID, msgID)
	add(headerInReplyTo, m.InReplyTo)
	add(headerReferences, referenceChain(m.References, m.InReplyTo))
	for _, h := range headers {
		if !isHeaderValue(h[1]) {
			return fmt.Errorf("smtp: the %s of this draft holds a line break", h[0])
		}
	}
	root.contentType += `; protected-headers="v1"`
	root.headers = append(headers, root.headers...)
	return nil
}

// isHeaderValue reports whether v can be written as a header value as it
// stands: no control characters but tab, and line breaks only as folds, a
// crlf followed by a space or tab.
func isHeaderValue(v string) bool {
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c == '\r':
			if i+2 >= len(v) || v[i+1] != '\n' || (v[i+2] != ' ' && v[i+2] != '\t') {
				return false
			}
			i++
		case c < ' ' && c != '\t', c == 0x7f:
			return false
		}
	}
	return true
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
	raw, _, err := msg.build(headersSend, engine, mode, opts)
	return raw, err
}

// build assembles the headers and content entity, wrapped by engine unless
// mode is ModeNone, and returns the bytes with the Message-ID they carry.
func (m *Message) build(set headerSet, engine crypto.Engine, mode crypto.Mode, opts crypto.Options) ([]byte, string, error) {
	date := nowDate()
	top, msgID, err := m.topHeaders(set, date)
	if err != nil {
		return nil, "", err
	}
	root, err := m.contentEntity()
	if err != nil {
		return nil, "", err
	}
	// headers hidden from the clear go inside the encryption instead.
	if set == headersHidden {
		if err := m.protectHeaders(root, date, msgID); err != nil {
			return nil, "", err
		}
	}
	content := serializePart(root)

	if mode == crypto.ModeNone {
		return append([]byte(top), content...), msgID, nil
	}

	if engine == nil {
		return nil, "", fmt.Errorf("smtp: crypto mode %s requested but no engine is configured", mode)
	}
	wrapped, err := engine.Wrap(content, mode, opts)
	if err != nil {
		return nil, "", fmt.Errorf("smtp: crypto wrap failed, refusing to send in plaintext: %w", err)
	}

	var b bytes.Buffer
	b.WriteString(top)
	b.WriteString(headerContentType + ": " + wrapped.ContentType + crlf)
	b.WriteString(crlf)
	b.Write(wrapped.Body)
	return b.Bytes(), msgID, nil
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
// still has those recipients, and it never signs or encrypts. A draft of a
// message meant to be encrypted goes through BuildEncryptedDraft instead.
func BuildDraft(msg *Message) ([]byte, string, error) {
	return msg.build(headersDraft, nil, crypto.ModeNone, crypto.Options{})
}

// BuildEncryptedDraft builds a draft for the server Drafts folder encrypted to
// the key of owner, the account's own address, and nobody else's, so the server
// and the recipients alike only ever get ciphertext, and returns it with its
// Message-ID. owner rather than the From address picks the key, so a draft
// sent from an address with no key of its own still goes up. It is not
// signed. Unlike an encrypted send its clear headers say nothing about the
// content: no To, Cc, Bcc, In-Reply-To or References, and "..." for the
// subject. From, Date and the Message-ID stay. The real ones, Bcc included,
// travel inside the encryption as protected headers. A failed encryption
// returns an error and no bytes.
func BuildEncryptedDraft(msg *Message, owner string, engine crypto.Engine) ([]byte, string, error) {
	opts := crypto.Options{SenderEmail: owner, Recipients: []string{owner}}
	return msg.build(headersHidden, engine, crypto.ModeEncrypt, opts)
}
