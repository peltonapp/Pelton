package chat

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/crypto/backup"
	"maunium.net/go/mautrix/crypto/ssss"
	"maunium.net/go/mautrix/crypto/verificationhelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Verification is an interactive verification with another device: one of
// the user's own, or someone else's.
type Verification struct {
	ID       string `json:"id"`
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
	DeviceID string `json:"deviceId"`
	// Self is true when the other device is one of the user's own.
	Self bool `json:"self"`
	// Incoming is true when the other side asked.
	Incoming bool `json:"incoming"`
	// State is requested, ready, sas, confirmed, done or cancelled.
	State    string     `json:"state"`
	Emojis   []SASEmoji `json:"emojis"`
	Decimals []int      `json:"decimals"`
	Reason   string     `json:"reason"`
}

// SASEmoji is one of the seven emoji both sides compare.
type SASEmoji struct {
	Emoji       string `json:"emoji"`
	Description string `json:"description"`
}

// Verification states.
const (
	VerifyRequested = "requested"
	VerifyReady     = "ready"
	VerifySAS       = "sas"
	VerifyConfirmed = "confirmed"
	VerifyDone      = "done"
	VerifyCancelled = "cancelled"
)

// updateVerification changes one verification and tells the frontend.
func (s *session) updateVerification(txn id.VerificationTransactionID, change func(v *Verification)) {
	s.mu.Lock()
	v, ok := s.verifications[txn]
	if !ok {
		v = &Verification{ID: txn.String(), Emojis: []SASEmoji{}, Decimals: []int{}}
		s.verifications[txn] = v
	}
	change(v)
	cp := *v
	s.mu.Unlock()
	s.m.emit(EventVerification, cp)
}

// The session is the verification helper's callbacks.

// VerificationRequested runs when another device asks to verify.
func (s *session) VerificationRequested(ctx context.Context, txn id.VerificationTransactionID, from id.UserID, fromDevice id.DeviceID) {
	name := from.Localpart()
	if p, err := s.cli.GetProfile(ctx, from); err == nil && p.DisplayName != "" {
		name = p.DisplayName
	}
	s.updateVerification(txn, func(v *Verification) {
		v.UserID, v.UserName, v.DeviceID = from.String(), name, fromDevice.String()
		v.Self = from == s.cli.UserID
		v.Incoming = true
		v.State = VerifyRequested
	})
}

// VerificationReady runs once both sides agreed to verify. The side that
// asked starts the emoji comparison.
func (s *session) VerificationReady(ctx context.Context, txn id.VerificationTransactionID, otherDevice id.DeviceID, supportsSAS, _ bool, _ *verificationhelper.QRCode) {
	var incoming bool
	s.updateVerification(txn, func(v *Verification) {
		v.DeviceID = otherDevice.String()
		v.State = VerifyReady
		incoming = v.Incoming
	})
	if supportsSAS && !incoming {
		go func() {
			if err := s.verify.StartSAS(context.WithoutCancel(ctx), txn); err != nil {
				s.log.Warn("chat: start emoji verification", "err", err)
			}
		}()
	}
}

// ShowSAS runs when the emoji to compare are known.
func (s *session) ShowSAS(_ context.Context, txn id.VerificationTransactionID, emojis []rune, descriptions []string, decimals []int) {
	list := make([]SASEmoji, len(emojis))
	for i, e := range emojis {
		list[i] = SASEmoji{Emoji: string(e)}
		if i < len(descriptions) {
			list[i].Description = descriptions[i]
		}
	}
	s.updateVerification(txn, func(v *Verification) {
		v.State = VerifySAS
		v.Emojis = list
		v.Decimals = append([]int{}, decimals...)
	})
}

