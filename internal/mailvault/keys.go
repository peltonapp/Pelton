package mailvault

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/argon2"
)

// KDFParams are the Argon2id costs a wrapped key was made with. They are
// stored next to it, so raising the defaults later still opens older vaults.
type KDFParams struct {
	Time      uint32 `json:"time"`
	MemoryKiB uint32 `json:"memoryKiB"`
	Threads   uint8  `json:"threads"`
}

// DefaultKDF is what new wraps use: three passes over 64 MiB on four lanes,
// which takes a fraction of a second on a current laptop.
var DefaultKDF = KDFParams{Time: 3, MemoryKiB: 64 * 1024, Threads: 4}

// maxKDFMemoryKiB keeps a tampered parameter row from asking for more memory
// than any vault was ever made with.
const maxKDFMemoryKiB = 4 * 1024 * 1024

// ErrBadParams is returned for stored KDF parameters outside what Argon2id
// accepts or what this package ever writes.
var ErrBadParams = errors.New("mailvault: unusable key derivation parameters")

// ErrWrongSecret is returned when a passphrase or recovery code does not
// open the vault.
var ErrWrongSecret = errors.New("mailvault: wrong passphrase or recovery code")

func (p KDFParams) valid() bool {
	return p.Time >= 1 && p.Time <= 64 && p.Threads >= 1 &&
		p.MemoryKiB >= 8*uint32(p.Threads) && p.MemoryKiB <= maxKDFMemoryKiB
}

// saltSize is the length of every KDF salt.
const saltSize = 16

// Wrapped is the data key encrypted under a key derived from a secret.
type Wrapped struct {
	Salt []byte
	KDF  KDFParams
	Box  []byte
}

// wrapKind names what a wrap was made from, as part of its associated data.
const (
	kindPassphrase = "passphrase"
	kindRecovery   = "recovery"
)

func deriveKEK(secret, salt []byte, p KDFParams) ([]byte, error) {
	if !p.valid() || len(salt) != saltSize {
		return nil, ErrBadParams
	}
	return argon2.IDKey(secret, salt, p.Time, p.MemoryKiB, p.Threads, KeySize), nil
}

func wrap(secret, dataKey, uid []byte, kind string, p KDFParams) (Wrapped, error) {
	salt, err := randomBytes(saltSize)
	if err != nil {
		return Wrapped{}, err
	}
	kek, err := deriveKEK(secret, salt, p)
	if err != nil {
		return Wrapped{}, err
	}
	defer Wipe(kek)
	box, err := seal(kek, dataKey, aad("wrap", kind, string(uid)))
	if err != nil {
		return Wrapped{}, err
	}
	return Wrapped{Salt: salt, KDF: p, Box: box}, nil
}

func unwrap(secret []byte, w Wrapped, uid []byte, kind string) ([]byte, error) {
	kek, err := deriveKEK(secret, w.Salt, w.KDF)
	if err != nil {
		return nil, err
	}
	defer Wipe(kek)
	dk, err := open(kek, w.Box, aad("wrap", kind, string(uid)))
	if err != nil {
		return nil, ErrWrongSecret
	}
	if len(dk) != KeySize {
		Wipe(dk)
		return nil, ErrOpen
	}
	return dk, nil
}

// Material is what a vault stores about its keys. Nothing in it opens mail
// without the passphrase or the recovery code. LinkKey is the exception that
// is stored in clear: it keys the hashes that let sealed messages find each
// other's conversations while the vault is locked.
type Material struct {
	UID        []byte
	Passphrase Wrapped
	Recovery   Wrapped
	PublicKey  []byte
	PrivateBox []byte
	LinkKey    []byte
}

// Keys is an unlocked vault. Wipe it when the vault locks.
type Keys struct {
	uid     []byte
	public  []byte
	dataKey []byte
	privRaw []byte
	private *ecdh.PrivateKey
}

// uidSize is the length of a vault's random identifier.
const uidSize = 16

// Create makes the keys for a new vault: a random data key wrapped under the
// passphrase and under a fresh recovery code, and an X25519 key pair whose
// private half is sealed under the data key. It returns the recovery code,
// which is shown once and never stored.
func Create(passphrase []byte, p KDFParams) (Material, string, *Keys, error) {
	uid, err := randomBytes(uidSize)
	if err != nil {
		return Material{}, "", nil, err
	}
	dataKey, err := randomBytes(KeySize)
	if err != nil {
		return Material{}, "", nil, err
	}
	priv, err := ecdh.X25519().GenerateKey(nil)
	if err != nil {
		return Material{}, "", nil, fmt.Errorf("mailvault: key pair: %w", err)
	}
	privRaw := priv.Bytes()
	privBox, err := seal(dataKey, privRaw, aad("private", string(uid)))
	if err != nil {
		return Material{}, "", nil, err
	}
	linkKey, err := randomBytes(KeySize)
	if err != nil {
		return Material{}, "", nil, err
	}
	pass, err := wrap(passphrase, dataKey, uid, kindPassphrase, p)
	if err != nil {
		return Material{}, "", nil, err
	}
	code, codeBytes, err := newRecoveryCode()
	if err != nil {
		return Material{}, "", nil, err
	}
	defer Wipe(codeBytes)
	rec, err := wrap(codeBytes, dataKey, uid, kindRecovery, p)
	if err != nil {
		return Material{}, "", nil, err
	}
	m := Material{
		UID:        uid,
		Passphrase: pass,
		Recovery:   rec,
		PublicKey:  priv.PublicKey().Bytes(),
		PrivateBox: privBox,
		LinkKey:    linkKey,
	}
	keys := &Keys{uid: uid, public: m.PublicKey, dataKey: dataKey, privRaw: privRaw, private: priv}
	return m, code, keys, nil
}

