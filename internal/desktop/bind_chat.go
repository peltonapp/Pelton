package desktop

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	"github.com/peltonapp/Pelton/internal/chat"
	"github.com/peltonapp/Pelton/internal/credentials"
)

// The chat space: a Matrix client, opt in. Nothing here starts, opens a file
// or contacts a server until the user turns chat on (settingChatEnabled) and
// signs in. Each profile has its own chat account, kept in its own database
// under the data directory, with its secrets in the OS credential store.

const (
	settingChatEnabled      = "chat_enabled"
	settingChatSendReceipts = "chat_send_receipts"
	settingChatSendTyping   = "chat_send_typing"
	settingChatNotify       = "chat_notify"
	settingChatMarkdown     = "chat_markdown"
)

// Chat event names emitted from here. The rest are the chat package's own
// Event constants, which it emits through Options.Emit.
const (
	EventChatMediaProgress = chat.EventMediaProgress
	// EventChatOpen asks the frontend to show a room, after a click on a
	// chat notification.
	EventChatOpen = "chat:open"
)

// ChatOpenEvent is the payload of EventChatOpen.
type ChatOpenEvent struct {
	RoomID string `json:"roomId"`
}

// ChatMediaProgressEvent is the payload of EventChatMediaProgress: a file
// being saved from a message, or sent.
type ChatMediaProgressEvent struct {
	// Key is the event id of a file being saved, or the upload id the
	// frontend chose for a file being sent.
	Key       string `json:"key"`
	Running   bool   `json:"running"`
	BytesDone int64  `json:"bytesDone"`
	// BytesTotal is 0 when the size is not known.
	BytesTotal int64  `json:"bytesTotal"`
	Error      string `json:"error"`
}

// chatState is the App's chat side: the manager of the active profile's
// account, and the room on screen.
type chatState struct {
	mu      sync.Mutex
	manager *chat.Manager
	// viewing is the room the user has open, so its messages do not notify
	// while the window has focus.
	viewing string
	// secrets replaces the OS credential store in tests.
	secrets chat.SecretStore
}

// chatKeyring keeps chat secrets in the OS credential store, through the same
// wrapper as mail passwords.
type chatKeyring struct{}

func (chatKeyring) Load(key string) (chat.Secret, error) {
	s, err := credentials.LoadChatSecret(key)
	if errors.Is(err, credentials.ErrNotFound) {
		return chat.Secret{}, chat.ErrNoSecret
	}
	return chat.Secret{AccessToken: s.AccessToken, PickleKey: s.PickleKey}, err
}

func (chatKeyring) Store(key string, s chat.Secret) error {
	return credentials.StoreChatSecret(key, credentials.ChatSecret{AccessToken: s.AccessToken, PickleKey: s.PickleKey})
}

func (chatKeyring) Delete(key string) error {
	return credentials.DeleteChatSecret(key)
}

// chatEnabled reports whether the user turned chat on.
func (a *App) chatEnabled() bool {
	return a.boolSetting(settingChatEnabled, false)
}

// chatManager returns the chat manager of the active profile, creating it on
// first use. It errors while chat is turned off, so no binding reaches a
// server then.
func (a *App) chatManager() (*chat.Manager, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if !a.chatEnabled() {
		return nil, errors.New("pelton: chat is turned off")
	}
	a.chat.mu.Lock()
	defer a.chat.mu.Unlock()
	if a.chat.manager == nil {
		a.chat.manager = a.newChatManager()
	}
	return a.chat.manager, nil
}

// newChatManager builds the manager for the active profile.
func (a *App) newChatManager() *chat.Manager {
	profile := a.store.ScopedProfileID()
	secrets := a.chat.secrets
	if secrets == nil {
		secrets = chatKeyring{}
	}
	return chat.New(chat.Options{
		Dir:        filepath.Join(a.dataDir, "chat", fmt.Sprintf("profile-%d", profile)),
		SecretKey:  fmt.Sprintf("profile-%d", profile),
		Secrets:    secrets,
		HTTPClient: func() *http.Client { return a.currentProxy().HTTPClient(0) },
		Emit:       a.emit,
		Notify:     a.notifyChat,
		Prefs: func() chat.Prefs {
			return chat.Prefs{
				SendReceipts: a.boolSetting(settingChatSendReceipts, true),
				SendTyping:   a.boolSetting(settingChatSendTyping, true),
			}
		},
		DeviceName: chatDeviceName(),
		OpenURL:    a.openURL,
		Log:        a.log,
	})
}

