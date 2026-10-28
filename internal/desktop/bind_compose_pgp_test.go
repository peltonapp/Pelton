package desktop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"

	"github.com/peltonapp/Pelton/internal/crypto"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/storage"
)

func TestCryptoModeMapping(t *testing.T) {
	tests := []struct {
		protection string
		want       crypto.Mode
	}{
		{protectionNone, crypto.ModeNone},
		{"", crypto.ModeNone},
		{protectionSign, crypto.ModeSign},
		{protectionEncrypt, crypto.ModeEncrypt},
		{protectionSignEncrypt, crypto.ModeSignEncrypt},
		{"nonsense", crypto.ModeNone},
	}
	for _, tt := range tests {
		if got := cryptoMode(tt.protection); got != tt.want {
			t.Errorf("cryptoMode(%q) = %v, want %v", tt.protection, got, tt.want)
		}
	}
}

func TestSuggestedProtection(t *testing.T) {
	tests := []struct {
		name                string
		accountDefault      string
		canSign, canEncrypt bool
		want                string
	}{
		{"off stays off", accountDefaultOff, true, true, protectionNone},
		{"sign with a key", accountDefaultSign, true, false, protectionSign},
		{"sign without a key falls back", accountDefaultSign, false, true, protectionNone},
		{"auto with everything", accountDefaultAuto, true, true, protectionSignEncrypt},
		{"auto with no key of my own", accountDefaultAuto, false, true, protectionEncrypt},
		{"auto with no recipient key", accountDefaultAuto, true, false, protectionSign},
		{"auto with nothing", accountDefaultAuto, false, false, protectionNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := suggestedProtection(tt.accountDefault, tt.canSign, tt.canEncrypt); got != tt.want {
				t.Errorf("suggestedProtection() = %q, want %q", got, tt.want)
			}
		})
	}
}

// pgpTestApp builds an app with a key store holding the given entities, so the
// send path can be exercised without a live account.
func pgpTestApp(t *testing.T, own, others []string) (*App, *storage.DB, int64) {
	t.Helper()
	ctx, stopBackground := testContext(t)
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	t.Cleanup(stopBackground)
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	app := &App{ctx: ctx, store: db, log: slog.New(slog.DiscardHandler), dataDir: dir}

	var pub, priv []*openpgp.Entity
	for _, email := range own {
		ent := testEntity(t, email)
		pub = append(pub, ent)
		priv = append(priv, ent)
	}
	for _, email := range others {
		pub = append(pub, testEntity(t, email))
	}
	if len(pub) > 0 {
		keyDir := filepath.Join(dir, keyDirName)
		if err := crypto.EnsureKeyDir(keyDir); err != nil {
			t.Fatalf("key dir: %v", err)
		}
		writeKeyrings(t, keyDir, pub, priv)
	}

	accountID, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com", DisplayName: "Me"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return app, db, accountID
}

