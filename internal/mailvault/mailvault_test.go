package mailvault

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
)

var testKDF = KDFParams{Time: 1, MemoryKiB: 64, Threads: 1}

func newVault(t *testing.T) (Material, string, *Keys) {
	t.Helper()
	m, code, keys, err := Create([]byte("correct horse"), testKDF)
	if err != nil {
		t.Fatal(err)
	}
	return m, code, keys
}

func TestCreateOpenAndRecovery(t *testing.T) {
	m, code, created := newVault(t)
	if m.Passphrase.KDF != testKDF || m.Recovery.KDF != testKDF {
		t.Fatalf("KDF params not stored with the wraps: %+v %+v", m.Passphrase.KDF, m.Recovery.KDF)
	}
	if len(m.Passphrase.Salt) != saltSize || bytes.Equal(m.Passphrase.Salt, m.Recovery.Salt) {
		t.Fatal("each wrap needs its own salt")
	}
	byPass, err := Open(m, []byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(byPass.dataKey, created.dataKey) {
		t.Fatal("passphrase opened a different data key")
	}
	spaced := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	byCode, err := OpenWithRecovery(m, spaced)
	if err != nil {
		t.Fatalf("recovery code with spaces and lower case: %v", err)
	}
	if !bytes.Equal(byCode.dataKey, created.dataKey) {
		t.Fatal("recovery code opened a different data key")
	}
	if _, err := Open(m, []byte("wrong")); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	if _, err := OpenWithRecovery(m, "AAAA-BBBB"); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("malformed recovery code: %v", err)
	}
	if _, err := Open(m, []byte(code)); !errors.Is(err, ErrWrongSecret) {
		t.Fatal("the recovery code must not work as a passphrase")
	}
}

