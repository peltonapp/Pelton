package phishing

import (
	"strings"
	"testing"
)

var vouched = Auth{SPF: "pass", SPFDomain: "spotify.com", DKIM: "pass", DKIMDomain: "spotify.com", DMARC: "pass"}

// An organisation linking its own short domain, its own subdomains or its
// sister domains is not sending anybody somewhere else, and mail doing it was
// getting "something is wrong with this message".
func TestASendersOwnDomainsAreQuiet(t *testing.T) {
	cases := map[string]Message{
		"its short domain": {
			From: "no-reply@spotify.com", Auth: vouched,
			HTML: `<a href="https://spoti.fi/abc">spotify.com</a><a href="https://spoti.fi/def">open.spotify.com/track</a>`,
		},
		"an abbreviation of its name": {
			From: "store-news@amazon.de", Auth: Auth{DMARC: "pass"},
			HTML: `<a href="https://amzn.to/x1">amazon.de</a><a href="https://amzn.to/x2">www.amazon.de/deals</a><a href="https://amzn.to/x3/account/login">Sign in</a>`,
		},
		"its own shortener": {
			From: "messages-noreply@linkedin.com", Auth: Auth{DKIM: "pass", DKIMDomain: "linkedin.com"},
			HTML: `<a href="https://lnkd.in/a">View</a><a href="https://lnkd.in/b">Reply</a><a href="https://lnkd.in/c">See all</a>`,
		},
		"a subdomain in the text": {
			From: "news@example.com", Auth: Auth{DMARC: "pass"},
			HTML: `<a href="https://example.com/a">shop.example.com</a><a href="https://links.example.com/b">example.com</a>`,
		},
		"its sister domain": {
			From: "info@example.com", Auth: Auth{DMARC: "pass"},
			HTML: `<a href="https://example.de/x">example.com</a><a href="https://www.example.de/login">Log in</a>`,
		},
	}
	for name, msg := range cases {
		if got := Analyse(msg); got.Level != LevelNone {
			t.Errorf("%s: level %q with signals %v, want none", name, got.Level, kinds(got))
		}
	}
}

// The allowance is only for a sender authentication vouches for, and only for
// domains that read as its own.
func TestOwnDomainsNeedAnAuthenticatedSender(t *testing.T) {
	cases := map[string]Message{
		"unauthenticated claim": {
			From: "no-reply@spotify.com", Auth: Auth{DMARC: "fail"},
			HTML: `<a href="https://spoti.fi/abc">spotify.com</a>`,
		},
		"no authentication stated": {
			From: "no-reply@spotify.com",
			HTML: `<a href="https://spoti.fi/abc">spotify.com</a>`,
		},
		"a domain that does not abbreviate the sender": {
			From: "no-reply@spotify.com", Auth: vouched,
			HTML: `<a href="https://evil.test/abc">spotify.com</a>`,
		},
		"text naming somebody else": {
			From: "no-reply@spotify.com", Auth: vouched,
			HTML: `<a href="https://spoti.fi/abc">paypal.com</a>`,
		},
	}
	for name, msg := range cases {
		if got := Analyse(msg); !hasKind(got, KindLinkTextMismatch) {
			t.Errorf("%s: signals %v, want a link text mismatch", name, kinds(got))
		}
	}

	// a shortener anyone can use stays a hint, whoever sends it.
	shared := Analyse(Message{From: "no-reply@spotify.com", Auth: vouched, HTML: `<a href="https://bit.ly/x">Listen</a>`})
	if !hasKind(shared, KindShortenedLink) {
		t.Errorf("bit.ly from an authenticated sender: signals %v, want a shortened link", kinds(shared))
	}
	// lnkd.in is LinkedIn's, not everybody's.
	other := Analyse(Message{From: "a@example.com", Auth: Auth{DMARC: "pass"}, HTML: `<a href="https://lnkd.in/x">Read</a>`})
	if !hasKind(other, KindShortenedLink) {
		t.Errorf("lnkd.in from another sender: signals %v, want a shortened link", kinds(other))
	}
}

// Twenty links through one shortener are one finding, not twenty.
func TestRepeatedLinksCountOnce(t *testing.T) {
	var b strings.Builder
	for range 20 {
		b.WriteString(`<a href="https://bit.ly/x">Read more</a>`)
	}
	got := Analyse(Message{From: "news@example.com", HTML: b.String()})
	if got.Level != LevelNone {
		t.Errorf("level %q for one shortener used twenty times, want none", got.Level)
	}

	// two different shorteners are still two findings.
	two := Analyse(Message{From: "news@example.com", HTML: `<a href="https://bit.ly/x">a</a><a href="https://tinyurl.com/y">b</a>`})
	if two.Level != LevelCaution {
		t.Errorf("level %q for two shorteners, want caution", two.Level)
	}
}

func TestAbbreviates(t *testing.T) {
	for _, c := range []struct {
		short, long string
		want        bool
	}{
		{"amzn", "amazon", true},
		{"spoti", "spotify", true},
		{"nyti", "nytimes", true},
		{"fb", "facebook", true},
		{"zon", "amazon", false},
		{"amazon", "amazon", false},
		{"amazonx", "amazon", false},
		{"a", "amazon", false},
		{"pypl", "paypal", true},
	} {
		if got := abbreviates(c.short, c.long); got != c.want {
			t.Errorf("abbreviates(%q, %q) = %t, want %t", c.short, c.long, got, c.want)
		}
	}
}
