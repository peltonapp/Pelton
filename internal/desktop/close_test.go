package desktop

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/peltonapp/Pelton/internal/storage/storagetest"
)

func newCloseTestApp(t *testing.T) *App {
	t.Helper()
	ctx, stopBackground := testContext(t)
	store, err := storagetest.Open(filepath.Join(t.TempDir(), "test.db"))
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

func TestHideOnClose(t *testing.T) {
	tests := []struct {
		name          string
		setting       string
		quitRequested bool
		want          bool
	}{
		{name: "unset keeps running in the background", want: true},
		{name: "background keeps running", setting: closeActionBackground, want: true},
		{name: "quit exits", setting: closeActionQuit},
		{name: "unknown value falls back to background", setting: "nonsense", want: true},
		// the Quit menu item and the tray's Quit must exit even while the close
		// button is set to keep running.
		{name: "explicit quit beats background", setting: closeActionBackground, quitRequested: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newCloseTestApp(t)
			if tt.setting != "" {
				if err := a.store.Set(a.ctx, settingCloseAction, tt.setting); err != nil {
					t.Fatalf("set: %v", err)
				}
			}
			a.quitRequested.Store(tt.quitRequested)
			if got := a.hideOnClose(); got != tt.want {
				t.Errorf("hideOnClose() = %v, want %v", got, tt.want)
			}
		})
	}
}
