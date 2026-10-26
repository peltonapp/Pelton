package credentials

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

// TestMain swaps go-keyring for its in-memory mock before any test runs, so
// nothing in this package ever reads or writes the machine's real credential
// store.
func TestMain(m *testing.M) {
	keyring.MockInit()
	m.Run()
}

func TestChunkBytesSplitsAndReassembles(t *testing.T) {
	cases := []struct {
		name   string
		length int
		size   int
		want   int
	}{
		{"empty", 0, 10, 1},
		{"under the size", 4, 10, 1},
		{"exactly the size", 10, 10, 1},
		{"one byte over", 11, 10, 2},
		{"an exact multiple", 30, 10, 3},
		{"a ragged multiple", 31, 10, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := bytes.Repeat([]byte("x"), c.length)
			chunks := chunkBytes(data, c.size)

			if len(chunks) != c.want {
				t.Errorf("chunkBytes produced %d chunks, want %d", len(chunks), c.want)
			}
			for i, chunk := range chunks {
				if len(chunk) > c.size {
					t.Errorf("chunk %d is %d bytes, over the %d-byte cap", i, len(chunk), c.size)
				}
			}
			if got := bytes.Join(chunks, nil); !bytes.Equal(got, data) {
				t.Errorf("rejoined %d bytes, want the original %d", len(got), len(data))
			}
		})
	}
}

// Every chunk has to fit Windows Credential Manager's per-entry cap, which is
// the only reason the splitting exists.
func TestChunkBytesHonoursTheEntryCap(t *testing.T) {
	data := bytes.Repeat([]byte("y"), maxEntrySize*3+17)
	for i, chunk := range chunkBytes(data, maxEntrySize) {
		if len(chunk) > maxEntrySize {
			t.Errorf("chunk %d is %d bytes, over the %d-byte entry cap", i, len(chunk), maxEntrySize)
		}
	}
}