// VerificationDone runs when both sides confirmed. Verifying one of the
// user's own devices also asks it for the cross-signing and backup keys, so
// this device can verify others and read old messages.
func (s *session) VerificationDone(ctx context.Context, txn id.VerificationTransactionID, _ event.VerificationMethod) {
	var self bool
	s.updateVerification(txn, func(v *Verification) {
		v.State = VerifyDone
		self = v.Self
	})
	bg := context.WithoutCancel(ctx)
	go func() {
		if self {
			s.requestSecrets(bg)
		}
		s.refreshCryptoStatus(bg)
	}()
}

// VerificationCancelled runs when either side stopped, or it timed out.
func (s *session) VerificationCancelled(_ context.Context, txn id.VerificationTransactionID, code event.VerificationCancelCode, reason string) {
	s.updateVerification(txn, func(v *Verification) {
		v.State = VerifyCancelled
		v.Reason = reason
		if v.Reason == "" {
			v.Reason = string(code)
		}
	})
}

// Verifications lists the verifications in flight.
func (m *Manager) Verifications() []Verification {
	s, err := m.active()
	if err != nil {
		return []Verification{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Verification, 0, len(s.verifications))
	for _, v := range s.verifications {
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b Verification) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// StartVerification asks another device to verify. An empty or own user id
// asks the user's other devices; anyone else is asked in the direct chat with
// them when there is one.
func (m *Manager) StartVerification(ctx context.Context, userID string) (string, error) {
	s, err := m.active()
	if err != nil {
		return "", err
	}
	to := s.cli.UserID
	if userID != "" {
		if to, err = parseUserID(userID); err != nil {
			return "", err
		}
	}
	if to != s.cli.UserID {
		if _, err := s.mach.FetchKeys(ctx, []id.UserID{to}, true); err != nil {
			return "", fmt.Errorf("chat: read their devices: %w", err)
		}
	} else if _, err := s.mach.FetchKeys(ctx, []id.UserID{to}, true); err != nil {
		return "", fmt.Errorf("chat: read your devices: %w", err)
	}
	var txn id.VerificationTransactionID
	if to != s.cli.UserID {
		if room, _ := m.FindDirect(ctx, to.String()); room != "" {
			txn, err = s.verify.StartInRoomVerification(ctx, id.RoomID(room), to)
		} else {
			txn, err = s.verify.StartVerification(ctx, to)
		}
	} else {
		txn, err = s.verify.StartVerification(ctx, to)
	}
	if err != nil {
		return "", fmt.Errorf("chat: start verification: %w", err)
	}
	name := to.Localpart()
	if to != s.cli.UserID {
		name = s.member(ctx, "", to).Name
		if p, err := s.cli.GetProfile(ctx, to); err == nil && p.DisplayName != "" {
			name = p.DisplayName
		}
	}
	s.updateVerification(txn, func(v *Verification) {
		v.UserID, v.UserName = to.String(), name
		v.Self = to == s.cli.UserID
		v.State = VerifyRequested
	})
	return txn.String(), nil
}

// AcceptVerification accepts a request from another device.
func (m *Manager) AcceptVerification(ctx context.Context, txn string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	return s.verify.AcceptVerification(ctx, id.VerificationTransactionID(txn))
}

// StartSAS starts the emoji comparison of a ready verification.
func (m *Manager) StartSAS(ctx context.Context, txn string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	return s.verify.StartSAS(ctx, id.VerificationTransactionID(txn))
}

// ConfirmSAS says the emoji match.
func (m *Manager) ConfirmSAS(ctx context.Context, txn string) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	if err := s.verify.ConfirmSAS(ctx, id.VerificationTransactionID(txn)); err != nil {
		return err
	}
	s.updateVerification(id.VerificationTransactionID(txn), func(v *Verification) {
		if v.State == VerifySAS {
			v.State = VerifyConfirmed
		}
	})
	return nil
}

// CancelVerification stops a verification, or says the emoji do not match
// when mismatch is set.
func (m *Manager) CancelVerification(ctx context.Context, txn string, mismatch bool) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	code, reason := event.VerificationCancelCodeUser, "The user cancelled the verification"
	if mismatch {
		code, reason = event.VerificationCancelCodeSASMismatch, "The emoji did not match"
	}
	err = s.verify.CancelVerification(ctx, id.VerificationTransactionID(txn), code, reason)
	s.updateVerification(id.VerificationTransactionID(txn), func(v *Verification) {
		v.State = VerifyCancelled
		v.Reason = reason
	})
	return err
}

