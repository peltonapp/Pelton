package desktop

import (
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/storage"
)

// The sender passport: a plain verdict on who sent a message, from what the
// user's own server recorded and what the local mail says about the sender.
// Everything here is computed from data already on the machine; nothing is
// looked up. The wording lives in the ui, keyed by State and Reason.

// Passport states, strongest first. Verified says only that the sending
// domain was confirmed, never that the person or the content is genuine.
const (
	passportImpostor   = "impostor"
	passportFailed     = "failed"
	passportLookalike  = "lookalike"
	passportVerified   = "verified"
	passportUnverified = "unverified"
	passportUnchecked  = "unchecked"
)

// Reasons for unchecked and unverified.
const (
	// reasonLegacy: the message was synced before Pelton kept the record.
	reasonLegacy = "legacy"
	// reasonNone: the server added no Authentication-Results header.
	reasonNone = "none"
	// reasonUntrusted: the topmost header came from another server.
	reasonUntrusted = "untrusted"
	// reasonOtherDomain: something passed, but for another domain.
	reasonOtherDomain = "other_domain"
	// reasonNoPass: the server checked, and nothing passed.
	reasonNoPass = "no_pass"
)

// Warnings strong enough for the banner and for friction on reply and links.
const (
	warnImpostor   = "impostor"
	warnLookalike  = "lookalike"
	warnDMARC      = "dmarc_policy"
	warnReplyFirst = "reply_first"
)

// Change kinds: something about the sender is different from before.
const (
	changeDomain  = "new_domain"
	changeReplyTo = "new_reply_to"
	changeName    = "name_elsewhere"
)

// PassportChangeDTO is one thing that changed about a sender. Detail is the
// new value; Before what it was, when there was one.
type PassportChangeDTO struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
	Before string `json:"before,omitempty"`
}

// PassportLookalikeDTO names who the sender looks like. Kind is "name" (a
// correspondent's display name at another address) or "domain" (a domain
// resembling a correspondent's); Of is that name or domain and Address the
// correspondent's real address, when the match was a person.
type PassportLookalikeDTO struct {
	Kind    string `json:"kind"`
	Of      string `json:"of"`
	Address string `json:"address,omitempty"`
}

// PassportEvidenceDTO is the technical record behind a verdict, for those
// who want it.
type PassportEvidenceDTO struct {
	AuthState     string   `json:"authState"`
	ServID        string   `json:"servId"`
	SPF           string   `json:"spf"`
	SPFDomain     string   `json:"spfDomain"`
	DKIM          string   `json:"dkim"`
	DKIMDomains   []string `json:"dkimDomains"`
	DMARC         string   `json:"dmarc"`
	DMARCPolicy   string   `json:"dmarcPolicy"`
	HeaderFrom    string   `json:"headerFrom"`
	AlignedSPF    bool     `json:"alignedSpf"`
	AlignedDKIM   bool     `json:"alignedDkim"`
	SendingDomain string   `json:"sendingDomain"`
	ReturnPath    string   `json:"returnPath"`
	Sender        string   `json:"sender"`
	Signers       []string `json:"signers"`
	ReceivedSPF   string   `json:"receivedSpf"`
	ARC           string   `json:"arc"`
	MixedScript   bool     `json:"mixedScript"`
}

