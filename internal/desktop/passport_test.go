package desktop

import (
	"slices"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/storage"
)

var passed = storage.MessageTrust{AuthState: phishing.AuthTrusted, ServID: "mx.google.com", DMARC: "pass",
	HeaderFrom: "example.com", DKIM: "pass", DKIMDomains: []string{"example.com"}}

func TestPassportStates(t *testing.T) {
	known := map[string]string{
		"anna.mueller@company.test": "Anna Müller",
		"billing@paypal.com":        "PayPal Billing",
	}
	tests := []struct {
		name     string
		in       passportInput
		state    string
		reason   string
		first    bool
		warnings []string
	}{
		{
			name:  "verified with a long correspondence",
			in:    passportInput{Address: "anna@example.com", Trust: passed, Received: 10, Sent: 13},
			state: passportVerified,
		},
		{
			name:  "verified, first mail",
			in:    passportInput{Address: "news@example.com", Trust: passed, Received: 1},
			state: passportVerified, first: true,
		},
		{
			name:  "synced before the record existed",
			in:    passportInput{Address: "anna@example.com", Received: 1},
			state: passportUnchecked, reason: reasonLegacy, first: true,
		},
		{
			name:  "the server added no header",
			in:    passportInput{Address: "anna@example.com", Trust: storage.MessageTrust{AuthState: phishing.AuthNone}, Received: 3},
			state: passportUnchecked, reason: reasonNone,
		},
		{
			name:  "the only header came from somewhere else",
			in:    passportInput{Address: "anna@example.com", Trust: storage.MessageTrust{AuthState: phishing.AuthUntrusted, ServID: "evil.test"}, Received: 3},
			state: passportUnchecked, reason: reasonUntrusted,
		},
		{
			name: "signed only by the newsletter service",
			in: passportInput{Address: "news@example.com", Received: 4, Trust: storage.MessageTrust{
				AuthState: phishing.AuthTrusted, DKIM: "pass", DKIMDomains: []string{"esp.test"}, SPF: "pass", SPFDomain: "esp.test"}},
			state: passportUnverified, reason: reasonOtherDomain,
		},
		{
			name: "checked, nothing passed",
			in: passportInput{Address: "x@example.com", Received: 4, Trust: storage.MessageTrust{
				AuthState: phishing.AuthTrusted, SPF: "softfail", SPFDomain: "example.com"}},
			state: passportUnverified, reason: reasonNoPass,
		},
		{
			name: "dmarc failed with a reject policy",
			in: passportInput{Address: "service@paypal.com", Received: 1, Trust: storage.MessageTrust{
				AuthState: phishing.AuthTrusted, DMARC: "fail", DMARCPolicy: "reject", HeaderFrom: "paypal.com"}},
			state: passportFailed, first: true, warnings: []string{warnDMARC},
		},
		{
			name: "dmarc failed under a policy of none is not a banner",
			in: passportInput{Address: "service@shop.test", Received: 5, Trust: storage.MessageTrust{
				AuthState: phishing.AuthTrusted, DMARC: "fail", DMARCPolicy: "none"}},
			state: passportFailed,
		},
		{
			name:  "a correspondent's name at another organisation",
			in:    passportInput{Address: "anna.mueller.ceo@gmail.test", Name: "Anna Müller", Trust: passed, Received: 1, Known: known},
			state: passportLookalike, first: true, warnings: []string{warnLookalike},
		},
		{
			name:  "a correspondent's domain, one letter off",
			in:    passportInput{Address: "service@paypa1.com", Name: "PayPal", Trust: passed, Received: 1, Known: known},
			state: passportLookalike, first: true, warnings: []string{warnLookalike},
		},
		{
			name:  "the user said they know this address",
			in:    passportInput{Address: "anna.mueller.ceo@gmail.test", Name: "Anna Müller", Trust: passed, Received: 1, Known: known, Mark: storage.SenderKnown},
			state: passportUnverified, reason: reasonOtherDomain, first: true,
		},
		{
			name:  "the user said they are not who they claim",
			in:    passportInput{Address: "anna@example.com", Trust: passed, Received: 8, Mark: storage.SenderImpostor},
			state: passportImpostor, warnings: []string{warnImpostor},
		},
		{
			name:  "a reply going elsewhere on first contact",
			in:    passportInput{Address: "ceo@example.com", ReplyTo: "ceo.private@other.test", Trust: passed, Received: 1},
			state: passportVerified, first: true, warnings: []string{warnReplyFirst},
		},
		{
			name:  "a reply going elsewhere from someone known",
			in:    passportInput{Address: "news@example.com", ReplyTo: "help@other.test", Trust: passed, Received: 9, Sent: 1},
			state: passportVerified,
		},
		{
			name:  "the user's own address imitates nobody",
			in:    passportInput{Address: "me@paypa1.com", Trust: passed, Received: 1, Known: known, Own: []string{"me@paypa1.com"}},
			state: passportUnverified, reason: reasonOtherDomain, first: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPassport(tt.in)
			if got.State != tt.state || got.Reason != tt.reason {
				t.Errorf("state = %q/%q, want %q/%q", got.State, got.Reason, tt.state, tt.reason)
			}
			if got.First != tt.first {
				t.Errorf("first = %t, want %t", got.First, tt.first)
			}
			if !slices.Equal(got.Warnings, nonNil(tt.warnings)) {
				t.Errorf("warnings = %v, want %v", got.Warnings, tt.warnings)
			}
		})
	}
}

