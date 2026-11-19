package phishing

import (
	"slices"
	"testing"
)

var gmail = ServerFor("me@gmail.com", "imap.gmail.com", "smtp.gmail.com")

func TestServerFor(t *testing.T) {
	tests := []struct {
		name               string
		email, imap, smtp  string
		trusted, untrusted []string
	}{
		{
			name:  "gmail names its receiving servers under google.com",
			email: "me@gmail.com", imap: "imap.gmail.com", smtp: "smtp.gmail.com",
			trusted:   []string{"mx.google.com", "MX.Google.com."},
			untrusted: []string{"", "google", "mx.google.com.evil.test", "evil.test", "mx.gmail-auth.com"},
		},
		{
			name:  "a self-hosted server trusts its own domain",
			email: "anna@example.org", imap: "mail.example.org", smtp: "mail.example.org",
			trusted:   []string{"mail.example.org", "mx2.example.org"},
			untrusted: []string{"mx.google.com", "example.org.evil.test"},
		},
		{
			name:  "an ip host falls back to the address",
			email: "anna@corp.test", imap: "192.168.1.5", smtp: "localhost",
			trusted:   []string{"mx.corp.test"},
			untrusted: []string{"192.168.1.5", "localhost"},
		},
		{
			name:  "icloud writes one header per method under icloud.com",
			email: "me@icloud.com", imap: "imap.mail.me.com", smtp: "smtp.mail.me.com",
			trusted: []string{"dmarc.icloud.com", "spf.icloud.com"},
		},
		{
			name:  "microsoft writes no authserv-id",
			email: "me@outlook.com", imap: "outlook.office365.com", smtp: "smtp.office365.com",
			trusted: []string{""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ServerFor(tt.email, tt.imap, tt.smtp)
			for _, id := range tt.trusted {
				if !s.Trusts(id) {
					t.Errorf("Trusts(%q) = false, want true (%+v)", id, s)
				}
			}
			for _, id := range tt.untrusted {
				if s.Trusts(id) {
					t.Errorf("Trusts(%q) = true, want false (%+v)", id, s)
				}
			}
		})
	}
}

func TestReadAuth(t *testing.T) {
	tests := []struct {
		name    string
		serv    AuthServ
		headers []string
		want    AuthRecord
	}{
		{
			name:    "no header at all",
			serv:    gmail,
			headers: nil,
			want:    AuthRecord{State: AuthNone},
		},
		{
			name: "the provider's header, folded over several lines",
			serv: gmail,
			headers: []string{"mx.google.com;\r\n       dkim=pass header.i=@example.com header.s=s1 header.b=abc;\r\n" +
				"\tspf=pass (google.com: domain of bounce@example.com designates 192.0.2.1 as permitted sender) smtp.mailfrom=bounce@example.com;\r\n" +
				"       dmarc=pass (p=QUARANTINE sp=NONE dis=NONE) header.from=example.com"},
			want: AuthRecord{
				State: AuthTrusted, ServID: "mx.google.com",
				SPF: "pass", SPFDomain: "example.com",
				DKIM: "pass", DKIMDomain: "example.com", DKIMDomains: []string{"example.com"},
				DMARC: "pass", DMARCPolicy: "quarantine", HeaderFrom: "example.com",
			},
		},
		{
			name: "a forged header further down cannot fill in what the provider said",
			serv: gmail,
			headers: []string{
				"mx.google.com; spf=softfail smtp.mailfrom=evil.test; dmarc=fail (p=REJECT) header.from=paypal.com",
				"evil.test; dkim=pass header.d=paypal.com; dmarc=pass header.from=paypal.com",
			},
			want: AuthRecord{
				State: AuthTrusted, ServID: "mx.google.com",
				SPF: "softfail", SPFDomain: "evil.test",
				DMARC: "fail", DMARCPolicy: "reject", HeaderFrom: "paypal.com",
			},
		},
		{
			name: "a forged copy of the provider's name below its real header loses to it",
			serv: gmail,
			headers: []string{
				"mx.google.com; dmarc=fail (p=REJECT) header.from=paypal.com",
				"mx.google.com; dmarc=pass header.from=paypal.com; dkim=pass header.d=paypal.com",
			},
			want: AuthRecord{
				State: AuthTrusted, ServID: "mx.google.com",
				DKIM: "pass", DKIMDomain: "paypal.com", DKIMDomains: []string{"paypal.com"},
				DMARC: "fail", DMARCPolicy: "reject", HeaderFrom: "paypal.com",
			},
		},
		{
			name: "a wrong authserv-id on top means nothing is trusted, even a lower one that looks right",
			serv: gmail,
			headers: []string{
				"relay.attacker.test; dmarc=pass header.from=paypal.com",
				"mx.google.com; dmarc=pass header.from=paypal.com",
			},
			want: AuthRecord{State: AuthUntrusted, ServID: "relay.attacker.test"},
		},
		{
			name: "a header with no authserv-id on a non-microsoft account",
			serv: gmail,
			headers: []string{
				"spf=pass smtp.mailfrom=paypal.com; dmarc=pass action=none header.from=paypal.com",
			},
			want: AuthRecord{State: AuthUntrusted},
		},
		{
			name: "microsoft's header without an authserv-id",
			serv: ServerFor("me@outlook.com", "outlook.office365.com", "smtp.office365.com"),
			headers: []string{
				"spf=pass (sender IP is 192.0.2.1) smtp.mailfrom=bank.test; dkim=pass (signature was verified) header.d=bank.test;dmarc=pass action=none header.from=bank.test;compauth=pass reason=100",
			},
			want: AuthRecord{
				State: AuthTrusted,
				SPF:   "pass", SPFDomain: "bank.test",
				DKIM: "pass", DKIMDomain: "bank.test", DKIMDomains: []string{"bank.test"},
				DMARC: "pass", HeaderFrom: "bank.test",
			},
		},
		{
			name: "icloud's split headers are read as one",
			serv: ServerFor("me@icloud.com", "imap.mail.me.com", "smtp.mail.me.com"),
			headers: []string{
				"dmarc.icloud.com; dmarc=pass header.from=example.com",
				"dkim-verifier.icloud.com; dkim=pass (2048-bit key) header.d=example.com header.i=@example.com",
				"spf.icloud.com; spf=pass (spf.icloud.com: domain of bounce@example.com designates 192.0.2.1 as permitted sender) smtp.mailfrom=bounce@example.com",
				"evil.test; spf=fail smtp.mailfrom=example.com",
			},
			want: AuthRecord{
				State: AuthTrusted, ServID: "dmarc.icloud.com",
				SPF: "pass", SPFDomain: "example.com",
				DKIM: "pass", DKIMDomain: "example.com", DKIMDomains: []string{"example.com"},
				DMARC: "pass", HeaderFrom: "example.com",
			},
		},
		{
			name: "several signatures: every passing signer is kept",
			serv: gmail,
			headers: []string{
				"mx.google.com; dkim=fail header.d=example.com; dkim=pass header.d=esp.test; dkim=pass header.d=mail.example.com; dkim=pass header.d=esp.test",
			},
			want: AuthRecord{
				State: AuthTrusted, ServID: "mx.google.com",
				DKIM: "pass", DKIMDomain: "esp.test", DKIMDomains: []string{"esp.test", "mail.example.com"},
			},
		},
		{
			name: "fastmail's published policy",
			serv: ServerFor("me@fastmail.com", "imap.fastmail.com", "smtp.fastmail.com"),
			headers: []string{
				"mx3.messagingengine.com; dmarc=fail policy.published-domain-policy=reject policy.applied-disposition=quarantine header.from=bank.test",
			},
			want: AuthRecord{State: AuthTrusted, ServID: "mx3.messagingengine.com", DMARC: "fail", DMARCPolicy: "reject", HeaderFrom: "bank.test"},
		},
		{
			name:    "junk does not panic",
			serv:    gmail,
			headers: []string{";;;=;=spf", "  "},
			want:    AuthRecord{State: AuthUntrusted},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadAuth(tt.headers, tt.serv)
			if !sameRecord(got, tt.want) {
				t.Errorf("ReadAuth() =\n %+v\nwant\n %+v", got, tt.want)
			}
		})
	}
}

