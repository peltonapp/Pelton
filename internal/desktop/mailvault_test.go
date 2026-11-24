package desktop

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/applock"
	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
)

func TestMailVaultWrongPassphraseWaitsLonger(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	for i := 1; i <= 3; i++ {
		res := a.UnlockMailVault(f.vault, "wrong")
		if res.OK || res.Reason != mailVaultWrong || res.RetryAfterMs != 0 {
			t.Fatalf("attempt %d: %+v", i, res)
		}
	}
	res := a.UnlockMailVault(f.vault, "wrong")
	if res.Reason != mailVaultWrong || res.RetryAfterMs <= 0 {
		t.Fatalf("fourth wrong attempt did not start a wait: %+v", res)
	}
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); res.OK || res.Reason != mailVaultWait {
		t.Fatalf("the right passphrase was tried during the wait: %+v", res)
	}
	v, err := a.store.GetMailVault(a.ctx, f.vault)
	if err != nil || v.Failures != 4 || v.RetryAt.IsZero() {
		t.Fatalf("failures are not kept across restarts: %+v %v", v, err)
	}
	if want := applock.Backoff(4); time.Until(v.RetryAt) > want {
		t.Fatalf("wait %s is longer than the backoff %s", time.Until(v.RetryAt), want)
	}
	if err := a.store.SetMailVaultFailures(a.ctx, f.vault, 4, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatalf("unlock after the wait: %+v", res)
	}
	if v, _ := a.store.GetMailVault(a.ctx, f.vault); v.Failures != 0 {
		t.Fatal("a successful unlock did not reset the count")
	}
}

func TestMailVaultPassphraseChange(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	if res := a.ChangeMailVaultPassphrase(f.vault, "correct horse battery", "short"); res.Reason != mailVaultWeak {
		t.Fatalf("a short passphrase was accepted: %+v", res)
	}
	if res := a.ChangeMailVaultPassphrase(f.vault, "correct horse battery", "a much longer one"); !res.OK {
		t.Fatalf("change: %+v", res)
	}
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); res.OK {
		t.Fatal("the old passphrase still opens the vault")
	}
	if res := a.UnlockMailVault(f.vault, "a much longer one"); !res.OK {
		t.Fatalf("new passphrase: %+v", res)
	}
	m, err := a.GetMessage(f.sealedID)
	if err != nil || m.Subject != vaultMarkSubject {
		t.Fatalf("mail sealed before the change: %+v %v", m, err)
	}
	a.LockMailVault(f.vault)

	if res := a.RecoverMailVault(f.vault, "AAAA-BBBB", "another passphrase"); res.OK {
		t.Fatal("a wrong recovery code opened the vault")
	}
}

func TestMailVaultRecoveryCodeSetsNewPassphrase(t *testing.T) {
	ctx, stopBackground := testContext(t)
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	a := &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler), dataDir: dir}
	a.mailVault.kdf = &testVaultKDF
	a.initMailVault()
	acct, _ := store.CreateAccount(ctx, &storage.Account{Email: "me@example.com", IMAPHost: "h", IMAPPort: 993})
	folder, _ := store.CreateFolder(ctx, &storage.Folder{AccountID: acct, Name: "Secret", IMAPPath: "Secret"})
	created, err := a.CreateMailVault(MailVaultCreateRequest{Passphrase: "forgotten passphrase",
		Members: []MailVaultMemberDTO{{AccountID: acct, FolderID: folder}}})
	if err != nil {
		t.Fatal(err)
	}
	waitForVaultState(t, a, created.ID, storage.MailVaultReady)
	a.LockAllMailVaults()
	if res := a.RecoverMailVault(created.ID, created.RecoveryCode, "brand new passphrase"); !res.OK {
		t.Fatalf("recovery: %+v", res)
	}
	a.LockAllMailVaults()
	if res := a.UnlockMailVault(created.ID, "brand new passphrase"); !res.OK {
		t.Fatalf("passphrase set by recovery: %+v", res)
	}
	a.LockAllMailVaults()
	if res := a.UnlockMailVault(created.ID, "forgotten passphrase"); res.OK {
		t.Fatal("the forgotten passphrase still works after recovery")
	}
}

