// Package rfc822 parses a raw internet message into the pieces Pelton stores:
// the envelope headers, a plain and an html body, and the decoded attachments.
//
// It exists because messages reach Pelton two ways. Over imap the server hands
// back a parsed ENVELOPE alongside the raw source, so only the body needs
// walking; a message read out of an .eml or .mbox file has no server to ask,
// so its headers have to be parsed too. Both paths share the body walk here
// rather than keeping two copies of it.
package rfc822

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"

	// decodes the legacy charsets, and guesses at the ones a message gets wrong
	"github.com/peltonapp/Pelton/internal/charsetguess"
	"github.com/peltonapp/Pelton/internal/messageid"
)

// Message is a parsed message. Text and HTML hold the first body part of each
// type; a message that has only one of them leaves the other empty.
type Message struct {
	MessageID string
	Subject   string
	From      string
	To        string
	Cc        string
	Date      time.Time
	Text      string
	HTML      string
	// InReplyTo and References contain validated bare IDs in wire order,
	// preserving duplicates. Both are non-nil even when absent or invalid.
	// Invalid or repeated In-Reply-To fields leave InReplyTo empty; References
	// keeps recoverable tokens from each physical field.
	InReplyTo  []string
	References []string
	// Size is the raw source length in bytes.
	Size        int64
	Attachments []Attachment
	// ListUnsubscribe is the raw List-Unsubscribe header value, and
	// ListUnsubscribePost whether the message declares RFC 8058 one-click
	// support via List-Unsubscribe-Post.
	ListUnsubscribe     string
	ListUnsubscribePost bool
	// ListID is the List-Id header value, '' when the message has none. It
	// marks mailing-list mail.
	ListID string
	// ReplyTo is the Reply-To header, and AuthResults every
	// Authentication-Results header in the order the message carries them: the
	// first was added by the receiving server and is the only trustworthy one.
	ReplyTo     string
	AuthResults []string
	// Claims are the sender headers the sender passport keeps.
	Claims Claims
	// CharsetGuess names the encoding a body part was read as when the message
	// declared none or declared one nothing knows. Empty for mail that was
	// right about itself, which is nearly all of it.
	CharsetGuess string
	// Header is the message's top-level header, for the fields Pelton reads
	// only in one place and so does not promote above (a client's own
	// X- headers, for instance).
	Header mail.Header
}

// Claims are what a message says about how it was sent. The sender or any hop
// can write them, so they are evidence to show, never a verdict.
type Claims struct {
	// ReturnPath and Sender are the bare addresses of those headers.
	ReturnPath string
	Sender     string
	// Signers is the d= domain of each DKIM-Signature, lowercased, once each.
	Signers []string
	// ReceivedSPF and ARC are the first Received-SPF and
	// ARC-Authentication-Results values, cut to claimCap.
	ReceivedSPF string
	ARC         string
	// ReadReceiptTo is the Disposition-Notification-To address: the message
	// asks for a read receipt there.
	ReadReceiptTo string
}

// claimCap keeps a stored header value short: the detail panel shows the start
// of it, and a hostile message could make it as long as it likes.
const claimCap = 400

// readClaims reads the sender headers out of a message header.
func readClaims(h *mail.Header) Claims {
	c := Claims{
		ReturnPath:    bareAddr(h.Get("Return-Path")),
		Sender:        bareAddr(addressList(h, "Sender")),
		ReadReceiptTo: bareAddr(addressList(h, "Disposition-Notification-To")),
	}
	if v := headerValues(h, "Received-SPF"); len(v) > 0 {
		c.ReceivedSPF = capText(v[0])
	}
	if v := headerValues(h, "ARC-Authentication-Results"); len(v) > 0 {
		c.ARC = capText(v[0])
	}
	for _, sig := range headerValues(h, "DKIM-Signature") {
		if d := dkimTag(sig, "d"); d != "" && !slices.Contains(c.Signers, d) {
			c.Signers = append(c.Signers, d)
		}
	}
	return c
}

// dkimTag returns one tag of a DKIM-Signature value, lowercased.
func dkimTag(sig, name string) string {
	for _, part := range strings.Split(sig, ";") {
		key, value, ok := strings.Cut(part, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.ToLower(strings.Join(strings.Fields(value), ""))
		}
	}
	return ""
}

