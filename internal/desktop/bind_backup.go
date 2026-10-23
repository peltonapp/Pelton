package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Backup import/export lets a user move their configuration between installs
// through a plain JSON file, chosen per category. It is the local, file-based
// replacement for the removed folder-based config sync: Pelton writes and reads
// a file the user picks, and never talks to any server.

// backupCategorySettings and friends are the export/import category ids the
// ui checkboxes map to.
const (
	backupCategorySettings   = "settings"
	backupCategoryWhitelist  = "whitelist"
	backupCategoryMailboxes  = "mailboxes"
	backupCategorySignatures = "signatures"
)

// backupFileTag identifies a Pelton backup file so import can reject unrelated
// json.
const backupFileTag = "pelton-backup"

// backupSkipSettings are settings that must not travel between installs: the
// search watermark and the pending-download marker are local, transient state,
// the whitelist keys are exported under their own category instead, and local
// drafts reference account ids that don't exist on the target install (and can
// carry whole attachments). The skip applies on import too, so backup files
// written before a key landed here are also filtered.
var backupSkipSettings = map[string]bool{
	settingSearchWatermark: true,
	settingDownloadPending: true,
	settingRemoteSenders:   true,
	settingRemoteDomains:   true,
	draftsKey:              true,
}

// whitelistBackup is the trusted-sender allowlist as exported.
type whitelistBackup struct {
	Senders []string `json:"senders"`
	Domains []string `json:"domains"`
}

// mailboxBackup is one account's server configuration as exported. Passwords
// and tokens live in the os keyring, keyed by an account id that a fresh
// install doesn't have yet; by default they never appear here at all, so an
// imported mailbox needs its credentials re-entered once before it can sync.
// If the user opted in and gave an export password, Secret carries the
// account's credential (from the keyring) encrypted under that password -
// never in plain text, and never using the same password as anything else in
// Pelton (there's no vault-unlock password to reuse).
type mailboxBackup struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	// LocalLabel and UseLocalLabel are the mailbox's local name and whether it
	// is in use. A backup is the user's own copy, so restoring it puts the
	// mailboxes back under the names they had here. Older backups have neither
	// and restore under the display name, which is what they meant.
	LocalLabel    string `json:"localLabel,omitempty"`
	UseLocalLabel bool   `json:"useLocalLabel,omitempty"`
	Username      string `json:"username,omitempty"`
	IMAPHost      string `json:"imapHost"`
	IMAPPort      int    `json:"imapPort"`
	SMTPHost      string `json:"smtpHost"`
	SMTPPort      int    `json:"smtpPort"`
	// SyncMaxParallel is the mailbox's own parallel-sync override; absent
	// means it follows the global setting.
	SyncMaxParallel *int `json:"syncMaxParallel,omitempty"`
	// TrustedCerts and CAPEM are the certificates and CA the mailbox trusts
	// beyond the system roots. They are not secrets, and without them a
	// restored Proton Bridge or self-hosted mailbox could not connect.
	TrustedCerts []string       `json:"trustedCerts,omitempty"`
	CAPEM        string         `json:"caPem,omitempty"`
	Secret       *encryptedBlob `json:"secret,omitempty"`
}

// signatureBackup is one reusable header/footer block as exported.
type signatureBackup struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Format  string `json:"format"`
	Content string `json:"content"`
}

// BackupFileDTO is the on-disk backup document (and what import inspects).
type BackupFileDTO struct {
	Tag        string            `json:"tag"`
	Version    int               `json:"version"`
	CreatedAt  string            `json:"createdAt"`
	AppVersion string            `json:"appVersion"`
	Settings   map[string]string `json:"settings,omitempty"`
	Whitelist  *whitelistBackup  `json:"whitelist,omitempty"`
	Mailboxes  []mailboxBackup   `json:"mailboxes,omitempty"`
	Signatures []signatureBackup `json:"signatures,omitempty"`
}

