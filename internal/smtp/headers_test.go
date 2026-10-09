package smtp

import (
	"net/mail"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/crypto"
)

// Every address header we generate has to parse back as the number of
// addresses we put in it. A display name carrying an rfc 5322 special, a comma
// above all, silently becomes a second address when it is not quoted: a strict
// server answers "multiple addresses in From header MUST have a Sender header"
// and a lax one delivers with the sender mangled. Round-tripping through
// net/mail is what catches it, so the tests do that rather than compare against
// a string somebody wrote out by hand.
func TestFormatAddressRoundTrips(t *testing.T) {
	cases := []struct {
		name    string
		display string
	}{
		{"plain", "Ann Sender"},
		{"trailing dot", "Arne K."},
		{"comma", "Kock, Arne"},
		{"comma and more", "Arne K., Stellar Foundry"},
		{"parentheses", "Support (Billing)"},
		{"angle brackets", "The <Team>"},
		{"quotes", `Say "hi"`},
		{"colon and semicolon", "Group: members;"},
		{"at sign", "arne@work"},
		{"backslash", `Path\Name`},
		{"non-ascii", "Anné Sénder"},
		{"non-ascii with comma", "Müller, Jürgen"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatAddress(Address{Name: tc.display, Email: "a@b.c"})

			list, err := mail.ParseAddressList(got)
			if err != nil {
				t.Fatalf("generated %q, which will not parse: %v", got, err)
			}
			if len(list) != 1 {
				t.Fatalf("generated %q, which parses as %d addresses", got, len(list))
			}
			if list[0].Address != "a@b.c" {
				t.Errorf("address came back as %q from %q", list[0].Address, got)
			}
			if list[0].Name != tc.display {
				t.Errorf("display name came back as %q, want %q (from %q)", list[0].Name, tc.display, got)
			}
		})
	}
}

// An address with no display name stays a bare address, which is what most
// recipients are and what the envelope uses.
func TestFormatAddressWithoutName(t *testing.T) {
	if got := formatAddress(Address{Email: "a@b.c"}); got != "a@b.c" {
		t.Errorf("got %q, want the bare address", got)
	}
}

// A list has to survive the same round trip: a comma in one recipient's name
// would otherwise split it into two recipients, so the mail goes somewhere
// nobody asked for.
func TestFormatAddressListRoundTrips(t *testing.T) {
	addrs := []Address{
		{Name: "Kock, Arne", Email: "arne@example.com"},
		{Email: "plain@example.com"},
		{Name: "Anné", Email: "anne@example.com"},
		{Name: "Support (Billing)", Email: "billing@example.com"},
	}
	got := formatAddressList(addrs)

	list, err := mail.ParseAddressList(got)
	if err != nil {
		t.Fatalf("generated %q, which will not parse: %v", got, err)
	}
	if len(list) != len(addrs) {
		t.Fatalf("generated %q, which parses as %d addresses, want %d", got, len(list), len(addrs))
	}
	for i, want := range addrs {
		if list[i].Address != want.Email {
			t.Errorf("address %d came back as %q, want %q", i, list[i].Address, want.Email)
		}
		if list[i].Name != want.Name {
			t.Errorf("name %d came back as %q, want %q", i, list[i].Name, want.Name)
		}
	}
}

// The header as it is actually written into the message, not just the helper's
// return, since that is what the server reads.
func TestBuiltFromHeaderIsOneAddress(t *testing.T) {
	msg := baseMessage()
	msg.From = Address{Name: "Kock, Arne", Email: "arne@example.com"}
	msg.To = []Address{{Name: "Müller, Jürgen", Email: "jm@example.com"}}

	raw, err := BuildRaw(msg, nil, crypto.ModeNone, crypto.Options{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("read built message: %v", err)
	}
	for _, header := range []string{"From", "To"} {
		list, err := parsed.Header.AddressList(header)
		if err != nil {
			t.Fatalf("%s header %q will not parse: %v", header, parsed.Header.Get(header), err)
		}
		if len(list) != 1 {
			t.Errorf("%s header %q parses as %d addresses, want 1", header, parsed.Header.Get(header), len(list))
		}
	}
}

func TestDomainOf(t *testing.T) {
	tests := []struct{ email, want string }{
		{"me@example.com", "example.com"},
		{"first@second@example.com", "example.com"},
		{"@example.com", "example.com"},
		{"me@", fallbackDomain},
		{"example.com", fallbackDomain},
		{"", fallbackDomain},
	}
	for _, tt := range tests {
		if got := domainOf(tt.email); got != tt.want {
			t.Errorf("domainOf(%q) = %q, want %q", tt.email, got, tt.want)
		}
	}
}
