package desktop

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/peltonapp/Pelton/internal/autocrypt"
	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Which key Pelton encrypts to when several places know one for the same
// address, strongest first:
//
//  1. a key the user imported into the keyring themselves;
//  2. the key from the person's own Autocrypt header, which is the one their
//     mail program currently decrypts with;
//  3. the key their own domain publishes (WKD);
//  4. keys.openpgp.org, which only hands out addresses it confirmed;
//  5. the user's own key servers, in the order they are listed;
//  6. a key another correspondent passed on (Autocrypt gossip).
//
// An unusable key (expired, revoked, no encryption key) is skipped, so the
// next source gets its turn. Whichever key wins is compared with the one last
// used for the address: a different one is a key change, which is never used
// for sending until the user accepts it.

// Per-contact preferences.
const (
	contactPrefAlways = "always"
	contactPrefNever  = "never"
)

// Recipient states the composer shows.
const (
	recipientReady    = "ready"
	recipientVerified = "verified"
	recipientMissing  = "missing"
	recipientChanged  = "changed"
)

// resolvedKey is the key Pelton would encrypt to for one address, and what it
// would recommend.
type resolvedKey struct {
	addr      string
	key       []byte
	info      crypto.PublicKeyInfo
	source    string
	rec       autocrypt.Recommendation
	changed   bool
	previous  string
	changedAt time.Time
	verified  bool
	pref      string
	problem   string
}

// state is the composer's word for this recipient.
func (r resolvedKey) state() string {
	switch {
	case r.key == nil:
		return recipientMissing
	case r.changed:
		return recipientChanged
	case r.verified:
		return recipientVerified
	default:
		return recipientReady
	}
}

// keySourceOrder lists the lookup sources consulted for resolution, in the
// order above, given the user's settings.
func (a *App) lookupSourceOrder(s KeySourcesDTO) []string {
	var out []string
	if s.WKD {
		out = append(out, keySourceWKD)
	}
	if s.KeysOpenPGPOrg {
		out = append(out, keySourceKOO)
	}
	if s.KeyServers {
		for _, srv := range s.Servers {
			out = append(out, keySourceServerPrefix+srv)
		}
	}
	return out
}

// resolveRecipient finds the key for addr and the recommendation for it.
// replyEncrypted is whether the message replies to an encrypted one, and
// ownPrefer the sending account's prefer-encrypt.
func (a *App) resolveRecipient(keys *crypto.PGPKeyStore, sources KeySourcesDTO, addr string, replyEncrypted bool, ownPrefer string) resolvedKey {
	addr = autocrypt.NormalizeAddr(addr)
	now := a.autoNow()
	r := resolvedKey{addr: addr, rec: autocrypt.Disable}
	usable := func(k []byte) bool { return crypto.KeyUsable(k, now) }
	note := func(info crypto.PublicKeyInfo) {
		if r.problem == "" && info.Problem != "" {
			r.problem = info.Problem
		}
	}

	choose := func(key []byte, info crypto.PublicKeyInfo, source string, rec autocrypt.Recommendation) {
		r.key, r.info, r.source, r.rec = key, info, source, rec
	}

	if keys != nil {
		if key, info, err := keys.RecipientKeyData(addr, now); err == nil {
			choose(key, info, keySourceManual, autocrypt.Encrypt)
		} else {
			note(info)
		}
	}

	var gossip []byte
	if r.key == nil && sources.Autocrypt {
		if peer, ok, err := a.store.AutocryptPeerFor(a.ctx, addr); err == nil && ok {
			p := peerFromStorage(peer)
			prelim, key := autocrypt.Preliminary(p, usable)
			switch {
			case key != nil && bytes.Equal(key, p.PublicKey):
				info, _ := crypto.InspectPublicKey(key, now)
				choose(key, info, keySourceAutocrypt, autocrypt.Final(prelim, p.PreferEncrypt, ownPrefer, replyEncrypted))
			case key != nil:
				gossip = key
			default:
				if len(p.PublicKey) > 0 {
					info, _ := crypto.InspectPublicKey(p.PublicKey, now)
					note(info)
				}
				if len(p.PublicKey) == 0 && len(p.GossipKey) > 0 {
					info, _ := crypto.InspectPublicKey(p.GossipKey, now)
					note(info)
				}
			}
		}
	}

	if r.key == nil {
		for _, source := range a.lookupSourceOrder(sources) {
			cached, ok, err := a.store.CachedKeyFor(a.ctx, addr, source)
			if err != nil || !ok || cached.Key == nil {
				continue
			}
			info, err := crypto.InspectPublicKey(cached.Key, now)
			if err != nil {
				continue
			}
			if !info.Usable {
				note(info)
				continue
			}
			choose(cached.Key, info, source, autocrypt.Encrypt)
			break
		}
	}

	if r.key == nil && gossip != nil {
		info, _ := crypto.InspectPublicKey(gossip, now)
		choose(gossip, info, keySourceGossip, autocrypt.Final(autocrypt.Discourage, "", ownPrefer, replyEncrypted))
	}

	if r.key == nil {
		return r
	}
	r.problem = ""

	accepted, ok, err := a.store.AcceptedKeyFor(a.ctx, addr)
	if err == nil && ok {
		r.pref = accepted.Preference
		fp := crypto.NormalizeFingerprint(r.info.Fingerprint)
		switch {
		case accepted.Fingerprint == "":
		case accepted.Fingerprint == fp:
			r.verified = accepted.Verified
		case r.source == keySourceManual:
			// the user put this key in the keyring themselves, which is
			// their decision about it.
		default:
			r.changed = true
			r.previous = accepted.Fingerprint
			if seen, ok, err := a.store.KeySightingFor(a.ctx, addr, fp); err == nil && ok {
				r.changedAt = seen.FirstSeen
			}
		}
	}

	switch r.pref {
	case contactPrefAlways:
		if r.rec == autocrypt.Available || r.rec == autocrypt.Discourage {
			r.rec = autocrypt.Encrypt
		}
	case contactPrefNever:
		if r.rec == autocrypt.Encrypt {
			r.rec = autocrypt.Available
		}
	}
	if r.changed {
		r.rec = autocrypt.Disable
	}
	return r
}

