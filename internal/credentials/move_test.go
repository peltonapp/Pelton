package credentials

import (
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// A nightly that files under "Pelton-nightly" finds the secrets an older
// nightly filed under "Pelton": each known one is copied over, the old entry
// stays, and a secret the new service already has is not overwritten.
func TestCopyFromMovesKnownSecrets(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(func() { UseService(DefaultService) })

	UseService(DefaultService)
	long := Secret{Method: MethodOAuth, RefreshToken: strings.Repeat("r", 3*maxEntrySize)}
	if err := Store(1, Secret{Method: MethodPassword, Password: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := Store(2, long); err != nil {
		t.Fatal(err)
	}
	if err := Store(3, Secret{Method: MethodPassword, Password: "old three"}); err != nil {
		t.Fatal(err)
	}
	if err := StoreProxyPassword("proxy"); err != nil {
		t.Fatal(err)
	}
	if err := StoreAccountProxyPassword(1, "account proxy"); err != nil {
		t.Fatal(err)
	}
	if err := StoreVirusTotalKey("vt"); err != nil {
		t.Fatal(err)
	}
	if err := StorePGPPassphrase("ABCD", "pgp"); err != nil {
		t.Fatal(err)
	}
	if err := StoreAddressBookPassword(5, "dav"); err != nil {
		t.Fatal(err)
	}
	// an account the database does not know is not looked for.
	if err := Store(9, Secret{Method: MethodPassword, Password: "unknown"}); err != nil {
		t.Fatal(err)
	}

	UseService("Pelton-nightly")
	if err := Store(3, Secret{Method: MethodPassword, Password: "new three"}); err != nil {
		t.Fatal(err)
	}
	n, err := CopyFrom(DefaultService, Known{AccountIDs: []int64{1, 2, 3}, AddressBookIDs: []int64{5}, PGPFingerprints: []string{"ABCD"}})
	if err != nil {
		t.Fatalf("CopyFrom: %v", err)
	}
	if n == 0 {
		t.Fatal("nothing copied")
	}

	if s, err := Load(1); err != nil || s.Password != "one" {
		t.Errorf("account 1 = %+v, %v", s, err)
	}
	if s, err := Load(2); err != nil || s.RefreshToken != long.RefreshToken {
		t.Errorf("chunked account 2 not copied whole: %v", err)
	}
	if s, err := Load(3); err != nil || s.Password != "new three" {
		t.Errorf("account 3 = %+v, %v, want the new service's own secret kept", s, err)
	}
	if _, err := Load(9); err != ErrNotFound {
		t.Errorf("unknown account 9 was copied: %v", err)
	}
	for name, load := range map[string]func() (string, error){
		"proxy":         LoadProxyPassword,
		"account proxy": func() (string, error) { return LoadAccountProxyPassword(1) },
		"vt":            LoadVirusTotalKey,
		"pgp":           func() (string, error) { return LoadPGPPassphrase("ABCD") },
		"dav":           func() (string, error) { return LoadAddressBookPassword(5) },
	} {
		if got, err := load(); err != nil || got != name {
			t.Errorf("%s = %q, %v", name, got, err)
		}
	}

	// the old entries stay for builds that still file under the old name.
	UseService(DefaultService)
	if s, err := Load(1); err != nil || s.Password != "one" {
		t.Errorf("old entry of account 1 gone: %+v, %v", s, err)
	}
}

// Copying to the service the process already uses does nothing.
func TestCopyFromSameServiceIsANoop(t *testing.T) {
	keyring.MockInit()
	UseService(DefaultService)
	if n, err := CopyFrom(DefaultService, Known{AccountIDs: []int64{1}}); n != 0 || err != nil {
		t.Fatalf("CopyFrom = %d, %v", n, err)
	}
}
