package desktop

import (
	"bufio"
	"bytes"
	"mime"
	"net/textproto"
	"time"

	"github.com/peltonapp/Pelton/internal/autocrypt"
	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Incoming Autocrypt headers are read during sync, where the raw message is
// at hand, but only a quick parse of the header block happens there. The
// rest, the database work, runs on a goroutine of its own from a bounded
// queue; when sync outpaces it the queue drops headers rather than holding
// sync up, which costs at most a slightly late key.

// incomingQueueLimit bounds the queue of headers waiting to be applied.
const incomingQueueLimit = 1000

type incomingHeader struct {
	in autocrypt.Incoming
}

// observeIncoming takes a newly stored message's raw bytes during sync. With
// Autocrypt off it returns at once.
func (a *App) observeIncoming(folder storage.Folder, raw []byte) {
	if !a.autocryptOn() {
		return
	}
	switch folderRole(folder) {
	case roleJunk, roleSent, roleDrafts:
		return
	}
	in, ok := autocrypt.ReadIncoming(raw)
	if !ok {
		return
	}
	st := &a.autoEnc
	st.incomingMu.Lock()
	defer st.incomingMu.Unlock()
	if len(st.incoming) >= incomingQueueLimit {
		return
	}
	st.incoming = append(st.incoming, incomingHeader{in: in})
	if !st.draining {
		st.draining = true
		goSafe("reading Autocrypt headers", a.drainIncoming)
	}
}

// drainIncoming applies queued headers until the queue is empty.
func (a *App) drainIncoming() {
	st := &a.autoEnc
	own, _ := a.ownAddresses(a.ctx)
	for {
		st.incomingMu.Lock()
		if len(st.incoming) == 0 || a.ctx.Err() != nil {
			st.incoming = nil
			st.draining = false
			st.incomingMu.Unlock()
			return
		}
		batch := st.incoming
		st.incoming = nil
		st.incomingMu.Unlock()
		for _, h := range batch {
			if own[h.in.From] {
				continue
			}
			if err := a.applyIncoming(h.in); err != nil {
				a.log.Error("apply Autocrypt header", "err", err)
			}
		}
	}
}

// applyIncoming updates the sender's Autocrypt state from one message. A
// sender Pelton has no state for is only recorded when the message carries a
// header, so ordinary mail does not leave a row per sender behind.
func (a *App) applyIncoming(in autocrypt.Incoming) error {
	stored, ok, err := a.store.AutocryptPeerFor(a.ctx, in.From)
	if err != nil {
		return err
	}
	var header *autocrypt.Header
	if h, found := autocrypt.SelectHeader(in.Headers, in.From); found {
		header = &h
	}
	if !ok && header == nil {
		return nil
	}
	peer := peerFromStorage(stored)
	peer.Addr = in.From
	if peer.PreferEncrypt == "" {
		peer.PreferEncrypt = autocrypt.PreferNoPreference
	}
	effective := autocrypt.EffectiveDate(in.Date, a.autoNow())
	if !peer.Update(effective, header) {
		return nil
	}
	if err := a.store.SaveAutocryptPeer(a.ctx, peerToStorage(peer)); err != nil {
		return err
	}
	if header != nil {
		a.recordSeen(in.From, header.KeyData, keySourceAutocrypt, effective)
	}
	return nil
}

func (a *App) recordSeen(addr string, key []byte, source string, at time.Time) {
	info, err := crypto.InspectPublicKey(key, a.autoNow())
	if err != nil {
		return
	}
	if err := a.store.RecordKeySeen(a.ctx, addr, crypto.NormalizeFingerprint(info.Fingerprint), source, at); err != nil {
		a.log.Error("record key", "err", err)
	}
}

// learnGossip applies the Autocrypt-Gossip headers of a decrypted message.
// Only gossip about the message's own recipients counts, and never about the
// user's own addresses.
func (a *App) learnGossip(m storage.Message, entity []byte) {
	if !a.autocryptOn() {
		return
	}
	recipients := append(autocrypt.AddressesIn(m.ToAddresses), autocrypt.AddressesIn(m.CcAddresses)...)
	gossip := autocrypt.GossipHeaders(entity, recipients)
	if len(gossip) == 0 {
		return
	}
	own, _ := a.ownAddresses(a.ctx)
	effective := autocrypt.EffectiveDate(m.Date, a.autoNow())
	for _, g := range gossip {
		if own[g.Addr] {
			continue
		}
		stored, _, err := a.store.AutocryptPeerFor(a.ctx, g.Addr)
		if err != nil {
			continue
		}
		peer := peerFromStorage(stored)
		peer.Addr = g.Addr
		if peer.PreferEncrypt == "" {
			peer.PreferEncrypt = autocrypt.PreferNoPreference
		}
		if !peer.UpdateGossip(effective, g.KeyData) {
			continue
		}
		if err := a.store.SaveAutocryptPeer(a.ctx, peerToStorage(peer)); err != nil {
			a.log.Error("save gossip", "err", err)
			continue
		}
		a.recordSeen(g.Addr, g.KeyData, keySourceGossip, effective)
	}
}

// signatureKeysFor is the learned keys a message's signature is also checked
// against: the sender's, while automatic encryption is on.
func (a *App) signatureKeysFor(m storage.Message) [][]byte {
	if !a.autoEncryptOn() {
		return nil
	}
	from := autocrypt.AddressesIn(m.FromAddress)
	if len(from) != 1 {
		return nil
	}
	return a.knownKeysFor(from[0])
}

// protectedSubject returns the subject a decrypted message carries inside its
// encryption, or nothing when it marks no protected headers.
func protectedSubject(entity []byte) string {
	headers, _, found := bytes.Cut(entity, []byte("\r\n\r\n"))
	if !found {
		headers, _, _ = bytes.Cut(entity, []byte("\n\n"))
	}
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(append(headers, "\r\n\r\n"...))))
	h, err := r.ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return ""
	}
	_, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil || params["protected-headers"] == "" {
		return ""
	}
	subject := h.Get("Subject")
	if decoded, err := new(mime.WordDecoder).DecodeHeader(subject); err == nil {
		subject = decoded
	}
	return subject
}