// DismissVerification forgets a finished or cancelled verification.
func (m *Manager) DismissVerification(txn string) {
	s, err := m.active()
	if err != nil {
		return
	}
	s.mu.Lock()
	delete(s.verifications, id.VerificationTransactionID(txn))
	s.mu.Unlock()
}

// secretTimeout is how long another device has to answer a secret request.
const secretTimeout = 30 * time.Second

// requestSecrets asks the user's verified devices for the cross-signing keys
// and the key backup key. Each answer is checked against the public keys
// before it is kept.
func (s *session) requestSecrets(ctx context.Context) {
	seeds := map[id.Secret][]byte{}
	for _, name := range []id.Secret{id.SecretXSMaster, id.SecretXSSelfSigning, id.SecretXSUserSigning} {
		err := s.mach.GetOrRequestSecret(ctx, name, func(secret string) (bool, error) {
			raw, err := decodeSecret(secret)
			if err != nil || len(raw) != 32 {
				return false, nil
			}
			seeds[name] = raw
			return true, nil
		}, secretTimeout)
		if err != nil {
			s.log.Warn("chat: request cross-signing key", "secret", string(name), "err", err)
		}
	}
	if len(seeds) == 3 {
		err := s.mach.ImportCrossSigningKeys(crypto.CrossSigningSeeds{
			MasterKey: seeds[id.SecretXSMaster], SelfSigningKey: seeds[id.SecretXSSelfSigning], UserSigningKey: seeds[id.SecretXSUserSigning],
		})
		if err != nil {
			s.log.Warn("chat: import cross-signing keys", "err", err)
		}
	}
	err := s.mach.GetOrRequestSecret(ctx, id.SecretMegolmBackupV1, func(secret string) (bool, error) {
		raw, err := decodeSecret(secret)
		if err != nil {
			return false, nil
		}
		key, err := backup.MegolmBackupKeyFromBytes(raw)
		if err != nil {
			return false, nil
		}
		if err := s.useBackupKey(ctx, key); err != nil {
			return false, nil
		}
		return true, nil
	}, secretTimeout)
	if err != nil {
		s.log.Warn("chat: request backup key", "err", err)
		return
	}
	go s.restoreBackup(ctx)
}

func decodeSecret(secret string) ([]byte, error) {
	secret = strings.TrimRight(strings.TrimSpace(secret), "=")
	return base64.RawStdEncoding.DecodeString(secret)
}

func encodeSecret(raw []byte) string {
	return base64.RawStdEncoding.EncodeToString(raw)
}

// storeCrossSigningSecrets keeps the cross-signing private keys in the crypto
// store, pickled with the account's pickle key, so they survive a restart and
// the user's other devices can ask for them once verified.
func (s *session) storeCrossSigningSecrets(ctx context.Context) error {
	if s.mach.CrossSigningKeys == nil {
		return nil
	}
	seeds := s.mach.ExportCrossSigningKeys()
	for name, seed := range map[id.Secret][]byte{
		id.SecretXSMaster: seeds.MasterKey, id.SecretXSSelfSigning: seeds.SelfSigningKey, id.SecretXSUserSigning: seeds.UserSigningKey,
	} {
		if err := s.cryptoStore.PutSecret(ctx, name, encodeSecret(seed)); err != nil {
			return err
		}
	}
	return nil
}

