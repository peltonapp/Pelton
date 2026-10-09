package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/crypto"
)

// keyDirName is the key directory under the app data dir, so a nightly build
// (which has its own data dir) keeps its own keys.
const keyDirName = "pgp"

// settingAccountPGPKey is prefixed with the account id. It records which
// private key that account signs with; only a fingerprint, never key material.
const settingAccountPGPKey = "pgp_key_account_"

// errNoKeyStore means the app started without a data directory, so there is
// nowhere to keep keys.
var errNoKeyStore = errors.New("pelton: the key store is unavailable")

// passphrases holds unlocked private key passphrases for the lifetime of the
// process, keyed by fingerprint. Anything in here was either typed this session
// or read from the OS keyring because the user asked for it to be remembered.
// It is never written to the settings database and never logged.
var (
	passphraseMu sync.Mutex
	passphrases  = map[string][]byte{}
)

// PGPKeyDTO is one key as the settings list shows it. It carries no key
// material, deliberately: the frontend has no use for it.
type PGPKeyDTO struct {
	Fingerprint string   `json:"fingerprint"`
	Name        string   `json:"name"`
	Email       string   `json:"email"`
	Emails      []string `json:"emails"`
	// Created and Expires are RFC 3339; Expires is empty when the key does not
	// expire.
	Created string `json:"created"`
	Expires string `json:"expires"`
	// Expired is resolved here rather than in the frontend so the list does not
	// depend on the renderer's clock.
	Expired    bool `json:"expired"`
	HasPrivate bool `json:"hasPrivate"`
	Locked     bool `json:"locked"`
	// Unlocked is true when this session already holds the passphrase.
	Unlocked bool `json:"unlocked"`
	// Remembered is true when the passphrase is in the OS keyring.
	Remembered bool   `json:"remembered"`
	Algorithm  string `json:"algorithm"`
	Bits       int    `json:"bits"`
}

// keyStore returns the key store rooted in the data directory, creating the
// directory with owner-only permissions the first time.
func (a *App) keyStore() (*crypto.PGPKeyStore, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if a.dataDir == "" {
		return nil, errNoKeyStore
	}
	dir := filepath.Join(a.dataDir, keyDirName)
	if err := crypto.EnsureKeyDir(dir); err != nil {
		return nil, err
	}
	return crypto.NewPGPKeyStore(dir), nil
}

// ListPGPKeys returns every imported key for the settings list.
func (a *App) ListPGPKeys() ([]PGPKeyDTO, error) {
	store, err := a.keyStore()
	if err != nil {
		return nil, err
	}
	keys, err := store.List()
	if err != nil {
		return nil, err
	}
	out := make([]PGPKeyDTO, 0, len(keys))
	for _, k := range keys {
		out = append(out, a.keyDTO(k))
	}
	return out, nil
}

// keyDTO converts a store record, resolving the session and keyring state that
// only this layer knows about.
func (a *App) keyDTO(k crypto.KeyInfo) PGPKeyDTO {
	dto := PGPKeyDTO{
		Fingerprint: k.Fingerprint,
		Name:        k.Name,
		Email:       k.Email,
		Emails:      k.Emails,
		Created:     k.Created.UTC().Format(time.RFC3339),
		HasPrivate:  k.HasPrivate,
		Locked:      k.Locked,
		Algorithm:   k.Algorithm,
		Bits:        k.Bits,
	}
	if !k.Expires.IsZero() {
		dto.Expires = k.Expires.UTC().Format(time.RFC3339)
		dto.Expired = k.Expires.Before(time.Now())
	}
	if k.Locked {
		fp := crypto.NormalizeFingerprint(k.Fingerprint)
		passphraseMu.Lock()
		_, dto.Unlocked = passphrases[fp]
		passphraseMu.Unlock()
		if stored, err := credentials.LoadPGPPassphrase(fp); err == nil && stored != "" {
			dto.Remembered = true
		}
	}
	return dto
}

