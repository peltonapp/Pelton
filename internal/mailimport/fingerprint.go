package mailimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Fingerprint identifies a message for deduplication: its Message-ID together
// with a hash of what it says. The same message read from two stores matches
// even though each store added headers of its own (Thunderbird's status
// headers, a Maildir's delivery lines), because only the Message-ID, the
// sender, recipients, date and subject, and the body with its line endings
// and trailing spaces evened out go in. Two different messages that share a
// Message-ID, which broken senders produce, stay apart.
func Fingerprint(raw []byte) string {
	head, body := splitHeader(raw)
	h := sha256.New()
	id := strings.ToLower(strings.Trim(strings.TrimSpace(headerValue(head, "Message-ID")), "<>"))
	h.Write([]byte("id\x00" + id + "\x00"))
	for _, name := range []string{"From", "To", "Cc", "Date", "Subject"} {
		h.Write([]byte(name + "\x00" + strings.Join(strings.Fields(headerValue(head, name)), " ") + "\x00"))
	}
	h.Write([]byte("body\x00"))
	pending := 0
	for len(body) > 0 {
		line := body
		if i := bytes.IndexByte(body, '\n'); i >= 0 {
			line, body = body[:i], body[i+1:]
		} else {
			body = nil
		}
		line = bytes.TrimRight(line, " \t\r")
		if len(line) == 0 {
			pending++
			continue
		}
		for ; pending > 0; pending-- {
			h.Write([]byte("\n"))
		}
		h.Write(line)
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