func peerFromStorage(p storage.AutocryptPeer) autocrypt.Peer {
	return autocrypt.Peer{
		Addr: p.Addr, LastSeen: p.LastSeen, AutocryptTimestamp: p.AutocryptTimestamp,
		PublicKey: p.PublicKey, PreferEncrypt: p.PreferEncrypt,
		GossipTimestamp: p.GossipTimestamp, GossipKey: p.GossipKey,
	}
}

func peerToStorage(p autocrypt.Peer) storage.AutocryptPeer {
	return storage.AutocryptPeer{
		Addr: p.Addr, LastSeen: p.LastSeen, AutocryptTimestamp: p.AutocryptTimestamp,
		PublicKey: p.PublicKey, PreferEncrypt: p.PreferEncrypt,
		GossipTimestamp: p.GossipTimestamp, GossipKey: p.GossipKey,
	}
}

// replyToEncrypted reports whether the message a reply answers was encrypted.
func (a *App) replyToEncrypted(inReplyTo string) bool {
	id := strings.Trim(strings.TrimSpace(inReplyTo), "<>")
	if id == "" {
		return false
	}
	ref, err := a.store.FindMessageByHeaderID(a.ctx, id)
	if err != nil {
		return false
	}
	raw, err := a.store.MessagePGPSource(a.ctx, ref.ID)
	return err == nil && crypto.IsEncrypted(raw)
}

// RecipientEncryptionDTO is what the composer shows for one recipient.
type RecipientEncryptionDTO struct {
	Email string `json:"email"`
	// State is ready, verified, missing or changed.
	State string `json:"state"`
	// Source is where the key came from: manual, autocrypt, gossip, wkd,
	// keys.openpgp.org or server:<url>.
	Source      string `json:"source"`
	Fingerprint string `json:"fingerprint"`
	// Recommendation is this recipient's share of the decision: disable,
	// discourage, available or encrypt.
	Recommendation string `json:"recommendation"`
	// Preference is the user's choice for this contact: '', always or never.
	Preference string `json:"preference"`
	// Previous and ChangedAt describe a key change: the fingerprint last used
	// and when the new key was first seen (RFC 3339, empty when unknown).
	Previous  string `json:"previous"`
	ChangedAt string `json:"changedAt"`
	// Problem says why a key that was found cannot be used: expired, revoked
	// or noencryption.
	Problem string `json:"problem"`
	Bcc     bool   `json:"bcc"`
}

// AutoEncryptStatusDTO is the composer's whole answer.
type AutoEncryptStatusDTO struct {
	Enabled bool `json:"enabled"`
	// OwnKey is false while the sending mailbox has no usable key of its own,
	// which encryption needs for the sent copy and the signature.
	OwnKey           bool   `json:"ownKey"`
	Recommendation   string `json:"recommendation"`
	ReplyToEncrypted bool   `json:"replyToEncrypted"`
	// Mode is the account's mode, auto or manual.
	Mode       string                   `json:"mode"`
	Recipients []RecipientEncryptionDTO `json:"recipients"`
}

