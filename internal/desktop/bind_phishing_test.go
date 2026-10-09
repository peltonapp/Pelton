package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestAuthStatus(t *testing.T) {
	tests := []struct {
		name string
		auth storage.MessageAuth
		want string
	}{
		{"nothing reported", storage.MessageAuth{}, authUnavailable},
		{"dmarc pass settles it", storage.MessageAuth{DMARC: "pass", SPF: "fail"}, authPass},
		{"dmarc fail settles it", storage.MessageAuth{DMARC: "fail", SPF: "pass", DKIM: "pass"}, authFail},
		{"both methods pass", storage.MessageAuth{SPF: "pass", DKIM: "pass"}, authPass},
		{"one passes", storage.MessageAuth{SPF: "pass"}, authPartial},
		{"one fails", storage.MessageAuth{SPF: "pass", DKIM: "fail"}, authFail},
		{"neither says anything useful", storage.MessageAuth{SPF: "none", DKIM: "neutral"}, authUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := authStatus(tt.auth); got != tt.want {
				t.Errorf("authStatus(%+v) = %q, want %q", tt.auth, got, tt.want)
			}
		})
	}
}

func TestAddressOnly(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Anna Beck <anna@example.com>", "anna@example.com"},
		{"anna@example.com", "anna@example.com"},
		{`"Beck, Anna" <ANNA@Example.com>`, "anna@example.com"},
		{"a@example.com, b@example.com", "a@example.com"},
		{"", ""},
		{"<anna@example.com>", "anna@example.com"},
		{"Anna <", "anna <"},
		{"Anna <anna@example.com", "anna <anna@example.com"},
		{"A <a@x.com>, B <b@x.com>", "b@x.com"},
		{"<a@x.com> <b@x.com>", "b@x.com"},
	}
	for _, tt := range tests {
		if got := addressOnly(tt.in); got != tt.want {
			t.Errorf("addressOnly(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDisplayNameOf(t *testing.T) {
	tests := []struct{ address, name, want string }{
		{"anna@example.com", "Anna Beck", "Anna Beck"},
		{"Anna Beck <anna@example.com>", "", "Anna Beck"},
		{`"Beck, Anna" <anna@example.com>`, "", "Beck, Anna"},
		{"anna@example.com", "", ""},
		{"<anna@example.com>", "", ""},
		{"Anna Beck <", "", "Anna Beck"},
		{"A <a@x.com> B <b@x.com>", "", "A <a@x.com> B"},
		{"  ", "", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := displayNameOf(tt.address, tt.name); got != tt.want {
			t.Errorf("displayNameOf(%q, %q) = %q, want %q", tt.address, tt.name, got, tt.want)
		}
	}
}

func phishingTestApp(t *testing.T) (*App, *storage.DB, context.Context) {
	t.Helper()
	ctx, stopBackground := testContext(t)
	db, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	t.Cleanup(stopBackground)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler)}, db, ctx
}

// TestCheckPhishingUsesTheAddressBook: the lookalike and display-name checks
// only mean anything against people this user actually writes to.
func TestCheckPhishingUsesTheAddressBook(t *testing.T) {
	a, db, ctx := phishingTestApp(t)
	if err := db.RecordAddress(ctx, "billing@paypal.com", "PayPal Billing"); err != nil {
		t.Fatalf("record address: %v", err)
	}

	got := a.checkPhishing(storage.Message{
		FromAddress: "PayPal Billing <billing@paypa1.com>",
		Auth:        storage.MessageAuth{DMARC: "pass"},
	})
	if got.Level != "warning" {
		t.Errorf("Level = %q with %+v, want warning", got.Level, got.Signals)
	}
}

// TestCheckPhishingIgnoresTheUsersOwnAccounts: mail from another of the user's
// own mailboxes is not somebody impersonating them.
func TestCheckPhishingIgnoresTheUsersOwnAccounts(t *testing.T) {
	a, db, ctx := phishingTestApp(t)
	if _, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := db.RecordAddress(ctx, "me@example.com", "Me"); err != nil {
		t.Fatalf("record address: %v", err)
	}

	got := a.checkPhishing(storage.Message{
		FromAddress: "Me <me@example.org>",
		Auth:        storage.MessageAuth{DMARC: "pass"},
	})
	if got.Level != "none" {
		t.Errorf("Level = %q with %+v, want none", got.Level, got.Signals)
	}
}

// TestCheckPhishingOnOrdinaryMail: an empty address book and a clean message
// must produce nothing at all.
func TestCheckPhishingOnOrdinaryMail(t *testing.T) {
	a, _, _ := phishingTestApp(t)
	got := a.checkPhishing(storage.Message{
		FromAddress: "Anna Beck <anna@example.com>",
		Auth:        storage.MessageAuth{DMARC: "pass", SPF: "pass", SPFDomain: "example.com"},
		BodyHTML:    `<a href="https://example.com/x">read</a>`,
	})
	if got.Level != "none" {
		t.Errorf("Level = %q with %+v, want none", got.Level, got.Signals)
	}
	if len(got.Signals) != 0 {
		t.Errorf("signals = %+v, want none", got.Signals)
	}
}

// TestCheckPhishingReadsStoredAuth end to end: a message inserted with a failed
// dmarc comes back as a warning after a round trip through the database.
func TestCheckPhishingReadsStoredAuth(t *testing.T) {
	a, db, ctx := phishingTestApp(t)
	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	folder := &storage.Folder{AccountID: accountID, Name: "INBOX", IMAPPath: "INBOX"}
	if _, err := db.CreateFolder(ctx, folder); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	msg := &storage.Message{
		AccountID: accountID, FolderID: folder.ID, UID: 1,
		FromAddress: "billing@paypal.com", FromName: "PayPal",
		ReplyTo: "collect@evil.test",
		Auth:    storage.MessageAuth{SPF: "fail", DMARC: "fail", SPFDomain: "evil.test"},
	}
	id, err := db.InsertMessage(ctx, msg)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	stored, err := db.GetMessage(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Auth.DMARC != "fail" || stored.ReplyTo != "collect@evil.test" {
		t.Fatalf("stored auth = %+v, reply-to = %q", stored.Auth, stored.ReplyTo)
	}
	if got := authStatus(stored.Auth); got != authFail {
		t.Errorf("authStatus = %q, want %q", got, authFail)
	}
	report := a.checkPhishing(*stored)
	if report.Level != "warning" {
		t.Errorf("Level = %q with %+v, want warning", report.Level, report.Signals)
	}
}
