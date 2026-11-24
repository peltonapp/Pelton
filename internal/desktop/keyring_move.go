package desktop

import (
	"os"
	"path/filepath"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/crypto"
)

// settingKeyringCopied records that this install's secrets were copied from
// the shared "Pelton" keyring service into its own (a nightly or dev run), so
// the copy runs once.
const settingKeyringCopied = "keyring_copied_from_default"

// copyKeyringSecrets runs once for a build that files its secrets under its
// own keyring service: every secret the database knows of (accounts, their
// proxies, address books, remembered PGP passphrases, the global proxy and
// VirusTotal key) is copied from the "Pelton" service a build of this channel
// used before, when the own service has no entry of that name yet. The old
// entries stay. Nothing is enumerated: only those names are looked up. It
// runs before anything reads a secret.
func (a *App) copyKeyringSecrets() {
	if credentials.Service() == credentials.DefaultService || a.store == nil {
		return
	}
	if done, err := a.store.GetBool(a.ctx, settingKeyringCopied); err == nil && done {
		return
	}
	var known credentials.Known
	if accounts, err := a.store.ListAllAccounts(a.ctx); err == nil {
		for _, acc := range accounts {
			known.AccountIDs = append(known.AccountIDs, acc.ID)
		}
	} else {
		a.log.Error("keyring copy: list accounts", "err", err)
		return
	}
	if books, err := a.store.ListAddressBooks(a.ctx); err == nil {
		for _, b := range books {
			known.AddressBookIDs = append(known.AddressBookIDs, b.ID)
		}
	} else {
		a.log.Error("keyring copy: list address books", "err", err)
		return
	}
	if a.dataDir != "" {
		dir := filepath.Join(a.dataDir, keyDirName)
		if _, err := os.Stat(dir); err == nil {
			keys, err := crypto.NewPGPKeyStore(dir).List()
			if err != nil {
				a.log.Error("keyring copy: list pgp keys", "err", err)
				return
			}
			for _, k := range keys {
				if k.HasPrivate {
					known.PGPFingerprints = append(known.PGPFingerprints, k.Fingerprint)
				}
			}
		}
	}
	n, err := credentials.CopyFrom(credentials.DefaultService, known)
	if err != nil {
		// some entries could not be read or written: try again next start.
		a.log.Error("keyring copy", "copied", n, "err", err)
		return
	}
	if err := a.store.SetBool(a.ctx, settingKeyringCopied, true); err != nil {
		a.log.Error("keyring copy: record", "err", err)
	}
	a.log.Info("keyring copy", "service", credentials.Service(), "copied", n)
}