// ImportPGPKey opens a file picker and imports whatever keys the chosen file
// holds. It reports the keys it added; a cancelled dialog returns none and no
// error.
func (a *App) ImportPGPKey() ([]PGPKeyDTO, error) {
	store, err := a.keyStore()
	if err != nil {
		return nil, err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import PGP key",
		Filters: []runtime.FileFilter{
			{DisplayName: "PGP keys (*.asc, *.gpg, *.pgp, *.key)", Pattern: "*.asc;*.gpg;*.pgp;*.key"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	added, err := store.ImportFile(path)
	if err != nil {
		return nil, err
	}
	out := make([]PGPKeyDTO, 0, len(added))
	for _, k := range added {
		out = append(out, a.keyDTO(k))
	}
	return out, nil
}

// DeletePGPKey removes a key from both rings, forgets any passphrase held for
// it, and drops it from any account that was signing with it.
func (a *App) DeletePGPKey(fingerprint string) error {
	store, err := a.keyStore()
	if err != nil {
		return err
	}
	if err := store.Delete(fingerprint); err != nil {
		return err
	}
	a.forgetPassphrase(fingerprint)

	// an account left pointing at a deleted key would fail to sign with a
	// confusing "key not found" later; clear it now instead.
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return err
	}
	for _, acct := range accounts {
		if a.stringSetting(settingAccountPGPKey+strconv.FormatInt(acct.ID, 10), "") == fingerprint {
			if err := a.store.Set(a.ctx, settingAccountPGPKey+strconv.FormatInt(acct.ID, 10), ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// ExportPGPKey writes one key to a file the user picks and returns the path, or
// "" if they cancelled. includePrivate writes the private half: that is the
// only backup path for it, since key material is deliberately kept out of the
// settings backup archive.
func (a *App) ExportPGPKey(fingerprint string, includePrivate bool) (string, error) {
	store, err := a.keyStore()
	if err != nil {
		return "", err
	}
	blob, err := store.Export(fingerprint, includePrivate)
	if err != nil {
		return "", err
	}
	suffix := "public"
	if includePrivate {
		suffix = "private"
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export PGP key",
		DefaultFilename: fmt.Sprintf("%s-%s.asc", shortFingerprint(fingerprint), suffix),
	})
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil
	}
	// a private key export is as sensitive as the ring it came from.
	perm := os.FileMode(0o644)
	if includePrivate {
		perm = 0o600
	}
	if err := os.WriteFile(dest, blob, perm); err != nil {
		return "", fmt.Errorf("pelton: write key file: %w", err)
	}
	return dest, nil
}

// UnlockPGPKey checks a passphrase against the key and, if it fits, holds it
// for the rest of the session. remember also puts it in the OS keyring so it
// survives a restart; that is opt-in per key.
func (a *App) UnlockPGPKey(fingerprint, passphrase string, remember bool) error {
	store, err := a.keyStore()
	if err != nil {
		return err
	}
	ent, err := store.SenderKeyByFingerprint(fingerprint)
	if err != nil {
		return err
	}
	// verify before holding on to it, so a typo surfaces here rather than at
	// the moment the user tries to send.
	if err := crypto.Unlock(ent, []byte(passphrase)); err != nil {
		return err
	}

	passphraseMu.Lock()
	passphrases[crypto.NormalizeFingerprint(fingerprint)] = []byte(passphrase)
	passphraseMu.Unlock()

	if remember {
		return credentials.StorePGPPassphrase(crypto.NormalizeFingerprint(fingerprint), passphrase)
	}
	return nil
}

// ForgetPGPPassphrase drops a passphrase from this session and from the keyring.
func (a *App) ForgetPGPPassphrase(fingerprint string) error {
	a.forgetPassphrase(fingerprint)
	return nil
}

// forgetPassphrase clears both the session copy and the remembered one. Keyring
// failures are logged rather than returned: the session copy is gone either
// way, and a delete that finds nothing is the normal case.
func (a *App) forgetPassphrase(fingerprint string) {
	fp := crypto.NormalizeFingerprint(fingerprint)
	passphraseMu.Lock()
	delete(passphrases, fp)
	passphraseMu.Unlock()
	if err := credentials.DeletePGPPassphrase(fp); err != nil {
		a.log.Warn("forget pgp passphrase", "err", err)
	}
}

// GetAccountPGPKey returns the fingerprint the account signs with, or "" when
// none is chosen.
func (a *App) GetAccountPGPKey(accountID int64) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	return a.stringSetting(settingAccountPGPKey+strconv.FormatInt(accountID, 10), ""), nil
}

// SetAccountPGPKey pins which private key an account signs with. An empty
// fingerprint clears it, falling back to matching the account address against
// the key's user ids.
func (a *App) SetAccountPGPKey(accountID int64, fingerprint string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if fingerprint != "" {
		store, err := a.keyStore()
		if err != nil {
			return err
		}
		// refuse a key that cannot sign, rather than storing a choice that
		// only fails later.
		if _, err := store.SenderKeyByFingerprint(fingerprint); err != nil {
			return err
		}
	}
	return a.store.Set(a.ctx, settingAccountPGPKey+strconv.FormatInt(accountID, 10), fingerprint)
}

// shortFingerprint renders the last 16 hex digits, the long key id, for file
// names and compact display.
func shortFingerprint(fp string) string {
	fp = crypto.NormalizeFingerprint(fp)
	if len(fp) <= 16 {
		return fp
	}
	return fp[len(fp)-16:]
}
