package desktop

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/peltonapp/Pelton/internal/mailvault"
	"github.com/peltonapp/Pelton/internal/storage"
)

// MailVaultMemberDTO is a folder in a vault, or a whole account when FolderID
// is 0.
type MailVaultMemberDTO struct {
	AccountID int64 `json:"accountId"`
	FolderID  int64 `json:"folderId"`
}

// MailVaultDTO is one vault as the ui shows it. Pending counts the messages
// still waiting to be sealed, or to be unsealed while the vault is being
// removed; Sealed those stored sealed.
type MailVaultDTO struct {
	ID              int64                `json:"id"`
	Name            string               `json:"name"`
	Locked          bool                 `json:"locked"`
	State           string               `json:"state"`
	Pending         int                  `json:"pending"`
	Sealed          int                  `json:"sealed"`
	Members         []MailVaultMemberDTO `json:"members"`
	FolderIDs       []int64              `json:"folderIds"`
	AutoLockMinutes int                  `json:"autoLockMinutes"`
	Notify          string               `json:"notify"`
	HideLocked      bool                 `json:"hideLocked"`
	AllowScripts    bool                 `json:"allowScripts"`
	AllowMCP        bool                 `json:"allowMcp"`
	RetryAfterMs    int64                `json:"retryAfterMs"`
}

// MailVaultCreateRequest makes a vault. The passphrase is used once to wrap
// the vault's key and is not kept.
type MailVaultCreateRequest struct {
	Name            string               `json:"name"`
	Passphrase      string               `json:"passphrase"`
	Members         []MailVaultMemberDTO `json:"members"`
	AutoLockMinutes int                  `json:"autoLockMinutes"`
	Notify          string               `json:"notify"`
	HideLocked      bool                 `json:"hideLocked"`
}

// MailVaultCreatedDTO is a new vault and its recovery code, which is shown
// once and stored nowhere.
type MailVaultCreatedDTO struct {
	ID           int64  `json:"id"`
	RecoveryCode string `json:"recoveryCode"`
}

// MailVaultAuthDTO is the outcome of a passphrase or recovery code. Reason
// is wrong, wait, damaged or weak when OK is false; RetryAfterMs is how long
// the next attempt has to wait.
type MailVaultAuthDTO struct {
	OK           bool   `json:"ok"`
	Reason       string `json:"reason,omitempty"`
	RetryAfterMs int64  `json:"retryAfterMs,omitempty"`
}

// MailVaultSettingsDTO is what the user can change about a vault without its
// passphrase.
type MailVaultSettingsDTO struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	AutoLockMinutes int    `json:"autoLockMinutes"`
	Notify          string `json:"notify"`
	HideLocked      bool   `json:"hideLocked"`
	AllowScripts    bool   `json:"allowScripts"`
	AllowMCP        bool   `json:"allowMcp"`
}

// errMailVaultLocked is returned for a change that needs the vault unlocked.
var errMailVaultLocked = errors.New("pelton: unlock the vault first")

// errMailVaultTempConsent is returned the first time a vault attachment would
// be written to a temporary file, so the ui can say so before it happens.
var errMailVaultTempConsent = errors.New("pelton: vault-temp-consent")

const settingMailVaultTempConsent = "mailVaultTempConsent"