// AutoEncryptQuery is the composer's question: from which mailbox, to whom,
// and in reply to which message.
type AutoEncryptQuery struct {
	AccountID int64    `json:"accountId"`
	To        []string `json:"to"`
	Cc        []string `json:"cc"`
	Bcc       []string `json:"bcc"`
	InReplyTo string   `json:"inReplyTo"`
}

// AutoEncryptStatus says what would happen to a message from this mailbox to
// these recipients. It reads only what is stored; it never asks the network.
func (a *App) AutoEncryptStatus(q AutoEncryptQuery) (AutoEncryptStatusDTO, error) {
	if err := a.ready(); err != nil {
		return AutoEncryptStatusDTO{}, err
	}
	out := AutoEncryptStatusDTO{Recommendation: autocrypt.Disable.String(), Recipients: []RecipientEncryptionDTO{}}
	if !a.autoEncryptOn() {
		return out, nil
	}
	out.Enabled = true
	account, err := a.store.GetAccount(a.ctx, q.AccountID)
	if err != nil {
		return out, err
	}
	out.Mode = a.accountMode(account.ID)
	out.ReplyToEncrypted = a.replyToEncrypted(q.InReplyTo)
	keys, kerr := a.keyStore()
	if kerr == nil {
		if _, _, err := a.ownKey(keys, account.Email); err == nil {
			out.OwnKey = true
		}
	}
	if !out.OwnKey && kerr == nil {
		goSafe("making encryption keys", a.ensureOwnKeys)
	}

	resolved, bcc := a.resolveAll(keys, account.ID, q, out.ReplyToEncrypted)
	recs := make([]autocrypt.Recommendation, 0, len(resolved))
	for i, r := range resolved {
		recs = append(recs, r.rec)
		out.Recipients = append(out.Recipients, recipientDTO(r, bcc[i]))
	}
	combined := autocrypt.Combine(recs)
	if !out.OwnKey {
		combined = autocrypt.Disable
	}
	if combined == autocrypt.Encrypt && out.Mode == accountModeManual && !out.ReplyToEncrypted {
		combined = autocrypt.Available
	}
	out.Recommendation = combined.String()
	return out, nil
}

// resolveAll resolves every distinct recipient of a query, reporting for each
// whether it is a Bcc recipient.
func (a *App) resolveAll(keys *crypto.PGPKeyStore, accountID int64, q AutoEncryptQuery, replyEncrypted bool) ([]resolvedKey, []bool) {
	sources := a.keySources()
	own := a.ownPrefer(accountID)
	var out []resolvedKey
	var bcc []bool
	seen := map[string]bool{}
	for i, group := range [][]string{q.To, q.Cc, q.Bcc} {
		for _, addr := range group {
			addr = autocrypt.NormalizeAddr(addr)
			if addr == "" || !strings.Contains(addr, "@") || seen[addr] {
				continue
			}
			seen[addr] = true
			out = append(out, a.resolveRecipient(keys, sources, addr, replyEncrypted, own))
			bcc = append(bcc, i == 2)
		}
	}
	return out, bcc
}

func recipientDTO(r resolvedKey, bcc bool) RecipientEncryptionDTO {
	dto := RecipientEncryptionDTO{
		Email: r.addr, State: r.state(), Source: r.source, Fingerprint: r.info.Fingerprint,
		Recommendation: r.rec.String(), Preference: r.pref, Previous: r.previous, Problem: r.problem, Bcc: bcc,
	}
	if !r.changedAt.IsZero() {
		dto.ChangedAt = r.changedAt.UTC().Format(time.RFC3339)
	}
	return dto
}

// AcceptContactKey makes the key Pelton currently finds for addr the one to
// send with, after a key change. fingerprint is the key the user was shown,
// so a key that changed again in the meantime is not accepted unseen.
func (a *App) AcceptContactKey(addr, fingerprint string) error {
	if err := a.ready(); err != nil {
		return err
	}
	addr = autocrypt.NormalizeAddr(addr)
	keys, _ := a.keyStore()
	r := a.resolveRecipient(keys, a.keySources(), addr, false, autocrypt.PreferNoPreference)
	if r.key == nil || crypto.NormalizeFingerprint(r.info.Fingerprint) != crypto.NormalizeFingerprint(fingerprint) {
		return fmt.Errorf("pelton: the key for %s changed again; look at it once more before accepting it", addr)
	}
	return a.saveAccepted(addr, r, false)
}