func TestMailVaultAutoLockAndAppLock(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	if err := a.UpdateMailVaultSettings(MailVaultSettingsDTO{ID: f.vault, AutoLockMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatal(res)
	}
	a.mailVaultTick()
	if !a.mailVaultUnlocked(f.vault) {
		t.Fatal("locked before the idle time passed")
	}
	a.mailVault.mu.Lock()
	a.mailVault.lastUse = time.Now().Add(-6 * time.Minute)
	a.mailVault.mu.Unlock()
	a.mailVaultTick()
	if a.mailVaultUnlocked(f.vault) {
		t.Fatal("still unlocked after the idle time")
	}

	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatal(res)
	}
	a.onLockChanged(true)
	if a.mailVaultUnlocked(f.vault) {
		t.Fatal("the app lock did not lock the vault")
	}
}

func TestMailVaultTempFilesNeedConsentAndGoAtLock(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatal(res)
	}
	if err := a.ExportMessagePrintView(f.sealedID); !errors.Is(err, errMailVaultTempConsent) {
		t.Fatalf("a vault message went to a temp file without the warning: %v", err)
	}
	if err := a.ConsentMailVaultTempFiles(); err != nil {
		t.Fatal(err)
	}
	dir, err := a.mailVaultTempFor(f.sealedAtt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, vaultMarkFile)
	if err := os.WriteFile(file, []byte(vaultMarkBody), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(dir))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("vault temp folder is not private: %v %v", info.Mode(), err)
	}
	a.LockMailVault(f.vault)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("an opened vault attachment survived the lock: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(dir)); !os.IsNotExist(err) {
		t.Fatal("the vault temp folder survived the lock")
	}
}

// TestSealingRebuildsTheSearchIndex seals a folder whose mail was already
// indexed and scans the index directory afterwards for its text.
func TestSealingRebuildsTheSearchIndex(t *testing.T) {
	ctx, stopBackground := testContext(t)
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	a := &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler), dataDir: dir}
	a.mailVault.kdf = &testVaultKDF
	a.initMailVault()
	idx, err := openSearchIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.index = idx
	t.Cleanup(func() { _ = a.index.Close() })
	acct, _ := store.CreateAccount(ctx, &storage.Account{Email: "me@example.com", IMAPHost: "h", IMAPPort: 993})
	secret, _ := store.CreateFolder(ctx, &storage.Folder{AccountID: acct, Name: "Secret", IMAPPath: "Secret"})
	for i := range 3 {
		if _, err := store.InsertMessage(ctx, &storage.Message{AccountID: acct, FolderID: secret, UID: uint32(i + 1),
			Subject: "zebraquokka subject", BodyPlain: "pangolinwombat body", FromAddress: "narwhal@example.org",
			Date: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.indexNewMessages(); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dir, indexFileName)
	if found := scanDir(t, index, "pangolinwombat"); !found {
		t.Fatal("the control found nothing: the scan cannot see indexed text")
	}

	created, err := a.CreateMailVault(MailVaultCreateRequest{Passphrase: "correct horse battery",
		Members: []MailVaultMemberDTO{{AccountID: acct, FolderID: secret}}})
	if err != nil {
		t.Fatal(err)
	}
	waitForVaultState(t, a, created.ID, storage.MailVaultReady)
	for _, word := range []string{"zebraquokka", "pangolinwombat", "narwhal"} {
		if scanDir(t, index, word) {
			t.Errorf("the search index still holds %q after sealing", word)
		}
	}
	res, err := a.index.Search(search.Query{Text: "pangolinwombat", Limit: 10})
	if err != nil || res.Total != 0 {
		t.Fatalf("the index still finds sealed mail: %+v %v", res, err)
	}
}

func scanDir(t *testing.T, dir, word string) bool {
	t.Helper()
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil && bytes.Contains(bytes.ToLower(data), []byte(word)) {
			found = true
		}
		return nil
	})
	return found
}

func TestMailVaultRemoveUnsealsAndDeletes(t *testing.T) {
	f := newVaultApp(t)
	a := f.app
	if err := a.RemoveMailVault(f.vault); !errors.Is(err, errMailVaultLocked) {
		t.Fatalf("a locked vault was removed: %v", err)
	}
	if res := a.UnlockMailVault(f.vault, "correct horse battery"); !res.OK {
		t.Fatal(res)
	}
	if err := a.RemoveMailVault(f.vault); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := a.store.GetMailVault(a.ctx, f.vault); errors.Is(err, storage.ErrMailVaultNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("vault was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitForBackgroundWork(5 * time.Second)
	m, err := a.store.GetMessage(a.ctx, f.sealedID)
	if err != nil || m.Subject != vaultMarkSubject || m.VaultID != 0 {
		t.Fatalf("message after removal = %+v %v", m, err)
	}
}
