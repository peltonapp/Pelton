package desktop

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestBareAddress(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Boss <boss@corp.com>", "boss@corp.com"},
		{"boss@corp.com", "boss@corp.com"},
		{"  BOSS@Corp.com  ", "boss@corp.com"},
		{"\"Doe, Jane\" <jane@x.io>", "jane@x.io"},
		{"", ""},
		{"No Address Name", "no address name"},
		{"<boss@corp.com>", "boss@corp.com"},
		{"Boss <boss@corp.com", "boss <boss@corp.com"},
		{"Boss <", "boss <"},
		{"A <a@x.com> B <b@y.com>", "b@y.com"},
	}
	for _, tt := range tests {
		if got := bareAddress(tt.in); got != tt.want {
			t.Errorf("bareAddress(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func newVIPTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	t.Cleanup(stopBackground)
	if err := store.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler)}
}

func TestVIPSendersCRUD(t *testing.T) {
	a := newVIPTestApp(t)

	if list, err := a.ListVIPSenders(); err != nil || len(list) != 0 {
		t.Fatalf("empty list: got %v err %v", list, err)
	}

	// add normalizes name+address down to the bare address.
	if err := a.AddVIPSender("Boss <Boss@Corp.com>"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// adding the same address again (different display name) is a no-op.
	if err := a.AddVIPSender("The Boss <boss@corp.com>"); err != nil {
		t.Fatalf("add dup: %v", err)
	}
	list, err := a.ListVIPSenders()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0] != "boss@corp.com" {
		t.Fatalf("want [boss@corp.com], got %v", list)
	}

	// isVIP matches regardless of display name; vipSet mirrors the list.
	if !a.isVIP("Someone Else <boss@corp.com>") {
		t.Error("isVIP should match on address alone")
	}
	if a.isVIP("other@corp.com") {
		t.Error("isVIP should not match a non-VIP")
	}
	if set := a.vipSet(); !set["boss@corp.com"] {
		t.Errorf("vipSet missing entry: %v", set)
	}

	// empty address is rejected.
	if err := a.AddVIPSender("   "); err == nil {
		t.Error("expected error adding empty VIP address")
	}

	if err := a.RemoveVIPSender("boss@corp.com"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if a.isVIP("boss@corp.com") {
		t.Error("isVIP true after removal")
	}
	if a.vipSet() != nil {
		t.Error("vipSet should be nil when empty")
	}
}

func TestMarkSenderVIPFromMessage(t *testing.T) {
	a := newVIPTestApp(t)
	ctx := a.ctx

	accID, err := a.store.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folderID, err := a.store.CreateFolder(ctx, &storage.Folder{AccountID: accID, Name: "INBOX", IMAPPath: "INBOX"})
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	id, err := a.store.InsertMessageWithAttachments(ctx, &storage.Message{
		AccountID:   accID,
		FolderID:    folderID,
		UID:         1,
		FromAddress: "VIP Person <vip@corp.com>",
	}, nil)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if err := a.MarkSenderVIP(id); err != nil {
		t.Fatalf("mark vip: %v", err)
	}
	if !a.isVIP("vip@corp.com") {
		t.Error("sender not VIP after MarkSenderVIP")
	}
	if err := a.UnmarkSenderVIP(id); err != nil {
		t.Fatalf("unmark vip: %v", err)
	}
	if a.isVIP("vip@corp.com") {
		t.Error("sender still VIP after UnmarkSenderVIP")
	}
}
