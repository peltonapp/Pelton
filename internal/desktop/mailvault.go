package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/peltonapp/Pelton/internal/applock"
	"github.com/peltonapp/Pelton/internal/mailvault"
	"github.com/peltonapp/Pelton/internal/storage"
)

// EventMailVault is emitted when a mail vault locks, unlocks, changes its
// settings or moves along a sealing or unsealing job.
const EventMailVault = "mailvault:changed"

// MailVaultEvent is the payload of EventMailVault.
type MailVaultEvent struct {
	ID      int64  `json:"id"`
	Locked  bool   `json:"locked"`
	State   string `json:"state"`
	Pending int    `json:"pending"`
	Sealed  int    `json:"sealed"`
	Removed bool   `json:"removed"`
}

// mailVaultBatch is how many messages one step of a sealing or unsealing job
// moves before it reports progress.
const mailVaultBatch = 50

// mailVaultKeyCache bounds how many message keys an unlocked vault keeps, so
// scrolling a large folder does not redo an X25519 exchange per row.
const mailVaultKeyCache = 4096

// mailVaultState is the app's side of the mail vaults: which are unlocked and
// their keys, the running jobs and the private folder for opened attachments.
// Its zero value is every vault locked.
type mailVaultState struct {
	mu      sync.Mutex
	open    map[int64]*openMailVault
	jobs    map[int64]bool
	lastUse time.Time
	tempDir string
	// attempt serializes unlock attempts, so two at once cannot both slip
	// past the wait after a wrong passphrase.
	attempt sync.Mutex
	// kdf, when set, replaces mailvault.DefaultKDF for new wraps.
	kdf *mailvault.KDFParams
}

// openMailVault is an unlocked vault.
type openMailVault struct {
	keys     *mailvault.Keys
	scripts  bool
	mcp      bool
	autoLock time.Duration
	cache    map[string]*mailvault.MessageKey
	order    []string
}

func (o *openMailVault) wipe() {
	for _, k := range o.cache {
		k.Wipe()
	}
	o.cache, o.order = nil, nil
	o.keys.Wipe()
}

func (a *App) mailVaultKDF() mailvault.KDFParams {
	if a.mailVault.kdf != nil {
		return *a.mailVault.kdf
	}
	return mailvault.DefaultKDF
}

// mailCtx is the context the mail space reads with: it sees the mail of
// unlocked vaults. Everything else reads with a.ctx, which never does.
func (a *App) mailCtx() context.Context {
	return storage.WithVaultAudience(a.ctx, storage.VaultAudienceUser)
}

// scriptsCtx marks ctx as a script's read, which sees an unlocked vault only
// when the vault allows scripts.
func scriptsCtx(ctx context.Context) context.Context {
	return storage.WithVaultAudience(ctx, storage.VaultAudienceScripts)
}

// mcpCtx marks ctx as an MCP client's read, which sees an unlocked vault only
// when the vault allows MCP.
func mcpCtx(ctx context.Context) context.Context {
	return storage.WithVaultAudience(ctx, storage.VaultAudienceMCP)
}

// inMailVault reports whether a folder is in a vault, locked or not. A
// failure to tell counts as yes.
func (a *App) inMailVault(folderID int64) bool {
	if a.store == nil {
		return false
	}
	v, err := a.store.FolderVault(a.ctx, folderID)
	return err != nil || v != 0
}

// inMailVaultMessage is inMailVault for a message id, locked or not. A
// message it cannot place counts as in a vault.
func (a *App) inMailVaultMessage(id int64) bool {
	v, err := a.store.MessageVault(a.ctx, id)
	return err != nil || v != 0
}

// mailVaultGate is the storage.VaultGate the app installs.
type mailVaultGate struct{ a *App }

func (g mailVaultGate) Visible(vaultID int64, audience storage.VaultAudience) bool {
	st := &g.a.mailVault
	st.mu.Lock()
	defer st.mu.Unlock()
	o := st.open[vaultID]
	if o == nil {
		return false
	}
	switch audience {
	case storage.VaultAudienceUser:
		return true
	case storage.VaultAudienceScripts:
		return o.scripts
	case storage.VaultAudienceMCP:
		return o.mcp
	}
	return false
}

