package crypto

import (
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// The other tests use Ed25519 keys because RSA is slow to generate, but RSA
// is what most keys in the wild are. One RSA-2048 key, generated once, goes
// through sign+encrypt and back, and the key list names its algorithm.
func TestRSARoundTrip(t *testing.T) {
	alice, err := openpgp.NewEntity("Test User", "", "alice@example.test",
		&packet.Config{Algorithm: packet.PubKeyAlgoRSA, RSABits: 2048})
	if err != nil {
		t.Fatalf("new RSA entity: %v", err)
	}
	dir := t.TempDir()
	writeKeyrings(t, dir, []*openpgp.Entity{alice}, []*openpgp.Entity{alice})
	store := NewPGPKeyStore(dir)
	p := NewPGP(store)

	part, err := p.Wrap([]byte(sampleEntity), ModeSignEncrypt, Options{
		SenderEmail: "alice@example.test",
		Recipients:  []string{"alice@example.test"},
	})
	if err != nil {
		t.Fatalf("sign+encrypt: %v", err)
	}
	if strings.Contains(string(part.Body), plaintextMarker) {
		t.Fatal("the encrypted part carries the plaintext")
	}
	opened, err := p.Open(message("alice@example.test", part), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !opened.Encrypted || !strings.Contains(string(opened.Body), plaintextMarker) {
		t.Errorf("Encrypted %v, body %q", opened.Encrypted, opened.Body)
	}
	if opened.Signature.Status != SigValid {
		t.Errorf("Status = %q (%s), want %q", opened.Signature.Status, opened.Signature.Detail, SigValid)
	}

	keys, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0].Algorithm != "RSA" || keys[0].Bits != 2048 {
		t.Fatalf("keys %+v, want one RSA 2048-bit key", keys)
	}
}
