package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/crypto/backup"
	"maunium.net/go/mautrix/crypto/verificationhelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/sqlstatestore"
)

// cryptoAccountID names this app's row in the crypto store.
const cryptoAccountID = "pelton"

// apiTimeout bounds an ordinary request, and with it the sync long poll.
const apiTimeout = 2 * time.Minute

// syncTimeout is how long the server may hold a sync open.
const syncTimeout = 30 * time.Second

// session is one signed-in account while it runs.
type session struct {
	m   *Manager
	st  *store
	acc accountRow
	log *slog.Logger

	cli         *mautrix.Client
	media       *http.Client
	syncer      *mautrix.DefaultSyncer
	stateStore  *sqlstatestore.SQLStateStore
	cryptoStore *crypto.SQLCryptoStore
	mach        *crypto.OlmMachine
	verify      *verificationhelper.VerificationHelper

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// ingestMu serialises writes to the timeline: the sync loop and backwards
	// pagination both append to it.
	ingestMu sync.Mutex
	// encryptMu keeps two sends from sharing a new group session at once.
	encryptMu sync.Mutex
	// backupMu keeps one key backup upload running at a time.
	backupMu sync.Mutex

	cache *decryptCache

	mu sync.Mutex
	// typing is who is typing in each room, as the last sync said.
	typing map[id.RoomID][]id.UserID
	// keyRequests remembers when a missing session was last asked for, so a
	// timeline full of one undecryptable session asks once.
	keyRequests map[id.SessionID]time.Time
	// backupKey is the key backup's private key, once known.
	backupKey     *backup.MegolmBackupKey
	backupVersion id.KeyBackupVersion
	// verifications are the interactive verifications in flight.
	verifications map[id.VerificationTransactionID]*Verification
	// uploadLimit is the server's media size limit, 0 until read.
	uploadLimit int64
	// synced is set after the first sync, before which nothing notifies.
	synced bool

	// lastPrune is when the sync loop last pruned the media cache.
	lastPrune time.Time
}

// newSession sets up the client, the encryption machine and the verification
// helper for a signed-in account. It contacts the homeserver to make sure this
// device's keys are published, and fails when the stored sign-in no longer
// matches them.
func newSession(ctx context.Context, m *Manager, st *store, acc accountRow, secret Secret) (*session, error) {
	zlog := newZerolog(m.opts.Log)
	cli, err := mautrix.NewClient(acc.Homeserver, acc.UserID, secret.AccessToken)
	if err != nil {
		return nil, err
	}
	base := m.opts.HTTPClient()
	api := *base
	api.Timeout = apiTimeout
	media := *base
	media.Timeout = 0
	cli.Client = &api
	cli.DeviceID = acc.DeviceID
	cli.UserAgent = userAgent
	cli.Log = zlog

	s := &session{
		m:             m,
		st:            st,
		acc:           acc,
		log:           m.opts.Log,
		cli:           cli,
		media:         &media,
		cache:         newDecryptCache(4000),
		typing:        map[id.RoomID][]id.UserID{},
		keyRequests:   map[id.SessionID]time.Time{},
		verifications: map[id.VerificationTransactionID]*Verification{},
	}

	dblog := dbutil.ZeroLogger(zlog)
	s.stateStore = sqlstatestore.NewSQLStateStore(st.mdb, dblog, false)
	if err := s.stateStore.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("chat: prepare state store: %w", err)
	}
	s.cryptoStore = crypto.NewSQLCryptoStore(st.mdb, dblog, cryptoAccountID, acc.DeviceID, []byte(secret.PickleKey))
	if err := s.cryptoStore.DB.Upgrade(ctx); err != nil {
		return nil, fmt.Errorf("chat: prepare crypto store: %w", err)
	}
	cli.StateStore = s.stateStore
	cli.Store = s.cryptoStore

	s.syncer = mautrix.NewDefaultSyncer()
	s.syncer.FilterJSON = syncFilter()
	cli.Syncer = s.syncer

	s.mach = crypto.NewOlmMachine(cli, &zlog, s.cryptoStore, s.stateStore)
	if err := s.mach.Load(ctx); err != nil {
		return nil, fmt.Errorf("chat: load encryption keys: %w", err)
	}
	if err := s.ensureDeviceKeys(ctx); err != nil {
		return nil, err
	}
	s.mach.SessionReceived = s.onSessionReceived
	cli.Crypto = s

	s.syncer.OnSync(s.mach.ProcessSyncResponse)
	s.syncer.OnEvent(cli.StateStoreSyncHandler)
	s.syncer.OnEventType(event.StateMember, s.mach.HandleMemberEvent)
	s.syncer.OnEventType(event.StateHistoryVisibility, s.mach.HandleHistoryVisibility)
	s.syncer.OnEventType(event.EventEncrypted, s.dispatchDecrypted)

	s.verify = verificationhelper.NewVerificationHelper(cli, s.mach, nil, s, false, false, true)
	if err := s.verify.Init(ctx); err != nil {
		return nil, fmt.Errorf("chat: start verification: %w", err)
	}
	cli.Verification = s.verify
	s.loadBackupKey(ctx)
	return s, nil
}

