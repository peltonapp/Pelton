// Package chat is Pelton's Matrix client: sign-in to the user's own
// homeserver, the sync loop, rooms and timelines, sending, media, end to end
// encryption with device verification and key backup. It is built on
// mautrix-go with its pure Go Olm implementation (the goolm build tag).
//
// Nothing here runs until the user turns chat on and signs in. The access
// token and the key the encryption store is pickled with live in the OS
// credential store through a SecretStore; everything else (rooms, events,
// mautrix's state and crypto tables) lives in one SQLite file per account,
// apart from the mail database.
package chat

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// Event names the package emits through Options.Emit. The desktop layer
// forwards them to the frontend under the same names.
const (
	// EventStatus carries the new Status whenever sign-in, sync or the
	// encryption state changes.
	EventStatus = "chat:status"
	// EventRooms fires when the room list changed; the payload lists the
	// rooms that changed.
	EventRooms = "chat:rooms"
	// EventTimeline fires when a room's timeline changed: new events, edits,
	// reactions, redactions, or keys that make more of it readable.
	EventTimeline = "chat:timeline"
	// EventTyping carries who is typing in a room.
	EventTyping = "chat:typing"
	// EventReceipts fires when someone's read marker in a room moved.
	EventReceipts = "chat:receipts"
	// EventVerification carries the state of an interactive verification.
	EventVerification = "chat:verification"
	// EventMediaProgress reports a file being saved or sent.
	EventMediaProgress = "chat:media-progress"
)

// ErrNotSignedIn is returned by anything that needs an account when there is
// none.
var ErrNotSignedIn = errors.New("chat: not signed in")

// SecretStore keeps the per-account secrets. The app passes the OS credential
// store; tests pass a map.
type SecretStore interface {
	Load(key string) (Secret, error)
	Store(key string, s Secret) error
	Delete(key string) error
}

// ErrNoSecret is what a SecretStore returns when nothing is stored under a key.
var ErrNoSecret = errors.New("chat: no stored secret")

// Secret is what a signed-in account keeps in the SecretStore.
type Secret struct {
	AccessToken string
	PickleKey   string
}

// Notification is a new message from someone else. Direct and Mention say
// whether it is one the user asked to be told about.
type Notification struct {
	RoomID   string
	EventID  string
	RoomName string
	Sender   string
	// Body is empty when the message could not be decrypted yet.
	Body    string
	Direct  bool
	Mention bool
}

// Prefs are the user's chat choices the package reads as it works, so a change
// in settings applies without a restart.
type Prefs struct {
	// SendReceipts sends public read receipts. Off still moves the private
	// read marker, so other devices of the user stay in step.
	SendReceipts bool
	// SendTyping tells the room while the user types.
	SendTyping bool
}

// Options configure a Manager.
type Options struct {
	// Dir is the directory the account's database and media cache live in.
	Dir string
	// SecretKey names the account's entry in Secrets.
	SecretKey string
	Secrets   SecretStore
	// HTTPClient returns the client every request goes through, so the app's
	// proxy setting applies. It is called once per session.
	HTTPClient func() *http.Client
	// Emit sends an event to the frontend. It may be nil in tests.
	Emit func(name string, payload any)
	// Notify is called with the new messages from others each sync brings,
	// after the first.
	Notify func([]Notification)
	// Prefs returns the current chat preferences.
	Prefs func() Prefs
	// DeviceName is the name this sign-in shows in the user's session list.
	DeviceName string
	// OpenURL opens a link in the system browser, for single sign-on.
	OpenURL func(string)
	Log     *slog.Logger
}

// Manager owns the chat account of one profile. Only one account is signed in
// at a time; the account's files are keyed so a second could live beside it.
type Manager struct {
	opts Options

	mu     sync.Mutex
	sess   *session
	status Status
	// pending holds a sign-in that is waiting for the browser.
	ssoCancel context.CancelFunc
}

// New returns a Manager. Nothing is opened or contacted until Start.
func New(opts Options) *Manager {
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = func() *http.Client { return &http.Client{} }
	}
	if opts.Prefs == nil {
		opts.Prefs = func() Prefs { return Prefs{SendReceipts: true, SendTyping: true} }
	}
	return &Manager{opts: opts, status: Status{State: StateSignedOut}}
}

func (m *Manager) dbPath() string {
	return filepath.Join(m.opts.Dir, "chat.db")
}

func (m *Manager) emit(name string, payload any) {
	if m.opts.Emit != nil {
		m.opts.Emit(name, payload)
	}
}

// setStatus replaces the status and tells the frontend.
func (m *Manager) setStatus(update func(*Status)) {
	m.mu.Lock()
	update(&m.status)
	st := m.status
	m.mu.Unlock()
	m.emit(EventStatus, st)
}

// Status returns the account's state as it stands.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// active returns the running session, or ErrNotSignedIn.
func (m *Manager) active() (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sess == nil {
		return nil, ErrNotSignedIn
	}
	return m.sess, nil
}

