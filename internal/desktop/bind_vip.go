package desktop

import (
	"fmt"
	"slices"
	"strings"
)

// VIP senders (#126). A VIP is a from-address the user marks as important;
// when new mail arrives from one, Pelton raises a native OS notification even
// if general new-mail notifications are off, so the people who matter cut
// through. VIPs are matched by exact address, stored lowercased in the settings
// table, and shown with a star next to the sender in the list and reading pane.
const settingVIPSenders = "vip_senders"

// vipSenders returns the lowercased addresses the user has marked as VIP.
func (a *App) vipSenders() []string {
	var out []string
	_ = a.store.GetJSON(a.ctx, settingVIPSenders, &out)
	return out
}

// vipSet returns the VIP addresses as a lookup set for O(1) membership checks
// while marking a page of summaries.
func (a *App) vipSet() map[string]bool {
	senders := a.vipSenders()
	if len(senders) == 0 {
		return nil
	}
	set := make(map[string]bool, len(senders))
	for _, s := range senders {
		set[s] = true
	}
	return set
}

// isVIP reports whether a from-address (in any "Name <addr>" or bare form) is a
// VIP sender.
func (a *App) isVIP(fromAddress string) bool {
	addr := bareAddress(fromAddress)
	if addr == "" {
		return false
	}
	return slices.Contains(a.vipSenders(), addr)
}

// ListVIPSenders returns the VIP addresses for the settings ui.
func (a *App) ListVIPSenders() ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	senders := a.vipSenders()
	if senders == nil {
		senders = []string{}
	}
	return senders, nil
}

// AddVIPSender marks an address as VIP. The address is normalized to its bare
// lowercased form so display names never leak into the match.
func (a *App) AddVIPSender(address string) error {
	if err := a.ready(); err != nil {
		return err
	}
	addr := bareAddress(address)
	if addr == "" {
		return fmt.Errorf("pelton: empty VIP address")
	}
	return a.store.SetJSON(a.ctx, settingVIPSenders, appendUnique(a.vipSenders(), addr))
}

// RemoveVIPSender drops an address from the VIP list.
func (a *App) RemoveVIPSender(address string) error {
	if err := a.ready(); err != nil {
		return err
	}
	addr := bareAddress(address)
	return a.store.SetJSON(a.ctx, settingVIPSenders, removeValue(a.vipSenders(), addr))
}

// MarkSenderVIP adds a message's sender to the VIP list, the quick path from an
// open message.
func (a *App) MarkSenderVIP(messageID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return err
	}
	return a.AddVIPSender(m.FromAddress)
}

// UnmarkSenderVIP removes a message's sender from the VIP list.
func (a *App) UnmarkSenderVIP(messageID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	m, err := a.store.GetMessage(a.ctx, messageID)
	if err != nil {
		return err
	}
	return a.RemoveVIPSender(m.FromAddress)
}

// bareAddress extracts the lowercased email address from a formatted from field.
// Stored addresses are "Name <user@host>" or a bare "user@host"; VIP and other
// per-sender features match on the address alone so a changing display name
// never breaks the match.
func bareAddress(s string) string {
	s = strings.TrimSpace(s)
	if _, rest, ok := strings.CutLast(s, "<"); ok {
		if addr, _, ok := strings.Cut(rest, ">"); ok {
			s = addr
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}
