package desktop

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/chat"
	"github.com/peltonapp/Pelton/internal/storage"
)

// failingSecrets is a SecretStore that fails the test when it is touched, so
// a test proves chat stayed away from the credential store.
type failingSecrets struct{ t *testing.T }

func (f failingSecrets) Load(string) (chat.Secret, error) {
	f.t.Error("the chat secret was read")
	return chat.Secret{}, chat.ErrNoSecret
}

func (f failingSecrets) Store(string, chat.Secret) error {
	f.t.Error("a chat secret was stored")
	return nil
}

func (f failingSecrets) Delete(string) error {
	f.t.Error("a chat secret was deleted")
	return nil
}

func newChatTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a := &App{ctx: ctx, store: store, dataDir: dir, log: slog.New(slog.DiscardHandler)}
	a.chat.secrets = failingSecrets{t}
	t.Cleanup(a.stopChat)
	return a
}

func TestChatStaysOffUntilEnabled(t *testing.T) {
	a := newChatTestApp(t)

	if st := a.ChatStatus(); st.State != chat.StateSignedOut {
		t.Fatalf("status while off = %+v", st)
	}
	if _, err := a.ChatRooms(); err == nil {
		t.Fatal("rooms were listed while chat is off")
	}
	if _, err := a.ChatDiscover("matrix.org"); err == nil {
		t.Fatal("discovery ran while chat is off")
	}
	a.startChatIfEnabled()
	if _, err := os.Stat(filepath.Join(a.dataDir, "chat")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("chat files were created while chat is off")
	}
	if prefs, _ := a.GetUIPrefs(); prefs.ChatEnabled || !prefs.ChatSendReceipts || !prefs.ChatSendTyping || prefs.ChatNotify != "mentions" {
		t.Fatalf("chat prefs default = %+v", prefs)
	}
}

func TestChatEnabledWithoutAccountTouchesNothing(t *testing.T) {
	a := newChatTestApp(t)
	if err := a.store.Set(a.ctx, settingChatEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	a.startChatIfEnabled()
	if st := a.ChatStatus(); st.State != chat.StateSignedOut {
		t.Fatalf("status = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(a.dataDir, "chat")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("chat files were created with no account")
	}
	if _, err := a.ChatRooms(); !errors.Is(err, chat.ErrNotSignedIn) {
		t.Fatalf("rooms with no account: %v", err)
	}
}

func TestChatManagerIsPerProfile(t *testing.T) {
	a := newChatTestApp(t)
	if err := a.store.Set(a.ctx, settingChatEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	first, err := a.chatManager()
	if err != nil {
		t.Fatal(err)
	}
	again, _ := a.chatManager()
	if first != again {
		t.Fatal("the manager was rebuilt for the same profile")
	}
	a.stopChat()
	next, _ := a.chatManager()
	if next == first {
		t.Fatal("a stopped manager was reused")
	}
}

func TestChatNotificationText(t *testing.T) {
	s := chatNotifyStringsFor("xx")
	dm := chatNotification(chat.Notification{RoomID: "!a", Sender: "Bob", Body: "hi", Direct: true}, s)
	if dm.title != "Bob" || dm.body != "hi" || dm.chatRoom != "!a" {
		t.Errorf("direct = %+v", dm)
	}
	room := chatNotification(chat.Notification{RoomID: "!b", Sender: "Bob", RoomName: "Garden", Mention: true}, s)
	if room.title != "Bob in Garden" || room.body != "Encrypted message" {
		t.Errorf("room = %+v", room)
	}
}
