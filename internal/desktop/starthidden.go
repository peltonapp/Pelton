package desktop

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/peltonapp/Pelton/internal/storage"
)

// Starting in the background (#483): the window stays hidden at launch and
// the tray icon, the Dock icon or another launch brings it up. The choice has
// to be known before the window exists, which is before the store opens, so
// besides its settings row it lives as a marker file next to the database.
const (
	settingStartHidden = "start_hidden"
	startHiddenFile    = "start-hidden"
)

// startHiddenFlags are the launch arguments that start hidden whatever the
// setting says, for a login item or autostart entry.
var startHiddenFlags = []string{"--start-hidden", "--start-in-tray"}

// startHidden reports whether this launch should keep the window hidden. A
// mailto: launch always shows the window, since it opens a compose.
func startHidden(args []string, channel string) bool {
	if firstMailtoArg(args) != "" {
		return false
	}
	for _, flag := range startHiddenFlags {
		if slices.Contains(args, flag) {
			return true
		}
	}
	path, err := storage.DefaultPathForChannel(channel)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(path), startHiddenFile))
	return err == nil
}

// startHiddenEnabled reports whether the setting is on, read from the marker.
func (a *App) startHiddenEnabled() bool {
	if a.dataDir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(a.dataDir, startHiddenFile))
	return err == nil
}

// writeStartHidden creates or removes the marker to match the setting.
func (a *App) writeStartHidden(on bool) error {
	if a.dataDir == "" {
		return nil
	}
	path := filepath.Join(a.dataDir, startHiddenFile)
	if !on {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, nil, 0o600)
}
