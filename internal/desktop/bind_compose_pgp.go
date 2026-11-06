package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Protection is what the user asked for on one message, as the compose window
// names it. It maps onto a crypto.Mode; the string form exists so the ui and
// the stored per-account default do not depend on an integer enum.
const (
	protectionNone        = "none"
	protectionSign        = "sign"
	protectionEncrypt     = "encrypt"
	protectionSignEncrypt = "signencrypt"
)

// accountDefaultAuto encrypts and signs whenever every recipient has a key, and
// sends in the clear when they do not. It is a default, not a promise: the
// compose window always shows what is actually going to happen.
const (
	accountDefaultOff  = ""
	accountDefaultSign = "sign"
	accountDefaultAuto = "auto"
)

// errBccWithEncryption refuses a combination that would give away the thing bcc
// exists to hide. An OpenPGP message names every recipient key it was encrypted
// to, so a bcc'd reader would be listed for everyone else to see.
var errBccWithEncryption = errors.New("pelton: an encrypted message cannot have Bcc recipients, because everyone on it would see who they are. Send them a separate copy")

// errNoProtectionKeys means the message asked for protection the key store
// cannot provide.
var errNoProtectionKeys = errors.New("pelton: no keys are imported, so this message cannot be signed or encrypted")

// validPGPDefault keeps an unknown value from reaching the send path, where it
// would silently mean "unprotected" anyway.
func validPGPDefault(value string) string {
	switch value {
	case accountDefaultSign, accountDefaultAuto:
		return value
	default:
		return accountDefaultOff
	}
}

// cryptoMode maps the ui's word onto the engine's mode.
func cryptoMode(protection string) crypto.Mode {
	switch protection {
	case protectionSign:
		return crypto.ModeSign
	case protectionEncrypt:
		return crypto.ModeEncrypt
	case protectionSignEncrypt:
		return crypto.ModeSignEncrypt
	default:
		return crypto.ModeNone
	}
}

// encrypts and signs report what a protection choice involves.
func encrypts(protection string) bool {
	return protection == protectionEncrypt || protection == protectionSignEncrypt
}

func signs(protection string) bool {
	return protection == protectionSign || protection == protectionSignEncrypt
}

// RecipientKeyDTO says whether one address can be encrypted to.
type RecipientKeyDTO struct {
	Email string `json:"email"`
	// HasKey is false when no public key in the store carries this address.
	HasKey bool `json:"hasKey"`
}

// ProtectionStatusDTO is what the compose window needs to offer the controls
// honestly: whether this account can sign at all, whether every recipient can
// be encrypted to, and which ones cannot.
type ProtectionStatusDTO struct {
	// CanSign is true when the account's own address has a private key.
	CanSign bool `json:"canSign"`
	// SignerLocked is true when that key needs a passphrase Pelton does not
	// currently hold, so sending will ask for one.
	SignerLocked bool `json:"signerLocked"`
	// CanEncrypt is true when every recipient given has a public key. With no
	// recipients yet it is false, since there is nobody to encrypt to.
	CanEncrypt bool `json:"canEncrypt"`
	// Recipients reports each address checked, so the ui can name the ones
	// holding encryption back rather than saying "someone".
	Recipients []RecipientKeyDTO `json:"recipients"`
	// Default is the account's configured starting point: '' (off), 'sign' or
	// 'auto'.
	Default string `json:"default"`
	// Suggested is what the compose window should start with for this account
	// and this set of recipients, resolved from Default and what the keys allow.
	Suggested string `json:"suggested"`
}

