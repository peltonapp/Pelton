package smtp

import (
	"crypto/rand"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/peltonapp/Pelton/internal/messageid"
	"golang.org/x/net/idna"
)

const (
	headerFrom                    = "From"
	headerTo                      = "To"
	headerCc                      = "Cc"
	headerBcc                     = "Bcc"
	headerSubject                 = "Subject"
	headerDate                    = "Date"
	headerMessageID               = "Message-ID"
	headerInReplyTo               = "In-Reply-To"
	headerReferences              = "References"
	headerMIMEVersion             = "MIME-Version"
	headerUserAgent               = "User-Agent"
	headerContentType             = "Content-Type"
	headerContentTransferEncoding = "Content-Transfer-Encoding"
	headerContentDisposition      = "Content-Disposition"
	headerContentID               = "Content-ID"

	mimeVersion = "1.0"
	userAgent   = "Pelton"

	// dateLayout is the RFC 5322 date format.
	dateLayout = "Mon, 02 Jan 2006 15:04:05 -0700"

	// messageIDBytes is the entropy in the local part of a generated Message-ID.
	messageIDBytes = 16
	// fallbackDomain is used in a Message-ID when the sender address has no
	// domain part to borrow.
	fallbackDomain = "localhost"

	crlf = "\r\n"
)

// writeHeader appends one "Key: value" header line terminated with crlf.
func writeHeader(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString(crlf)
}

// encodeWord RFC2047-encodes a header value (a subject) when it
// contains non-ASCII, leaving plain ASCII untouched.
func encodeWord(s string) string {
	return mime.QEncoding.Encode("utf-8", s)
}

// formatAddress renders one address with its display name.
//
// The name goes through net/mail rather than being pasted in, because RFC 5322
// gives several characters a meaning inside an address list and a name carrying
// one has to be quoted. A comma is the dangerous one: "Kock, Arne" pasted in
// bare reads as two addresses, which a strict server rejects outright ("multiple
// addresses in From header MUST have a Sender header") and a lax one delivers
// with the sender mangled. Parentheses are a comment and would silently drop
// what is inside them. Address.String also handles the non-ascii case, encoding
// the name as an rfc 2047 word.
func formatAddress(a Address) string {
	if a.Name == "" {
		return a.Email
	}
	return (&mail.Address{Name: a.Name, Address: a.Email}).String()
}

// formatAddressList renders a comma-separated address header value.
func formatAddressList(addrs []Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, formatAddress(a))
	}
	return strings.Join(parts, ", ")
}

// NewMessageID builds a unique Message-ID using the sender's domain so it
// looks like it came from that host. Domains are IDNA-encoded; an invalid or
// oversized token uses a safe ASCII fallback.
func NewMessageID(senderEmail string) (string, error) {
	buf := make([]byte, messageIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("smtp: generate message id: %w", err)
	}
	id := fmt.Sprintf("<%x@%s>", buf, domainOf(senderEmail))
	if _, ok := messageid.Normalize(id); !ok {
		id = fmt.Sprintf("<%x@%s>", buf, fallbackDomain)
	}
	return id, nil
}

// domainOf returns the part after the @ in an address, or a safe fallback.
func domainOf(email string) string {
	email = strings.TrimSpace(email)
	i := strings.LastIndexByte(email, '@')
	if i < 0 || i == len(email)-1 {
		return fallbackDomain
	}
	domain := email[i+1:]
	// anything that cannot sit in a header, like a line break, falls back.
	if strings.ContainsFunc(domain, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == '<' || r == '>'
	}) {
		return fallbackDomain
	}
	ascii, err := idna.ToASCII(domain)
	if err != nil {
		return fallbackDomain
	}
	return ascii
}

// referenceChain validates and bounds reply ancestry, then folds between whole
// bracketed identifiers so unfolding produces single spaces between tokens.
func referenceChain(references []string, inReplyTo string) string {
	chain := messageid.References(references, inReplyTo)
	for i, id := range chain {
		chain[i], _ = messageid.Bracket(id)
	}
	return strings.Join(chain, crlf+" ")
}

// nowDate renders the current time in the rfc 5322 layout. Split out so callers
// and tests share one source of the date format.
func nowDate() string {
	return time.Now().Format(dateLayout)
}
