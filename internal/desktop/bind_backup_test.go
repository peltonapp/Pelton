package desktop

import (
	"encoding/json"
	"testing"

	"github.com/peltonapp/Pelton/internal/credentials"
	"github.com/peltonapp/Pelton/internal/storage"
)

func onlyAccount(t *testing.T, a *App) storage.Account {
	t.Helper()
	list, err := a.store.ListAccounts(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var out []storage.Account
	for _, acc := range list {
		if !acc.Local {
			out = append(out, acc)
		}
	}
	if len(out) != 1 {
		t.Fatalf("want 1 account, got %d", len(out))
	}
	return out[0]
}

func TestBackupRoundTripKeepsParallel(t *testing.T) {
	a := newAccountTestApp(t)
	n := 2
	acc := storage.Account{Email: "bob@example.org", IMAPHost: "mail.example.org", IMAPPort: 993}
	id, err := a.store.CreateAccount(a.ctx, &acc)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetAccountSyncMaxParallel(a.ctx, id, &n); err != nil {
		t.Fatal(err)
	}
	secret := credentials.Secret{Method: credentials.MethodPassword, Password: "pw"}
	if err := credentials.Store(id, secret); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credentials.Delete(id) })

	boxes, err := a.exportMailboxes("export-pass")
	if err != nil {
		t.Fatal(err)
	}
	if boxes[0].SyncMaxParallel == nil || *boxes[0].SyncMaxParallel != 2 {
		t.Fatalf("export lost fields: %+v", boxes[0])
	}

	b := newAccountTestApp(t)
	if _, err := b.importMailboxes(boxes, "export-pass"); err != nil {
		t.Fatal(err)
	}
	got := onlyAccount(t, b)
	t.Cleanup(func() { _ = credentials.Delete(got.ID) })
	if got.SyncMaxParallel == nil || *got.SyncMaxParallel != 2 {
		t.Fatalf("import lost fields: %+v", got)
	}
}

func TestImportOldBackupWithoutParallelFollowsGlobal(t *testing.T) {
	b := newAccountTestApp(t)
	old := []mailboxBackup{{Email: "a@example.org", IMAPHost: "h", IMAPPort: 993, SMTPHost: "h", SMTPPort: 465}}
	if _, err := b.importMailboxes(old, ""); err != nil {
		t.Fatal(err)
	}
	if got := onlyAccount(t, b); got.SyncMaxParallel != nil {
		t.Fatalf("got %+v, want no override", got)
	}
}

// A backup from a build with JMAP holds a JMAP mailbox. A build without JMAP
// restores it as an IMAP mailbox rather than refusing the file.
func TestImportJMAPMailboxWithoutJMAPRestoresAsIMAP(t *testing.T) {
	b := newAccountTestApp(t)
	var boxes []mailboxBackup
	raw := `[{"email":"a@example.org","imapHost":"mail.example.org","imapPort":993,"smtpHost":"mail.example.org","smtpPort":465,"protocol":"jmap"}]`
	if err := json.Unmarshal([]byte(raw), &boxes); err != nil {
		t.Fatal(err)
	}
	if _, err := b.importMailboxes(boxes, ""); err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := onlyAccount(t, b); got.IMAPHost != "mail.example.org" || got.IMAPPort != 993 {
		t.Fatalf("restored %+v, want the mailbox with its IMAP host", got)
	}
}
