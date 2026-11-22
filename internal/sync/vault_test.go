package sync

import (
	"context"
	"testing"

	"github.com/peltonapp/Pelton/internal/mailvault"
	"github.com/peltonapp/Pelton/internal/phishing"
	"github.com/peltonapp/Pelton/internal/storage"
)

// TestSyncIntoAVaultKeepsHeadersSealed: mail fetched into a vaulted folder
// while the vault is locked is sealed, gets no passport record and never
// reaches the raw-header observers, so neither the trust table nor the
// Autocrypt peers learn who wrote it.
func TestSyncIntoAVaultKeepsHeadersSealed(t *testing.T) {
	raw := "Authentication-Results: mx.google.com; dkim=pass header.d=lawyer.example; dmarc=pass header.from=lawyer.example\r\n" +
		"Return-Path: <bounce@lawyer.example>\r\n" +
		"Autocrypt: addr=counsel@lawyer.example; keydata=AAAA\r\n" +
		"From: Counsel <counsel@lawyer.example>\r\n" +
		"Content-Type: text/plain\r\n\r\nprivileged\r\n"
	ctx := context.Background()
	db, folder := newSyncTestFolder(t)
	material, _, keys, err := mailvault.Create([]byte("passphrase"), mailvault.KDFParams{Time: 1, MemoryKiB: 64, Threads: 1})
	if err != nil {
		t.Fatal(err)
	}
	keys.Wipe()
	if _, err := db.CreateMailVault(ctx, &storage.MailVault{Material: material},
		[]storage.MailVaultMember{{AccountID: folder.AccountID, FolderID: folder.ID}}); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(newTrustAdapter(raw, 5), db, nil)
	engine.AuthServ = phishing.ServerFor("me@gmail.com", "imap.gmail.com", "smtp.gmail.com")
	observed := 0
	engine.ObserveRaw = func(storage.Folder, []byte) { observed++ }
	res, err := engine.SyncFolder(ctx, folder)
	if err != nil || len(res.NewIDs) != 1 {
		t.Fatalf("SyncFolder = %+v, %v", res, err)
	}
	if observed != 0 {
		t.Fatal("the raw headers of vault mail reached an observer")
	}
	if v, err := db.MessageVault(ctx, res.NewIDs[0]); err != nil || v == 0 {
		t.Fatalf("message was not sealed: vault %d %v", v, err)
	}
	if got, err := db.GetMessageTrust(ctx, res.NewIDs[0]); err != nil || got.ReturnPath != "" || got.AuthState != "" {
		t.Fatalf("vault mail got a trust record in clear: %+v %v", got, err)
	}
}
