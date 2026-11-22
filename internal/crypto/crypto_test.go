package crypto

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// the entity body used across tests, a minimal but realistic mime part.
const sampleEntity = "Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"the eagle lands at dawn\n"

// plaintextMarker is a distinctive string inside the entity. No test that asks
// for encryption may ever see this string in the output.
const plaintextMarker = "the eagle lands at dawn"

func newTestEntity(t *testing.T, email string) *openpgp.Entity {
	t.Helper()
	ent, err := openpgp.NewEntity("Test User", "", email, testKeyConfig)
	if err != nil {
		t.Fatalf("new entity for %s: %v", email, err)
	}
	return ent
}

// testKeyConfig makes test keys Ed25519 with a Curve25519 encryption subkey.
// The default, RSA-2048, took up to a second per key to generate, several
// under -race, and nothing here depends on the algorithm.
var testKeyConfig = &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA}

// writeKeyrings serialises public and private keyrings into dir so a PGPKeyStore
// can read them back, mirroring real on-disk keyrings.
func writeKeyrings(t *testing.T, dir string, pub, priv []*openpgp.Entity) {
	t.Helper()
	writeArmored(t, filepath.Join(dir, pubringFile), "PGP PUBLIC KEY BLOCK", func(w io.Writer) error {
		for _, e := range pub {
			if err := e.Serialize(w); err != nil {
				return err
			}
		}
		return nil
	})
	writeArmored(t, filepath.Join(dir, secringFile), "PGP PRIVATE KEY BLOCK", func(w io.Writer) error {
		for _, e := range priv {
			// serialize without re-signing, which would need the key unlocked; the
			// self-signatures from NewEntity are preserved as-is.
			if err := e.SerializePrivateWithoutSigning(w, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeArmored(t *testing.T, path, blockType string, write func(io.Writer) error) {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, blockType, nil)
	if err != nil {
		t.Fatalf("armor encode: %v", err)
	}
	if err := write(w); err != nil {
		t.Fatalf("serialize keys: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close armor: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSignRoundTrip(t *testing.T) {
	dir := t.TempDir()
	alice := newTestEntity(t, "alice@example.com")
	writeKeyrings(t, dir, []*openpgp.Entity{alice}, []*openpgp.Entity{alice})

	engine := NewPGP(NewPGPKeyStore(dir))
	part, err := engine.Wrap([]byte(sampleEntity), ModeSign, Options{SenderEmail: "alice@example.com"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if !bytes.Contains(part.Body, []byte("-----BEGIN PGP SIGNATURE-----")) {
		t.Fatal("signed body has no armored signature")
	}
	if !strings.HasPrefix(part.ContentType, "multipart/signed;") {
		t.Fatalf("unexpected content type %q", part.ContentType)
	}

	// the signature covers the canonical entity; verify it against alice's key.
	canonical := toCRLF([]byte(sampleEntity))
	keyring := openpgp.EntityList{alice}
	signer, err := openpgp.CheckArmoredDetachedSignature(keyring, bytes.NewReader(canonical), bytes.NewReader(part.Body), cryptoConfig())
	if err != nil {
		t.Fatalf("verify signature: %v", err)
	}
	if signer == nil {
		t.Fatal("signature verified but no signer returned")
	}
}

func TestEncryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	alice := newTestEntity(t, "alice@example.com")
	bob := newTestEntity(t, "bob@example.com")
	// alice's store knows bob's public key and her own private key.
	writeKeyrings(t, dir, []*openpgp.Entity{alice, bob}, []*openpgp.Entity{alice})

	engine := NewPGP(NewPGPKeyStore(dir))
	part, err := engine.Wrap([]byte(sampleEntity), ModeEncrypt, Options{
		SenderEmail: "alice@example.com",
		Recipients:  []string{"bob@example.com"},
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if bytes.Contains(part.Body, []byte(plaintextMarker)) {
		t.Fatal("encrypted output leaked plaintext")
	}
	if !strings.HasPrefix(part.ContentType, "multipart/encrypted;") {
		t.Fatalf("unexpected content type %q", part.ContentType)
	}

	// bob decrypts with his private key and gets the original entity back.
	got := decryptMessage(t, part.Body, openpgp.EntityList{bob})
	if !bytes.Equal(got, toCRLF([]byte(sampleEntity))) {
		t.Fatalf("decrypted mismatch:\n got %q\nwant %q", got, toCRLF([]byte(sampleEntity)))
	}
}

func TestSignAndEncryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	alice := newTestEntity(t, "alice@example.com")
	bob := newTestEntity(t, "bob@example.com")
	writeKeyrings(t, dir, []*openpgp.Entity{alice, bob}, []*openpgp.Entity{alice})

	engine := NewPGP(NewPGPKeyStore(dir))
	part, err := engine.Wrap([]byte(sampleEntity), ModeSignEncrypt, Options{
		SenderEmail: "alice@example.com",
		Recipients:  []string{"bob@example.com"},
	})
	if err != nil {
		t.Fatalf("sign+encrypt: %v", err)
	}
	if bytes.Contains(part.Body, []byte(plaintextMarker)) {
		t.Fatal("sign+encrypt output leaked plaintext")
	}

	// bob needs his private key to decrypt and alice's public key to verify.
	got := decryptMessage(t, part.Body, openpgp.EntityList{bob, alice})
	if !bytes.Equal(got, toCRLF([]byte(sampleEntity))) {
		t.Fatalf("decrypted mismatch:\n got %q\nwant %q", got, toCRLF([]byte(sampleEntity)))
	}
}

// TestEncryptMissingRecipientKeyNeverYieldsPlaintext is the safety-critical test:
// when a recipient key is missing, Wrap must return an error and no output at
// all, so the plaintext can never reach the transport.
func TestEncryptMissingRecipientKeyNeverYieldsPlaintext(t *testing.T) {
	dir := t.TempDir()
	alice := newTestEntity(t, "alice@example.com")
	// note: bob is NOT in the store, so encrypting to him must fail.
	writeKeyrings(t, dir, []*openpgp.Entity{alice}, []*openpgp.Entity{alice})

	engine := NewPGP(NewPGPKeyStore(dir))
	part, err := engine.Wrap([]byte(sampleEntity), ModeEncrypt, Options{
		SenderEmail: "alice@example.com",
		Recipients:  []string{"bob@example.com"},
	})

	if err == nil {
		t.Fatal("expected an error when the recipient key is missing")
	}
	if part != nil {
		t.Fatalf("expected nil part on failure, got body of %d bytes", len(part.Body))
	}
	// belt and braces: the error string itself must not carry the plaintext.
	if bytes.Contains([]byte(err.Error()), []byte(plaintextMarker)) {
		t.Fatal("error message leaked plaintext")
	}
}

// TestSignLockedKeyWithoutPassphraseFails proves a locked private key with no
// passphrase is a hard failure, not a silent skip.
func TestSignLockedKeyWithoutPassphraseFails(t *testing.T) {
	dir := t.TempDir()
	alice := newTestEntity(t, "alice@example.com")
	passphrase := []byte("correct horse battery staple")
	if err := alice.EncryptPrivateKeys(passphrase, nil); err != nil {
		t.Fatalf("lock private keys: %v", err)
	}
	writeKeyrings(t, dir, []*openpgp.Entity{alice}, []*openpgp.Entity{alice})

	engine := NewPGP(NewPGPKeyStore(dir))

	if _, err := engine.Wrap([]byte(sampleEntity), ModeSign, Options{SenderEmail: "alice@example.com"}); err == nil {
		t.Fatal("expected ErrPassphraseRequired with a locked key and no passphrase")
	}

	// with the passphrase, signing succeeds.
	part, err := engine.Wrap([]byte(sampleEntity), ModeSign, Options{
		SenderEmail: "alice@example.com",
		Passphrase:  passphrase,
	})
	if err != nil {
		t.Fatalf("sign with passphrase: %v", err)
	}
	if part == nil {
		t.Fatal("expected a signed part with the correct passphrase")
	}
}

func TestSMIMEAlwaysFails(t *testing.T) {
	part, err := NewSMIME().Wrap([]byte(sampleEntity), ModeEncrypt, Options{Recipients: []string{"bob@example.com"}})
	if err == nil {
		t.Fatal("expected s/mime to fail")
	}
	if part != nil {
		t.Fatal("s/mime must not return a part")
	}
}

// decryptMessage extracts the armored PGP message from a multipart/encrypted
// body and decrypts it with the given keyring, returning the plaintext.
func decryptMessage(t *testing.T, body []byte, keyring openpgp.EntityList) []byte {
	t.Helper()
	block, err := armor.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode armor: %v", err)
	}
	md, err := openpgp.ReadMessage(block.Body, keyring, nil, cryptoConfig())
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	got, err := io.ReadAll(md.UnverifiedBody)
	if err != nil {
		t.Fatalf("read decrypted body: %v", err)
	}
	// if the message was signed, the signature is only checked after the body is
	// fully read; surface any failure.
	if md.IsSigned && md.SignatureError != nil {
		t.Fatalf("embedded signature did not verify: %v", md.SignatureError)
	}
	return got
}
