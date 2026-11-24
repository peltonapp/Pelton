package desktop

import (
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/storage"
)

// auth statuses the ui shows on a message. "unavailable" means the receiving
// server said nothing, which is most older cached mail and plenty of live mail
// too; it is not a failure and must never be shown as one.
const (
	authUnavailable = "unavailable"
	authPass        = "pass"
	authPartial     = "partial"
	authFail        = "fail"
)

// PhishingSignalDTO is one thing the checks found. Kind is a stable identifier
// the ui turns into a sentence; Detail is the domain, address or url involved.
type PhishingSignalDTO struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
	Target string `json:"target,omitempty"`
}

// PhishingDTO is the verdict for one message. Level is "none", "caution" or
// "warning"; the ui shows nothing at all for "none". Links are the urls the
// link signals were about, so the body renderer can mark them where they sit.
type PhishingDTO struct {
	Level   string              `json:"level"`
	Signals []PhishingSignalDTO `json:"signals,omitempty"`
	Links   []string            `json:"links,omitempty"`
}

// authStatus folds the stored results into the one word the badge shows. A
// dmarc verdict settles it on its own, since it already accounts for spf and
// dkim plus alignment.
func authStatus(auth storage.MessageAuth) string {
	switch auth.DMARC {
	case phishing.ResultPass:
		return authPass
	case phishing.ResultFail:
		return authFail
	}
	spf, dkim := auth.SPF, auth.DKIM
	if spf == "" && dkim == "" {
		return authUnavailable
	}
	if spf == phishing.ResultFail || dkim == phishing.ResultFail {
		return authFail
	}
	if spf == phishing.ResultPass && dkim == phishing.ResultPass {
		return authPass
	}
	if spf == phishing.ResultPass || dkim == phishing.ResultPass {
		return authPartial
	}
	return authUnavailable
}

// correspondentTTL is how long the contact list is reused between messages.
// Reading down a folder should not requery the address book for every message,
// and a contact harvested in the last minute changes no verdict.
const correspondentTTL = time.Minute

// correspondentCache holds the address book in the shape the checks want, plus
// the user's own addresses, which come from the same pass over the accounts.
type correspondentCache struct {
	mu      sync.Mutex
	loaded  time.Time
	entries map[string]string
	own     []string
}

// correspondents returns who the user exchanges mail with, address to display
// name, both lowercased. The checks that depend on it are the ones that make
// this specific to the user rather than a list of well-known brands somebody
// else picked, so an empty book simply means those checks stay quiet.
func (a *App) correspondents() (map[string]string, []string) {
	a.contacts.mu.Lock()
	defer a.contacts.mu.Unlock()
	if a.contacts.entries != nil && time.Since(a.contacts.loaded) < correspondentTTL {
		return a.contacts.entries, a.contacts.own
	}
	entries, err := a.store.ListAddresses(a.ctx)
	if err != nil {
		a.log.Error("phishing: load address book", "err", err)
		return a.contacts.entries, a.contacts.own
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		address := strings.ToLower(strings.TrimSpace(e.Email))
		if address == "" {
			continue
		}
		out[address] = strings.ToLower(strings.TrimSpace(e.Name))
	}
	// the user's own addresses are not somebody to be impersonated by, and a
	// message from another of their own accounts is not a lookalike of this one.
	var own []string
	if accounts, err := a.store.ListAccounts(a.ctx); err == nil {
		own = make([]string, 0, len(accounts))
		for _, acct := range accounts {
			address := strings.ToLower(strings.TrimSpace(acct.Email))
			if address == "" {
				continue
			}
			delete(out, address)
			own = append(own, address)
		}
	}
	a.contacts.entries = out
	a.contacts.own = own
	a.contacts.loaded = time.Now()
	return out, own
}

// checkPhishing runs the local checks over one stored message. It costs a
// regex pass over the body and no io beyond the cached contact list, so it runs
// on every message open rather than behind a button.
func (a *App) checkPhishing(m storage.Message) PhishingDTO {
	correspondents, own := a.correspondents()
	report := phishing.Analyse(phishing.Message{
		From:     addressOnly(m.FromAddress),
		FromName: displayNameOf(m.FromAddress, m.FromName),
		ReplyTo:  addressOnly(m.ReplyTo),
		Auth: phishing.Auth{
			SPF:        m.Auth.SPF,
			DKIM:       m.Auth.DKIM,
			DMARC:      m.Auth.DMARC,
			SPFDomain:  m.Auth.SPFDomain,
			DKIMDomain: m.Auth.DKIMDomain,
		},
		HTML:           m.BodyHTML,
		Text:           m.BodyPlain,
		Correspondents: correspondents,
		Own:            own,
	})

	dto := PhishingDTO{Level: report.Level, Links: report.Links}
	for _, s := range report.Signals {
		dto.Signals = append(dto.Signals, PhishingSignalDTO{
			Kind:   string(s.Kind),
			Detail: s.Detail,
			Target: s.Target,
		})
	}
	return dto
}

// addressOnly pulls the bare address out of a stored header value, which can be
// "Name <addr>" or just the address.
func addressOnly(header string) string {
	s := strings.TrimSpace(header)
	if _, rest, ok := strings.CutLast(s, "<"); ok {
		if addr, _, ok := strings.Cut(rest, ">"); ok {
			return strings.ToLower(strings.TrimSpace(addr))
		}
	}
	// several addresses, which Reply-To is allowed to carry: the first is the
	// one a reply goes to.
	if comma := strings.Index(s, ","); comma >= 0 {
		s = s[:comma]
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// displayNameOf returns the sender's display name. Older cached mail kept the
// whole "Name <addr>" string in FromAddress with FromName empty, so the name is
// recovered from there when the column has nothing.
func displayNameOf(fromAddress, fromName string) string {
	if name := strings.TrimSpace(fromName); name != "" {
		return name
	}
	s := strings.TrimSpace(fromAddress)
	before, _, ok := strings.CutLast(s, "<")
	if !ok || before == "" {
		return ""
	}
	return strings.Trim(strings.TrimSpace(before), `"`)
}