// loadStoredKeys restores the cross-signing and backup keys this device kept
// from an earlier run.
func (s *session) loadBackupKey(ctx context.Context) {
	var seeds crypto.CrossSigningSeeds
	ok := true
	for name, dst := range map[id.Secret]*[]byte{
		id.SecretXSMaster: (*[]byte)(&seeds.MasterKey), id.SecretXSSelfSigning: (*[]byte)(&seeds.SelfSigningKey), id.SecretXSUserSigning: (*[]byte)(&seeds.UserSigningKey),
	} {
		secret, err := s.cryptoStore.GetSecret(ctx, name)
		if err != nil || secret == "" {
			ok = false
			break
		}
		raw, err := decodeSecret(secret)
		if err != nil {
			ok = false
			break
		}
		*dst = raw
	}
	if ok {
		if err := s.mach.ImportCrossSigningKeys(seeds); err != nil {
			s.log.Warn("chat: load cross-signing keys", "err", err)
		}
	}
	secret, err := s.cryptoStore.GetSecret(ctx, id.SecretMegolmBackupV1)
	if err != nil || secret == "" {
		return
	}
	raw, err := decodeSecret(secret)
	if err != nil {
		return
	}
	if key, err := backup.MegolmBackupKeyFromBytes(raw); err == nil {
		s.mu.Lock()
		s.backupKey = key
		s.mu.Unlock()
	}
}

// useBackupKey checks a backup key against the server's current backup and,
// when it fits, keeps it.
func (s *session) useBackupKey(ctx context.Context, key *backup.MegolmBackupKey) error {
	info, err := s.mach.GetAndVerifyLatestKeyBackupVersion(ctx, key)
	if err != nil {
		return fmt.Errorf("chat: the key backup does not match this key: %w", err)
	}
	if info == nil {
		return errors.New("chat: the account has no key backup")
	}
	if err := s.cryptoStore.PutSecret(ctx, id.SecretMegolmBackupV1, encodeSecret(key.Bytes())); err != nil {
		return err
	}
	s.mu.Lock()
	s.backupKey = key
	s.backupVersion = info.Version
	s.mu.Unlock()
	return nil
}

// restoreBackup downloads every key in the server backup, so messages sent
// before this device existed become readable.
func (s *session) restoreBackup(ctx context.Context) {
	s.mu.Lock()
	key := s.backupKey
	s.mu.Unlock()
	if key == nil {
		return
	}
	version, err := s.mach.DownloadAndStoreLatestKeyBackup(ctx, key)
	if err != nil {
		s.log.Warn("chat: restore key backup", "err", err)
		return
	}
	s.mu.Lock()
	s.backupVersion = version
	s.mu.Unlock()
	s.cache = newDecryptCache(4000)
	s.m.emit(EventTimeline, TimelineChange{})
	s.m.emit(EventRooms, RoomsChange{RoomIDs: []string{}})
	s.refreshCryptoStatus(ctx)
}

// restoreSessionFromBackup fetches one room key from the backup. It reports
// whether it found one.
func (s *session) restoreSessionFromBackup(ctx context.Context, roomID id.RoomID, sessionID id.SessionID) bool {
	s.mu.Lock()
	key, version := s.backupKey, s.backupVersion
	s.mu.Unlock()
	if key == nil || version == "" {
		return false
	}
	data, err := s.cli.GetKeyBackupForRoomAndSession(ctx, version, roomID, sessionID)
	if err != nil || data == nil {
		return false
	}
	sess, err := data.SessionData.Decrypt(key)
	if err != nil {
		return false
	}
	if _, err := s.mach.ImportRoomKeyFromBackup(ctx, version, roomID, sessionID, sess); err != nil {
		return false
	}
	s.cache.dropRoom(roomID)
	s.m.emit(EventTimeline, TimelineChange{RoomID: roomID.String()})
	return true
}

