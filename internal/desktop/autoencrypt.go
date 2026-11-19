package desktop

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/peltonapp/Pelton/internal/autocrypt"
	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/keylookup"
	"github.com/peltonapp/Pelton/internal/storage"
)

// Automatic encryption: Pelton keeps a key for each mailbox, learns other
// people's keys from the Autocrypt headers on their mail and, where the user
// allowed it, from their domain or a key server, and encrypts whenever every
// recipient can read the message. It is off until the user switches it on;
// before that nothing here generates, adds, looks up or records anything.

const (
	settingAutoEncrypt           = "auto_encrypt"
	settingKeySourceAutocrypt    = "key_source_autocrypt"
	settingKeySourceWKD          = "key_source_wkd"
	settingKeySourceKOO          = "key_source_keys_openpgp_org"
	settingKeySourceServers      = "key_source_servers"
	settingKeyServers            = "key_servers"
	settingAutoEncryptSubject    = "auto_encrypt_protect_subject"
	settingAutoEncryptOwnKeys    = "auto_encrypt_own_keys"
	settingAutoEncryptAccountPre = "auto_encrypt_account_"
)

// Account modes: auto turns encryption on whenever the recommendation says so
// and asks others to encrypt (prefer-encrypt=mutual); manual never turns it on
// by itself.
const (
	accountModeAuto   = "auto"
	accountModeManual = "manual"
)

// Where a key came from. A user-added key server is keySourceServerPrefix
// followed by its base URL.
const (
	keySourceManual       = "manual"
	keySourceAutocrypt    = "autocrypt"
	keySourceGossip       = "gossip"
	keySourceWKD          = "wkd"
	keySourceKOO          = "keys.openpgp.org"
	keySourceServerPrefix = "server:"
)

// EventAutoEncryptChanged fires when own keys were made or renewed, or a
// lookup learned keys, so open settings and composers refresh.
const EventAutoEncryptChanged = "autoencrypt:changed"

var errAutoEncryptOff = errors.New("pelton: automatic encryption is off")

// autoEncryptState is what the feature keeps in memory.
type autoEncryptState struct {
	// on mirrors the setting, so the sync path can check it per message
	// without a database read.
	on        atomic.Bool
	autocrypt atomic.Bool
	loaded    atomic.Bool

	// keysMu runs one own-key pass at a time.
	keysMu sync.Mutex

	// incoming is the bounded queue of headers sync handed over; draining
	// says a goroutine is working through it.
	incomingMu sync.Mutex
	incoming   []incomingHeader
	draining   bool

	// lookupMu guards inflight, the addresses being looked up right now.
	lookupMu sync.Mutex
	inflight map[string]chan struct{}

	// client, when set, replaces the network client; tests put a fake here.
	client *keylookup.Client
	// now, when set, replaces the clock.
	now func() time.Time
}

func (a *App) autoNow() time.Time {
	if a.autoEnc.now != nil {
		return a.autoEnc.now()
	}
	return time.Now()
}

// loadAutoEncryptFlags refreshes the in-memory switches from the settings.
func (a *App) loadAutoEncryptFlags() {
	on := a.boolSetting(settingAutoEncrypt, false)
	a.autoEnc.on.Store(on)
	a.autoEnc.autocrypt.Store(on && a.boolSetting(settingKeySourceAutocrypt, false))
	a.autoEnc.loaded.Store(true)
}

// autoEncryptOn reports whether the feature is switched on.
func (a *App) autoEncryptOn() bool {
	if !a.autoEnc.loaded.Load() {
		a.loadAutoEncryptFlags()
	}
	return a.autoEnc.on.Load()
}

// autocryptOn reports whether Autocrypt headers are read and written.
func (a *App) autocryptOn() bool {
	if !a.autoEnc.loaded.Load() {
		a.loadAutoEncryptFlags()
	}
	return a.autoEnc.autocrypt.Load()
}

