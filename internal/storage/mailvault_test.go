package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/mailvault"
)

var vaultTestKDF = mailvault.KDFParams{Time: 1, MemoryKiB: 64, Threads: 1}

// testGate is an unlocked or locked vault for the tests: Visible follows
// open and admits, MessageKey derives keys from the unlocked Keys.
type testGate struct {
	mu     sync.Mutex
	keys   *mailvault.Keys
	open   bool
	admits map[VaultAudience]bool
}

func (g *testGate) Visible(_ int64, a VaultAudience) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.open && g.admits[a]
}

func (g *testGate) MessageKey(_ int64, epk []byte) (*mailvault.MessageKey, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.open {
		return nil, ErrVaultLocked
	}
	return g.keys.MessageKey(epk)
}

func (g *testGate) set(open bool) {
	g.mu.Lock()
	g.open = open
	g.mu.Unlock()
}

type vaultFixture struct {
	db      *DB
	dir     string
	account int64
	inbox   int64
	secret  int64
	vault   int64
	gate    *testGate
}

func newVaultFixture(t *testing.T) *vaultFixture {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "pelton.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.RunMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	acct, err := db.CreateAccount(ctx, &Account{Email: "me@example.com", IMAPHost: "imap.example.com", IMAPPort: 993})
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := db.CreateFolder(ctx, &Folder{AccountID: acct, Name: "INBOX", IMAPPath: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := db.CreateFolder(ctx, &Folder{AccountID: acct, Name: "Secret", IMAPPath: "Secret"})
	if err != nil {
		t.Fatal(err)
	}
	return &vaultFixture{db: db, dir: dir, account: acct, inbox: inbox, secret: secret,
		gate: &testGate{admits: map[VaultAudience]bool{VaultAudienceUser: true}}}
}

// makeVault puts the Secret folder in a new vault and installs the gate,
// locked.
func (f *vaultFixture) makeVault(t *testing.T) {
	t.Helper()
	m, _, keys, err := mailvault.Create([]byte("pass"), vaultTestKDF)
	if err != nil {
		t.Fatal(err)
	}
	f.gate.keys = keys
	id, err := f.db.CreateMailVault(context.Background(), &MailVault{Name: "v", Material: m},
		[]MailVaultMember{{AccountID: f.account, FolderID: f.secret}})
	if err != nil {
		t.Fatal(err)
	}
	f.vault = id
	f.db.SetVaultGate(f.gate)
}

const (
	markSubject = "MARKERSUBJECTzq81"
	markBody    = "MARKERBODYxk22 with words"
	markFrom    = "markerfrom77@example.org"
	markFile    = "MARKERFILE55.pdf"
	markContent = "MARKERCONTENTpp09"
	markSource  = "MARKERSOURCEgg41"
	markMsgID   = "markermsgid31@example.org"
)

var markers = []string{markSubject, "MARKERBODYxk22", "markerfrom77", "MARKERFILE55", markContent, markSource, "markermsgid31"}

func (f *vaultFixture) insert(t *testing.T, folder int64, uid uint32, suffix string) int64 {
	t.Helper()
	m := &Message{
		AccountID: f.account, FolderID: folder, UID: uid,
		MessageID: "<" + suffix + markMsgID + ">", Subject: markSubject + suffix,
		FromAddress: markFrom, FromName: "Marker Name", ToAddresses: "me@example.com",
		Date: time.Date(2026, 11, 1, 10, 0, int(uid), 0, time.UTC), BodyPlain: markBody + suffix,
		BodyHTML: "<p>" + markBody + "</p>", SizeBytes: 1234, PGPSource: []byte(markSource),
	}
	id, err := f.db.InsertMessageWithAttachments(context.Background(), m, []IncomingAttachment{
		{Filename: markFile, ContentType: "application/pdf", Content: strings.NewReader(markContent + suffix)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func userCtx() context.Context {
	return WithVaultAudience(context.Background(), VaultAudienceUser)
}

func TestSealedOnInsertWhileLocked(t *testing.T) {
	f := newVaultFixture(t)
	f.makeVault(t)
	id := f.insert(t, f.secret, 1, "a")
	plain := f.insert(t, f.inbox, 1, "b")

	var subject, from, body, mid string
	var vaultID int64
	if err := f.db.sql.QueryRow(`SELECT subject, from_address, body_plain, message_id, vault_id FROM messages WHERE id = ?`, id).
		Scan(&subject, &from, &body, &mid, &vaultID); err != nil {
		t.Fatal(err)
	}
	if subject != "" || from != "" || body != "" || strings.Contains(mid, "markermsgid") || vaultID != f.vault {
		t.Fatalf("sealed row kept content: %q %q %q %q %d", subject, from, body, mid, vaultID)
	}

	ctx := context.Background()
	if _, err := f.db.GetMessage(ctx, id); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("locked vault message readable without an audience: %v", err)
	}
	if _, err := f.db.GetMessage(userCtx(), id); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("locked vault message readable by the user: %v", err)
	}
	if atts, err := f.db.ListAttachments(userCtx(), id); err != nil || len(atts) != 0 {
		t.Fatalf("locked vault attachments listed: %v %v", atts, err)
	}
	if got, err := f.db.QueryMessages(userCtx(), MessageQuery{FolderIDs: []int64{f.inbox, f.secret}}); err != nil || len(got) != 1 || got[0].ID != plain {
		t.Fatalf("unified listing while locked: %+v %v", got, err)
	}
	if n, err := f.db.UnreadCount(userCtx(), []int64{f.inbox, f.secret}); err != nil || n != 1 {
		t.Fatalf("unread count while locked = %d %v", n, err)
	}
	if total, _, err := f.db.FolderCounts(userCtx(), f.secret); err != nil || total != 0 {
		t.Fatalf("locked folder count = %d %v", total, err)
	}

	f.gate.set(true)
	if _, err := f.db.GetMessage(ctx, id); !errors.Is(err, ErrMessageNotFound) {
		t.Fatal("an unlocked vault must still be invisible to a read that did not ask")
	}
	got, err := f.db.GetMessage(userCtx(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != markSubject+"a" || got.FromAddress != markFrom || got.BodyPlain != markBody+"a" || got.MessageID != "<a"+markMsgID+">" {
		t.Fatalf("opened message = %+v", got)
	}
	atts, err := f.db.ListAttachments(userCtx(), id)
	if err != nil || len(atts) != 1 || atts[0].Filename != markFile || atts[0].ContentType != "application/pdf" {
		t.Fatalf("opened attachments = %+v %v", atts, err)
	}
	rc, err := f.db.OpenAttachment(userCtx(), atts[0])
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(rc)
	rc.Close()
	if string(content) != markContent+"a" {
		t.Fatalf("attachment bytes = %q", content)
	}
	if _, err := f.db.OpenAttachment(ctx, atts[0]); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("sealed attachment opened without an audience: %v", err)
	}
	src, err := f.db.MessagePGPSource(userCtx(), id)
	if err != nil || string(src) != markSource {
		t.Fatalf("pgp source = %q %v", src, err)
	}
	scripts := WithVaultAudience(ctx, VaultAudienceScripts)
	if _, err := f.db.GetMessage(scripts, id); !errors.Is(err, ErrMessageNotFound) {
		t.Fatal("scripts see vault mail without the per-vault switch")
	}
	f.gate.mu.Lock()
	f.gate.admits[VaultAudienceScripts] = true
	f.gate.mu.Unlock()
	if _, err := f.db.GetMessage(scripts, id); err != nil {
		t.Fatalf("scripts with the switch on: %v", err)
	}

	f.gate.set(false)
	if _, err := f.db.GetMessage(userCtx(), id); !errors.Is(err, ErrMessageNotFound) {
		t.Fatal("message still readable after locking again")
	}
}

func TestSealedRowsTamperedAreRefused(t *testing.T) {
	f := newVaultFixture(t)
	f.makeVault(t)
	a := f.insert(t, f.secret, 1, "a")
	b := f.insert(t, f.secret, 2, "b")
	f.gate.set(true)

	if _, err := f.db.sql.Exec(`
UPDATE mail_vault_sealed SET envelope = (SELECT envelope FROM mail_vault_sealed WHERE message_id = ?),
    epk = (SELECT epk FROM mail_vault_sealed WHERE message_id = ?) WHERE message_id = ?`, b, b, a); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.GetMessage(userCtx(), a); !errors.Is(err, ErrSealedTampered) {
		t.Fatalf("row with another row's envelope: %v", err)
	}
	list, err := f.db.QueryMessages(userCtx(), MessageQuery{FolderIDs: []int64{f.secret}})
	if err != nil || len(list) != 1 || list[0].ID != b {
		t.Fatalf("listing must drop the tampered row only: %+v %v", list, err)
	}

	if _, err := f.db.sql.Exec(`UPDATE mail_vault_sealed SET body = substr(body, 1, length(body) - 1) WHERE message_id = ?`, b); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.GetMessage(userCtx(), b); !errors.Is(err, ErrSealedTampered) {
		t.Fatalf("truncated body: %v", err)
	}
}

func TestSealExistingFolderResumesAndLeavesNoRemnant(t *testing.T) {
	f := newVaultFixture(t)
	ctx := context.Background()
	ids := []int64{f.insert(t, f.secret, 1, "a"), f.insert(t, f.secret, 2, "b"), f.insert(t, f.secret, 3, "c")}
	if _, err := f.db.InsertMessage(ctx, &Message{AccountID: f.account, FolderID: f.inbox, UID: 1,
		MessageID: "<clear@example.org>", Subject: "Clear subject", FromAddress: "someone@example.org",
		BodyPlain: "clear body", Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if len(findMarkers(t, f.dir)) < len(markers) {
		t.Fatal("the scan does not see the cleartext it is meant to catch")
	}
	f.makeVault(t)

	n, err := f.db.SealPending(ctx, f.vault, 1)
	if err != nil || n != 1 {
		t.Fatalf("first step sealed %d: %v", n, err)
	}

	// an attachment that cannot be read fails its message's step, which must
	// leave that message in clear rather than half sealed.
	var victimPath string
	if err := f.db.sql.QueryRow(`SELECT disk_path FROM attachments WHERE message_id = ?`, ids[1]).Scan(&victimPath); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(f.db.attachmentsDir, filepath.FromSlash(victimPath))
	if err := os.Chmod(victim, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.SealPending(ctx, f.vault, 10); err == nil {
		t.Fatal("unreadable attachment did not fail the step")
	}
	for _, id := range ids {
		var vaultID int64
		var subject string
		var sealedRows int
		if err := f.db.sql.QueryRow(`SELECT vault_id, subject, (SELECT COUNT(*) FROM mail_vault_sealed WHERE message_id = messages.id) FROM messages WHERE id = ?`, id).
			Scan(&vaultID, &subject, &sealedRows); err != nil {
			t.Fatal(err)
		}
		sealed := vaultID != 0 && subject == "" && sealedRows == 1
		clear := vaultID == 0 && subject != "" && sealedRows == 0
		if !sealed && !clear {
			t.Fatalf("message %d is in neither form: vault %d subject %q sealed rows %d", id, vaultID, subject, sealedRows)
		}
	}
	if err := os.Chmod(victim, 0o644); err != nil {
		t.Fatal(err)
	}
	for {
		n, err := f.db.SealPending(ctx, f.vault, 1)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	pending, sealed, err := f.db.CountVaultWork(ctx, f.vault)
	if err != nil || pending != 0 || sealed != 3 {
		t.Fatalf("work after sealing: pending %d sealed %d %v", pending, sealed, err)
	}
	truncated, err := f.db.ScrubAfterSealing(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatal("WAL was not truncated with no other reader open")
	}

	f.gate.set(true)
	got, err := f.db.GetMessage(userCtx(), ids[2])
	if err != nil || got.Subject != markSubject+"c" {
		t.Fatalf("sealed existing message reads back as %+v %v", got, err)
	}
	atts, _ := f.db.ListAttachments(userCtx(), ids[2])
	if len(atts) != 1 || atts[0].Filename != markFile {
		t.Fatalf("attachments after sealing: %+v", atts)
	}
	f.gate.set(false)

	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, hit := range findMarkers(t, f.dir) {
		t.Errorf("still on disk after sealing: %s", hit)
	}
}

// findMarkers reads every file under dir, the database, its WAL and shared
// memory and the attachment store, and reports each marker string found in
// any letter case, since the full-text index stores its terms folded.
func findMarkers(t *testing.T, dir string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		folded := bytes.ToLower(data)
		for _, m := range markers {
			if at := bytes.Index(folded, bytes.ToLower([]byte(m))); at >= 0 {
				hits = append(hits, filepath.Base(path)+": "+m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

func TestUnsealRestoresMessagesAndRemovesVault(t *testing.T) {
	f := newVaultFixture(t)
	ctx := context.Background()
	f.makeVault(t)
	id := f.insert(t, f.secret, 1, "a")

	if _, err := f.db.UnsealPending(ctx, f.vault, 10); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("unsealing a locked vault: %v", err)
	}
	f.gate.set(true)
	for {
		n, err := f.db.UnsealPending(ctx, f.vault, 1)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	removed, err := f.db.DeleteMailVaultIfEmpty(ctx, f.vault)
	if err != nil || !removed {
		t.Fatalf("vault not removed: %v %v", removed, err)
	}
	got, err := f.db.GetMessage(ctx, id)
	if err != nil || got.Subject != markSubject+"a" || got.VaultID != 0 || got.MessageID != "<a"+markMsgID+">" {
		t.Fatalf("unsealed message = %+v %v", got, err)
	}
	atts, err := f.db.ListAttachments(ctx, id)
	if err != nil || len(atts) != 1 || atts[0].Filename != markFile {
		t.Fatalf("unsealed attachments = %+v %v", atts, err)
	}
	rc, err := f.db.OpenAttachment(ctx, atts[0])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != markContent+"a" {
		t.Fatalf("unsealed attachment = %q", b)
	}
	if src, err := f.db.MessagePGPSource(ctx, id); err != nil || string(src) != markSource {
		t.Fatalf("pgp source after unseal = %q %v", src, err)
	}
	later := f.insert(t, f.secret, 2, "b")
	if m, err := f.db.GetMessage(ctx, later); err != nil || m.VaultID != 0 {
		t.Fatalf("mail after the vault was removed must be stored in clear: %+v %v", m, err)
	}
}

func TestVaultedAccountCoversNewFolders(t *testing.T) {
	f := newVaultFixture(t)
	ctx := context.Background()
	m, _, keys, err := mailvault.Create([]byte("pass"), vaultTestKDF)
	if err != nil {
		t.Fatal(err)
	}
	f.gate.keys = keys
	f.db.SetVaultGate(f.gate)
	if _, err := f.db.CreateMailVault(ctx, &MailVault{Material: m}, []MailVaultMember{{AccountID: f.account}}); err != nil {
		t.Fatal(err)
	}
	later, err := f.db.CreateFolder(ctx, &Folder{AccountID: f.account, Name: "Later", IMAPPath: "Later"})
	if err != nil {
		t.Fatal(err)
	}
	id := f.insert(t, later, 1, "a")
	var vaultID int64
	if err := f.db.sql.QueryRow(`SELECT vault_id FROM messages WHERE id = ?`, id).Scan(&vaultID); err != nil || vaultID == 0 {
		t.Fatalf("mail in a folder created after the vault was stored in clear: %d %v", vaultID, err)
	}
	if visible, _ := f.db.FolderVisible(userCtx(), later); visible {
		t.Fatal("new folder of a locked vaulted account is visible")
	}
	if _, err := f.db.CreateMailVault(ctx, &MailVault{Material: m}, []MailVaultMember{{AccountID: f.account, FolderID: f.inbox}}); !errors.Is(err, ErrAlreadyVaulted) {
		t.Fatalf("a folder of a vaulted account went into a second vault: %v", err)
	}
}

func TestSealingDropsPassportAndShieldRecords(t *testing.T) {
	f := newVaultFixture(t)
	ctx := context.Background()
	before := f.insert(t, f.secret, 1, "a")
	m, err := f.db.GetMessage(ctx, before)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetMessageTrust(ctx, MessageTrust{MessageID: before, AuthState: "trusted", ReturnPath: markFrom, Sender: markFrom}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.RecordPrivacyScan(ctx, PrivacyScan{Key: PrivacyKey(*m), Sender: markFrom, ListID: "list", Date: time.Now(), Pixels: 1}); err != nil {
		t.Fatal(err)
	}
	f.makeVault(t)
	for {
		n, err := f.db.SealPending(ctx, f.vault, 10)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	var trust, scans int
	f.db.sql.QueryRow(`SELECT COUNT(*) FROM message_trust`).Scan(&trust)
	f.db.sql.QueryRow(`SELECT COUNT(*) FROM privacy_scans`).Scan(&scans)
	if trust != 0 || scans != 0 {
		t.Fatalf("sealing left %d trust records and %d privacy scans", trust, scans)
	}

	after := f.insert(t, f.secret, 2, "b")
	if err := f.db.SetMessageTrust(ctx, MessageTrust{MessageID: after, AuthState: "trusted", Sender: markFrom}); err != nil {
		t.Fatal(err)
	}
	f.db.sql.QueryRow(`SELECT COUNT(*) FROM message_trust`).Scan(&trust)
	if trust != 0 {
		t.Fatal("a trust record was stored in clear for vault mail")
	}
	f.gate.set(true)
	if got, err := f.db.GetMessageTrust(userCtx(), after); err != nil || got.Sender != "" {
		t.Fatalf("trust record for vault mail: %+v %v", got, err)
	}
}

func TestSealedThreadsStayApart(t *testing.T) {
	f := newVaultFixture(t)
	ctx := context.Background()
	f.makeVault(t)
	root := &Message{AccountID: f.account, FolderID: f.inbox, UID: 9, MessageID: "<root@example.org>",
		Subject: "Plans", Date: time.Now().Add(-time.Hour)}
	rootID, err := f.db.InsertMessage(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	reply := &Message{AccountID: f.account, FolderID: f.secret, UID: 1, MessageID: "<reply@example.org>",
		InReplyTo: []string{"root@example.org"}, Subject: "Re: Plans", Date: time.Now()}
	replyID, err := f.db.InsertMessage(ctx, reply)
	if err != nil {
		t.Fatal(err)
	}
	var rootThread, replyThread int64
	f.db.sql.QueryRow(`SELECT thread_id FROM messages WHERE id = ?`, rootID).Scan(&rootThread)
	f.db.sql.QueryRow(`SELECT thread_id FROM messages WHERE id = ?`, replyID).Scan(&replyThread)
	if rootThread == replyThread {
		t.Fatal("a sealed reply joined the clear thread of its parent")
	}
	var leaked int
	f.db.sql.QueryRow(`SELECT COUNT(*) FROM thread_keys WHERE key LIKE '%reply@example.org%'`).Scan(&leaked)
	if leaked != 0 {
		t.Fatal("a sealed message's Message-ID was written to thread_keys in clear")
	}
}