// ComposeProtectionStatus reports what signing and encryption are possible for
// a message from this account to these recipients. The compose window calls it
// as the recipient list changes, so the controls never offer something that
// would fail at send time.
func (a *App) ComposeProtectionStatus(accountID int64, recipients []string) (ProtectionStatusDTO, error) {
	if err := a.ready(); err != nil {
		return ProtectionStatusDTO{}, err
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return ProtectionStatusDTO{}, err
	}
	status := ProtectionStatusDTO{Default: account.PGPDefault}

	keys, err := a.keyStore()
	if err != nil {
		// no key store at all: everything stays off, which is not an error the
		// compose window should be stopped by.
		status.Suggested = protectionNone
		return status, nil
	}

	if signer, err := keys.SenderKey(account.Email); err == nil {
		status.CanSign = true
		status.SignerLocked = crypto.KeyLocked(signer) && a.passphraseFor(crypto.Fingerprint(signer)) == nil
	}

	status.CanEncrypt = len(recipients) > 0
	for _, email := range recipients {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		_, err := keys.RecipientKey(email)
		has := err == nil
		if !has {
			status.CanEncrypt = false
		}
		status.Recipients = append(status.Recipients, RecipientKeyDTO{Email: email, HasKey: has})
	}

	status.Suggested = suggestedProtection(account.PGPDefault, status.CanSign, status.CanEncrypt)
	return status, nil
}

// suggestedProtection resolves the account default against what is actually
// possible. A default can never turn into a send that would fail: "sign always"
// with no key of one's own is simply off.
func suggestedProtection(accountDefault string, canSign, canEncrypt bool) string {
	switch accountDefault {
	case accountDefaultSign:
		if canSign {
			return protectionSign
		}
	case accountDefaultAuto:
		switch {
		case canEncrypt && canSign:
			return protectionSignEncrypt
		case canEncrypt:
			return protectionEncrypt
		case canSign:
			return protectionSign
		}
	}
	return protectionNone
}

// protectionOptions builds the crypto inputs for one send, or an error saying
// exactly what is missing. It resolves every recipient key up front: a message
// to five people where one has no key is refused here rather than silently
// going out in the clear.
func (a *App) protectionOptions(account storage.Account, req ComposeRequest) (crypto.Mode, crypto.Options, crypto.Engine, error) {
	mode := cryptoMode(req.Protection)
	if mode == crypto.ModeNone {
		return crypto.ModeNone, crypto.Options{}, nil, nil
	}
	if encrypts(req.Protection) && len(req.Bcc) > 0 {
		return 0, crypto.Options{}, nil, errBccWithEncryption
	}

	keys, err := a.keyStore()
	if err != nil {
		return 0, crypto.Options{}, nil, errNoProtectionKeys
	}

	opts := crypto.Options{SenderEmail: account.Email}

	if encrypts(req.Protection) {
		var missing []string
		for _, group := range [][]AddressDTO{req.To, req.Cc, req.Bcc} {
			for _, addr := range group {
				email := strings.ToLower(strings.TrimSpace(addr.Email))
				if email == "" {
					continue
				}
				if _, err := keys.RecipientKey(email); err != nil {
					missing = append(missing, email)
					continue
				}
				opts.Recipients = append(opts.Recipients, email)
			}
		}
		if len(missing) > 0 {
			return 0, crypto.Options{}, nil, fmt.Errorf("pelton: no public key for %s, so this message cannot be encrypted to them: %w",
				strings.Join(missing, ", "), crypto.ErrRecipientKeyNotFound)
		}
		if len(opts.Recipients) == 0 {
			return 0, crypto.Options{}, nil, crypto.ErrNoRecipients
		}
	}

	if signs(req.Protection) {
		signer, err := keys.SenderKey(account.Email)
		if err != nil {
			return 0, crypto.Options{}, nil, fmt.Errorf("pelton: no private key for %s, so this message cannot be signed: %w",
				account.Email, crypto.ErrSenderKeyNotFound)
		}
		if crypto.KeyLocked(signer) {
			opts.Passphrase = a.passphraseFor(crypto.Fingerprint(signer))
			if opts.Passphrase == nil {
				return 0, crypto.Options{}, nil, crypto.ErrPassphraseRequired
			}
		}
	}

	return mode, opts, crypto.NewPGP(keys), nil
}

// passphraseFor returns a passphrase held for a key this session, or one the
// user asked the os keyring to remember. Nil means the key is still locked, and
// the caller must refuse rather than send in the clear.
func (a *App) passphraseFor(fingerprint string) []byte {
	fp := crypto.NormalizeFingerprint(fingerprint)

	passphraseMu.Lock()
	held, ok := passphrases[fp]
	passphraseMu.Unlock()
	if ok {
		return held
	}

	stored, err := credentials.LoadPGPPassphrase(fp)
	if err != nil || stored == "" {
		return nil
	}
	passphraseMu.Lock()
	passphrases[fp] = []byte(stored)
	passphraseMu.Unlock()
	return []byte(stored)
}