// uploadBackup sends room keys the backup does not have yet. It runs after
// each sync and does nothing when there is no backup key or nothing new.
func (s *session) uploadBackup(ctx context.Context) {
	if !s.backupMu.TryLock() {
		return
	}
	defer s.backupMu.Unlock()
	s.mu.Lock()
	key, version := s.backupKey, s.backupVersion
	s.mu.Unlock()
	if key == nil || version == "" {
		return
	}
	sessions, err := s.cryptoStore.GetGroupSessionsWithoutKeyBackupVersion(ctx, version).AsList()
	if err != nil || len(sessions) == 0 {
		return
	}
	req := &mautrix.ReqKeyBackup{Rooms: map[id.RoomID]mautrix.ReqRoomKeyBackup{}}
	for _, sess := range sessions {
		data, err := backupData(key, sess)
		if err != nil {
			continue
		}
		room := req.Rooms[sess.RoomID]
		if room.Sessions == nil {
			room.Sessions = map[id.SessionID]mautrix.ReqKeyBackupData{}
		}
		room.Sessions[sess.ID()] = data
		req.Rooms[sess.RoomID] = room
	}
	if _, err := s.cli.PutKeysInBackup(ctx, version, req); err != nil {
		s.log.Warn("chat: upload room keys to backup", "err", err)
		return
	}
	for _, sess := range sessions {
		if err := s.cryptoStore.SetGroupSessionKeyBackupVersion(ctx, sess.RoomID, sess.ID(), version); err != nil {
			s.log.Warn("chat: mark room key backed up", "err", err)
		}
	}
}

// backupData encrypts one room key for the backup.
func backupData(key *backup.MegolmBackupKey, sess *crypto.InboundGroupSession) (mautrix.ReqKeyBackupData, error) {
	first := sess.Internal.FirstKnownIndex()
	exported, err := sess.Internal.Export(first)
	if err != nil {
		return mautrix.ReqKeyBackupData{}, err
	}
	chain := sess.ForwardingChains
	if chain == nil {
		chain = []string{}
	}
	enc, err := backup.EncryptSessionData(key, backup.MegolmSessionData{
		Algorithm:          id.AlgorithmMegolmV1,
		ForwardingKeyChain: chain,
		SenderClaimedKeys:  backup.SenderClaimedKeys{Ed25519: sess.SigningKey},
		SenderKey:          sess.SenderKey,
		SessionKey:         string(exported),
		SharedHistory:      sess.SharedHistory,
	})
	if err != nil {
		return mautrix.ReqKeyBackupData{}, err
	}
	raw, err := jsonMarshal(enc)
	if err != nil {
		return mautrix.ReqKeyBackupData{}, err
	}
	return mautrix.ReqKeyBackupData{
		FirstMessageIndex: int(first),
		ForwardedCount:    len(sess.ForwardingChains),
		IsVerified:        sess.KeySource == id.KeySourceDirect,
		SessionData:       raw,
	}, nil
}

// refreshCryptoStatus reads cross-signing, secret storage and backup state
// and publishes it in Status, along with the user's profile.
func (s *session) refreshCryptoStatus(ctx context.Context) {
	var enc EncryptionStatus
	if _, err := s.mach.FetchKeys(ctx, []id.UserID{s.cli.UserID}, true); err != nil {
		s.log.Warn("chat: read own keys", "err", err)
	}
	own := s.mach.OwnIdentity()
	enc.Fingerprint = s.mach.Fingerprint()
	if trust, err := s.mach.ResolveTrustContext(ctx, own); err == nil {
		enc.DeviceVerified = trust >= id.TrustStateCrossSignedTOFU
	}
	if pub, err := s.mach.GetOwnCrossSigningPublicKeys(ctx); err == nil && pub != nil {
		enc.CrossSigning = true
	}
	if _, keyData, err := s.mach.SSSS.GetDefaultKeyData(ctx); err == nil && keyData != nil {
		enc.SecretStorage = true
		enc.SecretStoragePassphrase = keyData.Passphrase != nil
	}
	if info, err := s.cli.GetKeyBackupLatestVersion(ctx); err == nil && info != nil {
		enc.BackupVersion = info.Version.String()
		enc.BackupKeys = info.Count
		s.mu.Lock()
		key := s.backupKey
		s.mu.Unlock()
		if key != nil {
			if verified, err := s.mach.GetAndVerifyLatestKeyBackupVersion(ctx, key); err == nil && verified != nil {
				enc.BackupActive = true
				s.mu.Lock()
				s.backupVersion = verified.Version
				s.mu.Unlock()
			}
		}
	}
	enc.Ready = true

	name, avatar := "", ""
	if resp, err := s.cli.GetOwnDisplayName(ctx); err == nil {
		name = resp.DisplayName
	}
	if uri, err := s.cli.GetOwnAvatarURL(ctx); err == nil && !uri.IsEmpty() {
		avatar = uri.String()
	}
	if name != "" || avatar != "" {
		_ = s.st.setProfile(ctx, name, avatar)
	}
	s.m.setStatus(func(st *Status) {
		st.Encryption = enc
		if name != "" {
			st.DisplayName = name
		}
		st.Avatar = avatar
	})
	if enc.BackupActive {
		go s.uploadBackup(ctx)
	}
}

