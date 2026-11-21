package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/mailvault"
)

// ErrMailVaultNotFound is returned for a vault id with no row.
var ErrMailVaultNotFound = errors.New("storage: mail vault not found")

// ErrAlreadyVaulted is returned when a folder or account is already covered
// by a vault.
var ErrAlreadyVaulted = errors.New("storage: already in a mail vault")

// Mail vault states. A vault seals its members' mail while sealing, is ready
// once every message is sealed, and unseals everything before it is removed.
const (
	MailVaultSealing   = "sealing"
	MailVaultReady     = "ready"
	MailVaultUnsealing = "unsealing"
)

// Mail vault notification modes: no notification at all, or one that names
// neither sender nor subject.
const (
	MailVaultNotifyNone    = "none"
	MailVaultNotifyGeneric = "generic"
)

// MailVault is one vault: its key material and its settings.
type MailVault struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	Material  mailvault.Material
	// AutoLockMinutes locks the vault after that long without use. 0 leaves
	// it to the app lock, sleep and quit.
	AutoLockMinutes int
	Notify          string
	HideLocked      bool
	AllowScripts    bool
	AllowMCP        bool
	AllowCLI        bool
	Failures        int
	RetryAt         time.Time
	State           string
	JobTotal        int
}

// MailVaultMember is a folder in a vault, or a whole account when FolderID is
// 0.
type MailVaultMember struct {
	VaultID   int64
	AccountID int64
	FolderID  int64
}

// VaultAudience says who a read is for. Sealed mail is visible only to a read
// whose audience the vault admits, and a context without one admits nothing.
type VaultAudience uint8

const (
	// VaultAudienceNone is every read that did not ask: background jobs,
	// aggregates, notifications. It never sees vault mail.
	VaultAudienceNone VaultAudience = iota
	// VaultAudienceUser is the mail space the user reads in.
	VaultAudienceUser
	// VaultAudienceScripts is a user script.
	VaultAudienceScripts
	// VaultAudienceMCP is an MCP client.
	VaultAudienceMCP
	// VaultAudienceCLI is the pelton command.
	VaultAudienceCLI
)

type vaultAudienceKey struct{}

// WithVaultAudience marks ctx as a read for audience. Only a read made with
// such a context can see the mail of an unlocked vault that admits it.
func WithVaultAudience(ctx context.Context, audience VaultAudience) context.Context {
	return context.WithValue(ctx, vaultAudienceKey{}, audience)
}

func vaultAudience(ctx context.Context) VaultAudience {
	a, _ := ctx.Value(vaultAudienceKey{}).(VaultAudience)
	return a
}

// VaultGate is what storage asks before showing or opening vault mail. The
// desktop's vault manager implements it; without one nothing in a vault is
// visible.
type VaultGate interface {
	// Visible reports whether the vault is unlocked and admits audience.
	Visible(vaultID int64, audience VaultAudience) bool
	// MessageKey returns the key of a sealed message, or an error when the
	// vault is locked. The gate owns the key; callers do not wipe it.
	MessageKey(vaultID int64, epk []byte) (*mailvault.MessageKey, error)
}

// vaultState is the store's view of the vaults: which folder belongs to
// which, and the gate that decides visibility.
type vaultState struct {
	mu      sync.RWMutex
	gate    VaultGate
	folders map[int64]int64
	loaded  bool
}

// SetVaultGate installs the gate that decides whether vault mail is visible.
func (d *DB) SetVaultGate(g VaultGate) {
	d.vault.mu.Lock()
	d.vault.gate = g
	d.vault.mu.Unlock()
}

func (d *DB) invalidateVaultFolders() {
	d.vault.mu.Lock()
	d.vault.loaded = false
	d.vault.mu.Unlock()
}

// vaultFolders returns folder id to vault id for every vaulted folder.
func (d *DB) vaultFolders(ctx context.Context) (map[int64]int64, VaultGate, error) {
	d.vault.mu.RLock()
	if d.vault.loaded {
		f, g := d.vault.folders, d.vault.gate
		d.vault.mu.RUnlock()
		return f, g, nil
	}
	d.vault.mu.RUnlock()

	rows, err := d.sql.QueryContext(ctx, `SELECT folder_id, vault_id FROM mail_vault_folders`)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: load vault folders: %w", err)
	}
	defer rows.Close()
	m := map[int64]int64{}
	for rows.Next() {
		var f, v int64
		if err := rows.Scan(&f, &v); err != nil {
			return nil, nil, fmt.Errorf("storage: scan vault folder: %w", err)
		}
		m[f] = v
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("storage: iterate vault folders: %w", err)
	}
	d.vault.mu.Lock()
	d.vault.folders, d.vault.loaded = m, true
	g := d.vault.gate
	d.vault.mu.Unlock()
	return m, g, nil
}