// Start resumes the stored account, if there is one, and starts syncing it.
// Without one it does nothing: chat stays signed out until a sign-in.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	running := m.sess != nil
	m.mu.Unlock()
	if running {
		return nil
	}
	if _, err := os.Stat(m.dbPath()); err != nil {
		return nil
	}
	st, err := openStore(m.dbPath())
	if err != nil {
		return err
	}
	acc, ok, err := st.loadAccount(ctx)
	if err != nil || !ok {
		st.close()
		return err
	}
	secret, err := m.opts.Secrets.Load(m.opts.SecretKey)
	if errors.Is(err, ErrNoSecret) {
		st.close()
		m.setStatus(func(s *Status) {
			*s = Status{State: StateSignedOut, Error: "The stored sign-in is missing. Sign in again."}
		})
		return nil
	}
	if err != nil {
		st.close()
		return err
	}
	return m.startSession(ctx, st, acc, secret)
}

// startSession brings a signed-in account up: crypto, verification and the
// sync loop.
func (m *Manager) startSession(ctx context.Context, st *store, acc accountRow, secret Secret) error {
	m.setStatus(func(s *Status) {
		*s = Status{State: StateStarting, UserID: acc.UserID.String(), DeviceID: acc.DeviceID.String(),
			Homeserver: acc.Homeserver, DisplayName: acc.DisplayName}
	})
	sess, err := newSession(ctx, m, st, acc, secret)
	if err != nil {
		st.close()
		m.setStatus(func(s *Status) {
			s.State = StateError
			s.Error = err.Error()
		})
		return err
	}
	m.mu.Lock()
	m.sess = sess
	m.mu.Unlock()
	sess.run()
	go sess.refreshCryptoStatus(context.WithoutCancel(ctx))
	return nil
}

// Stop ends the sync loop and closes the account's files. The account stays
// signed in; Start resumes it.
func (m *Manager) Stop() {
	m.mu.Lock()
	sess := m.sess
	m.sess = nil
	if m.ssoCancel != nil {
		m.ssoCancel()
		m.ssoCancel = nil
	}
	m.mu.Unlock()
	if sess != nil {
		sess.stop()
	}
	m.setStatus(func(s *Status) {
		if s.State != StateSignedOut {
			s.State = StateStopped
		}
	})
}

// SignOut ends this sign-in on the server, then removes everything kept for
// it on this computer: the database with its encryption keys, the media cache
// and the stored secret. The server call is best effort, so signing out works
// offline too.
func (m *Manager) SignOut(ctx context.Context) error {
	m.mu.Lock()
	sess := m.sess
	m.sess = nil
	m.mu.Unlock()
	if sess != nil {
		_, _ = sess.cli.Logout(ctx)
		sess.stop()
	}
	if err := m.opts.Secrets.Delete(m.opts.SecretKey); err != nil {
		return err
	}
	if err := os.RemoveAll(m.opts.Dir); err != nil {
		return err
	}
	m.setStatus(func(s *Status) { *s = Status{State: StateSignedOut} })
	return nil
}

// Session states.
const (
	StateSignedOut = "signedOut"
	StateStarting  = "starting"
	StateSyncing   = "syncing"
	StateOnline    = "online"
	StateOffline   = "offline"
	StateError     = "error"
	StateStopped   = "stopped"
)

// Status is the chat account as the settings page and the banner show it.
type Status struct {
	// State is one of the State constants.
	State       string `json:"state"`
	UserID      string `json:"userId"`
	DeviceID    string `json:"deviceId"`
	Homeserver  string `json:"homeserver"`
	DisplayName string `json:"displayName"`
	Avatar      string `json:"avatar"`
	// Error explains a failed state; empty otherwise.
	Error string `json:"error"`
	// Encryption is the end to end encryption state of this device.
	Encryption EncryptionStatus `json:"encryption"`
}

// EncryptionStatus describes cross-signing, secret storage and key backup.
type EncryptionStatus struct {
	// Ready is false until the encryption state has been read.
	Ready bool `json:"ready"`
	// DeviceVerified is true once this device is signed by the user's own
	// cross-signing key.
	DeviceVerified bool `json:"deviceVerified"`
	// CrossSigning is true when the account has cross-signing keys at all.
	CrossSigning bool `json:"crossSigning"`
	// SecretStorage is true when the account has a recovery key set up.
	SecretStorage bool `json:"secretStorage"`
	// SecretStoragePassphrase is true when that key can also be unlocked with
	// a passphrase.
	SecretStoragePassphrase bool `json:"secretStoragePassphrase"`
	// BackupVersion is the server's current key backup, or empty.
	BackupVersion string `json:"backupVersion"`
	// BackupKeys is how many keys the server backup holds.
	BackupKeys int `json:"backupKeys"`
	// BackupActive is true when this device holds the backup key, so it
	// restores from and uploads to the backup.
	BackupActive bool `json:"backupActive"`
	// Fingerprint is this device's Ed25519 key, for manual comparison.
	Fingerprint string `json:"fingerprint"`
}