func (g mailVaultGate) MessageKey(vaultID int64, epk []byte) (*mailvault.MessageKey, error) {
	st := &g.a.mailVault
	st.mu.Lock()
	defer st.mu.Unlock()
	o := st.open[vaultID]
	if o == nil {
		return nil, storage.ErrVaultLocked
	}
	if k, ok := o.cache[string(epk)]; ok {
		return k, nil
	}
	k, err := o.keys.MessageKey(epk)
	if err != nil {
		return nil, err
	}
	if len(o.order) >= mailVaultKeyCache {
		delete(o.cache, o.order[0])
		o.order = o.order[1:]
	}
	o.cache[string(epk)] = k
	o.order = append(o.order, string(epk))
	return k, nil
}

// initMailVault installs the gate and picks up a sealing job a quit or a
// crash interrupted. Sealing needs no passphrase, so it resumes at once;
// unsealing resumes when its vault is next unlocked.
func (a *App) initMailVault() {
	a.store.SetVaultGate(mailVaultGate{a})
	a.sweepMailVaultTemp()
	vaults, err := a.store.ListMailVaults(a.ctx)
	if err != nil {
		a.log.Error("list mail vaults", "err", err)
		return
	}
	for _, v := range vaults {
		if v.State == storage.MailVaultSealing {
			a.startMailVaultJob(v.ID)
		}
	}
}

// touchMailVault records use of the app, which keeps unlocked vaults open.
func (a *App) touchMailVault() {
	a.mailVault.mu.Lock()
	a.mailVault.lastUse = time.Now()
	a.mailVault.mu.Unlock()
}

// mailVaultTick locks every vault that has gone unused for its auto-lock time.
func (a *App) mailVaultTick() {
	st := &a.mailVault
	st.mu.Lock()
	idle := time.Since(st.lastUse)
	var due []int64
	for id, o := range st.open {
		if o.autoLock > 0 && idle >= o.autoLock {
			due = append(due, id)
		}
	}
	st.mu.Unlock()
	for _, id := range due {
		a.lockMailVault(id)
	}
}

// lockMailVault wipes a vault's keys. When it was the last unlocked vault,
// the private folder of opened attachments is removed too.
func (a *App) lockMailVault(id int64) {
	st := &a.mailVault
	st.mu.Lock()
	o := st.open[id]
	delete(st.open, id)
	last := len(st.open) == 0
	st.mu.Unlock()
	if o == nil {
		return
	}
	o.wipe()
	if last {
		a.removeMailVaultTemp()
	}
	a.emitMailVault(id)
}

// lockMailVaults locks every vault: on the app lock, system sleep, the
// screen locking and quit.
func (a *App) lockMailVaults() {
	st := &a.mailVault
	st.mu.Lock()
	ids := make([]int64, 0, len(st.open))
	for id := range st.open {
		ids = append(ids, id)
	}
	st.mu.Unlock()
	for _, id := range ids {
		a.lockMailVault(id)
	}
	a.removeMailVaultTemp()
}

func (a *App) mailVaultUnlocked(id int64) bool {
	a.mailVault.mu.Lock()
	defer a.mailVault.mu.Unlock()
	return a.mailVault.open[id] != nil
}

// openMailVaultKeys registers unlocked keys with the vault's settings.
func (a *App) openMailVaultKeys(v *storage.MailVault, keys *mailvault.Keys) {
	st := &a.mailVault
	st.mu.Lock()
	if st.open == nil {
		st.open = map[int64]*openMailVault{}
	}
	if old := st.open[v.ID]; old != nil {
		old.wipe()
	}
	st.open[v.ID] = &openMailVault{
		keys: keys, scripts: v.AllowScripts, mcp: v.AllowMCP,
		autoLock: time.Duration(v.AutoLockMinutes) * time.Minute,
		cache:    map[string]*mailvault.MessageKey{},
	}
	st.lastUse = time.Now()
	st.mu.Unlock()
}

// applyMailVaultSettings updates an unlocked vault's switches in place.
func (a *App) applyMailVaultSettings(v *storage.MailVault) {
	st := &a.mailVault
	st.mu.Lock()
	defer st.mu.Unlock()
	if o := st.open[v.ID]; o != nil {
		o.scripts, o.mcp = v.AllowScripts, v.AllowMCP
		o.autoLock = time.Duration(v.AutoLockMinutes) * time.Minute
	}
}

// Reasons an unlock attempt fails, as the ui tells them apart.
const (
	mailVaultWrong   = "wrong"
	mailVaultWait    = "wait"
	mailVaultDamaged = "damaged"
	mailVaultWeak    = "weak"
)