// Open unlocks a vault with its passphrase. A wrong passphrase is
// ErrWrongSecret.
func Open(m Material, passphrase []byte) (*Keys, error) {
	dk, err := unwrap(passphrase, m.Passphrase, m.UID, kindPassphrase)
	if err != nil {
		return nil, err
	}
	return openPrivate(m, dk)
}

// OpenWithRecovery unlocks a vault with its recovery code. Spaces, hyphens
// and letter case in the code do not matter.
func OpenWithRecovery(m Material, code string) (*Keys, error) {
	raw, ok := parseRecoveryCode(code)
	if !ok {
		return nil, ErrWrongSecret
	}
	defer Wipe(raw)
	dk, err := unwrap(raw, m.Recovery, m.UID, kindRecovery)
	if err != nil {
		return nil, err
	}
	return openPrivate(m, dk)
}

func openPrivate(m Material, dk []byte) (*Keys, error) {
	privRaw, err := open(dk, m.PrivateBox, aad("private", string(m.UID)))
	if err != nil {
		Wipe(dk)
		return nil, err
	}
	priv, err := ecdh.X25519().NewPrivateKey(privRaw)
	if err != nil {
		Wipe(dk)
		Wipe(privRaw)
		return nil, ErrOpen
	}
	if subtle.ConstantTimeCompare(priv.PublicKey().Bytes(), m.PublicKey) != 1 {
		Wipe(dk)
		Wipe(privRaw)
		return nil, ErrOpen
	}
	return &Keys{uid: m.UID, public: m.PublicKey, dataKey: dk, privRaw: privRaw, private: priv}, nil
}

// Rewrap wraps the data key under a new passphrase. The mail and the
// recovery wrap stay as they are.
func (k *Keys) Rewrap(passphrase []byte, p KDFParams) (Wrapped, error) {
	if k.dataKey == nil {
		return Wrapped{}, ErrWiped
	}
	return wrap(passphrase, k.dataKey, k.uid, kindPassphrase, p)
}

// ErrWiped is returned by a Keys that has been wiped.
var ErrWiped = errors.New("mailvault: keys were wiped")

// MessageKey derives the key of a sealed message from its ephemeral public
// key.
func (k *Keys) MessageKey(epk []byte) (*MessageKey, error) {
	if k.private == nil {
		return nil, ErrWiped
	}
	peer, err := ecdh.X25519().NewPublicKey(epk)
	if err != nil {
		return nil, ErrOpen
	}
	shared, err := k.private.ECDH(peer)
	if err != nil {
		return nil, ErrOpen
	}
	defer Wipe(shared)
	return deriveMessageKey(shared, epk, k.public, k.uid)
}

// Wipe zeroes the data key and the private key bytes this Keys holds and
// drops its reference to the X25519 key object.
func (k *Keys) Wipe() {
	Wipe(k.dataKey)
	Wipe(k.privRaw)
	k.dataKey, k.privRaw, k.private = nil, nil, nil
}

// LinkHash keys a Message-ID with the vault's link key, so sealed messages of
// one conversation can share a thread while the ids themselves stay out of
// the database. Someone holding the database and a Message-ID they already
// know can still test whether it is in the vault.
func LinkHash(linkKey []byte, value string) string {
	mac := hmac.New(sha256.New, linkKey)
	mac.Write(aad("link", value))
	return "v:" + hex.EncodeToString(mac.Sum(nil)[:20])
}

// recoveryBytes is the entropy of a recovery code: 160 bits, 32 characters.
const recoveryBytes = 20

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func newRecoveryCode() (string, []byte, error) {
	raw, err := randomBytes(recoveryBytes)
	if err != nil {
		return "", nil, err
	}
	enc := recoveryEncoding.EncodeToString(raw)
	groups := make([]string, 0, len(enc)/4)
	for i := 0; i < len(enc); i += 4 {
		groups = append(groups, enc[i:min(i+4, len(enc))])
	}
	return strings.Join(groups, "-"), raw, nil
}

func parseRecoveryCode(code string) ([]byte, bool) {
	var b strings.Builder
	for _, r := range code {
		if r == '-' || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	raw, err := recoveryEncoding.DecodeString(b.String())
	if err != nil || len(raw) != recoveryBytes {
		return nil, false
	}
	return raw, true
}