// SignerFingerprint returns the fingerprint of the key an account signs with,
// so the compose window can send the user to the right passphrase prompt. Empty
// when the account has no signing key.
func (a *App) SignerFingerprint(accountID int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return "", err
	}
	keys, err := a.keyStore()
	if err != nil {
		return "", nil
	}
	signer, err := keys.SenderKey(account.Email)
	if err != nil {
		return "", nil
	}
	return crypto.Fingerprint(signer), nil
}

// sealDraft encrypts a compose request to the sender's own key and returns the
// armored form. The whole request goes in, not only the body: the subject and
// the recipient list of an encrypted message are as much the point as the text.
func (a *App) sealDraft(req ComposeRequest) (string, error) {
	account, err := a.store.GetAccount(a.ctx, req.AccountID)
	if err != nil {
		return "", err
	}
	keys, err := a.keyStore()
	if err != nil {
		return "", errNoProtectionKeys
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("pelton: prepare draft for sealing: %w", err)
	}
	sealed, err := crypto.NewPGP(keys).Seal(account.Email, payload)
	if err != nil {
		return "", fmt.Errorf("pelton: this draft cannot be saved because there is no key for %s to encrypt it to. Import your own key, or turn encryption off to save it: %w",
			account.Email, err)
	}
	return string(sealed), nil
}

// unsealDraft opens a sealed draft. A nil passphrase means "use whatever this
// session already holds", which is how the drafts list opens what it can
// without prompting for anything.
func (a *App) unsealDraft(draft storedDraft, passphrase []byte) (ComposeRequest, error) {
	keys, err := a.keyStore()
	if err != nil {
		return ComposeRequest{}, err
	}
	if len(passphrase) == 0 {
		passphrase = a.draftPassphrase(draft.AccountID)
	}
	plain, err := crypto.NewPGP(keys).Unseal([]byte(draft.Sealed), passphrase)
	if err != nil {
		return ComposeRequest{}, err
	}
	var req ComposeRequest
	if err := json.Unmarshal(plain, &req); err != nil {
		return ComposeRequest{}, fmt.Errorf("pelton: this draft could not be read back: %w", err)
	}
	// the Message-ID kept outside the seal is the one the server copy carries.
	req.DraftMessageID = draft.Request.DraftMessageID
	// the passphrase worked, so hold it for the rest of the session and save
	// the user answering the same prompt for the next draft.
	if len(passphrase) > 0 {
		a.holdDraftPassphrase(draft.AccountID, passphrase)
	}
	return req, nil
}

// draftPassphrase finds a passphrase already held for the account's own key.
func (a *App) draftPassphrase(accountID int64) []byte {
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return a.anyPassphrase()
	}
	keys, err := a.keyStore()
	if err != nil {
		return nil
	}
	signer, err := keys.SenderKey(account.Email)
	if err != nil {
		return a.anyPassphrase()
	}
	if held := a.passphraseFor(crypto.Fingerprint(signer)); held != nil {
		return held
	}
	return a.anyPassphrase()
}

// holdDraftPassphrase remembers a working passphrase for this session, filed
// under the account's own key when there is one. It is never written to the
// settings database and never put in the os keyring from here: storing it
// belongs to the Encryption pane, where the user is asked about it explicitly.
func (a *App) holdDraftPassphrase(accountID int64, passphrase []byte) {
	key := sessionPassphraseKey
	if account, err := a.store.GetAccount(a.ctx, accountID); err == nil {
		if keys, err := a.keyStore(); err == nil {
			if signer, err := keys.SenderKey(account.Email); err == nil {
				key = crypto.NormalizeFingerprint(crypto.Fingerprint(signer))
			}
		}
	}
	passphraseMu.Lock()
	passphrases[key] = passphrase
	passphraseMu.Unlock()
}
