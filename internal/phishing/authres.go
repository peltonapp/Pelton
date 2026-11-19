package phishing

import (
	"net"
	"regexp"
	"slices"
	"strings"
)

// The trusted reading of Authentication-Results.
//
// Anyone can write an Authentication-Results header into a message before
// sending it, so a header only means something when the user's own receiving
// server wrote it. Two things identify that one:
//
//   - position: each server adds its header above everything already in the
//     message, so the receiving server's header is the topmost one. A header
//     the sender wrote sits further down, below the receiving server's.
//   - name: the header starts with the authserv-id of the server that wrote it,
//     which has to belong to the account's own provider (see AuthServ).
//
// ReadAuth takes the run of headers at the top whose authserv-id belongs to
// the provider and stops at the first one that does not. When the topmost
// header is not the provider's, nothing is trusted at all: the server did not
// say anything, and whatever is below was written by somebody else. RFC 8601
// section 5 also asks a receiving server to remove headers carrying its own
// authserv-id from incoming mail, which closes the case of a forged header
// that copies the provider's name, on servers that check.

// AuthServ names the receiving servers whose Authentication-Results headers
// are trusted for one account.
type AuthServ struct {
	// Domains are registrable domains; an authserv-id under one of them is the
	// provider's. Empty trusts nothing.
	Domains []string
	// Unnamed trusts a header with no authserv-id, which is how Microsoft's
	// servers write it.
	Unnamed bool
}

// providerDomains maps the domain of a provider's mail hosts or addresses to
// the domain its receiving servers name themselves under, where the two differ.
var providerDomains = map[string][]string{
	"gmail.com":           {"google.com"},
	"googlemail.com":      {"google.com"},
	"icloud.com":          {"icloud.com", "apple.com"},
	"me.com":              {"icloud.com", "apple.com"},
	"mac.com":             {"icloud.com", "apple.com"},
	"fastmail.com":        {"messagingengine.com"},
	"fastmail.fm":         {"messagingengine.com"},
	"messagingengine.com": {"messagingengine.com"},
	"yahoo.com":           {"yahoo.com"},
	"ymail.com":           {"yahoo.com"},
	"aol.com":             {"yahoo.com", "aol.com"},
	"zoho.com":            {"zoho.com", "zohomail.com"},
	"zohomail.com":        {"zoho.com", "zohomail.com"},
	"proton.me":           {"protonmail.ch", "proton.ch"},
	"protonmail.com":      {"protonmail.ch", "proton.ch"},
	"pm.me":               {"protonmail.ch", "proton.ch"},
	"gmx.net":             {"gmx.net"},
	"gmx.de":              {"gmx.net"},
	"web.de":              {"web.de"},
	"mailbox.org":         {"mailbox.org"},
	"posteo.de":           {"posteo.de"},
	"tutanota.com":        {"tutanota.de"},
	"outlook.com":         {"outlook.com", "protection.outlook.com"},
	"hotmail.com":         {"outlook.com", "protection.outlook.com"},
	"live.com":            {"outlook.com", "protection.outlook.com"},
	"office365.com":       {"outlook.com", "protection.outlook.com"},
}

// microsoftDomains are the hosts whose servers write the header without an
// authserv-id.
var microsoftDomains = []string{"outlook.com", "hotmail.com", "live.com", "office365.com", "microsoft.com"}

// ServerFor works out whose headers to trust for an account from what the
// account already knows: its address and its imap and smtp hosts. A host given
// as an ip address or a bare name says nothing about who writes the headers
// and is left out; an account with nothing usable trusts nothing.
func ServerFor(email, imapHost, smtpHost string) AuthServ {
	var out AuthServ
	add := func(d string) {
		if d != "" && !slices.Contains(out.Domains, d) {
			out.Domains = append(out.Domains, d)
		}
	}
	for _, host := range []string{domainOf(email), hostDomain(imapHost), hostDomain(smtpHost)} {
		if host == "" {
			continue
		}
		reg := registrable(host)
		add(reg)
		for _, d := range providerDomains[reg] {
			add(d)
		}
		if slices.Contains(microsoftDomains, reg) {
			out.Unnamed = true
		}
	}
	return out
}