// knownKeysFor is every key Pelton learned for an address, for checking a
// signature on mail from it.
func (a *App) knownKeysFor(addr string) [][]byte {
	var out [][]byte
	if p, ok, err := a.store.AutocryptPeerFor(a.ctx, addr); err == nil && ok {
		if len(p.PublicKey) > 0 {
			out = append(out, p.PublicKey)
		}
		if len(p.GossipKey) > 0 {
			out = append(out, p.GossipKey)
		}
	}
	if cached, err := a.store.CachedKeysFor(a.ctx, addr); err == nil {
		for _, c := range cached {
			if c.Key != nil {
				out = append(out, c.Key)
			}
		}
	}
	return out
}

// Signature words the reader shows.
const (
	sigKnown     = "known"
	sigVerified  = "verified"
	sigNew       = "new"
	sigInvalid   = "invalid"
	sigUnchecked = "unchecked"
)

// MessageEncryptionDTO is what the new reader says about a message's
// encryption and signature, in plain words.
type MessageEncryptionDTO struct {
	// Encrypted is true for mail that was encrypted on its way.
	Encrypted bool `json:"encrypted"`
	// Signature is '' (not signed), known (valid, from a key seen before),
	// verified (valid, from a key the user compared), new (valid, from a key
	// first seen with this message), invalid (the content was changed after
	// signing) or unchecked (no key to check it with).
	Signature   string `json:"signature"`
	SignerEmail string `json:"signerEmail"`
	// Subject is the real subject of a message that hid it, '' otherwise.
	Subject string `json:"subject"`
	// SetupMessage is true for a key transfer from one of the user's own
	// devices.
	SetupMessage bool `json:"setupMessage"`
}

// messageEncryption describes an opened message for the new reader. legacy
// is the verdict from the keyring alone and extended the one including keys
// Pelton learned.
func (a *App) messageEncryption(m storage.Message, opened *crypto.Opened) MessageEncryptionDTO {
	out := MessageEncryptionDTO{Encrypted: opened.Encrypted}
	if opened.Encrypted {
		out.Subject = protectedSubject(opened.Body)
	}
	ext := opened.Extended
	switch ext.Status {
	case crypto.SigInvalid:
		out.Signature = sigInvalid
	case crypto.SigUntrusted:
		out.Signature = sigUnchecked
	case crypto.SigValid:
		out.SignerEmail = ext.SignerEmail
		out.Signature = a.signatureTrust(m, ext.Fingerprint, opened.Signature.Status == crypto.SigValid)
	}
	return out
}

// signatureTrust says how well known a valid signature's key is: a key in the
// keyring or one first seen before this message is known, one the user
// compared is verified, and anything else is new.
func (a *App) signatureTrust(m storage.Message, fingerprint string, inKeyring bool) string {
	fp := crypto.NormalizeFingerprint(fingerprint)
	from := autocrypt.AddressesIn(m.FromAddress)
	if len(from) == 1 {
		if accepted, ok, err := a.store.AcceptedKeyFor(a.ctx, from[0]); err == nil && ok && accepted.Verified && accepted.Fingerprint == fp {
			return sigVerified
		}
	}
	if inKeyring {
		return sigKnown
	}
	if len(from) == 1 {
		if seen, ok, err := a.store.KeySightingFor(a.ctx, from[0], fp); err == nil && ok && !m.Date.IsZero() && seen.FirstSeen.Before(m.Date) {
			return sigKnown
		}
	}
	return sigNew
}
