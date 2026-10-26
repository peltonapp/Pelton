package desktop

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/peltonapp/Pelton/internal/mailexport"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func exportTestApp(t *testing.T) (*App, *storage.DB, context.Context) {
	t.Helper()
	ctx, stopBackground := testContext(t)
	db, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
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

// TestExportSettingsRoundTrip covers the column defaults and the setter: a new
// account exports nothing until it is told to.
func TestExportSettingsRoundTrip(t *testing.T) {
	_, db, ctx := exportTestApp(t)
	id, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	account, err := db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if account.ExportOnArchive {
		t.Error("a new account exports on archive by default")
	}
	if account.ExportSubfolders != mailexport.SubfoldersNone {
		t.Errorf("default subfolders = %q, want %q", account.ExportSubfolders, mailexport.SubfoldersNone)
	}

	if err := db.SetAccountArchiveExport(ctx, id, true, "/tmp/mail", mailexport.SubfoldersMonth, "{date}-{id}"); err != nil {
		t.Fatalf("set export: %v", err)
	}
	account, err = db.GetAccount(ctx, id)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if !account.ExportOnArchive || account.ExportDir != "/tmp/mail" ||
		account.ExportSubfolders != mailexport.SubfoldersMonth || account.ExportNameTemplate != "{date}-{id}" {
		t.Errorf("stored settings = %+v, want the ones just written", account)
	}
}

// TestUpdateAccountKeepsExportOffWithoutADirectory: the toggle alone is not
// enough, since there would be nowhere to write and the user would believe
// copies were being kept.
func TestUpdateAccountKeepsExportOffWithoutADirectory(t *testing.T) {
	a, db, ctx := exportTestApp(t)
	id, err := db.CreateAccount(ctx, &storage.Account{Email: "me@example.com", IMAPHost: "imap.example.com", IMAPPort: 993})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	dto, err := a.UpdateAccount(UpdateAccountRequest{
		ID:              id,
		IMAPHost:        "imap.example.com",
		IMAPPort:        993,
		ExportOnArchive: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if dto.ExportOnArchive {
		t.Error("export was left on with no directory set")
	}

	dto, err = a.UpdateAccount(UpdateAccountRequest{
		ID:               id,
		IMAPHost:         "imap.example.com",
		IMAPPort:         993,
		ExportOnArchive:  true,
		ExportDir:        t.TempDir(),
		ExportSubfolders: "nonsense",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !dto.ExportOnArchive {
		t.Error("export stayed off with a directory set")
	}
	if dto.ExportSubfolders != mailexport.SubfoldersNone {
		t.Errorf("subfolders = %q, want an unknown mode to fall back to %q", dto.ExportSubfolders, mailexport.SubfoldersNone)
	}
}

// TestExportWanted is the gate the archive path runs: only a real archive, only
// a configured account.
func TestExportWanted(t *testing.T) {
	archive := storage.Folder{Name: "Archive", IMAPPath: "Archive"}
	inbox := storage.Folder{Name: "INBOX", IMAPPath: "INBOX"}
	on := storage.Account{ExportOnArchive: true, ExportDir: "/tmp/mail"}

	tests := []struct {
		name    string
		account storage.Account
		dest    storage.Folder
		want    bool
	}{
		{"configured, archiving", on, archive, true},
		{"configured, moving elsewhere", on, inbox, false},
		{"toggle off", storage.Account{ExportDir: "/tmp/mail"}, archive, false},
		{"no directory", storage.Account{ExportOnArchive: true}, archive, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exportWanted(tt.account, tt.dest); got != tt.want {
				t.Errorf("exportWanted() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestPreviewArchiveExportName(t *testing.T) {
	a := &App{}
	got := a.PreviewArchiveExportName("", mailexport.SubfoldersMonth)
	if !strings.HasSuffix(got, ".eml") {
		t.Errorf("preview = %q, want an .eml name", got)
	}
	if !strings.Contains(got, "2026") || !strings.Contains(got, "Invoice 42") {
		t.Errorf("preview = %q, want the sample date and subject", got)
	}
	flat := a.PreviewArchiveExportName("{subject}", mailexport.SubfoldersNone)
	if flat != "Invoice 42.eml" {
		t.Errorf("preview = %q, want %q", flat, "Invoice 42.eml")
	}
}
