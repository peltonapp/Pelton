// Package contactmerge finds contacts that are probably the same person,
// combines them into one card without losing what the cards hold, and carries
// the merge out against the user's CardDAV servers so the survivor is updated
// and the others are deleted there too, with a record to put the originals
// back.
//
// Detection and combining are pure functions over stored contacts. Running a
// merge goes through the Remote interface, so the server side is tested with a
// fake and the real client satisfies it unchanged.
package contactmerge

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// minPhoneDigits is how many digits a number needs, after leading zeros, to be
// compared at all. Shorter ones are extensions and service codes that many
// people share.
const minPhoneDigits = 7

// freeMail lists mail providers whose domain says nothing about where a person
// works, so a shared one is not an overlap between two contacts.
var freeMail = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true,
	"live.com": true, "msn.com": true, "yahoo.com": true, "icloud.com": true, "me.com": true,
	"mac.com": true, "aol.com": true, "gmx.com": true, "gmx.de": true, "gmx.net": true,
	"web.de": true, "mail.com": true, "proton.me": true, "protonmail.com": true,
	"pm.me": true, "t-online.de": true, "posteo.de": true, "mailbox.org": true,
	"fastmail.com": true, "yandex.com": true, "zoho.com": true,
}

// normalizeEmail lowercases and trims an address.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// emailDomain returns the part after the last @, or empty.
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return email[at+1:]
}

// PhoneKey reduces a phone number to its digits, dropping a 00 or + prefix, so
// "+31 6 1234 5678" and "0031-6-12345678" compare equal. A number with too few
// digits to compare gives "".
func PhoneKey(s string) string {
	var digits strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	key := strings.TrimLeft(digits.String(), "0")
	if len(key) < minPhoneDigits {
		return ""
	}
	return key
}

// PhonesMatch says whether two numbers are the same line: equal digits, or one
// written nationally and the other with a country code in front of it.
func PhonesMatch(a, b string) bool {
	ka, kb := PhoneKey(a), PhoneKey(b)
	if ka == "" || kb == "" {
		return false
	}
	if len(ka) > len(kb) {
		ka, kb = kb, ka
	}
	return strings.HasSuffix(kb, ka)
}

// phoneBucket is what numbers are grouped by before they are compared: the
// last seven digits, which a national and an international form share.
func phoneBucket(key string) string {
	return key[len(key)-minPhoneDigits:]
}

// normalizeName folds a name for comparison: accents and case are dropped,
// punctuation becomes a space and the words are put in alphabetical order, so
// "Doe, Jörg" and "jorg DOE" are the same name. A name that is an address, or
// too short to mean anything, gives "".
func normalizeName(s string) string {
	if strings.Contains(s, "@") {
		return ""
	}
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(' ')
		}
	}
	words := strings.Fields(b.String())
	if len(words) == 0 {
		return ""
	}
	sort.Strings(words)
	name := strings.Join(words, " ")
	if len([]rune(name)) < 3 {
		return ""
	}
	return name
}

// normalizeOrg folds an organization the same way, without reordering.
func normalizeOrg(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
