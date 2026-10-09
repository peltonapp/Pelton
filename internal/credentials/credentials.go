// Package credentials stores account secrets in the OS keyring (macOS Keychain,
// Windows Credential Manager, libsecret on linux) via go-keyring. Only the
// non-secret account metadata lives in the sqlite store; passwords and oauth
// tokens live here, referenced by account id, and never touch the database.
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/peltonapp/Pelton/internal/logging"
)

// remember hands a secret to the log redactor so it can never appear in a log
// or crash file, whatever error string it ends up inside. Every path that
// stores or loads a secret calls it, so the redactor knows a value from the
// moment the process first holds it.
func remember(values ...string) {
	for _, v := range values {
		logging.Register(v)
	}
}

// rememberSecret registers every field of an account secret that is a secret.
func rememberSecret(s Secret) {
	remember(s.Password, s.ClientSecret, s.RefreshToken, s.AccessToken)
}

// defaultService is the keyring service name a normal install files its
// secrets under.
const defaultService = "Pelton"

// service is the keyring service name all secrets of this process are filed
// under. See UseService.
var service = defaultService

// UseService files every secret of this process under name instead of the
// default. Entries are keyed by account id, and a dev run or a nightly keeps
// its own database whose ids start at 1 like the installed app's; sharing one
// service let them overwrite the installed app's passwords. Call it once at
// startup, before any secret is read or written.
func UseService(name string) {
	service = name
}

// maxEntrySize keeps every keyring write under Windows Credential Manager's
// hard 2560-byte-per-entry cap (with margin), splitting anything larger
// across multiple entries. OAuth secrets with long access/refresh tokens can
// exceed that on their own.
const maxEntrySize = 2000

// chunkMarker prefixes the main entry's value when the secret was split
// across chunkMarker+N and the numbered chunk entries below it.
const chunkMarker = "pelton-chunked:v1:"

// ErrNotFound is returned when no secret is stored for an account.
var ErrNotFound = errors.New("credentials: not found")

// Method is how an account authenticates.
type Method string

const (
	// MethodPassword is plain username/password (or app-specific password).
	MethodPassword Method = "password"
	// MethodOAuth is XOAUTH2 with a refresh token (gmail, outlook).
	MethodOAuth Method = "oauth"
)

// Secret is everything needed to authenticate one account, kept out of the db.
// For oauth, AccessToken/Expiry are a cache the oauth package refreshes from
// RefreshToken; Provider and ClientID identify how to refresh.
type Secret struct {
	Method       Method    `json:"method"`
	Password     string    `json:"password,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	ClientID     string    `json:"clientId,omitempty"`
	ClientSecret string    `json:"clientSecret,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	AccessToken  string    `json:"accessToken,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

// Store writes the secret for an account, replacing any existing one. The new
// value (and, if chunked, every new chunk) is written before any stale chunk
// from a previous, larger secret is removed, so a failed write never leaves
// the account's secret unreadable.
func Store(accountID int64, s Secret) error {
	rememberSecret(s)
	encoded, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("credentials: encode secret: %w", err)
	}
	oldChunks := existingChunkCount(accountID)

	if len(encoded) <= maxEntrySize {
		if err := keyring.Set(service, key(accountID), string(encoded)); err != nil {
			return fmt.Errorf("credentials: store for account %d: %w", accountID, err)
		}
		deleteChunkRange(accountID, 0, oldChunks)
		return nil
	}

	chunks := chunkBytes(encoded, maxEntrySize)
	for i, part := range chunks {
		if err := keyring.Set(service, chunkKey(accountID, i), string(part)); err != nil {
			return fmt.Errorf("credentials: store chunk %d for account %d: %w", i, accountID, err)
		}
	}
	if err := keyring.Set(service, key(accountID), chunkMarker+strconv.Itoa(len(chunks))); err != nil {
		return fmt.Errorf("credentials: store for account %d: %w", accountID, err)
	}
	deleteChunkRange(accountID, len(chunks), oldChunks)
	return nil
}