func sameRecord(a, b AuthRecord) bool {
	return a.State == b.State && a.ServID == b.ServID && a.SPF == b.SPF && a.SPFDomain == b.SPFDomain &&
		a.DKIM == b.DKIM && a.DKIMDomain == b.DKIMDomain && slices.Equal(a.DKIMDomains, b.DKIMDomains) &&
		a.DMARC == b.DMARC && a.DMARCPolicy == b.DMARCPolicy && a.HeaderFrom == b.HeaderFrom
}

func TestAlignment(t *testing.T) {
	tests := []struct {
		name     string
		rec      AuthRecord
		from     string
		verified bool
		sending  string
	}{
		{
			name:     "dmarc pass for the from domain",
			rec:      AuthRecord{State: AuthTrusted, DMARC: "pass", HeaderFrom: "example.com", DKIMDomains: []string{"example.com"}},
			from:     "anna@example.com",
			verified: true, sending: "example.com",
		},
		{
			name:     "a subdomain signature aligns under relaxed alignment",
			rec:      AuthRecord{State: AuthTrusted, DKIM: "pass", DKIMDomains: []string{"mail.example.com"}},
			from:     "anna@example.com",
			verified: true, sending: "example.com",
		},
		{
			name:     "spf passing for the from domain",
			rec:      AuthRecord{State: AuthTrusted, SPF: "pass", SPFDomain: "bounce.example.com"},
			from:     "news@example.com",
			verified: true, sending: "example.com",
		},
		{
			name:     "a signature from the newsletter service only",
			rec:      AuthRecord{State: AuthTrusted, DKIM: "pass", DKIMDomains: []string{"esp.test"}, SPF: "pass", SPFDomain: "esp.test"},
			from:     "news@example.com",
			verified: false, sending: "esp.test",
		},
		{
			name:     "dmarc pass reported for another from domain",
			rec:      AuthRecord{State: AuthTrusted, DMARC: "pass", HeaderFrom: "other.test"},
			from:     "anna@example.com",
			verified: false,
		},
		{
			name:     "an untrusted record verifies nothing",
			rec:      AuthRecord{State: AuthUntrusted, DMARC: "pass", HeaderFrom: "example.com", DKIMDomains: []string{"example.com"}},
			from:     "anna@example.com",
			verified: false,
		},
		{
			name:     "a lookalike of the signer is not aligned",
			rec:      AuthRecord{State: AuthTrusted, DKIM: "pass", DKIMDomains: []string{"example.com.evil.test"}},
			from:     "anna@example.com",
			verified: false, sending: "evil.test",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rec.Verified(tt.from); got != tt.verified {
				t.Errorf("Verified() = %t, want %t", got, tt.verified)
			}
			if got := tt.rec.SendingDomain(tt.from); got != tt.sending {
				t.Errorf("SendingDomain() = %q, want %q", got, tt.sending)
			}
		})
	}
}

func TestRecordAuthIsEmptyUnlessTrusted(t *testing.T) {
	rec := AuthRecord{State: AuthUntrusted, DMARC: "pass"}
	if rec.Auth().Stated() {
		t.Error("an untrusted record handed its results to the checks")
	}
	rec.State = AuthTrusted
	if rec.Auth().DMARC != "pass" {
		t.Error("a trusted record lost its result")
	}
}