// RefreshEncryption reads the encryption state again.
func (m *Manager) RefreshEncryption(ctx context.Context) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	s.refreshCryptoStatus(ctx)
	return nil
}

// Unlock opens the account's secure storage with the recovery key, or its
// passphrase when passphrase is set. That verifies this device with the
// stored cross-signing keys and turns on the key backup, which is then
// restored in the background.
func (m *Manager) Unlock(ctx context.Context, secret string, passphrase bool) error {
	s, err := m.active()
	if err != nil {
		return err
	}
	keyID, keyData, err := s.mach.SSSS.GetDefaultKeyData(ctx)
	if err != nil {
		return errors.New("chat: this account has no secure backup set up yet")
	}
	var key *ssss.Key
	if passphrase {
		key, err = keyData.VerifyPassphrase(keyID, secret)
	} else {
		key, err = keyData.VerifyRecoveryKey(keyID, strings.TrimSpace(secret))
	}
	if err != nil && !errors.Is(err, ssss.ErrUnverifiableKey) {
		if passphrase {
			return errors.New("chat: that passphrase does not unlock the backup")
		}
		return errors.New("chat: that recovery key does not unlock the backup")
	}
	if err := s.mach.FetchCrossSigningKeysFromSSSS(ctx, key); err != nil {
		return fmt.Errorf("chat: read cross-signing keys: %w", err)
	}
	if err := s.storeCrossSigningSecrets(ctx); err != nil {
		return err
	}
	if err := s.mach.SignOwnDevice(ctx, s.mach.OwnIdentity()); err != nil {
		return fmt.Errorf("chat: verify this device: %w", err)
	}
	if err := s.mach.SignOwnMasterKey(ctx); err != nil {
		return fmt.Errorf("chat: sign master key: %w", err)
	}
	if raw, err := s.mach.SSSS.GetDecryptedAccountData(ctx, event.AccountDataMegolmBackupKey, key); err == nil {
		if bk, err := backup.MegolmBackupKeyFromBytes(raw); err == nil {
			if err := s.useBackupKey(ctx, bk); err == nil {
				go s.restoreBackup(context.WithoutCancel(ctx))
			}
		}
	}
	s.refreshCryptoStatus(ctx)
	return nil
}

// SetupResult is what setting up secure backup hands the user to keep.
type SetupResult struct {
	RecoveryKey string `json:"recoveryKey"`
}