// chatDeviceName is how this sign-in is listed among the user's sessions.
func chatDeviceName() string {
	switch goruntime.GOOS {
	case "darwin":
		return "Pelton on macOS"
	case "windows":
		return "Pelton on Windows"
	case "linux":
		return "Pelton on Linux"
	}
	return "Pelton"
}

// startChatIfEnabled resumes the profile's chat account when chat is on and
// an account is signed in. It runs off the ui thread.
func (a *App) startChatIfEnabled() {
	if a.store == nil || a.demoMode || !a.chatEnabled() {
		return
	}
	m, err := a.chatManager()
	if err != nil {
		return
	}
	if err := m.Start(a.ctx); err != nil {
		a.log.Warn("chat: start", "err", err)
	}
}

// stopChat stops the chat account's sync and closes its files, and forgets
// the manager so the next profile gets its own.
func (a *App) stopChat() {
	a.chat.mu.Lock()
	m := a.chat.manager
	a.chat.manager = nil
	a.chat.viewing = ""
	a.chat.mu.Unlock()
	if m != nil {
		m.Stop()
	}
}

// chatSettingChanged reacts to the chat switch in settings.
func (a *App) chatSettingChanged(value string) {
	a.scriptChatAvailability()
	if value == "true" {
		goSafe("starting chat", a.startChatIfEnabled)
		return
	}
	goSafe("stopping chat", a.stopChat)
}

// ChatStatus returns the chat account's state. While chat is off it reports
// signed out without opening anything.
func (a *App) ChatStatus() chat.Status {
	m, err := a.chatManager()
	if err != nil {
		return chat.Status{State: chat.StateSignedOut}
	}
	return m.Status()
}

// ChatDiscover finds the homeserver for a server name, Matrix ID or URL and
// the ways it offers to sign in.
func (a *App) ChatDiscover(input string) (chat.Server, error) {
	m, err := a.chatManager()
	if err != nil {
		return chat.Server{}, err
	}
	return m.Discover(a.ctx, input)
}

// ChatLoginPassword signs in with a user name and password.
func (a *App) ChatLoginPassword(homeserver, user, password string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.LoginPassword(a.ctx, homeserver, user, password)
}

// ChatLoginSSO signs in through the homeserver's single sign-on page in the
// system browser, returning once that finished, failed or was cancelled.
func (a *App) ChatLoginSSO(homeserver, providerID string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.LoginSSO(a.ctx, homeserver, providerID)
}

// ChatCancelSSO abandons a browser sign-in that is waiting.
func (a *App) ChatCancelSSO() {
	if m, err := a.chatManager(); err == nil {
		m.CancelSSO()
	}
}

// ChatLoginToken signs in with a login token.
func (a *App) ChatLoginToken(homeserver, token string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.LoginToken(a.ctx, homeserver, token)
}

// ChatSignOut signs this device out and deletes its chat data and keys.
func (a *App) ChatSignOut() error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.SignOut(a.ctx)
}

// ChatRooms lists the rooms, direct chats, spaces and invites.
func (a *App) ChatRooms() ([]chat.Room, error) {
	m, err := a.chatManager()
	if err != nil {
		return nil, err
	}
	return m.Rooms(a.ctx)
}

// ChatTimeline returns up to limit messages of a room older than before, or
// the newest when before is empty.
func (a *App) ChatTimeline(roomID, before string, limit int) (chat.TimelinePage, error) {
	m, err := a.chatManager()
	if err != nil {
		return chat.TimelinePage{}, err
	}
	return m.Timeline(a.ctx, roomID, before, limit)
}

// ChatSend sends a message, reply or edit and returns its event id.
func (a *App) ChatSend(req chat.SendRequest) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	return m.Send(a.ctx, req)
}

// ChatReact reacts to a message.
func (a *App) ChatReact(roomID, eventID, key string) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	return m.React(a.ctx, roomID, eventID, key)
}