// Load reads the secret for an account, or ErrNotFound.
func Load(accountID int64) (Secret, error) {
	raw, err := keyring.Get(service, key(accountID))
	if errors.Is(err, keyring.ErrNotFound) {
		return Secret{}, ErrNotFound
	}
	if err != nil {
		return Secret{}, fmt.Errorf("credentials: load for account %d: %w", accountID, err)
	}
	if n, ok := chunkCount(raw); ok {
		var sb strings.Builder
		for i := range n {
			part, err := keyring.Get(service, chunkKey(accountID, i))
			if err != nil {
				return Secret{}, fmt.Errorf("credentials: load chunk %d for account %d: %w", i, accountID, err)
			}
			sb.WriteString(part)
		}
		raw = sb.String()
	}
	var s Secret
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Secret{}, fmt.Errorf("credentials: decode secret for account %d: %w", accountID, err)
	}
	rememberSecret(s)
	return s, nil
}

// Delete removes the secret for an account. A missing entry is not an error so
// account deletion is idempotent.
func Delete(accountID int64) error {
	deleteChunkRange(accountID, 0, existingChunkCount(accountID))
	err := keyring.Delete(service, key(accountID))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentials: delete for account %d: %w", accountID, err)
	}
	return nil
}

// proxyPasswordKey is the keyring entry for the outbound proxy password. It is a
// non-numeric name so it can never collide with a per-account entry (those are
// the decimal account id).
const proxyPasswordKey = "proxy-password"

// StoreProxyPassword saves the proxy authentication password, or clears it when
// empty. It is kept out of the settings db like every other secret.
func StoreProxyPassword(password string) error {
	if password == "" {
		return DeleteProxyPassword()
	}
	remember(password)
	if err := keyring.Set(service, proxyPasswordKey, password); err != nil {
		return fmt.Errorf("credentials: store proxy password: %w", err)
	}
	return nil
}

// LoadProxyPassword returns the stored proxy password, or "" when none is set.
func LoadProxyPassword() (string, error) {
	raw, err := keyring.Get(service, proxyPasswordKey)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("credentials: load proxy password: %w", err)
	}
	remember(raw)
	return raw, nil
}

// DeleteProxyPassword removes the stored proxy password. A missing entry is not
// an error.
func DeleteProxyPassword() error {
	err := keyring.Delete(service, proxyPasswordKey)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentials: delete proxy password: %w", err)
	}
	return nil
}

// accountProxyPasswordKey is the keyring entry for the password of a mailbox's
// own proxy. The prefix keeps it apart from the account secret, which is the
// bare decimal id.
func accountProxyPasswordKey(accountID int64) string {
	return proxyPasswordKey + "-" + strconv.FormatInt(accountID, 10)
}

// StoreAccountProxyPassword saves the password of a mailbox's own proxy, or
// clears it when empty.
func StoreAccountProxyPassword(accountID int64, password string) error {
	if password == "" {
		return DeleteAccountProxyPassword(accountID)
	}
	remember(password)
	if err := keyring.Set(service, accountProxyPasswordKey(accountID), password); err != nil {
		return fmt.Errorf("credentials: store proxy password for account %d: %w", accountID, err)
	}
	return nil
}

// LoadAccountProxyPassword returns the password of a mailbox's own proxy, or
// "" when none is set.
func LoadAccountProxyPassword(accountID int64) (string, error) {
	raw, err := keyring.Get(service, accountProxyPasswordKey(accountID))
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("credentials: load proxy password for account %d: %w", accountID, err)
	}
	remember(raw)
	return raw, nil
}

// DeleteAccountProxyPassword removes the password of a mailbox's own proxy. A
// missing entry is not an error.
func DeleteAccountProxyPassword(accountID int64) error {
	err := keyring.Delete(service, accountProxyPasswordKey(accountID))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentials: delete proxy password for account %d: %w", accountID, err)
	}
	return nil
}

// virusTotalKeyName is the keyring entry holding the VirusTotal API key.
const virusTotalKeyName = "virustotal-api-key"