// syncFilter keeps syncs small: members load lazily, and presence is not
// asked for at all, so the account never learns or shares who is online.
func syncFilter() *mautrix.Filter {
	lazy := &mautrix.FilterPart{LazyLoadMembers: true}
	return &mautrix.Filter{
		EventFormat: mautrix.EventFormatClient,
		Presence:    &mautrix.FilterPart{NotTypes: []event.Type{{Type: "*"}}},
		Room: &mautrix.RoomFilter{
			State:    lazy,
			Timeline: &mautrix.FilterPart{LazyLoadMembers: true, Limit: 30},
		},
	}
}

// ensureDeviceKeys publishes this device's keys the first time, and checks on
// later starts that the server still has the ones this store holds.
func (s *session) ensureDeviceKeys(ctx context.Context) error {
	resp, err := s.cli.QueryKeys(ctx, &mautrix.ReqQueryKeys{
		DeviceKeys: map[id.UserID]mautrix.DeviceIDList{s.cli.UserID: {s.cli.DeviceID}},
	})
	if err != nil {
		return fmt.Errorf("chat: read this device's keys: %w", err)
	}
	shared := s.mach.GetAccount().Shared
	device, ok := resp.DeviceKeys[s.cli.UserID][s.cli.DeviceID]
	switch {
	case !ok || len(device.Keys) == 0:
		if shared {
			return errors.New("chat: this device's keys are gone from the server; sign in again")
		}
		if err := s.mach.ShareKeys(ctx, -1); err != nil {
			return fmt.Errorf("chat: publish this device's keys: %w", err)
		}
	case !shared:
		return errors.New("chat: the server holds keys this device does not know; sign in again")
	case device.Keys.GetEd25519(s.cli.DeviceID) != s.mach.OwnIdentity().SigningKey:
		return errors.New("chat: this device's keys on the server do not match; sign in again")
	}
	return nil
}

// run starts the sync loop.
func (s *session) run() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.done = make(chan struct{})
	go s.loop()
}

// stop ends the sync loop, waits for it, and closes the database.
func (s *session) stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
	s.mach.Destroy()
	_ = s.st.close()
}

// backoff is the wait before retry n of a failed sync: doubling from a
// second up to a minute, with jitter so many clients do not retry in step.
func backoff(n int) time.Duration {
	d := time.Second << min(n-1, 6)
	if d > time.Minute {
		d = time.Minute
	}
	return d + time.Duration(rand.Int64N(int64(d/5)+1))
}