// hostDomain returns a server host when it is a dotted name, empty for an ip
// address, localhost or anything without a dot.
func hostDomain(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" || net.ParseIP(h) != nil || !strings.Contains(h, ".") {
		return ""
	}
	return h
}

// Trusts reports whether a header with this authserv-id was written by the
// account's own provider.
func (s AuthServ) Trusts(id string) bool {
	if id == "" {
		return s.Unnamed
	}
	host := strings.ToLower(strings.Trim(id, ". "))
	if !strings.Contains(host, ".") {
		return false
	}
	return slices.Contains(s.Domains, registrable(host))
}

// Trust states of an AuthRecord.
const (
	// AuthNone: the message carried no Authentication-Results header at all.
	AuthNone = "none"
	// AuthUntrusted: it carried some, but the topmost was not written by the
	// account's provider, so none of them counts.
	AuthUntrusted = "untrusted"
	// AuthTrusted: the provider's server wrote the topmost header.
	AuthTrusted = "trusted"
)

// AuthRecord is what the user's own server recorded about a message's
// authentication, plus the sender headers that go with it.
type AuthRecord struct {
	// State is AuthNone, AuthUntrusted or AuthTrusted. Only a trusted record
	// carries results.
	State string
	// ServID is the authserv-id of the topmost header, trusted or not, so the
	// detail panel can say whose header was read or ignored.
	ServID string
	SPF    string
	// SPFDomain is the smtp.mailfrom (or helo) domain spf was checked for.
	SPFDomain string
	DKIM      string
	// DKIMDomain is the best signing domain (a passing one when there is one),
	// and DKIMDomains every domain with a passing signature.
	DKIMDomain  string
	DKIMDomains []string
	DMARC       string
	// DMARCPolicy is the policy the From domain publishes, lowercased: reject,
	// quarantine or none, empty when the header did not say.
	DMARCPolicy string
	// HeaderFrom is the From domain dmarc was evaluated for.
	HeaderFrom string
}

// Auth returns the record in the shape Analyse reads. An untrusted or empty
// record gives an empty Auth, which Analyse treats as unstated.
func (r AuthRecord) Auth() Auth {
	if r.State != AuthTrusted {
		return Auth{}
	}
	return Auth{SPF: r.SPF, DKIM: r.DKIM, DMARC: r.DMARC, SPFDomain: r.SPFDomain, DKIMDomain: r.DKIMDomain}
}

// ReadAuth reads the trusted part of a message's Authentication-Results
// headers, given in the order the message carries them (topmost first).
func ReadAuth(headers []string, serv AuthServ) AuthRecord {
	var rec AuthRecord
	var present bool
	for _, raw := range headers {
		header := unfold(raw)
		if header == "" {
			continue
		}
		id, clauses := splitAuthServ(header)
		if !present {
			present = true
			rec.ServID = id
			rec.State = AuthUntrusted
		}
		if !serv.Trusts(id) {
			break
		}
		rec.State = AuthTrusted
		readClauses(&rec, clauses)
	}
	if !present {
		rec.State = AuthNone
	}
	return rec
}

// unfold joins a folded header onto one line.
func unfold(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)), " ")
}

// commentPattern matches a parenthesised comment without nesting; nested ones
// are rare and come apart into harmless words.
var commentPattern = regexp.MustCompile(`\([^()]*\)`)

// policyInComment reads a dmarc policy written in a comment, as Gmail does:
// "dmarc=pass (p=REJECT sp=REJECT dis=NONE)".
var policyInComment = regexp.MustCompile(`(?i)\bp=([a-z]+)`)

// splitAuthServ returns the authserv-id and the result clauses. A header whose
// first clause is already a method=result has no authserv-id.
func splitAuthServ(header string) (string, []string) {
	parts := strings.Split(header, ";")
	first := strings.TrimSpace(commentPattern.ReplaceAllString(parts[0], " "))
	if strings.Contains(first, "=") {
		return "", parts
	}
	fields := strings.Fields(first)
	if len(fields) == 0 {
		return "", parts[1:]
	}
	return strings.ToLower(fields[0]), parts[1:]
}