// FolderVault returns the vault a folder belongs to, or 0.
func (d *DB) FolderVault(ctx context.Context, folderID int64) (int64, error) {
	m, _, err := d.vaultFolders(ctx)
	if err != nil {
		return 0, err
	}
	return m[folderID], nil
}

// VaultFolderIDs returns every folder of a vault.
func (d *DB) VaultFolderIDs(ctx context.Context, vaultID int64) ([]int64, error) {
	m, _, err := d.vaultFolders(ctx)
	if err != nil {
		return nil, err
	}
	var out []int64
	for f, v := range m {
		if v == vaultID {
			out = append(out, f)
		}
	}
	return out, nil
}

func folderHidden(folders map[int64]int64, gate VaultGate, audience VaultAudience, folderID int64) bool {
	v := folders[folderID]
	if v == 0 {
		return false
	}
	return gate == nil || !gate.Visible(v, audience)
}

// visibleFolderIDs drops the folders whose vault the context may not see.
func (d *DB) visibleFolderIDs(ctx context.Context, ids []int64) ([]int64, error) {
	folders, gate, err := d.vaultFolders(ctx)
	if err != nil {
		return nil, err
	}
	if len(folders) == 0 {
		return ids, nil
	}
	audience := vaultAudience(ctx)
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !folderHidden(folders, gate, audience, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// FolderVisible reports whether the context may see a folder's mail.
func (d *DB) FolderVisible(ctx context.Context, folderID int64) (bool, error) {
	ids, err := d.visibleFolderIDs(ctx, []int64{folderID})
	return len(ids) == 1, err
}

// visibleMessages drops the rows the context may not see and opens the
// sealed rows it may. A sealed row that does not authenticate is dropped.
func (d *DB) visibleMessages(ctx context.Context, msgs []Message) ([]Message, error) {
	folders, gate, err := d.vaultFolders(ctx)
	if err != nil {
		return nil, err
	}
	if len(folders) == 0 {
		return msgs, nil
	}
	audience := vaultAudience(ctx)
	out := msgs[:0]
	var sealed []*Message
	for _, m := range msgs {
		if folderHidden(folders, gate, audience, m.FolderID) {
			continue
		}
		out = append(out, m)
	}
	for i := range out {
		if out[i].VaultID != 0 {
			sealed = append(sealed, &out[i])
		}
	}
	if len(sealed) == 0 {
		return out, nil
	}
	bad, err := d.openSealed(ctx, gate, sealed)
	if err != nil {
		return nil, err
	}
	if len(bad) == 0 {
		return out, nil
	}
	kept := out[:0]
	for _, m := range out {
		if !bad[m.ID] {
			kept = append(kept, m)
		}
	}
	return kept, nil
}

// visibleMessage is visibleMessages for one row: ErrMessageNotFound when the
// context may not see it, ErrSealedTampered when it does not open.
func (d *DB) visibleMessage(ctx context.Context, m *Message) error {
	folders, gate, err := d.vaultFolders(ctx)
	if err != nil {
		return err
	}
	if folderHidden(folders, gate, vaultAudience(ctx), m.FolderID) {
		return ErrMessageNotFound
	}
	if m.VaultID == 0 {
		return nil
	}
	bad, err := d.openSealed(ctx, gate, []*Message{m})
	if err != nil {
		return err
	}
	if bad[m.ID] {
		return ErrSealedTampered
	}
	return nil
}

// notVaulted is the predicate every aggregate over messages carries: vault
// mail is never part of one, locked or not.
const notVaulted = `folder_id NOT IN (SELECT folder_id FROM mail_vault_folders)`

// notVaultedM is notVaulted for a query that names messages m.
const notVaultedM = `m.folder_id NOT IN (SELECT folder_id FROM mail_vault_folders)`

const selectMailVault = `
SELECT id, uid, name, created_at, pass_salt, pass_time, pass_memory, pass_threads, pass_box,
       rec_salt, rec_time, rec_memory, rec_threads, rec_box, public_key, private_box, link_key,
       auto_lock_minutes, notify, hide_locked, allow_scripts, allow_mcp, allow_cli, failures, retry_at,
       state, job_total
FROM mail_vaults`

func scanMailVault(row rowScanner) (*MailVault, error) {
	var (
		v                        MailVault
		created, retry           string
		hide, scripts, mcp, cli  int
		pt, pm, pth, rt, rm, rth int64
	)
	mat := &v.Material
	if err := row.Scan(&v.ID, &mat.UID, &v.Name, &created,
		&mat.Passphrase.Salt, &pt, &pm, &pth, &mat.Passphrase.Box,
		&mat.Recovery.Salt, &rt, &rm, &rth, &mat.Recovery.Box,
		&mat.PublicKey, &mat.PrivateBox, &mat.LinkKey,
		&v.AutoLockMinutes, &v.Notify, &hide, &scripts, &mcp, &cli, &v.Failures, &retry,
		&v.State, &v.JobTotal); err != nil {
		return nil, err
	}
	mat.Passphrase.KDF = mailvault.KDFParams{Time: uint32(pt), MemoryKiB: uint32(pm), Threads: uint8(pth)}
	mat.Recovery.KDF = mailvault.KDFParams{Time: uint32(rt), MemoryKiB: uint32(rm), Threads: uint8(rth)}
	v.HideLocked, v.AllowScripts, v.AllowMCP, v.AllowCLI = hide != 0, scripts != 0, mcp != 0, cli != 0
	if t, err := parseTime(created); err == nil {
		v.CreatedAt = t
	}
	if retry != "" {
		if t, err := parseTime(retry); err == nil {
			v.RetryAt = t
		}
	}
	return &v, nil
}

// CreateMailVault stores a new vault with its members, in the sealing state.
// A folder or account that is already in a vault is ErrAlreadyVaulted.
func (d *DB) CreateMailVault(ctx context.Context, v *MailVault, members []MailVaultMember) (int64, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("storage: begin vault create: %w", err)
	}
	defer tx.Rollback()
	m := v.Material
	res, err := tx.ExecContext(ctx, `
INSERT INTO mail_vaults (uid, name, created_at, pass_salt, pass_time, pass_memory, pass_threads, pass_box,
    rec_salt, rec_time, rec_memory, rec_threads, rec_box, public_key, private_box, link_key,
    auto_lock_minutes, notify, hide_locked, allow_scripts, allow_mcp, state)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.UID, v.Name, nowText(),
		m.Passphrase.Salt, m.Passphrase.KDF.Time, m.Passphrase.KDF.MemoryKiB, m.Passphrase.KDF.Threads, m.Passphrase.Box,
		m.Recovery.Salt, m.Recovery.KDF.Time, m.Recovery.KDF.MemoryKiB, m.Recovery.KDF.Threads, m.Recovery.Box,
		m.PublicKey, m.PrivateBox, m.LinkKey,
		v.AutoLockMinutes, notifyMode(v.Notify), boolToInt(v.HideLocked), boolToInt(v.AllowScripts),
		boolToInt(v.AllowMCP), MailVaultSealing)
	if err != nil {
		return 0, fmt.Errorf("storage: insert vault: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("storage: vault id: %w", err)
	}
	for _, mem := range members {
		mem.VaultID = id
		if err := addVaultMember(ctx, tx, mem); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("storage: commit vault create: %w", err)
	}
	d.invalidateVaultFolders()
	v.ID = id
	return id, nil
}

func notifyMode(s string) string {
	if s == MailVaultNotifyNone {
		return MailVaultNotifyNone
	}
	return MailVaultNotifyGeneric
}

func addVaultMember(ctx context.Context, tx *sql.Tx, mem MailVaultMember) error {
	var taken int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM mail_vault_members WHERE account_id = ? AND (folder_id = 0 OR folder_id = ? OR ? = 0)`,
		mem.AccountID, mem.FolderID, mem.FolderID).Scan(&taken); err != nil {
		return fmt.Errorf("storage: check vault member: %w", err)
	}
	if taken > 0 {
		return ErrAlreadyVaulted
	}
	if mem.FolderID != 0 {
		var owner int64
		if err := tx.QueryRowContext(ctx, `SELECT account_id FROM folders WHERE id = ?`, mem.FolderID).Scan(&owner); err != nil {
			return fmt.Errorf("storage: vault member folder %d: %w", mem.FolderID, err)
		}
		if owner != mem.AccountID {
			return fmt.Errorf("storage: folder %d is not in account %d", mem.FolderID, mem.AccountID)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mail_vault_members (vault_id, account_id, folder_id) VALUES (?, ?, ?)`,
		mem.VaultID, mem.AccountID, mem.FolderID); err != nil {
		return fmt.Errorf("storage: insert vault member: %w", err)
	}
	q := `INSERT OR IGNORE INTO mail_vault_folders (folder_id, vault_id) SELECT id, ? FROM folders WHERE id = ?`
	args := []any{mem.VaultID, mem.FolderID}
	if mem.FolderID == 0 {
		q = `INSERT OR IGNORE INTO mail_vault_folders (folder_id, vault_id) SELECT id, ? FROM folders WHERE account_id = ?`
		args = []any{mem.VaultID, mem.AccountID}
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("storage: insert vault folders: %w", err)
	}
	return nil
}

// AddMailVaultMember adds a folder or a whole account to an existing vault
// and puts the vault back into the sealing state.
func (d *DB) AddMailVaultMember(ctx context.Context, mem MailVaultMember) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin vault member: %w", err)
	}
	defer tx.Rollback()
	if err := addVaultMember(ctx, tx, mem); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mail_vaults SET state = ? WHERE id = ? AND state = ?`,
		MailVaultSealing, mem.VaultID, MailVaultReady); err != nil {
		return fmt.Errorf("storage: reopen vault sealing: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit vault member: %w", err)
	}
	d.invalidateVaultFolders()
	return nil
}

// GetMailVault returns one vault, or ErrMailVaultNotFound.
func (d *DB) GetMailVault(ctx context.Context, id int64) (*MailVault, error) {
	v, err := scanMailVault(d.sql.QueryRowContext(ctx, selectMailVault+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMailVaultNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get vault %d: %w", id, err)
	}
	return v, nil
}

// ListMailVaults returns every vault, oldest first.
func (d *DB) ListMailVaults(ctx context.Context) ([]MailVault, error) {
	rows, err := d.sql.QueryContext(ctx, selectMailVault+` ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list vaults: %w", err)
	}
	defer rows.Close()
	var out []MailVault
	for rows.Next() {
		v, err := scanMailVault(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan vault: %w", err)
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// ListMailVaultMembers returns what the user put in a vault.
func (d *DB) ListMailVaultMembers(ctx context.Context, vaultID int64) ([]MailVaultMember, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT vault_id, account_id, folder_id FROM mail_vault_members WHERE vault_id = ? ORDER BY account_id, folder_id`, vaultID)
	if err != nil {
		return nil, fmt.Errorf("storage: list vault members: %w", err)
	}
	defer rows.Close()
	var out []MailVaultMember
	for rows.Next() {
		var m MailVaultMember
		if err := rows.Scan(&m.VaultID, &m.AccountID, &m.FolderID); err != nil {
			return nil, fmt.Errorf("storage: scan vault member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateMailVaultSettings stores a vault's settings.
func (d *DB) UpdateMailVaultSettings(ctx context.Context, v MailVault) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE mail_vaults SET name = ?, auto_lock_minutes = ?, notify = ?, hide_locked = ?, allow_scripts = ?, allow_mcp = ?,
    allow_cli = ?
WHERE id = ?`, v.Name, max(v.AutoLockMinutes, 0), notifyMode(v.Notify), boolToInt(v.HideLocked),
		boolToInt(v.AllowScripts), boolToInt(v.AllowMCP), boolToInt(v.AllowCLI), v.ID)
	if err != nil {
		return fmt.Errorf("storage: update vault %d: %w", v.ID, err)
	}
	return requireOneRow(res, ErrMailVaultNotFound)
}

// SetMailVaultPassphrase replaces the passphrase wrap of a vault's data key.
func (d *DB) SetMailVaultPassphrase(ctx context.Context, id int64, w mailvault.Wrapped) error {
	res, err := d.sql.ExecContext(ctx, `
UPDATE mail_vaults SET pass_salt = ?, pass_time = ?, pass_memory = ?, pass_threads = ?, pass_box = ? WHERE id = ?`,
		w.Salt, w.KDF.Time, w.KDF.MemoryKiB, w.KDF.Threads, w.Box, id)
	if err != nil {
		return fmt.Errorf("storage: rewrap vault %d: %w", id, err)
	}
	return requireOneRow(res, ErrMailVaultNotFound)
}

// SetMailVaultFailures records wrong unlock attempts in a row and when the
// next attempt is allowed, so a restart does not reset the wait.
func (d *DB) SetMailVaultFailures(ctx context.Context, id int64, failures int, retryAt time.Time) error {
	retry := ""
	if !retryAt.IsZero() {
		retry = formatTime(retryAt)
	}
	if _, err := d.sql.ExecContext(ctx, `UPDATE mail_vaults SET failures = ?, retry_at = ? WHERE id = ?`,
		failures, retry, id); err != nil {
		return fmt.Errorf("storage: vault failures %d: %w", id, err)
	}
	return nil
}

// SetMailVaultState records a vault's job state and, when starting a job, how
// many messages it covers.
func (d *DB) SetMailVaultState(ctx context.Context, id int64, state string, total int) error {
	if _, err := d.sql.ExecContext(ctx, `UPDATE mail_vaults SET state = ?, job_total = ? WHERE id = ?`,
		state, total, id); err != nil {
		return fmt.Errorf("storage: vault state %d: %w", id, err)
	}
	return nil
}