// loop syncs until the session stops. A failed request backs off and
// retries; a token the server no longer accepts ends the session.
func (s *session) loop() {
	defer close(s.done)
	ctx := s.ctx
	s.pruneMediaCacheDue(time.Now())
	filterID, err := s.filterID(ctx)
	failures := 0
	for err != nil {
		failures++
		if !s.waitRetry(ctx, failures, err) {
			return
		}
		filterID, err = s.filterID(ctx)
	}
	failures = 0
	for ctx.Err() == nil {
		s.pruneMediaCacheDue(time.Now())
		since, err := s.cryptoStore.LoadNextBatch(ctx, s.cli.UserID)
		if err != nil {
			s.log.Error("chat: read sync position", "err", err)
			return
		}
		timeout := int(syncTimeout.Milliseconds())
		if failures > 0 || since == "" {
			timeout = 0
		}
		if since == "" {
			s.m.setStatus(func(st *Status) { st.State = StateSyncing; st.Error = "" })
		}
		resp, err := s.cli.FullSyncRequest(ctx, mautrix.ReqSync{Timeout: timeout, Since: since, FilterID: filterID})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, mautrix.MUnknownToken) {
				s.m.setStatus(func(st *Status) {
					st.State = StateError
					st.Error = "This sign-in has ended on the server. Sign out and sign in again."
				})
				return
			}
			failures++
			if !s.waitRetry(ctx, failures, err) {
				return
			}
			continue
		}
		if err := s.syncer.ProcessResponse(ctx, resp, since); err != nil {
			s.log.Error("chat: process sync", "err", err)
		}
		s.ingest(ctx, resp, since)
		if err := s.cryptoStore.SaveNextBatch(ctx, s.cli.UserID, resp.NextBatch); err != nil {
			s.log.Error("chat: save sync position", "err", err)
		}
		if failures > 0 || since == "" || s.m.Status().State != StateOnline {
			s.m.setStatus(func(st *Status) { st.State = StateOnline; st.Error = "" })
		}
		failures = 0
		go s.uploadBackup(context.WithoutCancel(ctx))
	}
}

// waitRetry reports the failure and waits out the backoff. It returns false
// when the session stopped meanwhile.
func (s *session) waitRetry(ctx context.Context, failures int, err error) bool {
	s.log.Warn("chat: sync failed", "attempt", failures, "err", err)
	s.m.setStatus(func(st *Status) {
		st.State = StateOffline
		st.Error = err.Error()
	})
	select {
	case <-ctx.Done():
		return false
	case <-time.After(backoff(failures)):
		return true
	}
}

// filterID returns the stored sync filter, creating it on the server the
// first time.
func (s *session) filterID(ctx context.Context) (string, error) {
	fid, err := s.cryptoStore.LoadFilterID(ctx, s.cli.UserID)
	if err != nil || fid != "" {
		return fid, err
	}
	resp, err := s.cli.CreateFilter(ctx, s.syncer.FilterJSON)
	if err != nil {
		return "", err
	}
	return resp.FilterID, s.cryptoStore.SaveFilterID(ctx, s.cli.UserID, resp.FilterID)
}

// The session is the client's CryptoHelper: sends to encrypted rooms are
// encrypted through it.

// Encrypt encrypts content for a room, sharing a new group session with the
// room's members first when there is none or it ran out.
func (s *session) Encrypt(ctx context.Context, roomID id.RoomID, evtType event.Type, content any) (*event.EncryptedEventContent, error) {
	s.encryptMu.Lock()
	defer s.encryptMu.Unlock()
	encrypted, err := s.mach.EncryptMegolmEvent(ctx, roomID, evtType, content)
	if err == nil {
		return encrypted, nil
	}
	if !errors.Is(err, crypto.ErrSessionExpired) && !errors.Is(err, crypto.ErrNoGroupSession) && !errors.Is(err, crypto.ErrSessionNotShared) {
		return nil, err
	}
	if err := s.ensureMembers(ctx, roomID); err != nil {
		return nil, err
	}
	users, err := s.stateStore.GetRoomJoinedOrInvitedMembers(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("chat: read room members: %w", err)
	}
	if err := s.mach.ShareGroupSession(ctx, roomID, users); err != nil {
		return nil, fmt.Errorf("chat: share room key: %w", err)
	}
	return s.mach.EncryptMegolmEvent(ctx, roomID, evtType, content)
}

