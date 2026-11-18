// Command pelton is the desktop mail client entrypoint. It embeds the built
// frontend and hands control to the desktop package, which owns the wails app
// and all the frontend bindings. Keeping this file tiny keeps the repo root
// uncluttered; the application code lives in internal/desktop.
package main

import (
	"embed"
	"os"
	"slices"

	"github.com/peltonapp/Pelton/internal/desktop"
	"github.com/peltonapp/Pelton/internal/storage"
)

//go:embed all:frontend/dist
var assets embed.FS

// licenseManifest is the generated list of third-party licenses (run
// `make licenses`); programLicense is Pelton's own GPL-3.0 text. They are
// embedded here, at the module root where the files live, and handed to the
// desktop layer to serve to the about section on demand.
//
//go:embed licenses/manifest.json
var licenseManifest string

//go:embed LICENSE
var programLicense string

// trayIcon is the tray icon for Windows and Linux (see the desktop package's
// tray_windows.go and tray_linux.go). Embedded on every platform - it is a
// few KB - but unused on macOS. nightlyTrayIcon is the nightly build's own
// icon, so a nightly in the tray is never mistaken for a real install.
//
//go:embed build/windows/icon.ico
var trayIcon []byte

//go:embed build/windows/icon-nightly.ico
var nightlyTrayIcon []byte

// version is overridden at build time with -ldflags "-X main.version=<v>" (see
// the Makefile) and defaults to "dev".
var version = "dev"

// channel is the build channel, overridden at build time with -ldflags
// "-X main.channel=nightly" by the nightly workflow. Empty means a normal
// build; see internal/desktop/nightly.go for what a nightly does differently.
var channel = ""

func main() {
	// --potatoes-are-nice launches a purely-cosmetic demo mode used for website
	// screenshots: the ui shows fixed potato-themed sample data instead of real
	// accounts and mail. Nothing else changes.
	demoMode := slices.Contains(os.Args[1:], "--potatoes-are-nice")

	tray := trayIcon
	if channel == storage.ChannelNightly {
		tray = nightlyTrayIcon
	}

	if err := desktop.Run(desktop.Config{
		Assets:          assets,
		Version:         version,
		Channel:         channel,
		LicenseManifest: licenseManifest,
		ProgramLicense:  programLicense,
		TrayIcon:        tray,
		DemoMode:        demoMode,
	}); err != nil {
		println("Error:", err.Error())
	}
}
