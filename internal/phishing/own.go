package phishing

import "slices"

// authenticated reports whether the receiving server vouched for the From
// domain: dmarc passed, or spf or dkim passed for that same organisation.
func authenticated(auth Auth, from string) bool {
	if from == "" {
		return false
	}
	if auth.DMARC == ResultPass {
		return true
	}
	if auth.DKIM == ResultPass && sameOrg(auth.DKIMDomain, from) {
		return true
	}
	return auth.SPF == ResultPass && sameOrg(auth.SPFDomain, from)
}

// ownedBy reports whether host is one of the sender's own other domains, the
// ones an organisation keeps next to its main one:
//
//   - its short domain, an abbreviation of its name: spoti.fi for spotify.com,
//     amzn.to for amazon.de, nyti.ms for nytimes.com;
//   - the same name under another suffix: example.de for example.com;
//   - a shortener only it uses, like lnkd.in for linkedin.com.
//
// It is only asked about a sender that authentication vouches for, so nobody
// gets these by putting a famous name in the From line. And it never covers a
// shared shortener like bit.ly, where anyone's link can hide.
func ownedBy(host, from string) bool {
	h, f := registrable(host), registrable(from)
	if h == "" || f == "" {
		return false
	}
	if h == f {
		return true
	}
	if owners, ok := shortenerOwners[h]; ok {
		return slices.ContainsFunc(owners, func(o string) bool { return o == f })
	}
	if shorteners[h] {
		return false
	}
	hn, fn := nameOf(h), nameOf(f)
	if hn == "" || fn == "" {
		return false
	}
	return hn == fn || abbreviates(hn, fn)
}

// abbreviates reports whether short reads as a shortening of long: shorter,
// starting with the same letter, and made of long's letters in long's order.
// "amzn" shortens "amazon" and "spoti" shortens "spotify"; "zon" does not.
func abbreviates(short, long string) bool {
	if len(short) < 2 || len(short) >= len(long) || short[0] != long[0] {
		return false
	}
	i := 0
	for j := 0; j < len(long) && i < len(short); j++ {
		if short[i] == long[j] {
			i++
		}
	}
	return i == len(short)
}
