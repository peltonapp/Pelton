package autocrypt

import "time"

// Peer is the Autocrypt state kept for one correspondent address, as section
// 2.3 of the specification defines it. Zero times mean never.
type Peer struct {
	Addr               string
	LastSeen           time.Time
	AutocryptTimestamp time.Time
	PublicKey          []byte
	PreferEncrypt      string
	GossipTimestamp    time.Time
	GossipKey          []byte
}

// EffectiveDate is the date a message counts as sent at: its Date header, or
// now when the header is missing or lies in the future, so a sender cannot pin
// its state ahead of every later message.
func EffectiveDate(date, now time.Time) time.Time {
	if date.IsZero() || date.After(now) {
		return now
	}
	return date
}

// Update applies one incoming message from the peer's address, with h nil when
// the message carried no valid Autocrypt header. It reports whether anything
// changed. A message older than the last Autocrypt header seen changes
// nothing; a newer one without a header only moves last_seen forward, which is
// how a peer that stopped sending headers ends up discouraged.
func (p *Peer) Update(effective time.Time, h *Header) bool {
	if effective.Before(p.AutocryptTimestamp) {
		return false
	}
	changed := false
	if effective.After(p.LastSeen) {
		p.LastSeen = effective
		changed = true
	}
	if h == nil {
		return changed
	}
	p.AutocryptTimestamp = effective
	p.PublicKey = h.KeyData
	p.PreferEncrypt = h.PreferEncrypt
	if p.PreferEncrypt != PreferMutual {
		p.PreferEncrypt = PreferNoPreference
	}
	return true
}

// UpdateGossip applies an Autocrypt-Gossip header seen in an encrypted message
// sent at effective. Only a newer gossip replaces an older one.
func (p *Peer) UpdateGossip(effective time.Time, key []byte) bool {
	if !effective.After(p.GossipTimestamp) {
		return false
	}
	p.GossipTimestamp = effective
	p.GossipKey = key
	return true
}
