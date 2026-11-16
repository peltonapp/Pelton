package desktop

import (
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/peltonapp/Pelton/internal/storage"
)

func newVaultTestApp(t *testing.T) *App {
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
	return &App{ctx: ctx, store: store, log: slog.New(slog.DiscardHandler), dataDir: dir}
}

func waitVaultReady(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.vaultService().Ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the notes folder never finished its first scan")
}

func TestVaultOpenCreatesDefaultFolderOnlyOnUse(t *testing.T) {
	a := newVaultTestApp(t)
	a.startVault()
	if _, err := os.Stat(filepath.Join(a.dataDir, defaultVaultDir)); err == nil {
		t.Fatal("startup created the notes folder")
	}
	info, err := a.VaultOpen()
	if err != nil || info.Error != "" {
		t.Fatalf("open: %v %q", err, info.Error)
	}
	if !info.Default || !strings.HasSuffix(info.Folder, defaultVaultDir) {
		t.Errorf("info %+v", info)
	}
	t.Cleanup(a.closeVault)
	waitVaultReady(t, a)
}

func TestVaultChosenFolderIsNeverCreated(t *testing.T) {
	a := newVaultTestApp(t)
	missing := filepath.Join(t.TempDir(), "gone")
	if err := a.store.Set(a.ctx, settingNotesVault, missing); err != nil {
		t.Fatal(err)
	}
	info, err := a.VaultOpen()
	if err != nil {
		t.Fatal(err)
	}
	if info.Error == "" {
		t.Error("opening a missing chosen folder did not report an error")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("the chosen folder was created")
	}
}

func TestVaultRoundTrip(t *testing.T) {
	a := newVaultTestApp(t)
	if _, err := a.VaultOpen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.closeVault)
	waitVaultReady(t, a)
	p, err := a.VaultCreate("Ideas/First.md", "# First\nsee [mail](pelton://mail/x%40y)")
	if err != nil {
		t.Fatal(err)
	}
	f, err := a.VaultRead(p)
	if err != nil || !strings.HasPrefix(f.Content, "# First") {
		t.Fatalf("read %+v %v", f, err)
	}
	res, err := a.VaultSave(p, f.Content+"\nmore", f.Hash, false)
	if err != nil || res.Conflict {
		t.Fatalf("save %+v %v", res, err)
	}
	linking, _ := a.VaultNotesLinkingTo("pelton://mail/x%40y")
	if len(linking) != 1 || linking[0] != p {
		t.Errorf("linking %v", linking)
	}
	img, err := a.VaultSaveAttachment("shot.png", base64.StdEncoding.EncodeToString([]byte("png")))
	if err != nil || img != "Attachments/shot.png" {
		t.Fatalf("attachment %q %v", img, err)
	}
	data, err := a.VaultFileData(img)
	if err != nil || !strings.HasPrefix(data, "data:image/png;base64,") {
		t.Errorf("file data %q %v", data, err)
	}
	if _, err := a.VaultRead("../outside.md"); err == nil {
		t.Error("read outside the vault")
	}
	snap, err := a.VaultSnapshot()
	if err != nil || len(snap.Notes) != 1 || len(snap.Files) != 1 {
		t.Errorf("snapshot %+v %v", snap, err)
	}
}