// KeySourcesDTO is which places keys may be found in, and the user's own key
// servers.
type KeySourcesDTO struct {
	Autocrypt      bool     `json:"autocrypt"`
	WKD            bool     `json:"wkd"`
	KeysOpenPGPOrg bool     `json:"keysOpenpgpOrg"`
	KeyServers     bool     `json:"keyServers"`
	Servers        []string `json:"servers"`
}

// OwnKeyDTO is the key one mailbox encrypts and signs with.
type OwnKeyDTO struct {
	AccountID   int64  `json:"accountId"`
	Email       string `json:"email"`
	Fingerprint string `json:"fingerprint"`
	// Created and Expires are RFC 3339; Expires is empty for a key that does
	// not expire.
	Created string `json:"created"`
	Expires string `json:"expires"`
	// Generated is true for a key Pelton made and renews by itself.
	Generated bool `json:"generated"`
	// Usable is false for an expired or revoked key, or one Pelton cannot
	// unlock without asking.
	Usable bool `json:"usable"`
	// Mode is the account's mode: auto or manual.
	Mode string `json:"mode"`
}

// AutoEncryptSettingsDTO is everything the settings page shows.
type AutoEncryptSettingsDTO struct {
	Enabled        bool          `json:"enabled"`
	Sources        KeySourcesDTO `json:"sources"`
	ProtectSubject bool          `json:"protectSubject"`
	Accounts       []OwnKeyDTO   `json:"accounts"`
}

func (a *App) keySources() KeySourcesDTO {
	var servers []string
	_ = a.store.GetJSON(a.ctx, settingKeyServers, &servers)
	if servers == nil {
		servers = []string{}
	}
	return KeySourcesDTO{
		Autocrypt:      a.boolSetting(settingKeySourceAutocrypt, false),
		WKD:            a.boolSetting(settingKeySourceWKD, false),
		KeysOpenPGPOrg: a.boolSetting(settingKeySourceKOO, false),
		KeyServers:     a.boolSetting(settingKeySourceServers, false),
		Servers:        servers,
	}
}

// GetAutoEncryptSettings returns the state of automatic encryption.
func (a *App) GetAutoEncryptSettings() (AutoEncryptSettingsDTO, error) {
	if err := a.ready(); err != nil {
		return AutoEncryptSettingsDTO{}, err
	}
	out := AutoEncryptSettingsDTO{
		Enabled:        a.boolSetting(settingAutoEncrypt, false),
		Sources:        a.keySources(),
		ProtectSubject: a.boolSetting(settingAutoEncryptSubject, true),
		Accounts:       []OwnKeyDTO{},
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return out, err
	}
	keys, kerr := a.keyStore()
	generated := a.generatedKeys()
	for _, acct := range accounts {
		if acct.Local {
			continue
		}
		dto := OwnKeyDTO{AccountID: acct.ID, Email: acct.Email, Mode: a.accountMode(acct.ID)}
		if kerr == nil {
			if ent, err := keys.SenderKey(acct.Email); err == nil {
				fp := crypto.Fingerprint(ent)
				info, _ := crypto.InspectPublicKey(mustPublic(keys, fp), a.autoNow())
				dto.Fingerprint = fp
				dto.Created = info.Created.UTC().Format(time.RFC3339)
				if !info.Expires.IsZero() {
					dto.Expires = info.Expires.UTC().Format(time.RFC3339)
				}
				dto.Generated = slices.Contains(generated, fp)
				dto.Usable = info.Usable && (!crypto.KeyLocked(ent) || a.passphraseFor(fp) != nil)
			}
		}
		out.Accounts = append(out.Accounts, dto)
	}
	return out, nil
}

// mustPublic returns a key's public half, or nil.
func mustPublic(keys *crypto.PGPKeyStore, fingerprint string) []byte {
	blob, err := keys.Export(fingerprint, false)
	if err != nil {
		return nil
	}
	return blob
}

// EnableAutoEncrypt switches automatic encryption on with the sources the
// user allowed in the setup, and makes a key for every mailbox that has none.
// The keys are made in the background; EventAutoEncryptChanged says when.
func (a *App) EnableAutoEncrypt(sources KeySourcesDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.saveKeySources(sources); err != nil {
		return err
	}
	if err := a.store.SetBool(a.ctx, settingAutoEncrypt, true); err != nil {
		return err
	}
	a.loadAutoEncryptFlags()
	goSafe("making encryption keys", a.ensureOwnKeys)
	return nil
}