// ListMailVaults returns every vault with its state and progress.
func (a *App) ListMailVaults() ([]MailVaultDTO, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	vaults, err := a.store.ListMailVaults(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MailVaultDTO, 0, len(vaults))
	for i := range vaults {
		dto, err := a.mailVaultDTO(&vaults[i])
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

func (a *App) mailVaultDTO(v *storage.MailVault) (MailVaultDTO, error) {
	dto := MailVaultDTO{
		ID: v.ID, Name: v.Name, Locked: !a.mailVaultUnlocked(v.ID), State: v.State,
		AutoLockMinutes: v.AutoLockMinutes, Notify: v.Notify, HideLocked: v.HideLocked,
		AllowScripts: v.AllowScripts, AllowMCP: v.AllowMCP,
		Members: []MailVaultMemberDTO{}, FolderIDs: []int64{},
	}
	if wait := time.Until(v.RetryAt); wait > 0 {
		dto.RetryAfterMs = wait.Milliseconds()
	}
	var err error
	if dto.Pending, dto.Sealed, err = a.store.CountVaultWork(a.ctx, v.ID); err != nil {
		return dto, err
	}
	members, err := a.store.ListMailVaultMembers(a.ctx, v.ID)
	if err != nil {
		return dto, err
	}
	for _, m := range members {
		dto.Members = append(dto.Members, MailVaultMemberDTO{AccountID: m.AccountID, FolderID: m.FolderID})
	}
	folders, err := a.store.VaultFolderIDs(a.ctx, v.ID)
	if err != nil {
		return dto, err
	}
	dto.FolderIDs = append(dto.FolderIDs, folders...)
	return dto, nil
}

func weakMailVaultPassphrase(p string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(p)) < minMailVaultPassphrase
}

// CreateMailVault makes a vault over the given folders and accounts and
// starts sealing their mail in the background. The vault starts unlocked.
// The recovery code in the result is the only copy there will ever be.
func (a *App) CreateMailVault(req MailVaultCreateRequest) (MailVaultCreatedDTO, error) {
	if err := a.ready(); err != nil {
		return MailVaultCreatedDTO{}, err
	}
	if weakMailVaultPassphrase(req.Passphrase) {
		return MailVaultCreatedDTO{}, fmt.Errorf("pelton: the passphrase needs at least %d characters", minMailVaultPassphrase)
	}
	if len(req.Members) == 0 {
		return MailVaultCreatedDTO{}, errors.New("pelton: choose a folder or an account for the vault")
	}
	material, code, keys, err := mailvault.Create([]byte(req.Passphrase), a.mailVaultKDF())
	if err != nil {
		return MailVaultCreatedDTO{}, err
	}
	v := &storage.MailVault{
		Name: strings.TrimSpace(req.Name), Material: material,
		AutoLockMinutes: max(req.AutoLockMinutes, 0), Notify: req.Notify, HideLocked: req.HideLocked,
	}
	members := make([]storage.MailVaultMember, 0, len(req.Members))
	for _, m := range req.Members {
		members = append(members, storage.MailVaultMember{AccountID: m.AccountID, FolderID: m.FolderID})
	}
	id, err := a.store.CreateMailVault(a.ctx, v, members)
	if err != nil {
		keys.Wipe()
		return MailVaultCreatedDTO{}, err
	}
	v.State = storage.MailVaultSealing
	a.unlockedMailVault(v, keys)
	return MailVaultCreatedDTO{ID: id, RecoveryCode: code}, nil
}

// AddToMailVault puts another folder, or a whole account when folderID is 0,
// into an unlocked vault and seals its mail.
func (a *App) AddToMailVault(vaultID, accountID, folderID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if !a.mailVaultUnlocked(vaultID) {
		return errMailVaultLocked
	}
	if err := a.store.AddMailVaultMember(a.ctx, storage.MailVaultMember{VaultID: vaultID, AccountID: accountID, FolderID: folderID}); err != nil {
		return err
	}
	a.startMailVaultJob(vaultID)
	a.emitMailVault(vaultID)
	return nil
}

// UnlockMailVault opens a vault with its passphrase. Wrong attempts past the
// first few wait longer each time, also across restarts.
func (a *App) UnlockMailVault(id int64, passphrase string) MailVaultAuthDTO {
	if a.ready() != nil {
		return MailVaultAuthDTO{Reason: mailVaultDamaged}
	}
	if a.mailVaultUnlocked(id) {
		return MailVaultAuthDTO{OK: true}
	}
	v, keys, res := a.tryMailVault(id, func(m mailvault.Material) (*mailvault.Keys, error) {
		return mailvault.Open(m, []byte(passphrase))
	})
	if res.OK {
		a.unlockedMailVault(v, keys)
	}
	return res
}

// RecoverMailVault opens a vault with its recovery code and sets a new
// passphrase. The recovery code keeps working afterwards.
func (a *App) RecoverMailVault(id int64, code, newPassphrase string) MailVaultAuthDTO {
	if a.ready() != nil {
		return MailVaultAuthDTO{Reason: mailVaultDamaged}
	}
	if weakMailVaultPassphrase(newPassphrase) {
		return MailVaultAuthDTO{Reason: mailVaultWeak}
	}
	v, keys, res := a.tryMailVault(id, func(m mailvault.Material) (*mailvault.Keys, error) {
		return mailvault.OpenWithRecovery(m, code)
	})
	if !res.OK {
		return res
	}
	w, err := keys.Rewrap([]byte(newPassphrase), a.mailVaultKDF())
	if err == nil {
		err = a.store.SetMailVaultPassphrase(a.ctx, id, w)
	}
	if err != nil {
		keys.Wipe()
		a.log.Error("set mail vault passphrase after recovery", "err", err)
		return MailVaultAuthDTO{Reason: mailVaultDamaged}
	}
	a.unlockedMailVault(v, keys)
	return res
}

// ChangeMailVaultPassphrase rewraps a vault's key under a new passphrase. The
// current one is checked like an unlock, with the same waits.
func (a *App) ChangeMailVaultPassphrase(id int64, current, next string) MailVaultAuthDTO {
	if a.ready() != nil {
		return MailVaultAuthDTO{Reason: mailVaultDamaged}
	}
	if weakMailVaultPassphrase(next) {
		return MailVaultAuthDTO{Reason: mailVaultWeak}
	}
	_, keys, res := a.tryMailVault(id, func(m mailvault.Material) (*mailvault.Keys, error) {
		return mailvault.Open(m, []byte(current))
	})
	if !res.OK {
		return res
	}
	defer keys.Wipe()
	w, err := keys.Rewrap([]byte(next), a.mailVaultKDF())
	if err == nil {
		err = a.store.SetMailVaultPassphrase(a.ctx, id, w)
	}
	if err != nil {
		a.log.Error("change mail vault passphrase", "err", err)
		return MailVaultAuthDTO{Reason: mailVaultDamaged}
	}
	return res
}

// LockMailVault locks one vault now.
func (a *App) LockMailVault(id int64) {
	a.lockMailVault(id)
}

// LockAllMailVaults locks every vault now.
func (a *App) LockAllMailVaults() {
	a.lockMailVaults()
}

// UpdateMailVaultSettings stores a vault's name, auto-lock time,
// notification mode, sidebar visibility and the scripts and MCP switches.
func (a *App) UpdateMailVaultSettings(s MailVaultSettingsDTO) error {
	if err := a.ready(); err != nil {
		return err
	}
	v, err := a.store.GetMailVault(a.ctx, s.ID)
	if err != nil {
		return err
	}
	v.Name, v.AutoLockMinutes, v.Notify = strings.TrimSpace(s.Name), max(s.AutoLockMinutes, 0), s.Notify
	v.HideLocked, v.AllowScripts, v.AllowMCP = s.HideLocked, s.AllowScripts, s.AllowMCP
	if err := a.store.UpdateMailVaultSettings(a.ctx, *v); err != nil {
		return err
	}
	a.applyMailVaultSettings(v)
	a.emitMailVault(v.ID)
	return nil
}

// RemoveMailVault unseals an unlocked vault's mail back into ordinary
// storage in the background and removes the vault once nothing is left
// sealed. It resumes at the next unlock if the app quits first.
func (a *App) RemoveMailVault(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if !a.mailVaultUnlocked(id) {
		return errMailVaultLocked
	}
	if err := a.store.SetMailVaultState(a.ctx, id, storage.MailVaultUnsealing, 0); err != nil {
		return err
	}
	a.startMailVaultJob(id)
	a.emitMailVault(id)
	return nil
}

// ConsentMailVaultTempFiles records that the user was told vault attachments
// are written to a private temporary folder while open.
func (a *App) ConsentMailVaultTempFiles() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.Set(a.ctx, settingMailVaultTempConsent, "true")
}