// minMailVaultPassphrase is the shortest passphrase a vault accepts.
const minMailVaultPassphrase = 8

// tryMailVault runs one unlock attempt under the wait that wrong attempts
// build up, and records the outcome. open is the passphrase or recovery
// check; it only runs once the wait is over.
func (a *App) tryMailVault(id int64, open func(mailvault.Material) (*mailvault.Keys, error)) (*storage.MailVault, *mailvault.Keys, MailVaultAuthDTO) {
	st := &a.mailVault
	st.attempt.Lock()
	defer st.attempt.Unlock()
	v, err := a.store.GetMailVault(a.ctx, id)
	if err != nil {
		return nil, nil, MailVaultAuthDTO{Reason: mailVaultDamaged}
	}
	if wait := time.Until(v.RetryAt); wait > 0 {
		return nil, nil, MailVaultAuthDTO{Reason: mailVaultWait, RetryAfterMs: wait.Milliseconds()}
	}
	keys, err := open(v.Material)
	if errors.Is(err, mailvault.ErrWrongSecret) {
		failures := v.Failures + 1
		retry := time.Time{}
		if wait := applock.Backoff(failures); wait > 0 {
			retry = time.Now().Add(wait)
		}
		if err := a.store.SetMailVaultFailures(a.ctx, id, failures, retry); err != nil {
			a.log.Error("record mail vault failure", "err", err)
		}
		res := MailVaultAuthDTO{Reason: mailVaultWrong}
		if !retry.IsZero() {
			res.RetryAfterMs = time.Until(retry).Milliseconds()
		}
		return nil, nil, res
	}
	if err != nil {
		a.log.Error("open mail vault", "vault", id, "err", err)
		return nil, nil, MailVaultAuthDTO{Reason: mailVaultDamaged}
	}
	if v.Failures != 0 {
		if err := a.store.SetMailVaultFailures(a.ctx, id, 0, time.Time{}); err != nil {
			a.log.Error("reset mail vault failures", "err", err)
		}
	}
	return v, keys, MailVaultAuthDTO{OK: true}
}

// unlockedMailVault finishes an unlock: the keys go live, the vault's mail
// appears, and an unsealing job waiting for the passphrase carries on.
func (a *App) unlockedMailVault(v *storage.MailVault, keys *mailvault.Keys) {
	a.openMailVaultKeys(v, keys)
	if v.State != storage.MailVaultReady {
		a.startMailVaultJob(v.ID)
	}
	a.emitMailVault(v.ID)
}

func (a *App) emitMailVault(id int64) {
	ev := MailVaultEvent{ID: id, Locked: !a.mailVaultUnlocked(id)}
	if v, err := a.store.GetMailVault(a.ctx, id); err == nil {
		ev.State = v.State
		ev.Pending, ev.Sealed, _ = a.store.CountVaultWork(a.ctx, id)
	} else if errors.Is(err, storage.ErrMailVaultNotFound) {
		ev.Removed = true
	}
	a.emit(EventMailVault, ev)
}

// startMailVaultJob runs a vault's sealing or unsealing job in the
// background unless one is already running for it.
func (a *App) startMailVaultJob(id int64) {
	st := &a.mailVault
	st.mu.Lock()
	if st.jobs == nil {
		st.jobs = map[int64]bool{}
	}
	if st.jobs[id] {
		st.mu.Unlock()
		return
	}
	st.jobs[id] = true
	st.mu.Unlock()
	goSafe("sealing or unsealing a mail vault", func() {
		defer func() {
			st.mu.Lock()
			delete(st.jobs, id)
			st.mu.Unlock()
		}()
		a.runMailVaultJob(id)
	})
}

func (a *App) runMailVaultJob(id int64) {
	v, err := a.store.GetMailVault(a.ctx, id)
	if err != nil {
		return
	}
	switch v.State {
	case storage.MailVaultSealing:
		a.sealMailVault(v)
	case storage.MailVaultUnsealing:
		a.unsealMailVault(v)
	}
}