// ExportData writes the selected categories to a user-chosen json file and
// returns its path, or an empty string if the dialog was cancelled.
// credentialPassword, when non-empty, additionally encrypts and includes each
// exported mailbox's stored credential; it is otherwise ignored.
func (a *App) ExportData(categories []string, credentialPassword string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	want := toSet(categories)

	doc := BackupFileDTO{
		Tag:        backupFileTag,
		Version:    1,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		AppVersion: a.version,
	}
	if want[backupCategorySettings] {
		settings, err := a.exportSettings()
		if err != nil {
			return "", err
		}
		doc.Settings = settings
	}
	if want[backupCategoryWhitelist] {
		doc.Whitelist = &whitelistBackup{Senders: a.remoteSenders(), Domains: a.remoteDomains()}
	}
	if want[backupCategoryMailboxes] {
		mailboxes, err := a.exportMailboxes(credentialPassword)
		if err != nil {
			return "", err
		}
		doc.Mailboxes = mailboxes
	}
	if want[backupCategorySignatures] {
		signatures, err := a.exportSignatures()
		if err != nil {
			return "", err
		}
		doc.Signatures = signatures
	}

	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: fmt.Sprintf("pelton-backup-%s.json", time.Now().Format("2006-01-02")),
		Title:           "Export Pelton data",
	})
	if err != nil {
		return "", err
	}
	if dest == "" {
		return "", nil
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	// 0600: the export always carries the full server config and settings, and
	// optionally encrypted mailbox credentials; no other local user should be
	// able to read it. The user can still loosen a file they intend to share.
	if err := os.WriteFile(filepath.Clean(dest), data, 0o600); err != nil {
		return "", err
	}
	// WriteFile keeps an existing file's mode, so overwriting an old export
	// left with looser permissions must be tightened explicitly.
	if err := os.Chmod(filepath.Clean(dest), 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

// exportSettings returns every persisted setting except the local/transient and
// whitelist keys (the whitelist has its own category).
func (a *App) exportSettings() (map[string]string, error) {
	all, err := a.store.AllSettings(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(all))
	for _, s := range all {
		if backupSkipSettings[s.Key] {
			continue
		}
		out[s.Key] = s.Value
	}
	return out, nil
}

// exportMailboxes returns every account's server configuration for the
// mailboxes backup category. When credentialPassword is non-empty, each
// account's stored credential (if it has one) is also encrypted under that
// password and attached; accounts with no stored credential (e.g. never
// finished setup) are simply exported without one.
func (a *App) exportMailboxes(credentialPassword string) ([]mailboxBackup, error) {
	accounts, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mailboxBackup, 0, len(accounts))
	for _, acc := range accounts {
		// Local Folders has no server configuration to carry anywhere, and the
		// imported mail it holds is not part of a settings backup.
		if acc.Local {
			continue
		}
		m := mailboxBackup{
			Email:         acc.Email,
			DisplayName:   acc.DisplayName,
			LocalLabel:    acc.LocalLabel,
			UseLocalLabel: acc.UseLocalLabel,
			Username:      acc.Username,
			IMAPHost:      acc.IMAPHost,
			IMAPPort:      acc.IMAPPort,
			SMTPHost:      acc.SMTPHost,
			SMTPPort:      acc.SMTPPort,
			TrustedCerts:  acc.TrustedCerts,
			CAPEM:         acc.CAPEM,
		}
		if acc.SyncMaxParallel != nil {
			n := *acc.SyncMaxParallel
			m.SyncMaxParallel = &n
		}
		if credentialPassword != "" {
			secret, err := credentials.Load(acc.ID)
			if err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, err
			}
			if err == nil {
				encoded, err := json.Marshal(secret)
				if err != nil {
					return nil, err
				}
				blob, err := encryptWithPassword(credentialPassword, encoded)
				if err != nil {
					return nil, err
				}
				m.Secret = blob
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// exportSignatures returns every saved signature for the signatures backup
// category.
func (a *App) exportSignatures() ([]signatureBackup, error) {
	signatures, err := a.store.ListSignatures(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]signatureBackup, 0, len(signatures))
	for _, s := range signatures {
		out = append(out, signatureBackup{Name: s.Name, Kind: s.Kind, Format: s.Format, Content: s.Content})
	}
	return out, nil
}

// BackupInfoDTO describes a picked backup file so the import ui can show what it
// holds (and when it was made) before the user commits to importing.
type BackupInfoDTO struct {
	Path                    string `json:"path"`
	CreatedAt               string `json:"createdAt"`
	AppVersion              string `json:"appVersion"`
	HasSettings             bool   `json:"hasSettings"`
	HasWhitelist            bool   `json:"hasWhitelist"`
	HasMailboxes            bool   `json:"hasMailboxes"`
	HasSignatures           bool   `json:"hasSignatures"`
	HasEncryptedCredentials bool   `json:"hasEncryptedCredentials"`
	SettingCount            int    `json:"settingCount"`
	MailboxCount            int    `json:"mailboxCount"`
	SignatureCount          int    `json:"signatureCount"`
}

// InspectBackupFile opens a file picker and parses the chosen backup, returning
// what it contains. An empty Path means the dialog was cancelled.
func (a *App) InspectBackupFile() (BackupInfoDTO, error) {
	if err := a.ready(); err != nil {
		return BackupInfoDTO{}, err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Import Pelton data",
		Filters: []runtime.FileFilter{{DisplayName: "Pelton backup (*.json)", Pattern: "*.json"}},
	})
	if err != nil {
		return BackupInfoDTO{}, err
	}
	if path == "" {
		return BackupInfoDTO{}, nil
	}
	doc, err := readBackupFile(path)
	if err != nil {
		return BackupInfoDTO{}, err
	}
	return BackupInfoDTO{
		Path:                    path,
		CreatedAt:               doc.CreatedAt,
		AppVersion:              doc.AppVersion,
		HasSettings:             len(doc.Settings) > 0,
		HasWhitelist:            doc.Whitelist != nil,
		HasMailboxes:            len(doc.Mailboxes) > 0,
		HasSignatures:           len(doc.Signatures) > 0,
		HasEncryptedCredentials: anyMailboxHasSecret(doc.Mailboxes),
		SettingCount:            len(doc.Settings),
		MailboxCount:            len(doc.Mailboxes),
		SignatureCount:          len(doc.Signatures),
	}, nil
}

// anyMailboxHasSecret reports whether at least one exported mailbox carries
// an encrypted credential, so the import ui only asks for a password when
// there's actually something to decrypt.
func anyMailboxHasSecret(mailboxes []mailboxBackup) bool {
	for _, m := range mailboxes {
		if m.Secret != nil {
			return true
		}
	}
	return false
}

// ImportData applies the selected categories from the backup file at path.
// credentialPassword, when non-empty, additionally decrypts and restores any
// mailbox credentials the file carries; it is otherwise ignored. A wrong
// password surfaces as an error (GCM's auth tag check fails on any key
// mismatch) rather than silently skipping the credentials.
func (a *App) ImportData(path string, categories []string, credentialPassword string) error {
	if err := a.ready(); err != nil {
		return err
	}
	doc, err := readBackupFile(path)
	if err != nil {
		return err
	}
	want := toSet(categories)
	if want[backupCategorySettings] {
		for key, value := range doc.Settings {
			if backupSkipSettings[key] {
				continue
			}
			if err := a.store.Set(a.ctx, key, value); err != nil {
				return err
			}
		}
	}
	if want[backupCategoryWhitelist] && doc.Whitelist != nil {
		if err := a.store.SetJSON(a.ctx, settingRemoteSenders, doc.Whitelist.Senders); err != nil {
			return err
		}
		if err := a.store.SetJSON(a.ctx, settingRemoteDomains, doc.Whitelist.Domains); err != nil {
			return err
		}
	}
	if want[backupCategoryMailboxes] {
		ready, err := a.importMailboxes(doc.Mailboxes, credentialPassword)
		if err != nil {
			return err
		}
		// sync and idle the new (and newly healed) accounts in the background,
		// same as the setup wizard does, so an import with credentials starts
		// pulling mail right away instead of waiting for a restart. Accounts
		// without credentials drop out of both with errNoCredentials, which is
		// not an error here.
		for _, acc := range ready {
			a.startAccountWorker(acc.ID)
		}
	}
	if want[backupCategorySignatures] {
		if err := a.importSignatures(doc.Signatures); err != nil {
			return err
		}
	}
	return nil
}

// importMailboxes recreates accounts from their backed-up server config and
// returns the ones now ready to start syncing, skipping any email already
// present locally so a re-import never duplicates a mailbox. When
// credentialPassword is non-empty and an entry carries an encrypted
// credential, it's decrypted and stored in the keyring; otherwise (no
// password given, or the entry has no credential) the mailbox still needs its
// password re-entered once before it can sync, same as before this existed.
//
// Every credential is decrypted up front, before anything is created: a
// wrong export password then fails cleanly with nothing half-imported, and
// the retry with the right password starts from the same clean state (#68).
func (a *App) importMailboxes(mailboxes []mailboxBackup, credentialPassword string) ([]storage.Account, error) {
	if len(mailboxes) == 0 {
		return nil, nil
	}
	secrets := make(map[string]credentials.Secret)
	if credentialPassword != "" {
		for _, m := range mailboxes {
			if m.Secret == nil {
				continue
			}
			plaintext, err := decryptWithPassword(credentialPassword, m.Secret)
			if err != nil {
				return nil, err
			}
			var secret credentials.Secret
			if err := json.Unmarshal(plaintext, &secret); err != nil {
				return nil, err
			}
			secrets[m.Email] = secret
		}
	}
	existing, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		return nil, err
	}
	have := make(map[string]int64, len(existing))
	for _, acc := range existing {
		have[acc.Email] = acc.ID
	}
	var ready []storage.Account
	for _, m := range mailboxes {
		if id, ok := have[m.Email]; ok {
			// the account exists but may have no stored credential - the
			// leftover of an import that failed after creating it (the pre-#68
			// bug). storing the credential now heals it, and it joins the
			// post-import sync like a fresh account.
			secret, hasSecret := secrets[m.Email]
			if !hasSecret {
				continue
			}
			if _, err := credentials.Load(id); err == nil {
				continue
			}
			if err := credentials.Store(id, secret); err != nil {
				return ready, err
			}
			for _, acc := range existing {
				if acc.ID == id {
					ready = append(ready, acc)
					break
				}
			}
			continue
		}
		account := storage.Account{
			Email:         m.Email,
			DisplayName:   m.DisplayName,
			LocalLabel:    m.LocalLabel,
			UseLocalLabel: m.UseLocalLabel,
			Username:      m.Username,
			IMAPHost:      m.IMAPHost,
			IMAPPort:      m.IMAPPort,
			SMTPHost:      m.SMTPHost,
			SMTPPort:      m.SMTPPort,
			TrustedCerts:  m.TrustedCerts,
			CAPEM:         m.CAPEM,
		}
		id, err := a.store.CreateAccount(a.ctx, &account)
		if err != nil {
			return ready, err
		}
		if m.SyncMaxParallel != nil {
			n := clampSyncMaxParallel(*m.SyncMaxParallel)
			if err := a.store.SetAccountSyncMaxParallel(a.ctx, id, &n); err != nil {
				return ready, err
			}
			account.SyncMaxParallel = &n
		}
		have[m.Email] = id
		ready = append(ready, account)
		if secret, ok := secrets[m.Email]; ok {
			if err := credentials.Store(id, secret); err != nil {
				return ready, err
			}
		}
	}
	return ready, nil
}

// importSignatures recreates signatures from the backup, skipping any
// name+kind pair already present locally so a re-import never duplicates one.
func (a *App) importSignatures(signatures []signatureBackup) error {
	if len(signatures) == 0 {
		return nil
	}
	existing, err := a.store.ListSignatures(a.ctx)
	if err != nil {
		return err
	}
	have := make(map[string]bool, len(existing))
	for _, s := range existing {
		have[s.Kind+"\x00"+s.Name] = true
	}
	for _, s := range signatures {
		key := s.Kind + "\x00" + s.Name
		if have[key] {
			continue
		}
		_, err := a.store.CreateSignature(a.ctx, &storage.Signature{
			Name:    s.Name,
			Kind:    s.Kind,
			Format:  s.Format,
			Content: s.Content,
		})
		if err != nil {
			return err
		}
		have[key] = true
	}
	return nil
}

// readBackupFile reads and validates a Pelton backup json file.
func readBackupFile(path string) (BackupFileDTO, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return BackupFileDTO{}, err
	}
	var doc BackupFileDTO
	if err := json.Unmarshal(data, &doc); err != nil {
		return BackupFileDTO{}, fmt.Errorf("pelton: not a valid backup file: %w", err)
	}
	if doc.Tag != backupFileTag {
		return BackupFileDTO{}, fmt.Errorf("pelton: not a Pelton backup file")
	}
	return doc, nil
}

// toSet turns a category slice into a lookup set.
func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}