// VerifyContactKey records that the user compared addr's fingerprint with
// the person and it matched.
func (a *App) VerifyContactKey(addr, fingerprint string) error {
	if err := a.ready(); err != nil {
		return err
	}
	addr = autocrypt.NormalizeAddr(addr)
	keys, _ := a.keyStore()
	r := a.resolveRecipient(keys, a.keySources(), addr, false, autocrypt.PreferNoPreference)
	if r.key == nil || crypto.NormalizeFingerprint(r.info.Fingerprint) != crypto.NormalizeFingerprint(fingerprint) {
		return fmt.Errorf("pelton: the key for %s is not the one shown; compare again", addr)
	}
	if r.changed {
		return fmt.Errorf("pelton: the key for %s changed; accept the new key before verifying it", addr)
	}
	return a.saveAccepted(addr, r, true)
}

func (a *App) saveAccepted(addr string, r resolvedKey, verified bool) error {
	rec, _, err := a.store.AcceptedKeyFor(a.ctx, addr)
	if err != nil {
		return err
	}
	rec.Addr = addr
	rec.Fingerprint = crypto.NormalizeFingerprint(r.info.Fingerprint)
	rec.Source = r.source
	rec.AcceptedAt = a.autoNow()
	rec.Verified = verified
	return a.store.SaveAcceptedKey(a.ctx, rec)
}

// SetContactEncryption sets the user's preference for one contact: empty to
// follow the recommendation, always or never.
func (a *App) SetContactEncryption(addr, pref string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if pref != "" && pref != contactPrefAlways && pref != contactPrefNever {
		return fmt.Errorf("pelton: unknown preference %q", pref)
	}
	addr = autocrypt.NormalizeAddr(addr)
	rec, ok, err := a.store.AcceptedKeyFor(a.ctx, addr)
	if err != nil {
		return err
	}
	if !ok {
		rec = storage.AcceptedKey{Addr: addr}
	}
	rec.Preference = pref
	return a.store.SaveAcceptedKey(a.ctx, rec)
}

// ForgetContactKey removes everything Pelton learned about addr's keys. A
// key the user imported into the keyring stays; that is removed there.
func (a *App) ForgetContactKey(addr string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.ForgetContactKeys(a.ctx, autocrypt.NormalizeAddr(addr))
}

// ContactKeyDTO is one correspondent as the settings list shows them.
type ContactKeyDTO struct {
	Email       string `json:"email"`
	Fingerprint string `json:"fingerprint"`
	Source      string `json:"source"`
	// FirstSeen is when Pelton first learned this key, RFC 3339.
	FirstSeen  string `json:"firstSeen"`
	Expires    string `json:"expires"`
	State      string `json:"state"`
	Problem    string `json:"problem"`
	Preference string `json:"preference"`
	Previous   string `json:"previous"`
}

// ListContactKeys returns every correspondent Pelton knows a key for, from
// Autocrypt and from lookups. Keys in the keyring are listed in the keyring
// section instead.
func (a *App) ListContactKeys() ([]ContactKeyDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	var addrs []string
	peers, err := a.store.ListAutocryptPeers(a.ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range peers {
		addrs = append(addrs, p.Addr)
	}
	found, err := a.store.ListFoundKeys(a.ctx)
	if err != nil {
		return nil, err
	}
	for _, f := range found {
		addrs = append(addrs, f.Addr)
	}
	accepted, err := a.store.ListAcceptedKeys(a.ctx)
	if err != nil {
		return nil, err
	}
	for _, ak := range accepted {
		addrs = append(addrs, ak.Addr)
	}
	slices.Sort(addrs)
	addrs = slices.Compact(addrs)

	sources := a.keySources()
	out := make([]ContactKeyDTO, 0, len(addrs))
	for _, addr := range addrs {
		r := a.resolveRecipient(nil, sources, addr, false, autocrypt.PreferNoPreference)
		dto := ContactKeyDTO{Email: addr, Fingerprint: r.info.Fingerprint, Source: r.source, State: r.state(), Problem: r.problem, Preference: r.pref, Previous: r.previous}
		if r.key == nil && r.pref == "" && r.problem == "" {
			continue
		}
		if r.key != nil {
			if seen, ok, err := a.store.KeySightingFor(a.ctx, addr, crypto.NormalizeFingerprint(r.info.Fingerprint)); err == nil && ok {
				dto.FirstSeen = seen.FirstSeen.UTC().Format(time.RFC3339)
			}
			if !r.info.Expires.IsZero() {
				dto.Expires = r.info.Expires.UTC().Format(time.RFC3339)
			}
		}
		out = append(out, dto)
	}
	return out, nil
}