func TestChunkCount(t *testing.T) {
	cases := []struct {
		raw    string
		want   int
		wantOK bool
	}{
		{chunkMarker + "3", 3, true},
		{chunkMarker + "0", 0, true},
		{chunkMarker + "notanumber", 0, false},
		{chunkMarker, 0, false},
		{`{"method":"password"}`, 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := chunkCount(c.raw)
		if got != c.want || ok != c.wantOK {
			t.Errorf("chunkCount(%q) = (%d, %t), want (%d, %t)", c.raw, got, ok, c.want, c.wantOK)
		}
	}
}

// A chunk entry must never be named the same as the plain entry of another
// account, or one account's secret would overwrite part of another's.
func TestChunkKeyCannotCollideWithAnAccountKey(t *testing.T) {
	seen := map[string]string{}
	for _, id := range []int64{1, 2, 10, 11, 100} {
		plain := key(id)
		if other, ok := seen[plain]; ok {
			t.Errorf("key(%d) = %q, already used by %s", id, plain, other)
		}
		seen[plain] = "account " + strconv.FormatInt(id, 10)

		for i := range 3 {
			name := chunkKey(id, i)
			if other, ok := seen[name]; ok {
				t.Errorf("chunkKey(%d, %d) = %q, already used by %s", id, i, name, other)
			}
			seen[name] = "chunk " + strconv.Itoa(i) + " of account " + strconv.FormatInt(id, 10)
		}
	}
}

// An oauth secret with long tokens goes over the entry cap on its own, so the
// chunked path is the normal one for those accounts, not an edge case.
func TestStoreAndLoadRoundTripAChunkedSecret(t *testing.T) {
	const accountID = 7
	want := Secret{
		Method:       MethodOAuth,
		Provider:     "gmail",
		ClientID:     "client-id",
		RefreshToken: strings.Repeat("r", maxEntrySize*2+31),
		AccessToken:  strings.Repeat("a", maxEntrySize),
	}

	if err := Store(accountID, want); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, err := Load(accountID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Error("the loaded secret does not match the stored one")
	}
}

// The bug this guards: a long secret is replaced by a short one, the short one
// is written to the main entry, and the chunk entries from the old secret are
// left behind. Loading then has to ignore them rather than appending stale
// bytes, and storing has to clean them up.
func TestStoreClearsChunksWhenASecretShrinks(t *testing.T) {
	const accountID = 8
	big := Secret{Method: MethodOAuth, RefreshToken: strings.Repeat("r", maxEntrySize*2)}
	if err := Store(accountID, big); err != nil {
		t.Fatalf("store the long secret: %v", err)
	}

	small := Secret{Method: MethodPassword, Password: "short"}
	if err := Store(accountID, small); err != nil {
		t.Fatalf("store the short secret: %v", err)
	}

	got, err := Load(accountID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != small {
		t.Errorf("loaded %+v, want the short secret that replaced it", got)
	}
	if _, err := keyring.Get(service, chunkKey(accountID, 0)); err == nil {
		t.Error("a chunk entry from the replaced secret is still in the store")
	}
}

func TestLoadReportsNotFoundForAnUnknownAccount(t *testing.T) {
	if _, err := Load(404); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load of an account with no secret returned %v, want ErrNotFound", err)
	}
}

// Deleting an account must not fail because there was nothing to delete, or
// removing a half-set-up account would error every time.
func TestDeleteIsIdempotent(t *testing.T) {
	const accountID = 9
	if err := Store(accountID, Secret{Method: MethodPassword, Password: "p"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	for range 2 {
		if err := Delete(accountID); err != nil {
			t.Fatalf("delete: %v", err)
		}
	}
	if _, err := Load(accountID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load after delete returned %v, want ErrNotFound", err)
	}
}

// A mailbox's proxy password is its own entry: it must not overwrite the
// account's mail secret or the app-wide proxy password, and clearing it must
// leave both alone.
func TestAccountProxyPasswordIsSeparate(t *testing.T) {
	const accountID = 12
	if err := Store(accountID, Secret{Method: MethodPassword, Password: "mail"}); err != nil {
		t.Fatalf("store account secret: %v", err)
	}
	if err := StoreProxyPassword("global"); err != nil {
		t.Fatalf("store global proxy password: %v", err)
	}
	t.Cleanup(func() { _ = DeleteProxyPassword(); _ = Delete(accountID) })

	if err := StoreAccountProxyPassword(accountID, "routed"); err != nil {
		t.Fatalf("store account proxy password: %v", err)
	}
	if got, err := LoadAccountProxyPassword(accountID); err != nil || got != "routed" {
		t.Errorf("LoadAccountProxyPassword = %q, %v, want routed", got, err)
	}

	if err := StoreAccountProxyPassword(accountID, ""); err != nil {
		t.Fatalf("clear account proxy password: %v", err)
	}
	if got, err := LoadAccountProxyPassword(accountID); err != nil || got != "" {
		t.Errorf("after clearing, LoadAccountProxyPassword = %q, %v, want empty", got, err)
	}
	if s, err := Load(accountID); err != nil || s.Password != "mail" {
		t.Errorf("account secret = %+v, %v, want it untouched", s, err)
	}
	if got, err := LoadProxyPassword(); err != nil || got != "global" {
		t.Errorf("global proxy password = %q, %v, want it untouched", got, err)
	}
}

// A dev run and a nightly keep their own databases, so their account ids start
// at 1 just like the installed app's. Their secrets must not land on the
// installed app's entries, or testing a build overwrites the real password.
func TestUseServiceKeepsInstallsApart(t *testing.T) {
	const accountID = 1
	t.Cleanup(func() {
		for _, name := range []string{"Pelton", "Pelton-dev"} {
			UseService(name)
			_ = Delete(accountID)
		}
		UseService(defaultService)
	})

	UseService("Pelton")
	if err := Store(accountID, Secret{Method: MethodPassword, Password: "real"}); err != nil {
		t.Fatalf("store stable: %v", err)
	}
	UseService("Pelton-dev")
	if _, err := Load(accountID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dev Load of the stable account returned %v, want ErrNotFound", err)
	}
	if err := Store(accountID, Secret{Method: MethodPassword, Password: "alice-e2e"}); err != nil {
		t.Fatalf("store dev: %v", err)
	}

	UseService("Pelton")
	got, err := Load(accountID)
	if err != nil {
		t.Fatalf("load stable: %v", err)
	}
	if got.Password != "real" {
		t.Errorf("stable password = %q after a dev store, want %q", got.Password, "real")
	}
}
