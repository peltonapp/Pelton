package smtp

import (
	"crypto/rand"
	"fmt"
	"mime"
	"net/mail"
	"slices"
	"strings"
	"time"
)

const (
	headerFrom                    = "From"
	headerTo                      = "To"
	headerCc                      = "Cc"
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

// generateMessageID builds a unique Message-ID using the sender's domain so it
// looks like it came from that host.
func generateMessageID(senderEmail string) (string, error) {
	buf := make([]byte, messageIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("smtp: generate message id: %w", err)
	}
	return fmt.Sprintf("<%x@%s>", buf, domainOf(senderEmail)), nil
}

// domainOf returns the part after the @ in an address, or a safe fallback.
func domainOf(email string) string {
	if _, domain, ok := strings.CutLast(email, "@"); ok && domain != "" {
		return domain
	}
	return fallbackDomain
}

// referenceChain builds the References header value for a reply. The chain is
// the parent's existing references followed by the message being replied to, so
// the recipient's client can thread correctly. Folded onto continuation lines
// to keep each line within rfc length limits.
func referenceChain(references []string, inReplyTo string) string {
	chain := make([]string, 0, len(references)+1)
	chain = append(chain, references...)
	if inReplyTo != "" && !contains(chain, inReplyTo) {
		chain = append(chain, inReplyTo)
	}
	if len(chain) == 0 {
		return ""
	}
	// fold with crlf + space between identifiers, a valid header continuation.
	return strings.Join(chain, crlf+" ")
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}

// nowDate renders the current time in the rfc 5322 layout. Split out so callers
// and tests share one source of the date format.
func nowDate() string {
	return time.Now().Format(dateLayout)
}