// ChatRedact removes a message or a reaction.
func (a *App) ChatRedact(roomID, eventID string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.Redact(a.ctx, roomID, eventID, "")
}

// ChatTyping tells a room the user is typing, when typing notices are on.
func (a *App) ChatTyping(roomID string, typing bool) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.Typing(a.ctx, roomID, typing)
}

// ChatMarkRead moves the read marker of a room.
func (a *App) ChatMarkRead(roomID, eventID string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.MarkRead(a.ctx, roomID, eventID)
}

// ChatTypists returns who is typing in a room.
func (a *App) ChatTypists(roomID string) []chat.Member {
	m, err := a.chatManager()
	if err != nil {
		return []chat.Member{}
	}
	return m.Typists(a.ctx, roomID)
}

// ChatMembers lists a room's members.
func (a *App) ChatMembers(roomID string) ([]chat.Member, error) {
	m, err := a.chatManager()
	if err != nil {
		return nil, err
	}
	return m.Members(a.ctx, roomID)
}

// ChatCreateRoom creates a room and returns its id.
func (a *App) ChatCreateRoom(req chat.CreateRoomRequest) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	return m.CreateRoom(a.ctx, req)
}

// ChatOpenDirect returns the direct chat with a person, starting one when
// there is none.
func (a *App) ChatOpenDirect(userID string) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	return m.OpenDirect(a.ctx, userID)
}

// ChatJoin joins a room by address or link, or accepts an invite.
func (a *App) ChatJoin(target string) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	return m.Join(a.ctx, target)
}

// ChatLeave leaves a room or declines an invite.
func (a *App) ChatLeave(roomID string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.Leave(a.ctx, roomID)
}

// ChatInvite invites a person to a room.
func (a *App) ChatInvite(roomID, userID string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.Invite(a.ctx, roomID, userID)
}

// ChatSetFavourite adds a room to or removes it from the favourites.
func (a *App) ChatSetFavourite(roomID string, favourite bool) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.SetFavourite(a.ctx, roomID, favourite)
}

// ChatLookupProfile reads a person's public profile.
func (a *App) ChatLookupProfile(userID string) (chat.Profile, error) {
	m, err := a.chatManager()
	if err != nil {
		return chat.Profile{}, err
	}
	return m.LookupProfile(a.ctx, userID)
}

// ChatMedia returns a message's file or its thumbnail.
func (a *App) ChatMedia(roomID, eventID string, thumbnail bool) (chat.MediaContent, error) {
	m, err := a.chatManager()
	if err != nil {
		return chat.MediaContent{}, err
	}
	return m.Media(a.ctx, roomID, eventID, thumbnail)
}

// ChatAvatar returns a small mxc picture as a data URL.
func (a *App) ChatAvatar(mxc string) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	return m.Avatar(a.ctx, mxc)
}

// ChatInlineImage returns an mxc image from message HTML as a data URL.
func (a *App) ChatInlineImage(mxc string) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	return m.InlineImage(a.ctx, mxc)
}

// ChatSaveMedia asks where to save a message's file and writes it there,
// reporting progress through EventChatMediaProgress. It returns the path, or
// empty when the user cancelled.
func (a *App) ChatSaveMedia(roomID, eventID string) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	name, err := m.MediaFileName(a.ctx, roomID, eventID)
	if err != nil {
		return "", err
	}
	path, err := a.saveFileDialog(saveDialogOptions{DefaultFilename: name})
	if err != nil || path == "" {
		return "", err
	}
	progress := func(done, total int64) {
		a.emit(EventChatMediaProgress, ChatMediaProgressEvent{Key: eventID, Running: true, BytesDone: done, BytesTotal: total})
	}
	err = m.SaveMedia(a.ctx, roomID, eventID, path, progress)
	end := ChatMediaProgressEvent{Key: eventID}
	if err != nil {
		end.Error = err.Error()
	}
	a.emit(EventChatMediaProgress, end)
	if err != nil {
		return "", err
	}
	return path, nil
}

// ChatUploadLimit returns the largest file the homeserver accepts, or 0.
func (a *App) ChatUploadLimit() (int64, error) {
	m, err := a.chatManager()
	if err != nil {
		return 0, err
	}
	return m.UploadLimit(a.ctx)
}

