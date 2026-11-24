package phishing

import "testing"

// TestLookalikeCorpus is a set of real-world style confusables, each against
// the domain it copies, and of hard negatives: real, different organisations
// whose names happen to be close.
func TestLookalikeCorpus(t *testing.T) {
	positives := []struct{ candidate, known string }{
		{"paypa1.com", "paypal.com"},
		{"rnicrosoft.com", "microsoft.com"},
		{"micros0ft.com", "microsoft.com"},
		{"g00gle.com", "google.com"},
		{"arnazon.com", "amazon.com"},
		{"xn--pypal-4ve.com", "paypal.com"},    // cyrillic a
		{"xn--80ak6aa92e.com", "apple.com"},    // all cyrillic "apple"
		{"paypla.com", "paypal.com"},           // swapped letters
		{"gooogle.com", "google.com"},          // doubled letter
		{"gogle.com", "google.com"},            // dropped letter
		{"linkedln.com", "linkedin.com"},       // l for i
		{"pay-pal.com", "paypal.com"},          // added hyphen
		{"paypal-secure.com", "paypal.com"},    // bait word
		{"secure-paypal.com", "paypal.com"},    // bait word in front
		{"paypalsupport.com", "paypal.com"},    // bait word, no hyphen
		{"paypal.com.evil.test", "paypal.com"}, // the real domain buried in another
		{"examp1e-bank.de", "example-bank.de"},
		{"anna-mueller.de", "annamueller.de"},
	}
	for _, tt := range positives {
		if got := lookalikeStrength(tt.candidate, tt.known); got != strong {
			t.Errorf("lookalikeStrength(%q, %q) = %d, want strong", tt.candidate, tt.known, got)
		}
	}

	negatives := []struct{ candidate, known string }{
		{"github.com", "gitlab.com"},
		{"netflix.com", "netlify.com"},
		{"spotify.com", "shopify.com"},
		{"notion.so", "motion.so"},
		{"zoom.us", "zoho.com"},
		{"apple.com", "apply.com"},
		{"slack.com", "stack.com"},
		{"medium.com", "podium.com"},
		{"uber.com", "huber.de"},
		{"mail.paypal.com", "paypal.com"},
		{"paypal.com", "paypal.com"},
		{"acme-billing.io", "acme-corp.io"},
		{"bookings.example", "booking.example"},
	}
	for _, tt := range negatives {
		if got := lookalikeStrength(tt.candidate, tt.known); got == strong {
			t.Errorf("lookalikeStrength(%q, %q) = strong, want a hard negative", tt.candidate, tt.known)
		}
	}
}

// TestLookalikeOfKnownSet: the check runs against who the user corresponds
// with, and a sender that is one of them is never an impostor of another.
func TestLookalikeOfKnownSet(t *testing.T) {
	known := []string{"anna@example-bank.de", "billing@paypal.com", "team@stripe.com"}
	tests := []struct {
		from, want string
		strong     bool
	}{
		{"service@paypa1.com", "paypal.com", true},
		{"alerts@examp1e-bank.de", "example-bank.de", true},
		{"hello@paypal.de", "paypal.com", false},
		{"team@stripe.com", "", false},
		{"someone@unrelated.test", "", false},
	}
	for _, tt := range tests {
		got, strong := LookalikeOf(tt.from, known)
		if got != tt.want || strong != tt.strong {
			t.Errorf("LookalikeOf(%q) = %q, %t; want %q, %t", tt.from, got, strong, tt.want, tt.strong)
		}
	}
	// two real organisations the user writes to are both real.
	both := []string{"billing@paypal.com", "info@paypa1.com"}
	if got, _ := LookalikeOf("info@paypa1.com", both); got != "" {
		t.Errorf("a correspondent was called a lookalike of another: %q", got)
	}
	if got, _ := LookalikeOf("x@paypa1.com", nil); got != "" {
		t.Errorf("no correspondents still produced %q", got)
	}
}

func TestMixedScript(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		{"xn--pypal-4ve.com", true},
		{"paypal.com", false},
		{"xn--80ak6aa92e.com", false},
		{"xn--mller-kva.de", false},
	}
	for _, tt := range tests {
		if got := MixedScript(tt.domain); got != tt.want {
			t.Errorf("MixedScript(%q) = %t, want %t", tt.domain, got, tt.want)
		}
	}
}
