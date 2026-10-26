// Package desktop is the wails application layer: the bound App struct, its
// frontend-facing methods (the bind_*.go files), the dtos, the native menu and
// the runtime event plumbing. main.go in the repo root is a thin entrypoint that
// embeds the built frontend and calls Run; everything else lives here so the
// project root stays clean. No mail, crypto, sync or storage logic lives in this
// package; it all delegates to internal/*.
package desktop

import (
	"embed"
	"os"

	"github.com/peltonapp/Pelton/internal/logging"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailswindows "github.com/wailsapp/wails/v2/pkg/options/windows"
)

// Config carries what the root entrypoint owns: the embedded frontend assets,
// the build version string and the embedded license texts.
type Config struct {
	Assets          embed.FS
	Version         string
	LicenseManifest string
	ProgramLicense  string
	// TrayIcon is the .ico shown in the Windows notification area and the
	// Linux tray; unused on macOS.
	TrayIcon []byte
	// DemoMode runs the app in the cosmetic screenshot mode (--potatoes-are-nice).
	DemoMode bool
	// Channel is the build channel ("" for a normal build, "nightly" for the
	// automated dev-branch builds). See nightly.go.
	Channel string
}

// Run constructs and runs the wails application. It returns wails.Run's error.
func Run(cfg Config) error {
	// the outermost panic handler. A panic that gets this far has already
	// taken the ui with it; this leaves a file behind saying what happened
	// instead of the window simply disappearing.
	defer logging.Guard("running the app")

	app := newApp(cfg.Version, cfg.Channel)
	app.debug = debugForced(os.Args[1:])
	app.licenseManifest = cfg.LicenseManifest
	app.programLicense = cfg.ProgramLicense
	app.trayIcon = cfg.TrayIcon
	app.demoMode = cfg.DemoMode

	// a mailto: click that launched the app arrives as argv on Linux/Windows.
	// stash it so the first webview mount can open the prefilled compose. macOS
	// delivers the same thing through OnUrlOpen (an Apple event) instead.
	if raw := firstMailtoArg(os.Args[1:]); raw != "" {
		app.setPendingMailto(parseMailto(raw))
	}

	// a dev build (PELTON_DEV) and a nightly both run against their own data and
	// must not share the single-instance lock with an installed Pelton, or
	// launching one would just surface the other and exit.
	instanceID := "com.pelton.app"
	switch {
	case os.Getenv("PELTON_DEV") != "":
		instanceID = "com.pelton.app.dev"
	case cfg.Channel != "":
		instanceID = "com.pelton.app." + cfg.Channel
	}

	title := "Pelton"
	if cfg.Channel == storage.ChannelNightly {
		title = "Pelton Nightly"
	}

	return wails.Run(&options.App{
		Title:     title,
		// the window opens at the default and startup resizes it to whatever was
		// remembered, once the store is open. see geometry.go.
		Width:     defaultWindowWidth,
		Height:    defaultWindowHeight,
		MinWidth:  minWindowWidth,
		MinHeight: minWindowHeight,
		AssetServer: &assetserver.Options{
			Assets: cfg.Assets,
		},
		// neutral dark surface so the native window chrome matches the ui before
		// the frontend paints. the real colors come from the css token theme.
		BackgroundColour: &options.RGBA{R: 17, G: 18, B: 20, A: 1},
		// what the close button does is a user setting, so beforeClose decides
		// per close whether to hide the window or exit. HideWindowOnClose is
		// deliberately left off: it is a static option and would take that
		// decision away before beforeClose ever runs.
		OnBeforeClose: app.beforeClose,
		OnStartup:     app.startup,
		OnDomReady:    app.domReady,
		OnShutdown:    app.shutdown,
		Menu:              app.initialMenu(),
		// keep a single instance: a second launch (e.g. another mailto click)
		// hands its argv to the running app and exits, so mailto links reuse the
		// open window instead of spawning a new one.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: instanceID,
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				if raw := firstMailtoArg(data.Args); raw != "" {
					app.deliverMailto(raw)
				}
				app.showWindow()
			},
		},
		Mac: &mac.Options{
			// draw the ui all the way to the top of the window instead of under an
			// opaque native title bar, which never matches the theme. the frontend
			// keeps the top strip clear of the traffic lights and makes it draggable
			// (see the mac-titlebar handling in App.svelte).
			//
			// hidden, not hidden-inset: the inset variant asks for an NSToolbar,
			// and the toolbar makes the title bar taller, which drops the traffic
			// lights below the middle of the 30px menu bar row they share.
			TitleBar: mac.TitleBarHidden(),
			About: &mac.AboutInfo{
				Title:   title,
				Message: aboutMessage(cfg.Channel, cfg.Version),
			},
			// macOS delivers a mailto: click as an Apple event, not argv.
			OnUrlOpen: func(url string) {
				app.deliverMailto(url)
				app.showWindow()
			},
		},
		Windows: &wailswindows.Options{
			Theme: wailswindows.SystemDefault,
			// Mica is the material a native Windows 11 window is made of, and
			// the caption bar picks it up instead of staying a flat grey slab.
			// wails only reads BackdropType when WindowIsTranslucent is set, and
			// on Windows 10 that same flag means something else entirely (a
			// blur-behind over the whole window), so it is only asked for where
			// the backdrop actually exists.
			WindowIsTranslucent: supportsMica(),
			BackdropType:        wailswindows.Mica,
		},
		Bind: []interface{}{
			app,
		},
	})
}