// ChatSendFileData sends a file the frontend holds, such as a pasted image.
// uploadID names it in the progress events.
func (a *App) ChatSendFileData(req chat.FileRequest, base64Data, uploadID string) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("pelton: read file: %w", err)
	}
	return a.sendChatFile(m, req, data, uploadID)
}

// ChatSendFilePaths sends files from disk, such as ones dropped on the room.
// uploadIDs name them in the progress events, one per path.
func (a *App) ChatSendFilePaths(roomID string, paths []string, uploadIDs []string, replyTo string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	var errs []error
	for i, p := range paths {
		uploadID := ""
		if i < len(uploadIDs) {
			uploadID = uploadIDs[i]
		}
		if err := a.sendChatPath(m, roomID, p, uploadID, replyTo); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(p), err))
		}
	}
	return errors.Join(errs...)
}

// ChatPickFiles asks for files and returns their paths, for the composer to
// show before sending.
func (a *App) ChatPickFiles() ([]string, error) {
	return a.openMultipleFilesDialog(openDialogOptions{})
}

func (a *App) sendChatPath(m *chat.Manager, roomID, path, uploadID, replyTo string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("pelton: folders cannot be sent")
	}
	if limit, _ := m.UploadLimit(a.ctx); limit > 0 && info.Size() > limit {
		return fmt.Errorf("%w (%d MB)", chat.ErrTooLarge, limit>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	req := chat.FileRequest{RoomID: roomID, Name: filepath.Base(path), Mime: mime.TypeByExtension(strings.ToLower(filepath.Ext(path))), ReplyTo: replyTo}
	_, err = a.sendChatFile(m, req, data, uploadID)
	return err
}

func (a *App) sendChatFile(m *chat.Manager, req chat.FileRequest, data []byte, uploadID string) (string, error) {
	progress := func(done, total int64) {
		if uploadID != "" {
			a.emit(EventChatMediaProgress, ChatMediaProgressEvent{Key: uploadID, Running: true, BytesDone: done, BytesTotal: total})
		}
	}
	eventID, err := m.SendFile(a.ctx, req, data, progress)
	if uploadID != "" {
		end := ChatMediaProgressEvent{Key: uploadID}
		if err != nil {
			end.Error = err.Error()
		}
		a.emit(EventChatMediaProgress, end)
	}
	return eventID, err
}

// ChatRetryDecryption asks again for the keys of undecryptable messages.
func (a *App) ChatRetryDecryption(roomID string, eventIDs []string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.RetryDecryption(a.ctx, roomID, eventIDs)
}

// ChatVerifications lists the device verifications in flight.
func (a *App) ChatVerifications() []chat.Verification {
	m, err := a.chatManager()
	if err != nil {
		return []chat.Verification{}
	}
	return m.Verifications()
}

// ChatStartVerification asks another device to verify: one of the user's own
// when userID is empty.
func (a *App) ChatStartVerification(userID string) (string, error) {
	m, err := a.chatManager()
	if err != nil {
		return "", err
	}
	return m.StartVerification(a.ctx, userID)
}

// ChatAcceptVerification accepts a verification request.
func (a *App) ChatAcceptVerification(txn string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.AcceptVerification(a.ctx, txn)
}

// ChatStartSAS starts comparing emoji in a ready verification.
func (a *App) ChatStartSAS(txn string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.StartSAS(a.ctx, txn)
}

// ChatConfirmSAS says the emoji match.
func (a *App) ChatConfirmSAS(txn string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.ConfirmSAS(a.ctx, txn)
}

// ChatCancelVerification stops a verification; mismatch says the emoji did
// not match.
func (a *App) ChatCancelVerification(txn string, mismatch bool) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.CancelVerification(a.ctx, txn, mismatch)
}

// ChatDismissVerification forgets a finished verification.
func (a *App) ChatDismissVerification(txn string) {
	if m, err := a.chatManager(); err == nil {
		m.DismissVerification(txn)
	}
}

// ChatRefreshEncryption reads the encryption state again.
func (a *App) ChatRefreshEncryption() error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.RefreshEncryption(a.ctx)
}

