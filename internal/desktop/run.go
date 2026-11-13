// Package desktop is the wails application layer: the bound App struct, its
// frontend-facing methods (the bind_*.go files), the dtos, the native menu and
// the runtime event plumbing. main.go in the repo root is a thin entrypoint that
// embeds the built frontend and calls Run; everything else lives here so the
// project root stays clean. No mail, crypto, sync or storage logic lives in this
// package; it all delegates to internal/*.
package desktop

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/peltonapp/Pelton/internal/logging"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Config carries what the root entrypoint owns: the embedded frontend assets,
// the build version string and the embedded license texts.
type Config struct {
	// Assets holds the built frontend under frontend/dist.
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

// Run constructs and runs the wails application. It returns Run's error.
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
	app.startedHidden = startHidden(os.Args[1:], cfg.Channel)

	// a mailto: click that launched the app arrives as argv on Linux/Windows.
	// stash it so the first webview mount can open the prefilled compose. macOS
	// delivers the same thing as an Apple event instead (see below).
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

	dist, err := fs.Sub(cfg.Assets, "frontend/dist")
	if err != nil {
		return fmt.Errorf("pelton: frontend assets: %w", err)
	}

	wapp := application.New(application.Options{
		Name:        title,
		Description: aboutMessage(cfg.Channel, cfg.Version),
		Logger:      app.log,
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(dist),
		},
		// saving the window frame and closing the store happen here, before
		// wails tears the window down.
		OnShutdown: app.shutdown,
		// keep a single instance: a second launch (e.g. another mailto click)
		// hands its argv to the running app and exits, so mailto links reuse the
		// open window instead of spawning a new one.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: instanceID,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				if raw := firstMailtoArg(data.Args); raw != "" {
					app.deliverMailto(raw)
				}
				app.showWindow()
			},
		},
		Mac: application.MacOptions{
			// the window's close button hides it (or quits through quitApp),
			// so the last window closing is never what ends the app.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{
			// closing hides the window to the tray; quitApp is the way out.
			DisableQuitOnLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
			ProgramName:                   "pelton",
		},
	})
	app.wapp = wapp

	// macOS delivers a mailto: click as an Apple event, not argv.
	wapp.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(e *application.ApplicationEvent) {
		app.deliverMailto(e.Context().URL())
		app.showWindow()
	})

	if menu := app.initialMenu(); menu != nil {
		wapp.Menu.Set(menu)
	}

	// the window itself is created in ServiceStartup, once the remembered
	// geometry can be read from the store.
	app.windowOptions = application.WebviewWindowOptions{
		Name:      "main",
		Title:     title,
		Width:     defaultWindowWidth,
		Height:    defaultWindowHeight,
		MinWidth:  minWindowWidth,
		MinHeight: minWindowHeight,
		// the tray icon, the Dock icon (quitevent_darwin.m) or another launch
		// shows it.
		Hidden: app.startedHidden,
		// neutral dark surface so the native window chrome matches the ui before
		// the frontend paints. the real colors come from the css token theme.
		BackgroundColour: application.NewRGB(17, 18, 20),
		URL:              "/",
		Mac: application.MacWindow{
			// draw the ui all the way to the top of the window instead of under an
			// opaque native title bar, which never matches the theme. the frontend
			// keeps the top strip clear of the traffic lights and makes it draggable
			// (see the mac-titlebar handling in App.svelte).
			//
			// hidden, not hidden-inset: the inset variant asks for an NSToolbar,
			// and the toolbar makes the title bar taller, which drops the traffic
			// lights below the middle of the 30px menu bar row they share.
			TitleBar: application.MacTitleBarHidden,
		},
		Windows: application.WindowsWindow{
			Theme: application.SystemDefault,
		},
	}

	return wapp.Run()
}