func TestPassportHistory(t *testing.T) {
	day := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	viaESP := storage.MessageTrust{AuthState: phishing.AuthTrusted, DKIM: "pass", DKIMDomains: []string{"esp.test"}}
	history := []storage.TrustHistoryEntry{
		{ID: 2, Date: day.AddDate(0, 0, -1), Trust: passed},
		{ID: 1, Date: day.AddDate(0, -3, 0), ReplyTo: "anna@example.com", Trust: passed},
	}

	same := buildPassport(passportInput{Address: "anna@example.com", Trust: passed, Received: 3, History: history,
		FirstReceived: day.AddDate(0, -3, 0), Sent: 2, FirstSent: day.AddDate(-1, 0, 0)})
	if !same.SameDomain || len(same.Changes) != 0 {
		t.Errorf("same domain = %t, changes = %+v; want the same domain and no change", same.SameDomain, same.Changes)
	}
	if same.Since != formatDate(day.AddDate(-1, 0, 0)) {
		t.Errorf("since = %q, want the first mail either way", same.Since)
	}

	moved := buildPassport(passportInput{Address: "anna@example.com", Trust: viaESP, ReplyTo: "Anna <anna@elsewhere.test>",
		Received: 3, History: history})
	kinds := []string{}
	for _, c := range moved.Changes {
		kinds = append(kinds, c.Kind)
	}
	if !slices.Equal(kinds, []string{changeDomain, changeReplyTo}) || moved.SameDomain {
		t.Fatalf("changes = %+v, want a new sending domain and a new reply address", moved.Changes)
	}
	if moved.Changes[0].Detail != "esp.test" || moved.Changes[0].Before != "example.com" {
		t.Errorf("domain change = %+v", moved.Changes[0])
	}

	known := map[string]string{"anna.mueller@company.test": "Anna Müller"}
	alias := buildPassport(passportInput{Address: "a.mueller@company.test", Name: "Anna Müller", Trust: passed, Received: 1, Known: known})
	if alias.Lookalike != nil || len(alias.Changes) != 1 || alias.Changes[0].Kind != changeName || alias.Changes[0].Before != "anna.mueller@company.test" {
		t.Errorf("a second address at the same organisation = %+v / %+v, want a name change, not a lookalike", alias.Lookalike, alias.Changes)
	}

	none := buildPassport(passportInput{Address: "anna@example.com", Trust: passed, Received: 1})
	if none.SameDomain || len(none.Changes) != 0 {
		t.Errorf("no history = %+v", none)
	}
}

// TestPassportThroughTheStore runs the binding over a seeded store: mail from
// a sender, mail to them, and the record sync would have written.
func TestPassportThroughTheStore(t *testing.T) {
	a := newTrashTestApp(t)
	forgetKnownPeople()
	t.Cleanup(forgetKnownPeople)
	accountID, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@gmail.com", IMAPHost: "imap.gmail.com"})
	if err != nil {
		t.Fatal(err)
	}
	inbox := seedFolder(t, a, accountID, "INBOX", "INBOX", nil, 0)
	sent := seedFolder(t, a, accountID, "Sent", "Sent", []string{`\Sent`}, 0)
	day := time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)
	insert := func(folder storage.Folder, uid uint32, from, to string, at time.Time) int64 {
		id, err := a.store.InsertMessage(a.ctx, &storage.Message{AccountID: accountID, FolderID: folder.ID, UID: uid,
			FromAddress: from, ToAddresses: to, Date: at})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	earlier := insert(inbox, 1, "Anna Beck <anna@example.com>", "me@gmail.com", day)
	insert(sent, 2, "me@gmail.com", "Anna Beck <anna@example.com>, hanna@example.com", day.Add(time.Hour))
	insert(sent, 3, "me@gmail.com", "hanna@example.com", day.Add(2*time.Hour))
	latest := insert(inbox, 4, "Anna Beck <anna@example.com>", "me@gmail.com", day.AddDate(0, 1, 0))
	for _, id := range []int64{earlier, latest} {
		rec := passed
		rec.MessageID = id
		if err := a.store.SetMessageTrust(a.ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	p, err := a.GetSenderPassport(latest)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != passportVerified || p.Received != 2 || p.Sent != 1 || p.First || !p.SameDomain {
		t.Errorf("passport = %+v, want verified, two received, one sent and the same domain", p)
	}
	if p.Banner.Level != phishing.LevelNone {
		t.Errorf("banner = %+v, want nothing for ordinary mail", p.Banner)
	}

	if err := a.SetSenderMark("Anna Beck <anna@example.com>", storage.SenderImpostor); err != nil {
		t.Fatal(err)
	}
	p, err = a.GetSenderPassport(latest)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != passportImpostor || p.Banner.Level != phishing.LevelWarning {
		t.Errorf("after marking = %+v, want impostor and a banner", p)
	}
}