// DisableAutoEncrypt switches automatic encryption off. Keys and what was
// learned stay, so switching it back on does not start from nothing.
func (a *App) DisableAutoEncrypt() error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.SetBool(a.ctx, settingAutoEncrypt, false); err != nil {
		return err
	}
	a.loadAutoEncryptFlags()
	return nil
}

// SetKeySources changes which places keys may be found in.
func (a *App) SetKeySources(sources KeySourcesDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.saveKeySources(sources); err != nil {
		return err
	}
	a.loadAutoEncryptFlags()
	return nil
}

func (a *App) saveKeySources(s KeySourcesDTO) error {
	servers := make([]string, 0, len(s.Servers))
	for _, raw := range s.Servers {
		base, err := keylookup.NormalizeServer(raw)
		if err != nil {
			return fmt.Errorf("pelton: %q cannot be used as a key server: only https servers are", raw)
		}
		if !slices.Contains(servers, base) {
			servers = append(servers, base)
		}
	}
	for key, on := range map[string]bool{
		settingKeySourceAutocrypt: s.Autocrypt,
		settingKeySourceWKD:       s.WKD,
		settingKeySourceKOO:       s.KeysOpenPGPOrg,
		settingKeySourceServers:   s.KeyServers,
	} {
		if err := a.store.SetBool(a.ctx, key, on); err != nil {
			return err
		}
	}
	return a.store.SetJSON(a.ctx, settingKeyServers, servers)
}

// SetProtectSubject chooses whether encrypted mail hides its subject.
func (a *App) SetProtectSubject(on bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetBool(a.ctx, settingAutoEncryptSubject, on)
}

// SetAutoEncryptAccountMode sets a mailbox to auto or manual.
func (a *App) SetAutoEncryptAccountMode(accountID int64, mode string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if mode != accountModeAuto && mode != accountModeManual {
		return fmt.Errorf("pelton: unknown encryption mode %q", mode)
	}
	return a.store.Set(a.ctx, settingAutoEncryptAccountPre+strconv.FormatInt(accountID, 10), mode)
}

func (a *App) accountMode(accountID int64) string {
	if a.stringSetting(settingAutoEncryptAccountPre+strconv.FormatInt(accountID, 10), accountModeAuto) == accountModeManual {
		return accountModeManual
	}
	return accountModeAuto
}

// ownPrefer is what the account tells others in its Autocrypt header.
func (a *App) ownPrefer(accountID int64) string {
	if a.accountMode(accountID) == accountModeAuto {
		return autocrypt.PreferMutual
	}
	return autocrypt.PreferNoPreference
}

// generatedKeys lists the fingerprints of keys Pelton made itself.
func (a *App) generatedKeys() []string {
	var fps []string
	_ = a.store.GetJSON(a.ctx, settingAutoEncryptOwnKeys, &fps)
	return fps
}

// startAutoEncrypt runs at startup: with the feature on it makes keys for new
// mailboxes and renews keys close to expiry.
func (a *App) startAutoEncrypt() {
	a.loadAutoEncryptFlags()
	if a.autoEncryptOn() {
		goSafe("checking encryption keys", a.ensureOwnKeys)
	}
}

// newKeyPassphrase is the random passphrase a generated key is protected
// with. It goes to the OS credential store, the same place a passphrase the
// user asked Pelton to remember goes, and is never shown.
func newKeyPassphrase() ([]byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return []byte(base64.RawStdEncoding.EncodeToString(buf)), nil
}

