package phishing

import (
	"slices"
	"strings"
	"testing"
)

// correspondents is the user's actual contact list in these tests: the checks
// are specific to who this person writes to, not to a list of famous brands.
var correspondents = map[string]string{
	"anna@example.com":       "Anna Beck",
	"billing@paypal.com":     "PayPal Billing",
	"colleague@company.test": "Sam Rivers",
}

func kinds(r Report) []Kind {
	out := make([]Kind, 0, len(r.Signals))
	for _, s := range r.Signals {
		out = append(out, s.Kind)
	}
	return out
}

func hasKind(r Report, k Kind) bool {
	return slices.Contains(kinds(r), k)
}

// TestOrdinaryMailIsQuiet is the false-positive guard and the most important
// test here: a warning on ordinary mail is worse than no feature, because it
// teaches the reader to click past the banner.
func TestOrdinaryMailIsQuiet(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{
			name: "authenticated mail from a known contact",
			msg: Message{
				From: "anna@example.com", FromName: "Anna Beck",
				Auth: Auth{SPF: "pass", SPFDomain: "example.com", DKIM: "pass", DKIMDomain: "example.com", DMARC: "pass"},
				HTML: `<a href="https://example.com/news">read this</a>`,
			},
		},
		{
			name: "a newsletter with a reply-to elsewhere and a tracking link",
			msg: Message{
				From: "news@shop.example", FromName: "The Shop",
				ReplyTo: "no-reply@mailer.example",
				Auth:    Auth{SPF: "pass", SPFDomain: "mailer.example", DKIM: "pass", DKIMDomain: "shop.example", DMARC: "pass"},
				HTML:    `<a href="https://click.mailer.example/x/123">Shop now</a>`,
			},
		},
		{
			name: "mail with no authentication header at all",
			msg: Message{
				From: "someone@unrelated.test", FromName: "Someone",
				HTML: `<a href="https://unrelated.test/page">page</a>`,
			},
		},
		{
			name: "a subdomain sender, which is not a lookalike",
			msg: Message{
				From: "receipts@mail.paypal.com", FromName: "PayPal",
				Auth: Auth{DMARC: "pass"},
			},
		},
		{
			name: "sign-in link on the sender's own domain",
			msg: Message{
				From: "security@example.com", FromName: "Example Security",
				Auth: Auth{DMARC: "pass"},
				HTML: `<a href="https://example.com/account/login">Sign in</a>`,
			},
		},
		{
			name: "link text that is prose, not a domain",
			msg: Message{
				From: "anna@example.com", FromName: "Anna Beck",
				Auth: Auth{DMARC: "pass"},
				HTML: `<a href="https://cdn.example.net/a">click here now</a><a href="https://x.example.net/b">3.14</a>`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.msg.Correspondents = correspondents
			got := Analyse(tt.msg)
			if got.Level != LevelNone {
				t.Errorf("Level = %q with signals %v, want %q", got.Level, kinds(got), LevelNone)
			}
		})
	}
}

func TestSenderSignals(t *testing.T) {
	tests := []struct {
		name      string
		msg       Message
		wantKind  Kind
		wantLevel string
	}{
		{
			name: "dmarc fail",
			msg: Message{
				From: "billing@paypal.com", FromName: "PayPal",
				Auth: Auth{SPF: "fail", DMARC: "fail"},
			},
			wantKind: KindDMARCFail, wantLevel: LevelWarning,
		},
		{
			name: "spf fail without a dmarc verdict",
			msg: Message{
				From: "someone@example.com",
				Auth: Auth{SPF: "fail", SPFDomain: "elsewhere.test"},
			},
			wantKind: KindSPFFail, wantLevel: LevelCaution,
		},
		{
			name: "authenticated, but for another organisation",
			msg: Message{
				From: "ceo@company.test",
				Auth: Auth{DKIM: "pass", DKIMDomain: "bulk-sender.example"},
			},
			wantKind: KindUnaligned, wantLevel: LevelCaution,
		},
		{
			name: "the display name is a contact, the address is not",
			msg: Message{
				From: "sam.rivers@gmail.test", FromName: "Sam Rivers",
				Auth: Auth{DMARC: "pass"},
			},
			wantKind: KindDisplayNameSpoof, wantLevel: LevelWarning,
		},
		{
			name: "the display name is an address that is not the sender",
			msg: Message{
				From: "x7f2@mailer.example", FromName: "billing@paypal.com",
				Auth: Auth{DMARC: "pass"},
			},
			wantKind: KindDisplayNameSpoof, wantLevel: LevelWarning,
		},
		{
			name: "a domain one character off a contact's",
			msg: Message{
				From: "billing@paypa1.com", FromName: "PayPal Billing",
				Auth: Auth{DMARC: "pass"},
			},
			wantKind: KindLookalikeDomain, wantLevel: LevelWarning,
		},
		{
			name: "a punycode sender domain",
			msg: Message{
				From: "billing@xn--pypal-4ve.com", FromName: "PayPal",
				Auth: Auth{DMARC: "pass"},
			},
			// it is also a lookalike of a contact's domain once decoded, which
			// is the whole reason a sender writes an address in punycode.
			wantKind: KindPunycodeSender, wantLevel: LevelWarning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.msg.Correspondents = correspondents
			got := Analyse(tt.msg)
			if !hasKind(got, tt.wantKind) {
				t.Errorf("signals = %v, want one of kind %q", kinds(got), tt.wantKind)
			}
			if got.Level != tt.wantLevel {
				t.Errorf("Level = %q, want %q (signals %v)", got.Level, tt.wantLevel, kinds(got))
			}
		})
	}
}

