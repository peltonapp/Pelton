package mailimport

import (
	"encoding/base64"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// displayName turns a name read off disk into readable text. File systems
// that store bytes rather than text (Linux, mostly) can hand back a name in
// an old 8-bit encoding; it is read as Latin-1 rather than shown as
// replacement characters.
func displayName(name string) string {
	if utf8.ValidString(name) {
		return name
	}
	runes := make([]rune, 0, len(name))
	for i := 0; i < len(name); i++ {
		runes = append(runes, rune(name[i]))
	}
	return string(runes)
}

// mailboxName decodes an IMAP folder name in modified UTF-7 (RFC 3501
// 5.1.3), the form Dovecot and Courier give Maildir++ folders on disk
// (".Entw&APw-rfe"). A name that is not valid modified UTF-7 comes back as
// it was.
func mailboxName(name string) string {
	if !strings.Contains(name, "&") {
		return displayName(name)
	}
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c != '&' {
			b.WriteByte(c)
			continue
		}
		end := strings.IndexByte(name[i+1:], '-')
		if end < 0 {
			return displayName(name)
		}
		chunk := name[i+1 : i+1+end]
		i += end + 1
		if chunk == "" {
			b.WriteByte('&')
			continue
		}
		raw, err := base64.RawStdEncoding.DecodeString(strings.ReplaceAll(chunk, ",", "/"))
		if err != nil || len(raw)%2 != 0 {
			return displayName(name)
		}
		units := make([]uint16, len(raw)/2)
		for j := range units {
			units[j] = uint16(raw[2*j])<<8 | uint16(raw[2*j+1])
		}
		b.WriteString(string(utf16.Decode(units)))
	}
	return b.String()
}
