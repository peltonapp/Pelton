package desktop

import (
	"errors"
	"fmt"
	"strings"

	"github.com/peltonapp/Pelton/internal/autocrypt"
	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/smtp"
	"github.com/peltonapp/Pelton/internal/storage"
)

// errReplyDowngrade refuses to answer an encrypted message in the clear
// unless the user chose that on purpose.
var errReplyDowngrade = errors.New("pelton: this is a reply to an encrypted message. Encrypt it, or confirm that it may go out unencrypted")

// errNoOwnKey means the sending mailbox has no key it can use yet.
var errNoOwnKey = errors.New("pelton: this mailbox has no encryption key it can use yet, so the message cannot be encrypted")

// messageProtection decides the crypto for one send. With automatic
// encryption off, or for a message from anywhere but its composer, this is
// the keyring path as before, plus the Autocrypt header when that is on. It
// also returns the keys it encrypted to, for pinning once the message is
// queued.
func (a *App) messageProtection(account storage.Account, req ComposeRequest, msg *smtp.Message) (crypto.Mode, crypto.Options, crypto.Engine, []resolvedKey, error) {
	if !a.autoEncryptOn() {
		mode, opts, engine, err := a.protectionOptions(account, req)
		return mode, opts, engine, nil, err
	}
	keys, kerr := a.keyStore()
	if a.autocryptOn() && kerr == nil {
		if header, ok := a.autocryptHeader(keys, account); ok {
			msg.Headers = append(msg.Headers, [2]string{autocrypt.HeaderName, header})
		}
	}
	if !req.AutoEncrypt {
		mode, opts, engine, err := a.protectionOptions(account, req)
		return mode, opts, engine, nil, err
	}
	if !encrypts(req.Protection) {
		if !req.PlaintextConfirmed && a.replyToEncrypted(req.InReplyTo) {
			return 0, crypto.Options{}, nil, nil, errReplyDowngrade
		}
		mode, opts, engine, err := a.protectionOptions(account, req)
		return mode, opts, engine, nil, err
	}
	if kerr != nil {
		return 0, crypto.Options{}, nil, nil, errNoProtectionKeys
	}
	return a.autoEncryptOptions(keys, account, req, msg)
}

// autocryptHeader is the account's Autocrypt header value, false when it has
// no usable key.
func (a *App) autocryptHeader(keys *crypto.PGPKeyStore, account storage.Account) (string, bool) {
	ent, err := keys.SenderKey(account.Email)
	if err != nil {
		return "", false
	}
	data, err := keys.AutocryptKeyData(crypto.Fingerprint(ent), account.Email, a.autoNow())
	if err != nil {
		return "", false
	}
	return autocrypt.Format(autocrypt.Header{
		Addr: autocrypt.NormalizeAddr(account.Email), PreferEncrypt: a.ownPrefer(account.ID), KeyData: data,
	}), true
}