// TestDMARCPassStopsTheMethodChecks: dmarc passing already means an aligned spf
// or dkim pass, so reporting the other method's failure on top would be noise
// about something the sender's own policy is satisfied with.
func TestDMARCPassStopsTheMethodChecks(t *testing.T) {
	got := Analyse(Message{
		From:           "news@shop.example",
		Auth:           Auth{SPF: "fail", SPFDomain: "bounce.example", DKIM: "pass", DKIMDomain: "shop.example", DMARC: "pass"},
		Correspondents: correspondents,
	})
	if got.Level != LevelNone {
		t.Errorf("Level = %q with signals %v, want none", got.Level, kinds(got))
	}
}

// TestAContactWhoChangedJobs is the case the wording has to survive: it looks
// exactly like a display-name spoof and sometimes it is one.
func TestAContactWhoChangedJobs(t *testing.T) {
	got := Analyse(Message{
		From: "sam@newplace.test", FromName: "Sam Rivers",
		Auth:           Auth{DMARC: "pass"},
		Correspondents: correspondents,
	})
	if !hasKind(got, KindDisplayNameSpoof) {
		t.Fatalf("signals = %v, want the display-name signal", kinds(got))
	}
	for _, s := range got.Signals {
		if s.Kind == KindDisplayNameSpoof && s.Detail != "colleague@company.test" {
			t.Errorf("detail = %q, want the address being impersonated", s.Detail)
		}
	}
}

func TestLinkSignals(t *testing.T) {
	tests := []struct {
		name       string
		msg        Message
		wantKind   Kind
		wantTarget string
	}{
		{
			name: "the text says one site and the link goes to another",
			msg: Message{
				From: "billing@paypal.com",
				HTML: `<a href="https://evil.test/pay">paypal.com</a>`,
			},
			wantKind: KindLinkTextMismatch, wantTarget: "https://evil.test/pay",
		},
		{
			name: "the text is a full url that is not the target",
			msg: Message{
				From: "billing@paypal.com",
				HTML: `<a href="https://evil.test/x">https://www.paypal.com/account</a>`,
			},
			wantKind: KindLinkTextMismatch, wantTarget: "https://evil.test/x",
		},
		{
			name: "a punycode link host",
			msg: Message{
				From: "a@example.com",
				HTML: `<a href="https://xn--pypal-4ve.com/login">Continue</a>`,
			},
			wantKind: KindPunycodeLink, wantTarget: "https://xn--pypal-4ve.com/login",
		},
		{
			name: "a shortener hiding the destination",
			msg: Message{
				From: "a@example.com",
				HTML: `<a href="https://bit.ly/3xY">Details</a><a href="https://t.co/abc">More</a>`,
			},
			wantKind: KindShortenedLink, wantTarget: "https://bit.ly/3xY",
		},
		{
			name: "a sign-in page on an unrelated domain",
			msg: Message{
				From: "security@paypal.com",
				HTML: `<a href="https://account-verify.test/secure/login">Verify your account</a>`,
			},
			wantKind: KindCredentialLink, wantTarget: "https://account-verify.test/secure/login",
		},
		{
			name: "a bare url in a plain-text body",
			msg: Message{
				From: "a@example.com",
				Text: "have a look at https://bit.ly/3xY when you can",
			},
			wantKind: KindShortenedLink, wantTarget: "https://bit.ly/3xY",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.msg.Correspondents = correspondents
			got := Analyse(tt.msg)
			if !hasKind(got, tt.wantKind) {
				t.Fatalf("signals = %v, want one of kind %q", kinds(got), tt.wantKind)
			}
			if !slices.Contains(got.Links, tt.wantTarget) {
				t.Errorf("Links = %v, want it to include %q", got.Links, tt.wantTarget)
			}
		})
	}
}

