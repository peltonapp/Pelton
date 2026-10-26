package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage"
)

func TestStartHiddenFlagsAndMarker(t *testing.T) {
	t.Setenv("PELTON_DEV", "1")
	cfg := t.TempDir()
	t.Setenv("HOME", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("AppData", cfg)

	if startHidden(nil, "") {
		t.Fatal("no flag and no marker should show the window")
	}
	if !startHidden([]string{"--start-in-tray"}, "") || !startHidden([]string{"--start-hidden"}, "") {
		t.Fatal("either flag should start hidden")
	}
	if startHidden([]string{"--start-hidden", "mailto:a@example.com"}, "") {
		t.Fatal("a mailto launch should show the window")
	}

	path, err := storage.DefaultPathForChannel("")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: filepath.Dir(path)}
	if err := os.MkdirAll(a.dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.writeStartHidden(true); err != nil {
		t.Fatal(err)
	}
	if !a.startHiddenEnabled() || !startHidden(nil, "") {
		t.Fatal("the marker should start hidden")
	}
	if err := a.writeStartHidden(false); err != nil {
		t.Fatal(err)
	}
	if a.startHiddenEnabled() || startHidden(nil, "") {
		t.Fatal("removing the marker should show the window again")
	}
	if err := a.writeStartHidden(false); err != nil {
		t.Fatalf("removing a missing marker: %v", err)
	}
}