func TestRewrapKeepsMailReadable(t *testing.T) {
	m, _, keys := newVault(t)
	mk, err := NewMessageKey(m.PublicKey, m.UID)
	if err != nil {
		t.Fatal(err)
	}
	box, err := mk.Seal([]byte("subject"), "messages", "7", "subject")
	if err != nil {
		t.Fatal(err)
	}
	w, err := keys.Rewrap([]byte("new passphrase"), testKDF)
	if err != nil {
		t.Fatal(err)
	}
	m.Passphrase = w
	if _, err := Open(m, []byte("correct horse")); !errors.Is(err, ErrWrongSecret) {
		t.Fatal("old passphrase still opens after a change")
	}
	reopened, err := Open(m, []byte("new passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := reopened.MessageKey(mk.EPK)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := again.Open(box, "messages", "7", "subject")
	if err != nil || string(plain) != "subject" {
		t.Fatalf("mail sealed before the change: %q %v", plain, err)
	}
}

func TestSealWhileLockedOpensWhenUnlocked(t *testing.T) {
	m, _, keys := newVault(t)
	keys.Wipe()
	sealer, err := NewMessageKey(m.PublicKey, m.UID)
	if err != nil {
		t.Fatal(err)
	}
	box, err := sealer.Seal([]byte("body text"), "messages", "1", "body")
	if err != nil {
		t.Fatal(err)
	}
	sealer.Wipe()
	if _, err := sealer.Open(box, "messages", "1", "body"); !errors.Is(err, ErrWiped) {
		t.Fatal("a wiped message key still opened")
	}
	if _, err := keys.MessageKey(sealer.EPK); !errors.Is(err, ErrWiped) {
		t.Fatal("wiped vault keys still derived a message key")
	}
	unlocked, err := Open(m, []byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	opener, err := unlocked.MessageKey(sealer.EPK)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := opener.Open(box, "messages", "1", "body")
	if err != nil || string(plain) != "body text" {
		t.Fatalf("got %q %v", plain, err)
	}
}

func TestTamperIsDetected(t *testing.T) {
	m, _, keys := newVault(t)
	sealA, _ := NewMessageKey(m.PublicKey, m.UID)
	sealB, _ := NewMessageKey(m.PublicKey, m.UID)
	boxA, _ := sealA.Seal([]byte("alpha"), "messages", "1", "subject")
	boxB, _ := sealB.Seal([]byte("beta"), "messages", "2", "subject")
	openA, err := keys.MessageKey(sealA.EPK)
	if err != nil {
		t.Fatal(err)
	}
	openB, _ := keys.MessageKey(sealB.EPK)

	flipped := append([]byte(nil), boxA...)
	flipped[len(flipped)/2] ^= 0x01
	cases := []struct {
		name string
		run  func() error
	}{
		{"flipped bit", func() error { _, err := openA.Open(flipped, "messages", "1", "subject"); return err }},
		{"truncated", func() error { _, err := openA.Open(boxA[:len(boxA)-1], "messages", "1", "subject"); return err }},
		{"too short", func() error { _, err := openA.Open(boxA[:10], "messages", "1", "subject"); return err }},
		{"empty", func() error { _, err := openA.Open(nil, "messages", "1", "subject"); return err }},
		{"other column", func() error { _, err := openA.Open(boxA, "messages", "1", "from"); return err }},
		{"swapped rows, same key", func() error { _, err := openA.Open(boxA, "messages", "2", "subject"); return err }},
		{"swapped rows, whole record", func() error { _, err := openB.Open(boxA, "messages", "2", "subject"); return err }},
		{"wrong version", func() error {
			v := append([]byte(nil), boxA...)
			v[0] = 9
			_, err := openA.Open(v, "messages", "1", "subject")
			return err
		}},
	}
	for _, tc := range cases {
		if err := tc.run(); !errors.Is(err, ErrOpen) {
			t.Errorf("%s: want ErrOpen, got %v", tc.name, err)
		}
	}
	if plain, err := openB.Open(boxB, "messages", "2", "subject"); err != nil || string(plain) != "beta" {
		t.Fatalf("untouched box: %q %v", plain, err)
	}
}

func TestTamperedMaterialIsRefused(t *testing.T) {
	m, _, _ := newVault(t)

	other, _, _ := newVault(t)
	swapped := m
	swapped.PublicKey = other.PublicKey
	if _, err := Open(swapped, []byte("correct horse")); !errors.Is(err, ErrOpen) {
		t.Fatalf("a replaced public key must be noticed: %v", err)
	}

	moved := m
	moved.Passphrase = other.Passphrase
	if _, err := Open(moved, []byte("correct horse")); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("a wrap from another vault must not open: %v", err)
	}

	greedy := m
	greedy.Passphrase.KDF.MemoryKiB = maxKDFMemoryKiB + 1
	if _, err := Open(greedy, []byte("correct horse")); !errors.Is(err, ErrBadParams) {
		t.Fatalf("out of range parameters: %v", err)
	}
}

func TestLinkHashIsKeyed(t *testing.T) {
	m1, _, _ := newVault(t)
	m2, _, _ := newVault(t)
	a := LinkHash(m1.LinkKey, "abc@example.com")
	if a != LinkHash(m1.LinkKey, "abc@example.com") {
		t.Fatal("link hash is not stable")
	}
	if a == LinkHash(m2.LinkKey, "abc@example.com") {
		t.Fatal("two vaults produced the same link hash")
	}
	if strings.Contains(a, "abc") || strings.Contains(a, "example") {
		t.Fatal("link hash leaks its input")
	}
}

// TestWipeWhileSealing locks a key while other goroutines seal with it. Each
// seal must either produce a box the vault opens or fail with ErrWiped; a box
// sealed with a half-zeroed key would be unreadable forever.
func TestWipeWhileSealing(t *testing.T) {
	m, _, keys := newVault(t)
	sealer, err := NewMessageKey(m.PublicKey, m.UID)
	if err != nil {
		t.Fatal(err)
	}
	epk := sealer.EPK
	shared, err := keys.MessageKey(epk)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	boxes := make(chan []byte, 400)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				box, err := shared.Seal([]byte("attachment bytes"), "attachments", "1", "0")
				if err != nil {
					if !errors.Is(err, ErrWiped) {
						t.Errorf("seal during a wipe: %v", err)
					}
					return
				}
				boxes <- box
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		shared.Wipe()
		keys.Wipe()
	}()
	wg.Wait()
	close(boxes)
	again, err := Open(m, []byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := again.MessageKey(epk)
	if err != nil {
		t.Fatal(err)
	}
	for box := range boxes {
		if _, err := fresh.Open(box, "attachments", "1", "0"); err != nil {
			t.Fatalf("a box sealed while the key was being wiped does not open: %v", err)
		}
	}
	if _, err := keys.MessageKey(epk); !errors.Is(err, ErrWiped) {
		t.Fatalf("wiped keys: %v", err)
	}
}

func TestAADIsUnambiguous(t *testing.T) {
	if bytes.Equal(aad("ab", "c"), aad("a", "bc")) {
		t.Fatal("different parts produced the same associated data")
	}
}