// StoreVirusTotalKey saves the VirusTotal API key, or clears it when empty. It
// is a credential to a third-party account, so it belongs in the keyring rather
// than the settings db.
func StoreVirusTotalKey(apiKey string) error {
	if apiKey == "" {
		return DeleteVirusTotalKey()
	}
	remember(apiKey)
	if err := keyring.Set(service, virusTotalKeyName, apiKey); err != nil {
		return fmt.Errorf("credentials: store virustotal key: %w", err)
	}
	return nil
}

// LoadVirusTotalKey returns the stored VirusTotal API key, or "" when none is set.
func LoadVirusTotalKey() (string, error) {
	raw, err := keyring.Get(service, virusTotalKeyName)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("credentials: load virustotal key: %w", err)
	}
	remember(raw)
	return raw, nil
}

// DeleteVirusTotalKey removes the stored VirusTotal API key. A missing entry is
// not an error.
func DeleteVirusTotalKey() error {
	err := keyring.Delete(service, virusTotalKeyName)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentials: delete virustotal key: %w", err)
	}
	return nil
}

// pgpPassphrasePrefix names the keyring entry holding one PGP private key's
// passphrase, keyed by fingerprint. Storing it is opt-in per key: the default
// is to hold it in memory for the session and forget it on quit.
const pgpPassphrasePrefix = "pgp-passphrase-"

// StorePGPPassphrase remembers a private key's passphrase, or clears it when
// empty. It never goes near the settings database.
func StorePGPPassphrase(fingerprint, passphrase string) error {
	if passphrase == "" {
		return DeletePGPPassphrase(fingerprint)
	}
	remember(passphrase)
	if err := keyring.Set(service, pgpPassphrasePrefix+fingerprint, passphrase); err != nil {
		return fmt.Errorf("credentials: store pgp passphrase: %w", err)
	}
	return nil
}

// LoadPGPPassphrase returns a remembered passphrase, or "" when the user chose
// not to remember this key.
func LoadPGPPassphrase(fingerprint string) (string, error) {
	raw, err := keyring.Get(service, pgpPassphrasePrefix+fingerprint)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("credentials: load pgp passphrase: %w", err)
	}
	remember(raw)
	return raw, nil
}

// DeletePGPPassphrase forgets a remembered passphrase. A missing entry is not
// an error, so deleting a key always cleans up after itself.
func DeletePGPPassphrase(fingerprint string) error {
	err := keyring.Delete(service, pgpPassphrasePrefix+fingerprint)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentials: delete pgp passphrase: %w", err)
	}
	return nil
}

// key is the per-account keyring entry name.
func key(accountID int64) string {
	return strconv.FormatInt(accountID, 10)
}

// chunkKey is the entry name for chunk i of a split secret.
func chunkKey(accountID int64, i int) string {
	return key(accountID) + "." + strconv.Itoa(i)
}

// chunkCount reports whether raw is a chunk marker and, if so, how many
// chunk entries to read.
func chunkCount(raw string) (int, bool) {
	if !strings.HasPrefix(raw, chunkMarker) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(raw, chunkMarker))
	if err != nil {
		return 0, false
	}
	return n, true
}

// chunkBytes splits data into pieces of at most size bytes.
func chunkBytes(data []byte, size int) [][]byte {
	var out [][]byte
	for len(data) > size {
		out = append(out, data[:size])
		data = data[size:]
	}
	return append(out, data)
}

// existingChunkCount returns how many chunk entries accountID's current
// secret is split across, or 0 if it isn't chunked (or doesn't exist).
func existingChunkCount(accountID int64) int {
	raw, err := keyring.Get(service, key(accountID))
	if err != nil {
		return 0
	}
	n, ok := chunkCount(raw)
	if !ok {
		return 0
	}
	return n
}

// deleteChunkRange removes chunk entries [from, to) for accountID.
func deleteChunkRange(accountID int64, from, to int) {
	for i := from; i < to; i++ {
		_ = keyring.Delete(service, chunkKey(accountID, i))
	}
}