// autoEncryptOptions resolves every recipient's key and builds the crypto
// inputs. Any recipient without a usable key, or whose key changed and was
// not accepted, stops the send: it never goes out to them unencrypted.
func (a *App) autoEncryptOptions(keys *crypto.PGPKeyStore, account storage.Account, req ComposeRequest, msg *smtp.Message) (crypto.Mode, crypto.Options, crypto.Engine, []resolvedKey, error) {
	q := AutoEncryptQuery{AccountID: account.ID, To: emailsOf(req.To), Cc: emailsOf(req.Cc), Bcc: emailsOf(req.Bcc), InReplyTo: req.InReplyTo}
	resolved, bcc := a.resolveAll(keys, account.ID, q, a.replyToEncrypted(req.InReplyTo))
	if len(resolved) == 0 {
		return 0, crypto.Options{}, nil, nil, crypto.ErrNoRecipients
	}
	var missing, changed []string
	for _, r := range resolved {
		switch {
		case r.key == nil:
			missing = append(missing, r.addr)
		case r.changed:
			changed = append(changed, r.addr)
		}
	}
	if len(missing) > 0 {
		return 0, crypto.Options{}, nil, nil, fmt.Errorf("pelton: no usable key for %s, so this message cannot be encrypted to them: %w",
			strings.Join(missing, ", "), crypto.ErrRecipientKeyNotFound)
	}
	if len(changed) > 0 {
		return 0, crypto.Options{}, nil, nil, fmt.Errorf("pelton: the key for %s changed. Accept the new key or send this message unencrypted",
			strings.Join(changed, ", "))
	}

	fp, pass, err := a.ownKey(keys, account.Email)
	if err != nil {
		return 0, crypto.Options{}, nil, nil, errNoOwnKey
	}
	self, err := keys.AutocryptKeyData(fp, account.Email, a.autoNow())
	if err != nil {
		return 0, crypto.Options{}, nil, nil, errNoOwnKey
	}

	opts := crypto.Options{SenderEmail: account.Email, Passphrase: pass}
	var visible []resolvedKey
	for i, r := range resolved {
		if bcc[i] {
			opts.HiddenKeys = append(opts.HiddenKeys, r.key)
			continue
		}
		opts.RecipientKeys = append(opts.RecipientKeys, r.key)
		visible = append(visible, r)
	}
	// the sender reads their own sent copy, so their key is always on it.
	opts.RecipientKeys = append(opts.RecipientKeys, self)

	msg.Protected = &smtp.ProtectedHeaders{HideSubject: a.boolSetting(settingAutoEncryptSubject, true)}
	if len(visible) > 1 && a.autocryptOn() {
		for _, r := range visible {
			msg.Protected.Gossip = append(msg.Protected.Gossip, autocrypt.FormatGossip(autocrypt.Header{Addr: r.addr, KeyData: r.key}))
		}
	}
	return cryptoMode(req.Protection), opts, crypto.NewPGP(keys), resolved, nil
}

func emailsOf(list []AddressDTO) []string {
	out := make([]string, 0, len(list))
	for _, ad := range list {
		out = append(out, ad.Email)
	}
	return out
}

// pinUsedKeys records the key each recipient of an encrypted message was sent
// with, the first time one is used for them. A later different key is then a
// change the user is told about.
func (a *App) pinUsedKeys(used []resolvedKey) {
	for _, r := range used {
		if r.key == nil {
			continue
		}
		rec, ok, err := a.store.AcceptedKeyFor(a.ctx, r.addr)
		if err != nil || (ok && rec.Fingerprint != "") {
			continue
		}
		rec.Addr = r.addr
		rec.Fingerprint = crypto.NormalizeFingerprint(r.info.Fingerprint)
		rec.Source = r.source
		rec.AcceptedAt = a.autoNow()
		if err := a.store.SaveAcceptedKey(a.ctx, rec); err != nil {
			a.log.Error("pin recipient key", "err", err)
		}
	}
}

// encryptionSummary is the line the before-send recipient confirmation shows
// about encryption, so a script that changed who a message goes to cannot
// change whether it is encrypted without the user seeing it. Empty with
// automatic encryption off.
func (a *App) encryptionSummary(req ComposeRequest) string {
	if !a.autoEncryptOn() {
		return ""
	}
	if !encrypts(req.Protection) {
		return "Encryption: off. This message would go out unencrypted."
	}
	keys, _ := a.keyStore()
	q := AutoEncryptQuery{AccountID: req.AccountID, To: emailsOf(req.To), Cc: emailsOf(req.Cc), Bcc: emailsOf(req.Bcc), InReplyTo: req.InReplyTo}
	var blocked []string
	if req.AutoEncrypt {
		resolved, _ := a.resolveAll(keys, req.AccountID, q, false)
		for _, r := range resolved {
			if r.key == nil || r.changed {
				blocked = append(blocked, r.addr)
			}
		}
	} else if keys != nil {
		for _, list := range [][]string{q.To, q.Cc, q.Bcc} {
			for _, e := range list {
				if _, err := keys.RecipientKey(e); err != nil {
					blocked = append(blocked, autocrypt.NormalizeAddr(e))
				}
			}
		}
	}
	if len(blocked) > 0 {
		return "Encryption: on, but there is no usable key for " + strings.Join(blocked, ", ") +
			", so the message will not be sent to anyone until that is fixed."
	}
	return "Encryption: on. Every recipient can read it."
}