// writeKeyrings serialises keyrings the way an imported gpg keyring lands on
// disk, so the store reads them back as it would a real one.
func writeKeyrings(t *testing.T, dir string, pub, priv []*openpgp.Entity) {
	t.Helper()
	write := func(name, block string, entities []*openpgp.Entity, private bool) {
		var buf bytes.Buffer
		w, err := armor.Encode(&buf, block, nil)
		if err != nil {
			t.Fatalf("armor %s: %v", name, err)
		}
		for _, ent := range entities {
			if private {
				err = ent.SerializePrivateWithoutSigning(w, nil)
			} else {
				err = ent.Serialize(w)
			}
			if err != nil {
				t.Fatalf("serialize into %s: %v", name, err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatalf("close armor %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("pubring.asc", "PGP PUBLIC KEY BLOCK", pub, false)
	write("secring.asc", "PGP PRIVATE KEY BLOCK", priv, true)
}

func testEntity(t *testing.T, email string) *openpgp.Entity {
	t.Helper()
	ent, err := openpgp.NewEntity("Test", "", email, nil)
	if err != nil {
		t.Fatalf("new entity: %v", err)
	}
	return ent
}

func addr(email string) []AddressDTO { return []AddressDTO{{Email: email}} }

// TestProtectionRefusesWhenARecipientHasNoKey is the case the issue calls out:
// five recipients, one without a key, must not quietly go out in the clear.
func TestProtectionRefusesWhenARecipientHasNoKey(t *testing.T) {
	app, db, accountID := pgpTestApp(t, []string{"me@example.com"}, []string{"has@example.com"})
	account, err := db.GetAccount(context.Background(), accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}

	_, _, _, err = app.protectionOptions(*account, ComposeRequest{
		AccountID:  accountID,
		Protection: protectionEncrypt,
		To:         []AddressDTO{{Email: "has@example.com"}, {Email: "missing@example.com"}},
	})
	if err == nil {
		t.Fatal("encrypting to a recipient with no key was allowed")
	}
	if !strings.Contains(err.Error(), "missing@example.com") {
		t.Errorf("error = %v, want it to name the recipient without a key", err)
	}
}

// TestProtectionRefusesBccWithEncryption: an OpenPGP message names every key it
// was encrypted to, so a bcc'd reader would be visible to everyone else.
func TestProtectionRefusesBccWithEncryption(t *testing.T) {
	app, db, accountID := pgpTestApp(t, []string{"me@example.com"}, []string{"a@example.com", "b@example.com"})
	account, _ := db.GetAccount(context.Background(), accountID)

	_, _, _, err := app.protectionOptions(*account, ComposeRequest{
		AccountID:  accountID,
		Protection: protectionEncrypt,
		To:         addr("a@example.com"),
		Bcc:        addr("b@example.com"),
	})
	if err == nil {
		t.Fatal("an encrypted message with a bcc recipient was allowed")
	}
}

// TestProtectionNoneTouchesNothing: an ordinary message must not acquire an
// engine, a mode or a key lookup.
func TestProtectionNoneTouchesNothing(t *testing.T) {
	app, db, accountID := pgpTestApp(t, nil, nil)
	account, _ := db.GetAccount(context.Background(), accountID)

	mode, opts, engine, err := app.protectionOptions(*account, ComposeRequest{
		AccountID: accountID,
		To:        addr("anyone@example.com"),
	})
	if err != nil {
		t.Fatalf("unprotected send: %v", err)
	}
	if mode != crypto.ModeNone || engine != nil || len(opts.Recipients) != 0 {
		t.Errorf("mode = %v, engine = %v, opts = %+v, want an untouched send", mode, engine, opts)
	}
}

// TestProtectionResolvesEveryRecipient covers the happy path: to and cc are all
// collected, so nobody is dropped from the encryption.
func TestProtectionResolvesEveryRecipient(t *testing.T) {
	app, db, accountID := pgpTestApp(t, []string{"me@example.com"}, []string{"a@example.com", "b@example.com"})
	account, _ := db.GetAccount(context.Background(), accountID)

	mode, opts, engine, err := app.protectionOptions(*account, ComposeRequest{
		AccountID:  accountID,
		Protection: protectionSignEncrypt,
		To:         addr("a@example.com"),
		Cc:         addr("b@example.com"),
	})
	if err != nil {
		t.Fatalf("protected send: %v", err)
	}
	if mode != crypto.ModeSignEncrypt || engine == nil {
		t.Fatalf("mode = %v, engine = %v", mode, engine)
	}
	if len(opts.Recipients) != 2 {
		t.Errorf("recipients = %v, want both", opts.Recipients)
	}
	if opts.SenderEmail != "me@example.com" {
		t.Errorf("sender = %q", opts.SenderEmail)
	}
}

// TestProtectionRefusesSigningWithoutAKey: no private key means no signature,
// and the send stops rather than going out unsigned.
func TestProtectionRefusesSigningWithoutAKey(t *testing.T) {
	app, db, accountID := pgpTestApp(t, nil, []string{"a@example.com"})
	account, _ := db.GetAccount(context.Background(), accountID)

	if _, _, _, err := app.protectionOptions(*account, ComposeRequest{
		AccountID:  accountID,
		Protection: protectionSign,
		To:         addr("a@example.com"),
	}); err == nil {
		t.Fatal("signing without a private key was allowed")
	}
}

func TestComposeProtectionStatus(t *testing.T) {
	app, _, accountID := pgpTestApp(t, []string{"me@example.com"}, []string{"has@example.com"})

	status, err := app.ComposeProtectionStatus(accountID, []string{"has@example.com", "missing@example.com"})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.CanSign {
		t.Error("CanSign is false with the account's own key present")
	}
	if status.CanEncrypt {
		t.Error("CanEncrypt is true with a recipient missing a key")
	}
	if len(status.Recipients) != 2 {
		t.Fatalf("recipients = %+v", status.Recipients)
	}
	if !status.Recipients[0].HasKey || status.Recipients[1].HasKey {
		t.Errorf("recipient key flags = %+v", status.Recipients)
	}
}

// TestComposeProtectionStatusWithNoRecipients: nothing to encrypt to yet is not
// the same as a failure, and the controls must not offer encryption.
func TestComposeProtectionStatusWithNoRecipients(t *testing.T) {
	app, _, accountID := pgpTestApp(t, []string{"me@example.com"}, nil)

	status, err := app.ComposeProtectionStatus(accountID, nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.CanEncrypt {
		t.Error("CanEncrypt is true with no recipients")
	}
	if !status.CanSign {
		t.Error("CanSign is false with the account's own key present")
	}
}

// TestSealedDraftIsNotStoredInTheClear: the whole point of the draft sealing is
// that the database does not hold the text of a message chosen to be encrypted.
func TestSealedDraftIsNotStoredInTheClear(t *testing.T) {
	for _, protection := range []string{protectionEncrypt, protectionSignEncrypt} {
		t.Run(protection, func(t *testing.T) {
			app, db, accountID := pgpTestApp(t, []string{"me@example.com"}, []string{"a@example.com"})
			client := &fakeIMAP{}
			useFake(t, app, accountID, client)
			addFolder(t, app, accountID, "Drafts", `\Drafts`)
			req := ComposeRequest{
				AccountID:  accountID,
				Protection: protection,
				To:         addr("a@example.com"),
				Text:       "not for the database",
				InReplyTo:  "parent@example.com",
				References: []string{"root@example.com", "parent@example.com"},
			}
			var id int64
			for _, subject := range []string{"quarterly numbers", "revised quarterly numbers"} {
				req.Subject = subject
				id = saveDraft(t, app, id, req)
				raw, err := db.Get(app.ctx, draftsKey)
				if err != nil {
					t.Fatalf("read drafts setting: %v", err)
				}
				for _, secret := range []string{subject, "not for the database", "a@example.com", "parent@example.com", "root@example.com"} {
					if strings.Contains(raw, secret) {
						t.Errorf("the stored draft still contains %q", secret)
					}
				}
				drafts := listDrafts(t, app)
				if len(drafts) != 1 || drafts[0].ID != id || drafts[0].Locked {
					t.Fatalf("drafts = %+v, want one unlocked draft %d", drafts, id)
				}
				if drafts[0].Request.Subject != subject || drafts[0].Request.Text != "not for the database" {
					t.Errorf("draft round trip lost content: %+v", drafts[0].Request)
				}
				assertDraftReplyRequest(t, drafts[0].Request)
				opened, err := app.UnsealDraft(id, "")
				if err != nil {
					t.Fatalf("UnsealDraft: %v", err)
				}
				if opened.ID != id || opened.Locked || opened.Request.Subject != subject || opened.Request.Text != "not for the database" || opened.Request.Protection != protection {
					t.Fatalf("unsealed draft = %+v, want draft %d with its content and protection", opened, id)
				}
				assertDraftReplyRequest(t, opened.Request)
				req = opened.Request
			}
			if messageID, err := app.UploadDraft(id); !errors.Is(err, ErrProtectedDraftStaysLocal) || messageID != "" {
				t.Fatalf("UploadDraft = %q, %v, want no Message-ID and ErrProtectedDraftStaysLocal", messageID, err)
			}
			settle(t)
			if len(client.drafts) != 0 {
				t.Fatalf("got %d uploads of an encrypted draft, want none", len(client.drafts))
			}
			opened, err := app.UnsealDraft(id, "")
			if err != nil {
				t.Fatalf("reopen after refused upload: %v", err)
			}
			assertDraftReplyRequest(t, opened.Request)
			app.queue = outbox.NewQueue(db)
			queuedID, err := app.SendMessage(opened.Request)
			if err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			queued, err := app.queue.List(app.ctx)
			if err != nil {
				t.Fatalf("list outbox: %v", err)
			}
			if len(queued) != 1 || queued[0].ID != queuedID {
				t.Fatalf("outbox = %+v, want queued message %d", queued, queuedID)
			}
			header := draftReplyHeaders(t, queued[0].Raw)
			if sentID := header.Get("Message-ID"); sentID == "" || sentID == "<parent@example.com>" {
				t.Errorf("sent Message-ID = %q, want a fresh ID distinct from the parent", sentID)
			}
			if !strings.HasPrefix(header.Get("Content-Type"), "multipart/encrypted;") {
				t.Errorf("outbox Content-Type = %q, want encrypted MIME", header.Get("Content-Type"))
			}
		})
	}
}

// TestUnprotectedDraftIsUnchanged: the sealing must not touch ordinary drafts,
// which are the overwhelming majority.
func TestUnprotectedDraftIsUnchanged(t *testing.T) {
	app, _, accountID := pgpTestApp(t, nil, nil)

	id, err := app.SaveDraft(0, ComposeRequest{AccountID: accountID, Subject: "hello", Text: "plain"})
	if err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	drafts, err := app.ListDrafts()
	if err != nil {
		t.Fatalf("ListDrafts: %v", err)
	}
	for _, d := range drafts {
		if d.ID == id {
			if d.Locked {
				t.Error("an unprotected draft came back locked")
			}
			if d.Request.Subject != "hello" {
				t.Errorf("draft = %+v", d.Request)
			}
		}
	}
}

// TestSealedDraftWithoutAKeyIsRefused: saving must fail loudly rather than
// writing the plaintext it was told to protect.
func TestSealedDraftWithoutAKeyIsRefused(t *testing.T) {
	app, _, accountID := pgpTestApp(t, nil, []string{"a@example.com"})

	if _, err := app.SaveDraft(0, ComposeRequest{
		AccountID:  accountID,
		Protection: protectionEncrypt,
		To:         addr("a@example.com"),
		Text:       "secret",
	}); err == nil {
		t.Fatal("a protected draft was saved with no key to seal it to")
	}
}