// ChatUnlock opens secure storage with the recovery key or passphrase,
// verifying this device and restoring the key backup.
func (a *App) ChatUnlock(secret string, passphrase bool) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.Unlock(a.ctx, secret, passphrase)
}

// ChatSetupEncryption sets up cross-signing, secure storage and key backup
// for an account that has none, and returns the recovery key to keep.
func (a *App) ChatSetupEncryption(passphrase, password string) (chat.SetupResult, error) {
	m, err := a.chatManager()
	if err != nil {
		return chat.SetupResult{}, err
	}
	return m.SetupEncryption(a.ctx, passphrase, password)
}

// ChatDevices lists the user's sessions.
func (a *App) ChatDevices() ([]chat.Device, error) {
	m, err := a.chatManager()
	if err != nil {
		return nil, err
	}
	return m.Devices(a.ctx)
}

// ChatSignOutDevice ends another session of the user's.
func (a *App) ChatSignOutDevice(deviceID, password string) error {
	m, err := a.chatManager()
	if err != nil {
		return err
	}
	return m.SignOutDevice(a.ctx, deviceID, password)
}

// ChatUserTrust reports whether the user verified a person.
func (a *App) ChatUserTrust(userID string) (bool, error) {
	m, err := a.chatManager()
	if err != nil {
		return false, err
	}
	return m.UserTrust(a.ctx, userID)
}

// ChatSetViewing tells the backend which room is on screen, or none.
func (a *App) ChatSetViewing(roomID string) {
	a.chat.mu.Lock()
	a.chat.viewing = roomID
	a.chat.mu.Unlock()
}

// chatNotifyLimit is the most chat notifications one sync raises one by one.
const chatNotifyLimit = 3

// notifyChat raises notifications for new chat messages, as the user chose:
// all messages, only direct messages and mentions (the default), or none.
// Nothing notifies during focus mode, and the room on screen stays quiet
// while the window has focus.
func (a *App) notifyChat(list []chat.Notification) {
	if a.store == nil || a.ctx == nil {
		return
	}
	a.scriptChatMessages(list)
	mode := a.stringSetting(settingChatNotify, "mentions")
	if mode == "off" || a.focusQuiet() {
		return
	}
	a.chat.mu.Lock()
	viewing := a.chat.viewing
	a.chat.mu.Unlock()
	focused := a.win != nil && a.runtimeReady.Load() && a.win.IsFocused()
	s := chatNotifyStringsFor(a.stringSetting(settingLanguage, "en"))
	var pending []notification
	for _, n := range list {
		if mode != "all" && !n.Direct && !n.Mention {
			continue
		}
		if focused && n.RoomID == viewing {
			continue
		}
		pending = append(pending, chatNotification(n, s))
	}
	if len(pending) > chatNotifyLimit {
		pending = []notification{{title: s.title, body: fmt.Sprintf(s.summary, len(pending))}}
	}
	for _, n := range pending {
		a.sendNotification(n)
	}
}

// chatNotification builds the notification for one message.
func chatNotification(n chat.Notification, s chatNotifyStrings) notification {
	title := n.Sender
	if !n.Direct && n.RoomName != "" {
		title = fmt.Sprintf(s.in, n.Sender, n.RoomName)
	}
	body := n.Body
	if body == "" {
		body = s.encrypted
	}
	return notification{title: title, body: body, chatRoom: n.RoomID}
}

// chatNotifyStrings is the localizable text of chat notifications. Languages
// without their own fall back to English.
type chatNotifyStrings struct {
	title     string // title of the summary for a burst
	summary   string // its body, with %d for the count
	in        string // "%s in %s": sender in room
	encrypted string // body of a message not decrypted yet
}

var chatNotifyLocales = map[string]chatNotifyStrings{
	"en": {title: "Chat", summary: "%d new messages", in: "%s in %s", encrypted: "Encrypted message"},
}

func chatNotifyStringsFor(lang string) chatNotifyStrings {
	if s, ok := chatNotifyLocales[lang]; ok {
		return s
	}
	return chatNotifyLocales["en"]
}

// openChatFromNotification shows the window and the room a clicked chat
// notification was about.
func (a *App) openChatFromNotification(roomID string) {
	a.showWindow()
	a.emit(EventChatOpen, ChatOpenEvent{RoomID: roomID})
}