// SetupEncryption creates cross-signing keys, secure storage and a key backup
// for an account that has none, and verifies this device with them. The
// recovery key it returns is shown to the user once. passphrase optionally
// lets the storage be unlocked with a passphrase too; password answers the
// server's request to confirm the user's identity.
func (m *Manager) SetupEncryption(ctx context.Context, passphrase, password string) (SetupResult, error) {
	s, err := m.active()
	if err != nil {
		return SetupResult{}, err
	}
	uia := func(resp *mautrix.RespUserInteractive) any {
		if password == "" {
			return nil
		}
		return &mautrix.ReqUIAuthLogin{
			BaseAuthData: mautrix.BaseAuthData{Type: mautrix.AuthTypePassword, Session: resp.Session},
			User:         s.cli.UserID.String(),
			Password:     password,
		}
	}
	recovery, _, err := s.mach.GenerateAndUploadCrossSigningKeys(ctx, uia, passphrase)
	if err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "M_FORBIDDEN") || strings.Contains(err.Error(), "M_UNAUTHORIZED") {
			return SetupResult{}, errors.New("chat: the server wants your account password to set this up")
		}
		return SetupResult{}, fmt.Errorf("chat: set up cross-signing: %w", err)
	}
	if err := s.storeCrossSigningSecrets(ctx); err != nil {
		return SetupResult{}, err
	}
	if err := s.mach.SignOwnDevice(ctx, s.mach.OwnIdentity()); err != nil {
		return SetupResult{}, fmt.Errorf("chat: verify this device: %w", err)
	}
	if err := s.mach.SignOwnMasterKey(ctx); err != nil {
		return SetupResult{}, fmt.Errorf("chat: sign master key: %w", err)
	}
	keyID, keyData, err := s.mach.SSSS.GetDefaultKeyData(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	ssssKey, err := keyData.VerifyRecoveryKey(keyID, recovery)
	if err != nil && !errors.Is(err, ssss.ErrUnverifiableKey) {
		return SetupResult{}, err
	}
	if err := s.createBackup(ctx, ssssKey); err != nil {
		return SetupResult{}, err
	}
	go s.uploadBackup(context.WithoutCancel(ctx))
	s.refreshCryptoStatus(ctx)
	return SetupResult{RecoveryKey: recovery}, nil
}

// createBackup starts a new server key backup, signed by this device and the
// master key, and stores its key in secure storage.
func (s *session) createBackup(ctx context.Context, ssssKey *ssss.Key) error {
	key, err := backup.NewMegolmBackupKey()
	if err != nil {
		return err
	}
	auth := backup.MegolmAuthData{PublicKey: id.Ed25519(base64.RawStdEncoding.EncodeToString(key.PublicKey().Bytes()))}
	sigs := map[id.UserID]map[id.KeyID]string{s.cli.UserID: {}}
	deviceSig, err := s.mach.GetAccount().SignJSON(auth)
	if err != nil {
		return err
	}
	sigs[s.cli.UserID][id.NewKeyID(id.KeyAlgorithmEd25519, s.cli.DeviceID.String())] = deviceSig
	if s.mach.CrossSigningKeys != nil {
		masterSig, err := s.mach.CrossSigningKeys.MasterKey.SignJSON(auth)
		if err != nil {
			return err
		}
		master := s.mach.CrossSigningKeys.MasterKey.PublicKey()
		sigs[s.cli.UserID][id.NewKeyID(id.KeyAlgorithmEd25519, master.String())] = masterSig
	}
	auth.Signatures = sigs
	resp, err := s.cli.CreateKeyBackupVersion(ctx, &mautrix.ReqRoomKeysVersionCreate[backup.MegolmAuthData]{
		Algorithm: id.KeyBackupAlgorithmMegolmBackupV1,
		AuthData:  auth,
	})
	if err != nil {
		return fmt.Errorf("chat: create key backup: %w", err)
	}
	if err := s.mach.SSSS.SetEncryptedAccountData(ctx, event.AccountDataMegolmBackupKey, key.Bytes(), ssssKey); err != nil {
		return fmt.Errorf("chat: store backup key: %w", err)
	}
	if err := s.cryptoStore.PutSecret(ctx, id.SecretMegolmBackupV1, encodeSecret(key.Bytes())); err != nil {
		return err
	}
	s.mu.Lock()
	s.backupKey = key
	s.backupVersion = resp.Version
	s.mu.Unlock()
	return nil
}