// PassportDTO is the passport of one message.
type PassportDTO struct {
	Address string `json:"address"`
	// Domain is the From domain as the reader should see it, decoded from
	// punycode.
	Domain string `json:"domain"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	// Mark is what the user said about the address: known, impostor or ''.
	Mark string `json:"mark"`
	// First is the first mail from this address, with nothing sent to it.
	First    bool   `json:"first"`
	Received int    `json:"received"`
	Sent     int    `json:"sent"`
	Since    string `json:"since"`
	Contact  bool   `json:"contact"`
	// SameDomain: earlier mail from this address came through the same
	// authenticated sending domain. False when there is no earlier record.
	SameDomain bool                  `json:"sameDomain"`
	Changes    []PassportChangeDTO   `json:"changes"`
	Lookalike  *PassportLookalikeDTO `json:"lookalike"`
	// ReplyGoesTo is where a reply goes; ReplyElsewhere marks a Reply-To that
	// belongs to another organisation than the From address.
	ReplyGoesTo    string `json:"replyGoesTo"`
	ReplyElsewhere bool   `json:"replyElsewhere"`
	// Warnings are the strong findings: they show in the banner and add a
	// confirmation before a reply or a link.
	Warnings []string            `json:"warnings"`
	Evidence PassportEvidenceDTO `json:"evidence"`
	// Banner is the one banner above the mail: the phishing checks read with
	// the trusted record, plus the warnings above.
	Banner PhishingDTO `json:"banner"`
}

// passportInput is everything the verdict is made from.
type passportInput struct {
	Address string
	Name    string
	ReplyTo string
	// Trust is the stored record; a zero AuthState means there is none.
	Trust storage.MessageTrust
	Mark  string
	// Received counts mail from the address, this one included; Sent counts
	// mail to it. The dates are the earliest of each.
	Received      int
	FirstReceived time.Time
	Sent          int
	FirstSent     time.Time
	Contact       bool
	// History is earlier mail from the address, newest first.
	History []storage.TrustHistoryEntry
	// Known is who the user actually corresponds with: everyone they have
	// written to and everyone in their address books, address to name.
	Known map[string]string
	// Own is the user's own addresses, which are nobody's impostor.
	Own []string
}

// recordOf turns a stored record back into the parsed form.
func recordOf(t storage.MessageTrust) phishing.AuthRecord {
	return phishing.AuthRecord{
		State: t.AuthState, ServID: t.ServID, SPF: t.SPF, SPFDomain: t.SPFDomain,
		DKIM: t.DKIM, DKIMDomain: t.DKIMDomain, DKIMDomains: t.DKIMDomains,
		DMARC: t.DMARC, DMARCPolicy: t.DMARCPolicy, HeaderFrom: t.HeaderFrom,
	}
}

// buildPassport makes the verdict. It reads no state of its own, so every
// case can be tested with made-up input.
func buildPassport(in passportInput) PassportDTO {
	addr := strings.ToLower(strings.TrimSpace(in.Address))
	domain := phishing.UnicodeDomain(domainPart(addr))
	rec := recordOf(in.Trust)
	align := rec.Aligned(addr)
	out := PassportDTO{
		Address:  addr,
		Domain:   domain,
		Mark:     in.Mark,
		Received: in.Received,
		Sent:     in.Sent,
		Contact:  in.Contact,
		Changes:  []PassportChangeDTO{},
		Warnings: []string{},
		Evidence: PassportEvidenceDTO{
			AuthState: in.Trust.AuthState, ServID: in.Trust.ServID,
			SPF: in.Trust.SPF, SPFDomain: in.Trust.SPFDomain,
			DKIM: in.Trust.DKIM, DKIMDomains: nonNil(in.Trust.DKIMDomains),
			DMARC: in.Trust.DMARC, DMARCPolicy: in.Trust.DMARCPolicy, HeaderFrom: in.Trust.HeaderFrom,
			AlignedSPF: align.SPF, AlignedDKIM: align.DKIM, SendingDomain: rec.SendingDomain(addr),
			ReturnPath: in.Trust.ReturnPath, Sender: in.Trust.Sender, Signers: nonNil(in.Trust.Signers),
			ReceivedSPF: in.Trust.ReceivedSPF, ARC: in.Trust.ARC,
			MixedScript: phishing.MixedScript(addr),
		},
	}
	out.Since = formatDate(earliest(in.FirstReceived, in.FirstSent))
	out.First = in.Received <= 1 && in.Sent == 0

	out.ReplyGoesTo = addr
	if reply := bareAddress(in.ReplyTo); reply != "" && reply != addr {
		out.ReplyGoesTo = reply
		out.ReplyElsewhere = !phishing.SameOrg(reply, addr)
	}

	// the user's own addresses imitate nobody, and an address the user said
	// they know is the one they mean.
	if !slices.Contains(in.Own, addr) && in.Mark != storage.SenderKnown {
		out.Lookalike = lookalikeFor(addr, in.Name, in.Known)
	}
	out.Changes, out.SameDomain = changesFor(in, rec, addr)

	out.State, out.Reason = stateFor(in, rec, addr, out.Lookalike != nil)

	if in.Mark == storage.SenderImpostor {
		out.Warnings = append(out.Warnings, warnImpostor)
	}
	if out.Lookalike != nil {
		out.Warnings = append(out.Warnings, warnLookalike)
	}
	if rec.State == phishing.AuthTrusted && rec.DMARC == phishing.ResultFail &&
		(rec.DMARCPolicy == "reject" || rec.DMARCPolicy == "quarantine") {
		out.Warnings = append(out.Warnings, warnDMARC)
	}
	if out.ReplyElsewhere && out.First && in.Mark != storage.SenderKnown {
		out.Warnings = append(out.Warnings, warnReplyFirst)
	}
	return out
}

// stateFor picks the one state the compact line shows.
func stateFor(in passportInput, rec phishing.AuthRecord, addr string, lookalike bool) (string, string) {
	switch {
	case in.Mark == storage.SenderImpostor:
		return passportImpostor, ""
	case rec.State == phishing.AuthTrusted && rec.DMARC == phishing.ResultFail:
		return passportFailed, ""
	case lookalike:
		return passportLookalike, ""
	}
	switch rec.State {
	case "":
		return passportUnchecked, reasonLegacy
	case phishing.AuthNone:
		return passportUnchecked, reasonNone
	case phishing.AuthUntrusted:
		return passportUnchecked, reasonUntrusted
	}
	if rec.Verified(addr) {
		return passportVerified, ""
	}
	if !rec.Auth().Stated() {
		return passportUnchecked, reasonNone
	}
	if rec.SendingDomain(addr) != "" {
		return passportUnverified, reasonOtherDomain
	}
	return passportUnverified, reasonNoPass
}

// lookalikeFor finds who the sender imitates among the people the user
// corresponds with: a known display name at an address of another
// organisation, or a domain resembling a correspondent's.
func lookalikeFor(addr, name string, known map[string]string) *PassportLookalikeDTO {
	if _, ok := known[addr]; ok {
		return nil
	}
	if folded := phishing.FoldName(name); folded != "" && strings.Contains(folded, " ") {
		var matches []string
		for address, knownName := range known {
			if phishing.FoldName(knownName) == folded && !phishing.SameOrg(address, addr) {
				matches = append(matches, address)
			}
		}
		if len(matches) > 0 {
			slices.Sort(matches)
			return &PassportLookalikeDTO{Kind: "name", Of: strings.TrimSpace(name), Address: matches[0]}
		}
	}
	domains := make([]string, 0, len(known))
	for address := range known {
		domains = append(domains, domainPart(address))
	}
	slices.Sort(domains)
	if of, strong := phishing.LookalikeOf(domainPart(addr), domains); of != "" && strong {
		return &PassportLookalikeDTO{Kind: "domain", Of: of}
	}
	return nil
}

// changesFor compares the message with earlier mail from the same address.
func changesFor(in passportInput, rec phishing.AuthRecord, addr string) ([]PassportChangeDTO, bool) {
	changes := []PassportChangeDTO{}
	current := rec.SendingDomain(addr)
	var domains []string
	replyTos := map[string]bool{}
	for _, h := range in.History {
		if d := recordOf(h.Trust).SendingDomain(addr); d != "" && !slices.Contains(domains, d) {
			domains = append(domains, d)
		}
		replyTos[bareAddress(h.ReplyTo)] = true
	}
	same := current != "" && slices.Contains(domains, current)
	if current != "" && len(domains) > 0 && !same {
		changes = append(changes, PassportChangeDTO{Kind: changeDomain, Detail: current, Before: strings.Join(domains, ", ")})
	}
	if reply := bareAddress(in.ReplyTo); reply != "" && reply != addr && len(in.History) > 0 && !replyTos[reply] {
		changes = append(changes, PassportChangeDTO{Kind: changeReplyTo, Detail: reply})
	}
	if folded := phishing.FoldName(in.Name); folded != "" && strings.Contains(folded, " ") {
		var others []string
		for address, name := range in.Known {
			if address != addr && phishing.FoldName(name) == folded && phishing.SameOrg(address, addr) {
				others = append(others, address)
			}
		}
		if len(others) > 0 {
			slices.Sort(others)
			changes = append(changes, PassportChangeDTO{Kind: changeName, Detail: addr, Before: others[0]})
		}
	}
	return changes, same
}

func domainPart(addr string) string {
	if at := strings.LastIndex(addr, "@"); at >= 0 {
		return strings.ToLower(strings.Trim(addr[at+1:], "> "))
	}
	return ""
}

func earliest(a, b time.Time) time.Time {
	switch {
	case a.IsZero():
		return b
	case b.IsZero() || a.Before(b):
		return a
	}
	return b
}