// TestLinksAreCapped: a newsletter with thousands of links must not turn one
// message open into a long scan.
func TestLinksAreCapped(t *testing.T) {
	var b strings.Builder
	for range maxLinks + 100 {
		b.WriteString(`<a href="https://bit.ly/x">go</a>`)
	}
	got := Analyse(Message{From: "a@example.com", HTML: b.String()})
	if len(got.Signals) > maxLinks {
		t.Errorf("produced %d signals, want at most the cap of %d", len(got.Signals), maxLinks)
	}
}

// TestNonWebLinksAreIgnored: mailto and cid cannot mislead about a destination.
func TestNonWebLinksAreIgnored(t *testing.T) {
	got := Analyse(Message{
		From: "a@example.com",
		HTML: `<a href="mailto:someone@else.test">paypal.com</a><a href="#top">bit.ly</a>`,
	})
	if got.Level != LevelNone {
		t.Errorf("Level = %q with signals %v, want none", got.Level, kinds(got))
	}
}

func TestLookalike(t *testing.T) {
	tests := []struct {
		candidate, known string
		want             bool
	}{
		{"paypa1.com", "paypal.com", true},
		{"paypaI.com", "paypal.com", true},
		{"pay-pal.com", "paypal.com", true},
		{"paypal.com", "paypal.com", false},
		{"mail.paypal.com", "paypal.com", false},
		{"paypal.com.evil.test", "paypal.com", true},
		{"example.org", "example.com", true},
		{"paypal.co", "paypal.com", true},
		{"bbc.co.uk", "abc.co.uk", true},
		{"totally-different.test", "paypal.com", false},
		{"ab.com", "ac.com", false},
	}
	for _, tt := range tests {
		if got := lookalike(tt.candidate, tt.known); got != tt.want {
			t.Errorf("lookalike(%q, %q) = %t, want %t", tt.candidate, tt.known, got, tt.want)
		}
	}
}

// TestSameNameAnotherSuffixIsOnlyACaution: an organisation owning both the .com
// and the .org is ordinary, so this must not accuse anybody on its own.
func TestSameNameAnotherSuffixIsOnlyACaution(t *testing.T) {
	got := Analyse(Message{
		From:           "hello@example.org",
		Auth:           Auth{DMARC: "pass"},
		Correspondents: correspondents,
	})
	if got.Level != LevelCaution {
		t.Errorf("Level = %q with signals %v, want %q", got.Level, kinds(got), LevelCaution)
	}
}

// TestNoCorrespondentsMeansNoLookalikes: a fresh install knows nobody, and the
// checks that depend on knowing the user must produce nothing rather than
// guessing from a list of brands.
func TestNoCorrespondentsMeansNoLookalikes(t *testing.T) {
	got := Analyse(Message{
		From: "billing@paypa1.com", FromName: "PayPal Billing",
		Auth: Auth{DMARC: "pass"},
	})
	if got.Level != LevelNone {
		t.Errorf("Level = %q with signals %v, want none", got.Level, kinds(got))
	}
}

// TestReportListsEachLinkOnce so the body renderer can mark them without
// deduplicating first.
func TestReportListsEachLinkOnce(t *testing.T) {
	got := Analyse(Message{
		From: "a@example.com",
		HTML: `<a href="https://bit.ly/x">one</a><a href="https://bit.ly/x">two</a><a href="https://t.co/y">three</a>`,
	})
	if len(got.Links) != 2 {
		t.Errorf("Links = %v, want two distinct urls", got.Links)
	}
}

func TestDomainOf(t *testing.T) {
	tests := []struct{ addr, want string }{
		{"me@Example.com", "example.com"},
		{"<me@example.com>", "example.com"},
		{"first@second@example.com", "example.com"},
		{"@example.com", "example.com"},
		{"me@example.com.", "example.com"},
		{"me@", ""},
		{"Example.COM", "example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := domainOf(tt.addr); got != tt.want {
			t.Errorf("domainOf(%q) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}