// Decrypt decrypts one encrypted room event.
func (s *session) Decrypt(ctx context.Context, evt *event.Event) (*event.Event, error) {
	return s.decrypt(ctx, evt)
}

// WaitForSession waits for the keys of a group session to arrive.
func (s *session) WaitForSession(ctx context.Context, roomID id.RoomID, senderKey id.SenderKey, sessionID id.SessionID, timeout time.Duration) bool {
	return s.mach.WaitForSession(ctx, roomID, senderKey, sessionID, timeout)
}

// RequestSession asks the sender's device and the user's own devices for a
// group session's keys.
func (s *session) RequestSession(ctx context.Context, roomID id.RoomID, senderKey id.SenderKey, sessionID id.SessionID, userID id.UserID, deviceID id.DeviceID) {
	if deviceID == "" {
		deviceID = "*"
	}
	err := s.mach.SendRoomKeyRequest(ctx, roomID, senderKey, sessionID, "", map[id.UserID][]id.DeviceID{
		userID:       {deviceID},
		s.cli.UserID: {"*"},
	})
	if err != nil {
		s.log.Warn("chat: request room key", "err", err)
	}
}

// Init is part of the CryptoHelper interface; the session sets itself up in
// newSession.
func (s *session) Init(context.Context) error { return nil }

// ensureMembers loads a room's full member list once. Syncs load members
// lazily, but a room key has to reach every member's devices.
func (s *session) ensureMembers(ctx context.Context, roomID id.RoomID) error {
	fetched, err := s.stateStore.HasFetchedMembers(ctx, roomID)
	if err != nil || fetched {
		return err
	}
	resp, err := s.cli.Members(ctx, roomID)
	if err != nil {
		return fmt.Errorf("chat: load room members: %w", err)
	}
	for _, evt := range resp.Chunk {
		evt.RoomID = roomID
		parseEvent(evt)
	}
	if err := s.stateStore.ReplaceCachedMembers(ctx, roomID, resp.Chunk); err != nil {
		return err
	}
	return s.stateStore.MarkMembersFetched(ctx, roomID)
}

// dispatchDecrypted decrypts an encrypted event from the sync and passes the
// result to the syncer's listeners, which is how in-room verification
// requests reach the verification helper.
func (s *session) dispatchDecrypted(ctx context.Context, evt *event.Event) {
	dec, err := s.decrypt(ctx, evt)
	if err != nil {
		return
	}
	if dec.Type.Type == event.EventMessage.Type || strings.HasPrefix(dec.Type.Type, "m.key.verification.") {
		dec.Mautrix.EventSource = evt.Mautrix.EventSource | event.SourceDecrypted
		s.syncer.Dispatch(ctx, dec)
	}
}

// onSessionReceived runs when keys for a group session arrive, from another
// device, a key request or the backup: events that could not be read may now
// be readable.
func (s *session) onSessionReceived(_ context.Context, roomID id.RoomID, sessionID id.SessionID, _ uint32) {
	s.mu.Lock()
	delete(s.keyRequests, sessionID)
	s.mu.Unlock()
	s.m.emit(EventTimeline, TimelineChange{RoomID: roomID.String()})
	s.m.emit(EventRooms, RoomsChange{RoomIDs: []string{roomID.String()}})
}

// newZerolog returns a zerolog logger that hands mautrix's warnings and errors
// to the app log. Debug output is dropped: it names rooms and users.
func newZerolog(log *slog.Logger) zerolog.Logger {
	return zerolog.New(slogWriter{log}).Level(zerolog.WarnLevel).With().Timestamp().Logger()
}

type slogWriter struct{ log *slog.Logger }

func (w slogWriter) Write(p []byte) (int, error) {
	w.log.Warn("matrix", "detail", strings.TrimSpace(string(p)))
	return len(p), nil
}

// removeDir deletes a directory tree, treating one that is not there as done.
func removeDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("chat: remove old data: %w", err)
	}
	return nil
}
