package autocrypt

import "time"

// Recommendation is the specification's answer to "should this message be
// encrypted", from weakest to strongest.
type Recommendation int

const (
	// Disable means encryption is not possible.
	Disable Recommendation = iota
	// Discourage means it is possible but the recipient may not be able to
	// read it, so it is not offered by default.
	Discourage
	// Available means it is possible and safe to offer.
	Available
	// Encrypt means it is possible and should be on by default.
	Encrypt
)

// String names a recommendation the way the frontend reads it.
func (r Recommendation) String() string {
	switch r {
	case Discourage:
		return "discourage"
	case Available:
		return "available"
	case Encrypt:
		return "encrypt"
	default:
		return "disable"
	}
}

// staleAfter is how much older than the last message the last Autocrypt
// header may be before the peer is discouraged: a peer that has stopped
// sending headers may have moved to a program that cannot decrypt.
const staleAfter = 35 * 24 * time.Hour

// Preliminary is the per-recipient recommendation of section 2.7.1. usable
// says whether a key can be encrypted to now (not expired, not revoked, has an
// encryption key). The key Autocrypt would use is returned with it, nil when
// there is none.
func Preliminary(p Peer, usable func([]byte) bool) (Recommendation, []byte) {
	target := p.PublicKey
	gossipOnly := false
	if len(target) == 0 {
		target = p.GossipKey
		gossipOnly = true
	}
	if len(target) == 0 || !usable(target) {
		return Disable, nil
	}
	if gossipOnly {
		return Discourage, target
	}
	if p.LastSeen.Sub(p.AutocryptTimestamp) > staleAfter {
		return Discourage, target
	}
	return Available, target
}

// Final applies section 2.7.2: a reply to an encrypted message is encrypted
// whenever it can be, and otherwise both sides must prefer encryption.
func Final(prelim Recommendation, peerPrefer, ownPrefer string, replyToEncrypted bool) Recommendation {
	switch prelim {
	case Available, Discourage:
		if replyToEncrypted {
			return Encrypt
		}
		if prelim == Available && peerPrefer == PreferMutual && ownPrefer == PreferMutual {
			return Encrypt
		}
	}
	return prelim
}

// Combine folds the recommendations for every recipient into one, per section
// 2.7.3: one recipient that cannot be encrypted to disables encryption, and it
// is on by default only when every recipient says encrypt. No recipients at
// all is disable, since there is nobody to encrypt to.
func Combine(recs []Recommendation) Recommendation {
	if len(recs) == 0 {
		return Disable
	}
	all := Encrypt
	discourage := false
	for _, r := range recs {
		if r == Disable {
			return Disable
		}
		if r == Discourage {
			discourage = true
		}
		if r != Encrypt {
			all = Available
		}
	}
	if all == Encrypt {
		return Encrypt
	}
	if discourage {
		return Discourage
	}
	return Available
}
