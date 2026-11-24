package autocrypt

import (
	"bufio"
	"bytes"
	"mime"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Incoming is what an arriving message contributes to Autocrypt state, read
// from its outer headers only.
type Incoming struct {
	// From is the single sender address, lowercased.
	From string
	// Date is the message's Date header, zero when missing or unreadable.
	Date time.Time
	// Headers holds the raw values of every Autocrypt header.
	Headers []string
}

// ReadIncoming reads the header block of a raw message. It reports false for a
// message Autocrypt must ignore: one with no single From address, or a
// delivery report, whose headers describe some other message.
func ReadIncoming(raw []byte) (Incoming, bool) {
	header, ok := readHeader(raw)
	if !ok {
		return Incoming{}, false
	}
	if mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type")); err == nil && mediaType == "multipart/report" {
		return Incoming{}, false
	}
	from, err := mail.ParseAddressList(header.Get("From"))
	if err != nil || len(from) != 1 {
		return Incoming{}, false
	}
	in := Incoming{From: NormalizeAddr(from[0].Address), Headers: header.Values(HeaderName)}
	if d, err := mail.ParseDate(header.Get("Date")); err == nil {
		in.Date = d
	}
	return in, true
}

// GossipHeaders returns the Autocrypt-Gossip headers in the header block of a
// decrypted message body whose address is one of the message's recipients.
// Gossip about anyone else is ignored, since the sender had no reason to tell
// this recipient about them.
func GossipHeaders(entity []byte, recipients []string) []Header {
	header, ok := readHeader(entity)
	if !ok {
		return nil
	}
	allowed := make(map[string]bool, len(recipients))
	for _, r := range recipients {
		allowed[NormalizeAddr(r)] = true
	}
	var out []Header
	for _, v := range header.Values(GossipHeaderName) {
		h, err := ParseGossip(v)
		if err != nil || !allowed[h.Addr] {
			continue
		}
		out = append(out, h)
	}
	return out
}

// readHeader parses the header block at the start of a message or entity.
func readHeader(raw []byte) (textproto.MIMEHeader, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw)))
	header, err := r.ReadMIMEHeader()
	if err != nil && len(header) == 0 {
		return nil, false
	}
	return header, true
}

// AddressesIn parses an address list header value into lowercased addresses,
// skipping anything unreadable.
func AddressesIn(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	list, err := mail.ParseAddressList(value)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, NormalizeAddr(a.Address))
	}
	return out
}
