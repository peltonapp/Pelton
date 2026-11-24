package crypto

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

var genNow = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func generated(t *testing.T, email string) (*PGPKeyStore, KeyInfo) {
	t.Helper()
	store := newStore(t)
	info, err := store.Generate("Me (Pelton)", email, []byte("stored secret"), genNow)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return store, info
}

func publicBytes(t *testing.T, ent *openpgp.Entity) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := ent.Serialize(&buf); err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return buf.Bytes()
}

func TestGenerateMakesAModernProtectedKey(t *testing.T) {
	store, info := generated(t, "me@example.test")
	if info.Algorithm != "EdDSA" {
		t.Errorf("primary algorithm = %s, want EdDSA (Ed25519)", info.Algorithm)
	}
	wantExpiry := genNow.Add(AutoKeyLifetime)
	if d := info.Expires.Sub(wantExpiry); d < -time.Minute || d > time.Minute {
		t.Errorf("expires %v, want about %v", info.Expires, wantExpiry)
	}
	if !info.Locked {
		t.Error("generated key is not reported as locked")
	}

	ent, err := store.SenderKey("me@example.test")
	if err != nil {
		t.Fatalf("SenderKey: %v", err)
	}
	if !KeyLocked(ent) {
		t.Fatal("private key is stored without a passphrase")
	}
	if err := Unlock(ent, []byte("wrong")); err == nil {
		t.Error("wrong passphrase unlocked the key")
	}
	if err := Unlock(ent, []byte("stored secret")); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	enc, ok := ent.EncryptionKey(genNow)
	if !ok {
		t.Fatal("generated key has no encryption key")
	}
	if enc.PublicKey.PubKeyAlgo != packet.PubKeyAlgoECDH {
		t.Errorf("encryption key algorithm = %v, want ECDH", enc.PublicKey.PubKeyAlgo)
	}
	if curve, err := enc.PublicKey.Curve(); err != nil || curve != packet.Curve25519 {
		t.Errorf("encryption curve = %v, %v; want Curve25519", curve, err)
	}
	if _, err := store.RecipientKey("me@example.test"); err != nil {
		t.Errorf("generated key is not in the public ring: %v", err)
	}
}

func TestExtendOnlyNearExpiry(t *testing.T) {
	store, info := generated(t, "me@example.test")

	if _, changed, err := store.Extend(info.Fingerprint, []byte("stored secret"), genNow.Add(365*24*time.Hour)); err != nil || changed {
		t.Fatalf("Extend a year in = changed %v, %v; want unchanged", changed, err)
	}

	later := info.Expires.Add(-90 * 24 * time.Hour)
	if _, _, err := store.Extend(info.Fingerprint, nil, later); !errors.Is(err, ErrPassphraseRequired) {
		t.Errorf("Extend without the passphrase = %v, want ErrPassphraseRequired", err)
	}
	renewed, changed, err := store.Extend(info.Fingerprint, []byte("stored secret"), later)
	if err != nil || !changed {
		t.Fatalf("Extend near expiry = changed %v, %v", changed, err)
	}
	if d := renewed.Expires.Sub(later.Add(AutoKeyLifetime)); d < -time.Minute || d > time.Minute {
		t.Errorf("renewed expiry %v, want about %v", renewed.Expires, later.Add(AutoKeyLifetime))
	}
	if renewed.Fingerprint != info.Fingerprint {
		t.Error("extending changed the fingerprint")
	}

	// the stored key now reads with the new expiry, is still protected by the
	// same passphrase and can still be encrypted to after the old expiry.
	ent, err := store.SenderKey("me@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if got := describe(ent).Expires; !got.Equal(renewed.Expires) {
		t.Errorf("stored expiry %v, want %v", got, renewed.Expires)
	}
	if err := Unlock(ent, []byte("stored secret")); err != nil {
		t.Errorf("renewed key does not unlock: %v", err)
	}
	if _, ok := ent.EncryptionKey(info.Expires.Add(24 * time.Hour)); !ok {
		t.Error("renewed key is unusable after the old expiry")
	}
	pub, err := store.RecipientKey("me@example.test")
	if err != nil || !describe(pub).Expires.Equal(renewed.Expires) {
		t.Errorf("public ring not updated: %v", err)
	}
}