func (a *App) sealMailVault(v *storage.MailVault) {
	pending, sealed, err := a.store.CountVaultWork(a.ctx, v.ID)
	if err != nil {
		a.log.Error("count mail vault work", "err", err)
		return
	}
	if err := a.store.SetMailVaultState(a.ctx, v.ID, storage.MailVaultSealing, pending+sealed); err != nil {
		a.log.Error("start sealing", "err", err)
		return
	}
	for {
		n, err := a.store.SealPending(a.ctx, v.ID, mailVaultBatch)
		if err != nil {
			a.log.Error("seal mail vault", "vault", v.ID, "err", err)
			a.emitMailVault(v.ID)
			return
		}
		a.emitMailVault(v.ID)
		if n == 0 {
			break
		}
	}
	a.scrubAfterMailVault()
	if err := a.store.SetMailVaultState(a.ctx, v.ID, storage.MailVaultReady, pending+sealed); err != nil {
		a.log.Error("finish sealing", "err", err)
	}
	a.emitMailVault(v.ID)
}

// scrubAfterMailVault clears what moving mail into or out of a vault left
// behind: the full-text and thread indexes, the write-ahead log and the
// search index, which is built again from what is now stored in clear.
func (a *App) scrubAfterMailVault() {
	for try := 0; try < 5; try++ {
		truncated, err := a.store.ScrubAfterSealing(a.ctx)
		if err != nil {
			a.log.Error("scrub after sealing", "err", err)
			break
		}
		if truncated {
			break
		}
		time.Sleep(time.Second)
	}
	if err := a.rebuildSearchIndex(); err != nil {
		a.log.Error("rebuild search index after sealing", "err", err)
	}
}

func (a *App) unsealMailVault(v *storage.MailVault) {
	if !a.mailVaultUnlocked(v.ID) {
		return
	}
	for {
		n, err := a.store.UnsealPending(a.ctx, v.ID, mailVaultBatch)
		if err != nil {
			if !errors.Is(err, storage.ErrVaultLocked) {
				a.log.Error("unseal mail vault", "vault", v.ID, "err", err)
			}
			a.emitMailVault(v.ID)
			return
		}
		a.emitMailVault(v.ID)
		if n == 0 {
			break
		}
	}
	removed, err := a.store.DeleteMailVaultIfEmpty(a.ctx, v.ID)
	if err != nil || !removed {
		if err != nil {
			a.log.Error("remove mail vault", "err", err)
		}
		return
	}
	a.lockMailVault(v.ID)
	a.scrubAfterMailVault()
	a.emitMailVault(v.ID)
}

// mailVaultTempDir is the private folder vault attachments are opened from.
// It is made on first use, readable only by the user, and removed when the
// last vault locks and at quit.
func (a *App) mailVaultTempDir() (string, error) {
	st := &a.mailVault
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.tempDir != "" {
		return st.tempDir, nil
	}
	dir, err := os.MkdirTemp("", a.mailVaultTempPrefix())
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	st.tempDir = dir
	return dir, nil
}

// mailVaultTempFor returns the folder a vault file is written to for opening,
// a subfolder per attachment id when id is not 0. Until the user has been
// told about these files once it returns errMailVaultTempConsent instead.
func (a *App) mailVaultTempFor(id int64) (string, error) {
	if !a.boolSetting(settingMailVaultTempConsent, false) {
		return "", errMailVaultTempConsent
	}
	dir, err := a.mailVaultTempDir()
	if err != nil || id == 0 {
		return dir, err
	}
	return filepath.Join(dir, strconv.FormatInt(id, 10)), nil
}

// mailVaultTempPrefix names this install's temporary vault folders, so a
// dev or nightly build running beside it never sweeps the other's files.
func (a *App) mailVaultTempPrefix() string {
	sum := sha256.Sum256([]byte(a.dataDir))
	return "pelton-vault-" + hex.EncodeToString(sum[:4]) + "-"
}

// removeMailVaultTemp overwrites and removes the files opened from vaults.
func (a *App) removeMailVaultTemp() {
	st := &a.mailVault
	st.mu.Lock()
	dir := st.tempDir
	st.tempDir = ""
	st.mu.Unlock()
	if dir != "" {
		a.wipeTempDir(dir)
	}
}

// sweepMailVaultTemp removes the temporary vault folders a crash left behind.
func (a *App) sweepMailVaultTemp() {
	left, err := filepath.Glob(filepath.Join(os.TempDir(), a.mailVaultTempPrefix()+"*"))
	if err != nil {
		return
	}
	for _, dir := range left {
		a.wipeTempDir(dir)
	}
}

func (a *App) wipeTempDir(dir string) {
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			_ = storage.WipeFile(path)
		}
		return nil
	})
	if err := os.RemoveAll(dir); err != nil {
		a.log.Error("remove vault temp folder", "err", err)
	}
}
