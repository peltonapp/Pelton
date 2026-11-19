package desktop

import (
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/storage"
)

// passportHistoryCap bounds how much earlier mail from one sender a passport
// reads; passportSentCap how many sent messages it scans for mail to them.
const (
	passportHistoryCap = 200
	passportSentCap    = 2000
	// knownPeopleTTL is how long the list of people the user writes to is
	// reused. Building it reads the To lines of the sent folders.
	knownPeopleTTL = 10 * time.Minute
	// knownPeopleSentCap bounds the sent messages that list is built from.
	knownPeopleSentCap = 5000
)

// knownPeopleCache holds who the user corresponds with: addresses they have
// written to and the addresses in their address books.
type knownPeopleCache struct {
	mu     sync.Mutex
	loaded time.Time
	people map[string]string
}

var knownPeople knownPeopleCache

// knownPeople returns the people the user actually corresponds with, address
// to name. Unlike the harvested address book this leaves out everyone who
// only ever wrote to the user, so a stranger cannot become a reference just
// by sending mail.
func (a *App) knownPeople() map[string]string {
	knownPeople.mu.Lock()
	defer knownPeople.mu.Unlock()
	if knownPeople.people != nil && time.Since(knownPeople.loaded) < knownPeopleTTL {
		return knownPeople.people
	}
	out := map[string]string{}
	if contacts, err := a.store.ContactAddresses(a.ctx); err == nil {
		for addr, name := range contacts {
			out[addr] = name
		}
	}
	if folders, err := a.loadInsightsFolders(); err == nil {
		lines, err := a.store.SentLines(a.ctx, folders.sent, "", knownPeopleSentCap)
		if err == nil {
			for _, l := range lines {
				for _, p := range parseRecipients(l.To + "," + l.Cc) {
					if _, ok := out[p.Address]; !ok || out[p.Address] == "" {
						out[p.Address] = p.Name
					}
				}
			}
		}
		for _, own := range folders.own {
			delete(out, own)
		}
	}
	knownPeople.people = out
	knownPeople.loaded = time.Now()
	return out
}

// forgetKnownPeople drops the cached list, after a send or a contact change.
func forgetKnownPeople() {
	knownPeople.mu.Lock()
	knownPeople.people = nil
	knownPeople.mu.Unlock()
}

// recipient is one parsed address of a To or Cc line.
type recipient struct {
	Name    string
	Address string
}

// parseRecipients reads a stored address line. A line the mail parser rejects
// is split on commas instead, so one odd entry does not hide the others.
func parseRecipients(line string) []recipient {
	var out []recipient
	if list, err := mail.ParseAddressList(line); err == nil {
		for _, a := range list {
			if addr := strings.ToLower(strings.TrimSpace(a.Address)); addr != "" {
				out = append(out, recipient{Name: a.Name, Address: addr})
			}
		}
		return out
	}
	for _, part := range strings.Split(line, ",") {
		if addr := bareAddress(part); strings.Contains(addr, "@") {
			out = append(out, recipient{Name: displayNameOf(part, ""), Address: addr})
		}
	}
	return out
}

// GetSenderPassport returns the passport of one message: who sent it, how
// sure that is, and what the local mail says about them.
func (a *App) GetSenderPassport(id int64) (PassportDTO, error) {
	if err := a.ready(); err != nil {
		return PassportDTO{}, err
	}
	m, err := a.store.GetMessage(a.ctx, id)
	if err != nil {
		return PassportDTO{}, err
	}
	return a.passportFor(*m)
}

// passportFor builds the passport of a stored message.
func (a *App) passportFor(m storage.Message) (PassportDTO, error) {
	addr := addressOnly(m.FromAddress)
	in := passportInput{
		Address: addr,
		Name:    displayNameOf(m.FromAddress, m.FromName),
		ReplyTo: addressOnly(m.ReplyTo),
		Known:   a.knownPeople(),
	}
	var err error
	if in.Trust, err = a.store.GetMessageTrust(a.ctx, m.ID); err != nil {
		return PassportDTO{}, err
	}
	mark, err := a.store.GetSenderMark(a.ctx, addr)
	if err != nil {
		return PassportDTO{}, err
	}
	in.Mark = mark.Mark
	folders, err := a.loadInsightsFolders()
	if err != nil {
		return PassportDTO{}, err
	}
	in.Own = folders.own
	stats, err := a.store.SenderStatsFor(a.ctx, addr, folders.counted)
	if err != nil {
		return PassportDTO{}, err
	}
	in.Received, in.FirstReceived = stats.Count, stats.First
	lines, err := a.store.SentLines(a.ctx, folders.sent, addr, passportSentCap)
	if err != nil {
		return PassportDTO{}, err
	}
	for _, l := range lines {
		for _, p := range parseRecipients(l.To + "," + l.Cc) {
			if p.Address == addr {
				in.Sent++
				in.FirstSent = earliest(in.FirstSent, l.Date)
				break
			}
		}
	}
	_, in.Contact = a.contactAddresses()[addr]
	if in.History, err = a.store.SenderTrustHistory(a.ctx, addr, folders.counted, m.Date, m.ID, passportHistoryCap); err != nil {
		return PassportDTO{}, err
	}

	p := buildPassport(in)
	p.Banner = a.passportBanner(m, in.Trust, p)
	return p, nil
}

// contactAddresses is the address book part of knownPeople, read fresh: it
// is one indexed query and a contact just added should count at once.
func (a *App) contactAddresses() map[string]string {
	out, err := a.store.ContactAddresses(a.ctx)
	if err != nil {
		return map[string]string{}
	}
	return out
}

// passportBanner is the one banner above a message: the phishing checks, read
// with the trusted record where the message has one, plus the passport's
// strong warnings that the checks do not already make.
func (a *App) passportBanner(m storage.Message, trust storage.MessageTrust, p PassportDTO) PhishingDTO {
	if trust.AuthState != "" {
		auth := recordOf(trust).Auth()
		m.Auth = storage.MessageAuth{SPF: auth.SPF, DKIM: auth.DKIM, DMARC: auth.DMARC, SPFDomain: auth.SPFDomain, DKIMDomain: auth.DKIMDomain}
	}
	dto := a.checkPhishing(m)
	has := func(kinds ...string) bool {
		for _, s := range dto.Signals {
			for _, k := range kinds {
				if s.Kind == k {
					return true
				}
			}
		}
		return false
	}
	add := func(kind, detail string) {
		dto.Signals = append(dto.Signals, PhishingSignalDTO{Kind: "passport_" + kind, Detail: detail})
		dto.Level = phishing.LevelWarning
	}
	for _, w := range p.Warnings {
		switch w {
		case warnImpostor:
			add(w, p.Address)
		case warnLookalike:
			if !has(string(phishing.KindDisplayNameSpoof), string(phishing.KindLookalikeDomain)) {
				add(w, p.Lookalike.Of)
			}
		case warnDMARC:
			add(w, p.Domain)
		case warnReplyFirst:
			add(w, p.ReplyGoesTo)
		}
	}
	return dto
}

// SetSenderMark records what the user said about a sender: "known" ("I know
// this sender") quiets first-contact notices, "impostor" ("not who they claim
// to be") marks their mail from now on, and "" takes either back.
func (a *App) SetSenderMark(address, mark string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetSenderMark(a.ctx, bareAddress(address), mark)
}
