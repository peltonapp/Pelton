package desktop

import (
	"testing"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/storage"
)

// A nightly or dev profile files its secrets under its own keyring service.
// On its first start it copies the ones its database knows of from the shared
// "Pelton" service, once, so nobody silently loses a saved password. The
// keyring is the in-memory mock (TestMain).
func TestKeyringSecretsCopiedOnceToTheChannelService(t *testing.T) {
	t.Cleanup(func() { credentials.UseService(credentials.DefaultService) })
	a, _, _ := moveTestApp(t)
	id, err := a.store.CreateAccount(a.ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	credentials.UseService(credentials.DefaultService)
	if err := credentials.Store(id, credentials.Secret{Method: credentials.MethodPassword, Password: "saved"}); err != nil {
		t.Fatal(err)
	}
	if err := credentials.StoreVirusTotalKey("vt-key"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		credentials.UseService(credentials.DefaultService)
		_ = credentials.Delete(id)
		_ = credentials.DeleteVirusTotalKey()
	})

	credentials.UseService("Pelton-nightly")
	t.Cleanup(func() {
		credentials.UseService("Pelton-nightly")
		_ = credentials.Delete(id)
		_ = credentials.DeleteVirusTotalKey()
	})
	a.copyKeyringSecrets()
	if s, err := credentials.Load(id); err != nil || s.Password != "saved" {
		t.Fatalf("account secret under the new service = %+v, %v", s, err)
	}
	if k, err := credentials.LoadVirusTotalKey(); err != nil || k != "vt-key" {
		t.Fatalf("VirusTotal key under the new service = %q, %v", k, err)
	}

	// once: a secret the user removed since is not brought back.
	if err := credentials.Delete(id); err != nil {
		t.Fatal(err)
	}
	a.copyKeyringSecrets()
	if _, err := credentials.Load(id); err != credentials.ErrNotFound {
		t.Fatalf("a second start copied again: %v", err)
	}

	// the installed app's entry is untouched.
	credentials.UseService(credentials.DefaultService)
	if s, err := credentials.Load(id); err != nil || s.Password != "saved" {
		t.Fatalf("old entry = %+v, %v", s, err)
	}
}

// The installed app files under "Pelton" already: there is nothing to copy.
func TestKeyringCopySkippedForTheDefaultService(t *testing.T) {
	credentials.UseService(credentials.DefaultService)
	a, _, _ := moveTestApp(t)
	a.copyKeyringSecrets()
	if done, err := a.store.GetBool(a.ctx, settingKeyringCopied); err == nil && done {
		t.Fatal("the default service recorded a copy")
	}
}