// ensureOwnKeys gives every mailbox without a key one, and renews generated
// keys close to expiry. A mailbox whose key the user imported is left alone.
func (a *App) ensureOwnKeys() {
	if !a.autoEncryptOn() {
		return
	}
	a.autoEnc.keysMu.Lock()
	defer a.autoEnc.keysMu.Unlock()
	keys, err := a.keyStore()
	if err != nil {
		a.log.Error("encryption keys", "err", err)
		return
	}
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		a.log.Error("encryption keys", "err", err)
		return
	}
	generated := a.generatedKeys()
	changed := false
	for _, acct := range accounts {
		if acct.Local || strings.TrimSpace(acct.Email) == "" {
			continue
		}
		if ent, err := keys.SenderKey(acct.Email); err == nil {
			fp := crypto.Fingerprint(ent)
			if !slices.Contains(generated, fp) {
				continue
			}
			pass := a.passphraseFor(fp)
			if pass == nil {
				continue
			}
			if _, renewed, err := keys.Extend(fp, pass, a.autoNow()); err != nil {
				a.log.Error("renew encryption key", "account", acct.ID, "err", err)
			} else if renewed {
				changed = true
			}
			continue
		}
		fp, err := a.generateOwnKey(keys, acct)
		if err != nil {
			a.log.Error("make encryption key", "account", acct.ID, "err", err)
			continue
		}
		generated = append(generated, fp)
		changed = true
	}
	if changed {
		if err := a.store.SetJSON(a.ctx, settingAutoEncryptOwnKeys, generated); err != nil {
			a.log.Error("record encryption keys", "err", err)
		}
		a.emit(EventAutoEncryptChanged, nil)
	}
}

// generateOwnKey makes and files a key for one mailbox and remembers its
// passphrase. A key whose passphrase cannot be kept is removed again rather
// than left locked behind a passphrase nobody knows.
func (a *App) generateOwnKey(keys *crypto.PGPKeyStore, acct storage.Account) (string, error) {
	pass, err := newKeyPassphrase()
	if err != nil {
		return "", err
	}
	info, err := keys.Generate(acct.DisplayName, acct.Email, pass, a.autoNow())
	if err != nil {
		return "", err
	}
	fp := crypto.NormalizeFingerprint(info.Fingerprint)
	if err := credentials.StorePGPPassphrase(fp, string(pass)); err != nil {
		_ = keys.Delete(fp)
		return "", fmt.Errorf("pelton: the key's passphrase could not be stored, so no key was made: %w", err)
	}
	passphraseMu.Lock()
	passphrases[fp] = pass
	passphraseMu.Unlock()
	return fp, nil
}

// ownKey returns an account's own key fingerprint and its unlocking
// passphrase, nil when the key is not locked.
func (a *App) ownKey(keys *crypto.PGPKeyStore, email string) (string, []byte, error) {
	ent, err := keys.SenderKey(email)
	if err != nil {
		return "", nil, err
	}
	fp := crypto.Fingerprint(ent)
	if !crypto.KeyLocked(ent) {
		return fp, nil, nil
	}
	pass := a.passphraseFor(fp)
	if pass == nil {
		return "", nil, crypto.ErrPassphraseRequired
	}
	return fp, pass, nil
}

// BackupOwnKey writes a mailbox's key to a file the user picks, protected by
// a passphrase they chose, and returns the path; "" when they cancelled.
func (a *App) BackupOwnKey(accountID int64, passphrase string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	if len(passphrase) < 8 {
		return "", fmt.Errorf("pelton: choose a passphrase of at least 8 characters for the backup")
	}
	account, err := a.store.GetAccount(a.ctx, accountID)
	if err != nil {
		return "", err
	}
	keys, err := a.keyStore()
	if err != nil {
		return "", err
	}
	fp, current, err := a.ownKey(keys, account.Email)
	if err != nil {
		return "", err
	}
	blob, err := keys.ExportProtected(fp, current, []byte(passphrase))
	if err != nil {
		return "", err
	}
	dest, err := a.saveFileDialog(saveDialogOptions{
		Title:           "Back up encryption key",
		DefaultFilename: fmt.Sprintf("%s-%s-backup.asc", account.Email, shortFingerprint(fp)),
	})
	if err != nil || dest == "" {
		return "", err
	}
	if err := os.WriteFile(dest, blob, 0o600); err != nil {
		return "", fmt.Errorf("pelton: write key backup: %w", err)
	}
	return dest, nil
}