// readClauses folds the method results of one trusted header into rec. The
// first statement of a method wins, since the headers are read topmost first;
// for dkim a later pass still wins over an earlier failure, because one valid
// signature is enough.
func readClauses(rec *AuthRecord, clauses []string) {
	for _, clause := range clauses {
		comments := strings.Join(commentPattern.FindAllString(clause, -1), " ")
		method, result, props := parseMethod(commentPattern.ReplaceAllString(clause, " "))
		switch method {
		case "spf":
			if rec.SPF == "" {
				rec.SPF = result
				rec.SPFDomain = domainOf(firstOf(props, "smtp.mailfrom", "smtp.helo"))
			}
		case "dkim":
			domain := domainOf(firstOf(props, "header.d", "header.i"))
			if result == ResultPass && domain != "" && !slices.Contains(rec.DKIMDomains, domain) {
				rec.DKIMDomains = append(rec.DKIMDomains, domain)
			}
			if rec.DKIM == "" || (rec.DKIM != ResultPass && result == ResultPass) {
				rec.DKIM = result
				rec.DKIMDomain = domain
			}
		case "dmarc":
			if rec.DMARC != "" {
				continue
			}
			rec.DMARC = result
			rec.HeaderFrom = domainOf(firstOf(props, "header.from"))
			rec.DMARCPolicy = dmarcPolicy(props, comments)
		}
	}
}

// dmarcPolicy reads the published policy from whichever spelling the server
// used: Fastmail's policy.published-domain-policy, the policy.dmarc some others
// write, Gmail's comment, or Microsoft's action.
func dmarcPolicy(props map[string]string, comments string) string {
	if p := firstOf(props, "policy.published-domain-policy", "policy.dmarc"); p != "" {
		return normalPolicy(p)
	}
	if m := policyInComment.FindStringSubmatch(comments); m != nil {
		return normalPolicy(m[1])
	}
	switch strings.ToLower(props["action"]) {
	case "reject", "oreject":
		return "reject"
	case "quarantine", "oquarantine":
		return "quarantine"
	}
	return ""
}

func normalPolicy(p string) string {
	switch p = strings.ToLower(strings.TrimSpace(p)); p {
	case "reject", "quarantine", "none":
		return p
	}
	return ""
}

// Alignment says which methods vouch for the visible From domain: a pass for
// the same organisation (relaxed alignment, RFC 7489 section 3.1).
type Alignment struct {
	SPF  bool
	DKIM bool
}

// Aligned reports which passing methods of a trusted record belong to the
// same organisation as from.
func (r AuthRecord) Aligned(from string) Alignment {
	from = domainOf(from)
	if r.State != AuthTrusted || from == "" {
		return Alignment{}
	}
	var a Alignment
	a.SPF = r.SPF == ResultPass && sameOrg(r.SPFDomain, from)
	for _, d := range r.DKIMDomains {
		if sameOrg(d, from) {
			a.DKIM = true
		}
	}
	return a
}

// Verified reports whether the user's own server established that the From
// domain sent the message: dmarc passed for it, or an aligned method passed.
// It says nothing about which person at that domain wrote it.
func (r AuthRecord) Verified(from string) bool {
	if r.State != AuthTrusted {
		return false
	}
	if r.DMARC == ResultPass && (r.HeaderFrom == "" || sameOrg(r.HeaderFrom, domainOf(from))) {
		return true
	}
	a := r.Aligned(from)
	return a.SPF || a.DKIM
}

// SendingDomain is the domain authentication vouched for: the aligned dkim
// signer, else the first passing signer, else the spf domain when spf passed.
// Empty when nothing passed.
func (r AuthRecord) SendingDomain(from string) string {
	if r.State != AuthTrusted {
		return ""
	}
	for _, d := range r.DKIMDomains {
		if sameOrg(d, domainOf(from)) {
			return registrable(d)
		}
	}
	if len(r.DKIMDomains) > 0 {
		return registrable(r.DKIMDomains[0])
	}
	if r.SPF == ResultPass && r.SPFDomain != "" {
		return registrable(r.SPFDomain)
	}
	return ""
}

// Registrable exposes the registrable-domain reading to other packages, so
// they compare organisations the way the checks here do.
func Registrable(domain string) string {
	return registrable(domainOf(domain))
}

// SameOrg reports whether two addresses or domains belong to the same
// registrable domain.
func SameOrg(a, b string) bool {
	return sameOrg(domainOf(a), domainOf(b))
}