// bareAddr returns the address of a header value like "Name <a@b>" or
// "<a@b>", lowercased; the first one when there are several.
func bareAddr(v string) string {
	s := strings.TrimSpace(v)
	if open := strings.Index(s, "<"); open >= 0 {
		if end := strings.Index(s[open:], ">"); end > 0 {
			return strings.ToLower(strings.TrimSpace(s[open+1 : open+end]))
		}
	}
	if comma := strings.Index(s, ","); comma >= 0 {
		s = s[:comma]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func capText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= claimCap {
		return s
	}
	cut := claimCap
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// unnamedGuess marks text that had to be guessed at when the encoding actually
// used is not known here. The decode happens inside the charset hook, which
// keeps the mixed-encoding header case right but has no way to report back
// which table it settled on.
const unnamedGuess = "detected"

// Attachment holds attachment metadata and its decoded content. ContentID is
// set for inline cid-referenced parts.
type Attachment struct {
	Filename    string
	ContentType string
	ContentID   string
	Content     []byte
}

// Parse reads a complete message, headers and all. A malformed date or address
// header is not fatal: the corresponding field is left empty, since a message
// that is readable apart from one bad header is still worth keeping.
func Parse(raw []byte) (*Message, error) {
	msg := &Message{Size: int64(len(raw))}
	mr, err := reader(raw)
	if err != nil {
		return nil, err
	}

	subject, _ := mr.Header.Subject()
	msg.Subject = charsetguess.Valid(subject)
	msg.MessageID, _ = mr.Header.MessageID()
	msg.From = addressList(&mr.Header, "From")
	msg.To = addressList(&mr.Header, "To")
	msg.Cc = addressList(&mr.Header, "Cc")
	if date, err := mr.Header.Date(); err == nil {
		msg.Date = date
	}

	if err := parts(mr, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// ParseBody reads the bodies, attachments and common metadata headers, including
// reply ancestry, for callers that already have the envelope from elsewhere
// (the imap server's ENVELOPE response).
func ParseBody(raw []byte) (*Message, error) {
	msg := &Message{Size: int64(len(raw))}
	mr, err := reader(raw)
	if err != nil {
		return nil, err
	}
	if err := parts(mr, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// reader opens a mail reader over raw. An unknown charset is reported by
// CreateReader but leaves the reader usable, so it is not treated as failure.
func reader(raw []byte) (*mail.Reader, error) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) {
		return nil, fmt.Errorf("rfc822: create mail reader: %w", err)
	}
	return mr, nil
}

// parts reads common metadata headers and walks the bodies and attachments.
func parts(mr *mail.Reader, msg *Message) error {
	msg.Header = mr.Header
	msg.ReplyTo = addressList(&mr.Header, "Reply-To")
	msg.AuthResults = headerValues(&mr.Header, "Authentication-Results")
	msg.Claims = readClaims(&mr.Header)
	msg.ListUnsubscribe = mr.Header.Get("List-Unsubscribe")
	msg.ListUnsubscribePost = strings.Contains(strings.ToLower(mr.Header.Get("List-Unsubscribe-Post")), "one-click")
	msg.ListID = charsetguess.Valid(strings.TrimSpace(mr.Header.Get("List-Id")))
	msg.InReplyTo = []string{}
	fields := mr.Header.FieldsByKey("In-Reply-To")
	count := 0
	for fields.Next() {
		count++
		ids, valid := messageid.ParseHeader(fields.Value())
		if count == 1 && valid {
			msg.InReplyTo = ids
		} else {
			msg.InReplyTo = []string{}
		}
	}
	msg.References = []string{}
	fields = mr.Header.FieldsByKey("References")
	for fields.Next() {
		ids, _ := messageid.ParseHeader(fields.Value())
		msg.References = append(msg.References, ids...)
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			return fmt.Errorf("rfc822: read part: %w", err)
		}

		header := part.Header
		if ih, ok := header.(*mail.InlineHeader); ok {
			if !isTextHeader(ih) || isCalendarHeader(ih) {
				header = &mail.AttachmentHeader{Header: ih.Header}
			}
		}

		switch header := header.(type) {
		case *mail.InlineHeader:
			body, err := io.ReadAll(part.Body)
			if err != nil {
				return fmt.Errorf("rfc822: read inline part: %w", err)
			}
			if bytes.IndexByte(body, 0) >= 0 {
				msg.Attachments = append(msg.Attachments, attachmentFrom(&mail.AttachmentHeader{Header: header.Header}, body))
				continue
			}
			contentType, params, _ := header.ContentType()
			// a part that named no charset was never converted, so what came
			// back is whatever the sender wrote. Nothing downstream can tell
			// the difference later, so it is decided here.
			text, guessed := charsetguess.Decode(body)
			if guessed == "" && !charsetguess.Known(params["charset"]) {
				// the part named something no table knows, so the text above
				// came back detected rather than converted. Which encoding was
				// picked is decided inside the decoder and does not travel back
				// here, so record that it was a guess without naming it.
				guessed = unnamedGuess
			}
			if guessed != "" && msg.CharsetGuess == "" {
				msg.CharsetGuess = guessed
			}
			if strings.EqualFold(contentType, "text/html") {
				if msg.HTML == "" {
					msg.HTML = text
				}
			} else if msg.Text == "" {
				msg.Text = text
			}
		case *mail.AttachmentHeader:
			content, err := io.ReadAll(part.Body)
			if err != nil {
				return fmt.Errorf("rfc822: read attachment part: %w", err)
			}
			msg.Attachments = append(msg.Attachments, attachmentFrom(header, content))
		}
	}
	return nil
}

// CalendarParts returns the decoded content of every iCalendar part in raw:
// a text/calendar part, which invitations usually carry inline as an
// alternative of the body and which the body walk above does not keep, and an
// application/ics or .ics attachment.
func CalendarParts(raw []byte) ([][]byte, error) {
	mr, err := reader(raw)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) {
			return out, fmt.Errorf("rfc822: read part: %w", err)
		}
		header := &mail.AttachmentHeader{Header: headerOf(part.Header)}
		contentType, _, _ := header.ContentType()
		filename, _ := header.Filename()
		if !IsCalendar(contentType, filename) {
			continue
		}
		body, err := io.ReadAll(part.Body)
		if err != nil {
			return out, fmt.Errorf("rfc822: read calendar part: %w", err)
		}
		out = append(out, body)
	}
	return out, nil
}

// IsCalendar reports whether a part's type or filename marks it as iCalendar
// data.
func IsCalendar(contentType, filename string) bool {
	contentType = strings.ToLower(contentType)
	return contentType == "text/calendar" || contentType == "application/ics" ||
		strings.HasSuffix(strings.ToLower(filename), ".ics")
}

func headerOf(h mail.PartHeader) message.Header {
	switch v := h.(type) {
	case *mail.InlineHeader:
		return v.Header
	case *mail.AttachmentHeader:
		return v.Header
	}
	return message.Header{}
}

// calendarFilename names an inline invitation kept as an attachment, which
// carries no filename of its own.
const calendarFilename = "invite.ics"

// isCalendarHeader reports whether an inline part is the text/calendar
// alternative an invitation carries. It is kept as an attachment rather than
// read as body text, so the invitation can be read again without the source.
func isCalendarHeader(h *mail.InlineHeader) bool {
	mediaType, _, _ := h.ContentType()
	return strings.EqualFold(mediaType, "text/calendar")
}

func attachmentFrom(header *mail.AttachmentHeader, content []byte) Attachment {
	filename, _ := header.Filename()
	contentType, _, _ := header.ContentType()
	if filename == "" && strings.EqualFold(contentType, "text/calendar") {
		filename = calendarFilename
	}
	return Attachment{
		Filename:    filename,
		ContentType: contentType,
		// content-id arrives wrapped in angle brackets, strip them
		ContentID: strings.Trim(header.Get("Content-Id"), "<>"),
		Content:   content,
	}
}

// isTextHeader reports whether an inline part can be a body. A part with no
// Content-Type defaults to text/plain, but one that names a file or carries a
// content id is a resource, not prose.
func isTextHeader(h *mail.InlineHeader) bool {
	if h.Get("Content-Type") == "" {
		name, _ := (&mail.AttachmentHeader{Header: h.Header}).Filename()
		return name == "" && h.Get("Content-Id") == ""
	}
	mediaType, _, _ := h.ContentType()
	return strings.HasPrefix(strings.ToLower(mediaType), "text/")
}

// addressList renders one address header as `Name <user@host>, ...`, matching
// how the imap path formats the server's ENVELOPE so both look the same in the
// message list. An unparseable header falls back to its raw value rather than
// dropping the sender entirely.
// headerValues returns every occurrence of a header, in order. go-message only
// exposes repeated fields through the field iterator, so Get is not enough for
// a header a message can carry once per hop.
func headerValues(h *mail.Header, key string) []string {
	var out []string
	fields := h.FieldsByKey(key)
	for fields.Next() {
		value, err := fields.Text()
		if err != nil {
			value = fields.Value()
		}
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func addressList(h *mail.Header, key string) string {
	addrs, err := h.AddressList(key)
	if err != nil {
		return charsetguess.Valid(strings.TrimSpace(h.Get(key)))
	}
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		switch {
		case a.Name != "" && a.Address != "":
			parts = append(parts, fmt.Sprintf("%s <%s>", a.Name, a.Address))
		case a.Address != "":
			parts = append(parts, a.Address)
		case a.Name != "":
			parts = append(parts, a.Name)
		}
	}
	return charsetguess.Valid(strings.Join(parts, ", "))
}
