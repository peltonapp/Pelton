package phishing

import (
	"errors"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// domainOf returns the lowercased domain part of an address, or the input
// itself when it is already a bare domain. Angle brackets and trailing dots go.
func domainOf(addr string) string {
	s := strings.ToLower(strings.TrimSpace(addr))
	s = strings.Trim(s, "<>")
	if _, after, ok := strings.CutLast(s, "@"); ok {
		s = after
	}
	return strings.Trim(s, ". ")
}

// registrable returns the domain a name is registered under, so mail.paypal.com
// and paypal.com compare as the same organisation while paypal.com.evil.tld does
// not. It falls back to the input when the public suffix list has nothing to
// say, which keeps unusual internal domains working.
func registrable(domain string) string {
	if domain == "" {
		return ""
	}
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		return etld1
	}
	return domain
}

// sameOrg reports whether two domains belong to the same registrable domain.
func sameOrg(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return registrable(a) == registrable(b)
}

// punycode reports whether any label of the domain is punycode, which is how an
// address made of lookalike characters reaches the wire. Not suspicious on its
// own: plenty of legitimate mail comes from internationalised domains.
func punycode(domain string) bool {
	for label := range strings.SplitSeq(domain, ".") {
		if strings.HasPrefix(label, "xn--") {
			return true
		}
	}
	return false
}

// unicodeDomain renders a punycode domain back to the characters it stands for,
// so a warning can show what the address is pretending to be rather than the
// encoded form nobody can read. Returns the input unchanged when it does not
// decode.
func unicodeDomain(domain string) string {
	decoded, err := idna.ToUnicode(domain)
	if err != nil {
		return domain
	}
	return decoded
}

// confusable folds the characters that exist to be mistaken for each other onto
// one representative, so paypa1.com and paypal.com compare equal. It is a small
// deliberate set: the digits and letters that are visually identical in the
// fonts a mail client uses, plus the cyrillic and greek letters that render the
// same as their latin counterparts.
var confusable = strings.NewReplacer(
	"0", "o", "1", "l", "3", "e", "4", "a", "5", "s", "7", "t", "8", "b",
	"i", "l", "rn", "m", "vv", "w",
	"а", "a", "с", "c", "е", "e", "ѕ", "s", "і", "l", "ј", "j", "о", "o",
	"р", "p", "ԛ", "q", "х", "x", "у", "y", "ь", "b", "һ", "h", "ԁ", "d",
	"ɑ", "a", "ο", "o", "ρ", "p", "ν", "v", "τ", "t", "ϲ", "c",
)

// skeleton is a domain reduced to what it looks like rather than what it is:
// unicode form, confusables folded, and the characters an eye skips over
// removed. Two domains with the same skeleton are visually the same string.
func skeleton(domain string) string {
	s := strings.ToLower(unicodeDomain(domain))
	s = confusable.Replace(s)
	return strings.NewReplacer("-", "", ".", "").Replace(s)
}

// lookalike reports whether candidate is trying to pass as known: the same
// string to the eye, the same name under another suffix, the known domain
// buried in a longer one, or one small edit away. An exact match is not a
// lookalike, it is the real thing, and so is a subdomain of it.
func lookalike(candidate, known string) bool {
	return lookalikeStrength(candidate, known) > 0
}

// lookalikeStrength grades the resemblance. A domain that reads as the known
// one, character for character, is a different thing from the same name under
// another suffix: plenty of organisations own both the .com and the .org, so
// that one is worth mentioning and not worth accusing anybody over.
func lookalikeStrength(candidate, known string) weight {
	c, k := registrable(candidate), registrable(known)
	if c == "" || k == "" || c == k {
		return 0
	}
	// the known domain sitting inside a longer one, as in paypal.com.evil.test.
	// Written that way it reads as the real domain to anyone scanning the
	// address, and it is not a subdomain of it: the registrable part is the
	// attacker's.
	if strings.Contains("."+strings.ToLower(candidate)+".", "."+k+".") {
		return strong
	}
	cs, ks := skeleton(c), skeleton(k)
	if cs == ks {
		return strong
	}
	// one edit apart only counts on names long enough for it to be a
	// substitution rather than a different word: at four characters, half the
	// short domains in the world are one edit from each other.
	if len(ks) >= 6 && editDistanceAtMost(cs, ks, 1) {
		return strong
	}
	// the same name under a different suffix, as in paypal.co for paypal.com.
	if cn, kn := nameOf(c), nameOf(k); cn != "" && cn == kn {
		return medium
	}
	return 0
}

// editDistanceAtMost reports whether a and b are within max insertions,
// deletions or substitutions of each other. It bails out as soon as the row
// minimum exceeds max, so a long pair of unrelated names costs little.
func editDistanceAtMost(a, b string, max int) bool {
	ra, rb := []rune(a), []rune(b)
	if abs(len(ra)-len(rb)) > max {
		return false
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		best := curr[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
			if curr[j] < best {
				best = curr[j]
			}
		}
		if best > max {
			return false
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)] <= max
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// nameOf returns a registrable domain without its public suffix: the label the
// owner actually chose. "paypal.co.uk" gives "paypal".
func nameOf(registrableDomain string) string {
	suffix, _ := publicsuffix.PublicSuffix(registrableDomain)
	name := strings.TrimSuffix(registrableDomain, "."+suffix)
	if name == registrableDomain {
		return ""
	}
	return name
}

// publicSuffixCheck reports whether a string is a plausible domain by asking
// the public suffix list, so anchor text like "3.14" or "e.g" is not read as a
// host.
func publicSuffixCheck(host string) (string, error) {
	// icann=false covers both a private suffix and a tld nobody has registered,
	// which is what "3.14" and "e.g" look like. Requiring the icann list keeps
	// prose from being read as a location.
	if _, icann := publicsuffix.PublicSuffix(host); !icann {
		return "", errNotADomain
	}
	return publicsuffix.EffectiveTLDPlusOne(host)
}

// errNotADomain marks a string the public suffix list does not recognise.
var errNotADomain = errors.New("phishing: not a registrable domain")