func TestInspectPublicKey(t *testing.T) {
	good := newTestEntity(t, "good@example.test")

	past := genNow.Add(-3 * 365 * 24 * time.Hour)
	expired, err := openpgp.NewEntity("Old", "", "old@example.test", &packet.Config{
		Time:            func() time.Time { return past },
		KeyLifetimeSecs: uint32((24 * time.Hour) / time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}

	revoked := newTestEntity(t, "gone@example.test")
	if err := revoked.RevokeKey(packet.KeyCompromised, "lost", nil); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		ent     *openpgp.Entity
		usable  bool
		problem string
	}{
		{"usable", good, true, ""},
		{"expired", expired, false, KeyProblemExpired},
		{"revoked", revoked, false, KeyProblemRevoked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := InspectPublicKey(publicBytes(t, tt.ent), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if info.Usable != tt.usable || info.Problem != tt.problem {
				t.Errorf("InspectPublicKey = %+v", info)
			}
			if KeyUsable(publicBytes(t, tt.ent), time.Now()) != tt.usable {
				t.Error("KeyUsable disagrees")
			}
		})
	}
	if _, err := InspectPublicKey([]byte("garbage"), time.Now()); err == nil {
		t.Error("garbage parsed as a key")
	}
}

func TestSelectKeyForMatchesTheAddress(t *testing.T) {
	anna := newTestEntity(t, "anna@example.test")
	other := newTestEntity(t, "other@example.test")
	data := append(publicBytes(t, other), publicBytes(t, anna)...)

	got, info, err := SelectKeyFor(data, "Anna@Example.test", time.Now())
	if err != nil {
		t.Fatalf("SelectKeyFor: %v", err)
	}
	if info.Fingerprint != Fingerprint(anna) {
		t.Error("picked the wrong key")
	}
	if back, _ := InspectPublicKey(got, time.Now()); back.Fingerprint != Fingerprint(anna) {
		t.Error("returned bytes are not anna's key")
	}

	if _, info, err := SelectKeyFor(publicBytes(t, other), "anna@example.test", time.Now()); !errors.Is(err, ErrUnusableKey) || info.Problem != KeyProblemNoAddress {
		t.Errorf("key without the address = %+v, %v", info, err)
	}
}

func TestAutocryptKeyDataIsMinimalAndUsable(t *testing.T) {
	store, info := generated(t, "me@example.test")
	data, err := store.AutocryptKeyData(info.Fingerprint, "me@example.test", genNow)
	if err != nil {
		t.Fatalf("AutocryptKeyData: %v", err)
	}
	list, err := openpgp.ReadKeyRing(bytes.NewReader(data))
	if err != nil || len(list) != 1 {
		t.Fatalf("read minimal key: %v", err)
	}
	ent := list[0]
	if ent.PrivateKey != nil {
		t.Fatal("minimal key carries private material")
	}
	if len(ent.Identities) != 1 || len(ent.Subkeys) != 1 {
		t.Errorf("minimal key has %d identities and %d subkeys", len(ent.Identities), len(ent.Subkeys))
	}
	if Fingerprint(ent) != info.Fingerprint || !KeyUsable(data, genNow) {
		t.Error("minimal key is not the same usable key")
	}
	if len(data) > 1024 {
		t.Errorf("minimal key is %d bytes", len(data))
	}
}

// pkeskKeyIDs lists the key ids the encrypted session keys of a message name.
func pkeskKeyIDs(t *testing.T, part *Part) []uint64 {
	t.Helper()
	start := bytes.Index(part.Body, []byte("-----BEGIN PGP MESSAGE-----"))
	end := bytes.Index(part.Body, []byte("-----END PGP MESSAGE-----"))
	block, err := armor.Decode(bytes.NewReader(part.Body[start : end+len("-----END PGP MESSAGE-----")]))
	if err != nil {
		t.Fatalf("armor: %v", err)
	}
	var ids []uint64
	r := packet.NewReader(block.Body)
	for {
		p, err := r.Next()
		if err != nil {
			break
		}
		ek, ok := p.(*packet.EncryptedKey)
		if !ok {
			break
		}
		ids = append(ids, ek.KeyId)
	}
	return ids
}

func TestExplicitEncryptionHidesBccRecipients(t *testing.T) {
	senderStore, sender := generated(t, "me@example.test")
	anna := newTestEntity(t, "anna@example.test")
	bea := newTestEntity(t, "bea@example.test")
	selfKey, err := senderStore.AutocryptKeyData(sender.Fingerprint, "me@example.test", genNow)
	if err != nil {
		t.Fatal(err)
	}

	part, err := NewPGP(senderStore).Wrap([]byte(sampleEntity), ModeSignEncrypt, Options{
		SenderEmail:   "me@example.test",
		Passphrase:    []byte("stored secret"),
		RecipientKeys: [][]byte{publicBytes(t, anna), selfKey},
		HiddenKeys:    [][]byte{publicBytes(t, bea)},
	})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if bytes.Contains(part.Body, []byte(plaintextMarker)) {
		t.Fatal("plaintext in the encrypted output")
	}

	ids := pkeskKeyIDs(t, part)
	annaEnc, _ := anna.EncryptionKey(time.Now())
	beaEnc, _ := bea.EncryptionKey(time.Now())
	var wildcard, named int
	for _, id := range ids {
		switch id {
		case 0:
			wildcard++
		case beaEnc.PublicKey.KeyId:
			t.Error("the bcc recipient's key id is on the message")
		case annaEnc.PublicKey.KeyId:
			named++
		}
	}
	if len(ids) != 3 || wildcard != 1 || named != 1 {
		t.Errorf("key ids %x: want anna named, me named and one hidden", ids)
	}

	// every recipient, the hidden one included, reads it with the signature
	// checking out once the sender's key is known.
	senderPub, _ := senderStore.RecipientKey("me@example.test")
	for _, reader := range []*openpgp.Entity{anna, bea} {
		pgp := newOpenTestPGP(t, []*openpgp.Entity{reader, senderPub}, []*openpgp.Entity{reader})
		opened, err := pgp.Open(message("me@example.test", part), nil)
		if err != nil {
			t.Fatalf("%s cannot open: %v", primaryEmail(reader), err)
		}
		if !strings.Contains(string(opened.Body), plaintextMarker) || opened.Signature.Status != SigValid {
			t.Errorf("%s: body %q, signature %+v", primaryEmail(reader), opened.Body, opened.Signature)
		}
	}
	// and the sender reads its own sent copy.
	if _, err := NewPGP(senderStore).Open(message("me@example.test", part), []byte("stored secret")); err != nil {
		t.Errorf("sender cannot read the sent copy: %v", err)
	}
}

func TestExplicitEncryptionRefusesUnreadableKeys(t *testing.T) {
	store, _ := generated(t, "me@example.test")
	_, err := NewPGP(store).Wrap([]byte(sampleEntity), ModeEncrypt, Options{RecipientKeys: [][]byte{[]byte("garbage")}})
	if !errors.Is(err, ErrRecipientKeyNotFound) {
		t.Errorf("Wrap with a garbage key = %v", err)
	}
}

// A message any other OpenPGP program would write, made with the library
// directly, opens; and what Pelton writes reads with the library directly.
func TestLibraryInteropBothWays(t *testing.T) {
	store, _ := generated(t, "me@example.test")
	stranger := newTestEntity(t, "stranger@example.test")
	myPub, _ := store.RecipientKey("me@example.test")

	var armored bytes.Buffer
	aw, _ := armor.Encode(&armored, "PGP MESSAGE", nil)
	w, err := openpgp.Encrypt(aw, []*openpgp.Entity{myPub}, stranger, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	io.WriteString(w, sampleEntity)
	w.Close()
	aw.Close()
	raw := "From: stranger@example.test\r\nContent-Type: text/plain\r\n\r\n" + armored.String()
	opened, err := NewPGP(store).OpenWith([]byte(raw), []byte("stored secret"), [][]byte{publicBytes(t, stranger)})
	if err != nil {
		t.Fatalf("open library message: %v", err)
	}
	if !strings.Contains(string(opened.Body), plaintextMarker) {
		t.Error("library message did not decrypt")
	}
	if opened.Signature.Status != SigUntrusted || opened.Extended.Status != SigValid {
		t.Errorf("verdicts = %v / %v; want untrusted without the extra key and valid with it", opened.Signature.Status, opened.Extended.Status)
	}

	part, err := NewPGP(store).Wrap([]byte(sampleEntity), ModeSignEncrypt, Options{
		SenderEmail: "me@example.test", Passphrase: []byte("stored secret"),
		RecipientKeys: [][]byte{publicBytes(t, stranger)},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(part.Body, []byte("-----BEGIN PGP MESSAGE-----"))
	got := decryptMessage(t, part.Body[start:], openpgp.EntityList{stranger, myPub})
	if !strings.Contains(string(got), plaintextMarker) {
		t.Error("library could not read Pelton's message")
	}
}

func TestOpenWithExtraKeysForDetachedSignatures(t *testing.T) {
	signer := newTestEntity(t, "signer@example.test")
	signerStore := NewPGP(NewPGPKeyStore(func() string {
		d := t.TempDir()
		writeKeyrings(t, d, []*openpgp.Entity{signer}, []*openpgp.Entity{signer})
		return d
	}()))
	part, err := signerStore.Wrap([]byte(sampleEntity), ModeSign, Options{SenderEmail: "signer@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	reader := newOpenTestPGP(t, nil, nil)
	opened, err := reader.OpenWith(message("signer@example.test", part), nil, [][]byte{publicBytes(t, signer)})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Signature.Status != SigUntrusted || opened.Extended.Status != SigValid || opened.Extended.Fingerprint != Fingerprint(signer) {
		t.Errorf("verdicts = %+v / %+v", opened.Signature, opened.Extended)
	}
	plain, err := reader.Open(message("signer@example.test", part), nil)
	if err != nil || plain.Extended.Status != SigUntrusted {
		t.Errorf("Open without extras: %+v, %v", plain, err)
	}
}

func TestSetupMessageRoundTrip(t *testing.T) {
	source, me := generated(t, "me@example.test")
	code, err := NewSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 44 || strings.Count(code, "-") != 8 {
		t.Fatalf("setup code %q is not nine groups of four", code)
	}
	payload, err := source.SetupPayload(me.Fingerprint, []byte("stored secret"), code, "mutual")
	if err != nil {
		t.Fatalf("SetupPayload: %v", err)
	}
	if !bytes.Contains(payload, []byte("Passphrase-Format: numeric9x4")) || !bytes.Contains(payload, []byte("Passphrase-Begin: "+code[:2])) {
		t.Errorf("payload armor headers missing:\n%s", payload)
	}
	html := append([]byte("<html><body><pre>\n"), append(payload, []byte("</pre></body></html>")...)...)

	target := newStore(t)
	if _, _, err := target.ImportSetupPayload(html, "1111-1111-1111-1111-1111-1111-1111-1111-1111", []byte("new")); !errors.Is(err, ErrWrongSetupCode) {
		t.Errorf("wrong code = %v", err)
	}
	typed := strings.ReplaceAll(code, "-", " ")
	info, prefer, err := target.ImportSetupPayload(html, typed, []byte("device two"))
	if err != nil {
		t.Fatalf("ImportSetupPayload: %v", err)
	}
	if info.Fingerprint != me.Fingerprint || prefer != "mutual" {
		t.Errorf("imported %s with prefer %q", info.Fingerprint, prefer)
	}
	ent, err := target.SenderKey("me@example.test")
	if err != nil || !KeyLocked(ent) || Unlock(ent, []byte("device two")) != nil {
		t.Errorf("imported key is not protected by the new passphrase: %v", err)
	}
}

func TestNormalizeSetupCode(t *testing.T) {
	digits := strings.Repeat("1234", 9)
	if got, ok := NormalizeSetupCode(digits); !ok || got != strings.TrimSuffix(strings.Repeat("1234-", 9), "-") {
		t.Errorf("NormalizeSetupCode = %q, %v", got, ok)
	}
	for _, bad := range []string{"", "1234", digits + "5", strings.Repeat("abcd", 9)} {
		if _, ok := NormalizeSetupCode(bad); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestExportProtected(t *testing.T) {
	store, me := generated(t, "me@example.test")
	if _, err := store.ExportProtected(me.Fingerprint, []byte("stored secret"), nil); err == nil {
		t.Error("backup without a passphrase was written")
	}
	blob, err := store.ExportProtected(me.Fingerprint, []byte("stored secret"), []byte("my backup words"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(blob))
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	if !KeyLocked(list[0]) || Unlock(list[0], []byte("my backup words")) != nil {
		t.Error("backup does not open with the chosen passphrase")
	}
	// the stored key is untouched.
	ent, _ := store.SenderKey("me@example.test")
	if Unlock(ent, []byte("stored secret")) != nil {
		t.Error("exporting changed the stored key's passphrase")
	}
}
