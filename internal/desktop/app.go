// app.go wires the existing internal/* backend to the wails frontend. The App
// type is the single bound struct: every method here is callable from
// typescript through the generated bindings. App orchestrates only. It opens the
// store, owns the outbox queue and background services, and translates between
// internal package types and the flat dtos the ui consumes. No mail, crypto,
// sync or storage logic lives here; it all delegates to internal/*.
package desktop

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/peltonapp/Pelton/internal/certtrust"
	"github.com/peltonapp/Pelton/internal/configsync"
	pimap "github.com/peltonapp/Pelton/internal/imap"
	"github.com/peltonapp/Pelton/internal/logging"
	"github.com/peltonapp/Pelton/internal/mcpserver"
	"github.com/peltonapp/Pelton/internal/outbox"
	"github.com/peltonapp/Pelton/internal/proxy"
	"github.com/peltonapp/Pelton/internal/search"
	"github.com/peltonapp/Pelton/internal/storage"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"golang.org/x/oauth2"
)

// App is the bound application object. Its exported methods form the api the
// frontend calls.
type App struct {
	ctx context.Context
	log *slog.Logger
	// logWriter is where log lines go. It always writes to stderr and, when
	// the setting is on, also to a rotating file in the data directory. See
	// logging.go.
	logWriter *logging.Writer
	// debug is set by --debug or PELTON_DEBUG and forces file logging on at
	// debug level, over the setting. It is the way out of "the app will not
	// start, so I cannot turn on logging in settings".
	debug bool

	store *storage.DB
	// dataDir is the app data directory the store opened in; themes and the
	// search index live next to the database. Empty if the store failed to
	// open.
	dataDir string
	// storeReady closes once startup has finished assigning (or failing to
	// assign) store, giving domReady a happens-before edge before it reads
	// store on its own goroutine - without it, the read is a data race even
	// though a nil check keeps it from crashing.
	storeReady chan struct{}
	index      *search.Index
	// streamTick rate-limits the "mail arrived" events a running sync emits, so
	// a fast first sync fills the list without asking the ui to redraw it
	// hundreds of times a minute.
	streamTick streamGate
	// searchMu serializes index backfills so a startup pass and a post-sync pass
	// do not advance the watermark concurrently.
	searchMu sync.Mutex
	// rejectedLogins holds the accounts whose credentials the server refused,
	// which is not something the keyring can tell us: the password is stored,
	// it is simply wrong. It is in memory on purpose, since the only way to
	// learn it is to try, and a restart tries again. See bind_account_manage.go.
	rejectedLogins   map[int64]struct{}
	rejectedLoginsMu sync.Mutex
	// startAccount runs an account's first sync and parks it on idle. It is a
	// field only so a test can watch which accounts an import starts without
	// the call reaching a real server or the os keyring. nil means
	// StartAccountSync, which is what the app always uses.
	startAccount func(accountID int64) error
	// newIMAPClient opens an imap connection. It is a field only so a test can
	// substitute a fake and reach the parts of a binding that run after the
	// connection. nil means pimap.Connect, which is what the app always uses.
	// See imapseam.go.
	newIMAPClient func(cfg pimap.Config) (mailClient, error)
	// secrets reaches the os keyring and oauthAuthorize runs the browser
	// consent flow. They are fields only so a test can drive the oauth
	// bindings without either. nil means the real ones. See oauthseam.go.
	secrets        secretStore
	oauthAuthorize func(ctx context.Context, provider, clientID, clientSecret, loginHint string, open func(string)) (*oauth2.Token, error)
	// checkTLS completes one server's TLS handshake with the given trust. It
	// is a field only so a test can report certificates without a server. nil
	// means the real imap and smtp handshakes. See bind_cert_trust.go.
	checkTLS func(server, host string, port int, trust certtrust.Trust) error
	// startedAt is when the process came up, for the process overlay's uptime.
	startedAt time.Time
	// runtimeReady is set once wails has handed us its context in startup.
	// Until then there is no ui to emit events at or menu to rebuild.
	runtimeReady atomic.Bool
	// the active profile's background work (idle loops, sync) runs under this
	// context, so switching profiles stops it. See bind_profiles.go.
	session     context.Context
	sessionStop context.CancelFunc
	sessionMu   sync.Mutex
	// dirSizes caches measured directory sizes for the process overlay, which
	// polls while it is open and must not walk the attachment tree every tick.
	// See bind_devtools.go.
	dirSizes   map[string]measuredDir
	dirSizesMu sync.Mutex
	// contacts caches the address book the phishing checks compare senders
	// against, so reading down a folder does not requery it per message.
	contacts correspondentCache
	queue    *outbox.Queue
	version  string
	// channel is the build channel: "" for a normal build, storage.ChannelNightly
	// for the automated dev-branch builds. See nightly.go.
	channel string
	// embedded license data served to the about section on demand.
	licenseManifest string
	programLicense  string
	// trayIcon is the embedded .ico for the tray (see tray_windows.go and
	// tray_linux.go); empty on macOS.
	trayIcon []byte
	// mailMenuItems are the native Mail-menu items that act on the open message;
	// they start disabled and SetMailActionsEnabled toggles them as the frontend's
	// open message changes. mailActionsEnabled mirrors that same state so a menu
	// rebuild (RebuildMenu, on a language change) can restore it instead of
	// resetting every item back to disabled.
	mailMenuItems      []*menu.MenuItem
	mailActionsEnabled bool

	// quitRequested marks a quit the user asked for directly (the Quit menu
	// item, the tray's Quit) so beforeClose does not mistake it for a window
	// close and hide the window instead of exiting.
	quitRequested atomic.Bool

	// dlMu guards dlCancel, the cancel function of the running bulk offline
	// download (nil when none is running). CancelDownload calls it to stop the
	// job without tearing down the whole app context.
	dlMu     sync.Mutex
	dlCancel context.CancelFunc

	// demoMode is the purely-cosmetic screenshot mode (the --potatoes-are-nice
	// flag): the frontend fills the ui with fixed sample data instead of reading
	// real accounts and mail. It never touches the store or the network.
	demoMode bool

	// proxyMu guards proxyCfg, the cached outbound proxy preference (with its
	// password from the keyring). It is loaded at startup and refreshed by
	// SetProxyConfig, so the mail and http paths read it without touching the
	// keyring on every connection.
	proxyMu  sync.RWMutex
	proxyCfg proxy.Config

	// mcpMu guards mcp, the read-only Model Context Protocol server. It is off by
	// default; the External settings section starts and stops it, and a settings
	// change (enable, port, token) restarts it under this lock so the lifecycle
	// never races.
	mcpMu sync.Mutex
	mcp   *mcpserver.Server

	// mailto holds a mailto: draft the app was launched with (or received from a
	// second launch) until the frontend consumes it. See mailto.go.
	mailto mailtoState

	// badgeMu guards unreadBadge, the last unread count the frontend reported
	// for the dock or tray icon. Kept so toggling the setting can re-apply it without
	// waiting for the next sidebar refresh.
	badgeMu     sync.Mutex
	unreadBadge int

	// workersMu guards workers, the per-account sync/idle goroutines.
	// stopAccountWorker cancels and joins one entry; profile shutdown cancels
	// the session and joins all.
	workersMu sync.Mutex
	workers   map[int64]*accountWorker

	// accountStatesMu guards accountStates and removedAccounts. A state holds
	// the account's lock, sync hold, tracked IMAP sessions, in-flight run and
	// progress tally. Entries outlive the account's scheduler so account
	// removal can abort, hold and lock only that account across the teardown.
	accountStatesMu sync.Mutex
	accountStates   map[int64]*accountState
	// removedAccounts are the ids DeleteAccount removed while the app ran.
	// Nothing may start sync work for them again.
	removedAccounts map[int64]struct{}

	// syncsMu guards syncs, one scheduler and sync pool per account. SMTP
	// send never checks a slot out and pauses nothing.
	syncsMu sync.Mutex
	syncs   map[int64]*accountSync

	// fullReconcileForTest, when set, replaces execFullReconcile. Tests only.
	fullReconcileForTest func(ctx context.Context, account storage.Account, folder storage.Folder) error
	// onDemandFetchForTest, when set, replaces the protocol fetch inside
	// fetchMessageBodyOnDemand. Production leaves it nil.
	onDemandFetchForTest func(ctx context.Context, account storage.Account, folder storage.Folder, remoteIDs []string) error
	// bodyRetryDelays, when set, replaces onDemandBodyRetryDelays (tests).
	bodyRetryDelays []time.Duration
	// bodyFetches holds the ids of messages whose body is being fetched for the
	// reading pane, so opening one again joins that fetch instead of racing it.
	bodyFetches sync.Map
	// syncProgressEmitForTest, when set, receives sync progress events before the
	// wails runtime (tests).
	syncProgressEmitForTest func(SyncProgressEvent)
	// emitForTest, when set, receives every event before the wails runtime
	// (tests).
	emitForTest func(name string, payload any)
	// notificationForTest, when set, receives OS notifications instead of the
	// platform backend (tests).
	notificationForTest func(notification)
	// progressHeartbeatEvery, when set, replaces syncProgressHeartbeat (tests).
	progressHeartbeatEvery time.Duration
}

// IsDemoMode reports whether the app was launched in the cosmetic demo mode. The
// frontend reads it once at startup to decide whether to render sample data.
func (a *App) IsDemoMode() bool {
	return a.demoMode
}

// IsDevMode reports whether the app is running against the separate dev data
// directory (the PELTON_DEV env var storage.DefaultPath checks), so the
// frontend can show a persistent indicator that this isn't a normal install -
// it's easy to forget a dev build is pointed at throwaway data instead of a
// real mailbox.
func (a *App) IsDevMode() bool {
	return os.Getenv("PELTON_DEV") != ""
}

// newApp creates the App with the build version and channel. The heavy
// initialization happens in startup once wails has handed us a context we can
// emit runtime events on.
func newApp(version, channel string) *App {
	w := logging.NewWriter()
	return &App{
		logWriter:  w,
		log:        w.Logger(),
		version:    version,
		channel:    channel,
		startedAt:  time.Now(),
		storeReady: make(chan struct{}),
		workers:    make(map[int64]*accountWorker),
	}
}

// startup is the wails OnStartup hook. It opens the store, runs migrations,
// builds the outbox queue and starts background services. A failure here is
// fatal to a useful app, so we log loudly; the ui surfaces the missing store via
// the bound methods returning errors.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// from here the wails runtime is usable: emitting events and rebuilding the
	// native menu both need the context wails itself handed us. Tests construct
	// an App with an ordinary context and never reach this, so those calls stay
	// no-ops there rather than taking the process down.
	a.runtimeReady.Store(true)

	// --debug has to work before the store is up, since "the app will not
	// start" is one of the reasons to reach for it. With no store there are no
	// settings to read, so this pass only ever turns logging on, never off.
	if a.debug {
		a.applyLogSettings()
	}

	store, dataDir, err := openStore(ctx, a.channel)
	if err != nil {
		a.log.Error("open store", "err", err)
		close(a.storeReady)
		// even without a store the tray must come up: with the window closed
		// it is the only visible way to reopen or quit the app.
		a.startTray()
		return
	}
	a.store = store
	a.dataDir = dataDir
	a.applyLogSettings()
	a.applyCharsetFallback()
	a.queue = outbox.NewQueue(store)
	a.loadProxy()
	close(a.storeReady)

	// as early as the store allows, so the window settles at its remembered size
	// before the frontend has painted anything into it.
	a.restoreGeometry()

	// the tray icon (Windows and Linux; no-op elsewhere). started after the
	// store is up so its menu labels can follow the language setting.
	a.startTray()

	// demo mode is purely cosmetic: the frontend renders fixed sample data, so we
	// skip everything that would touch the network or mutate the store (sync, idle,
	// the outbox worker, auto-update checks, download resume, migrations). the
	// store still opens so bound calls do not error, but nothing runs against it.
	if a.demoMode {
		return
	}

	// the old config-sync feature could redirect the data directory into a synced
	// folder ("in-place" mode). that feature is gone; if a device still has that
	// marker, migrate its data back to the normal app-support dir now, so the next
	// launch opens from the standard location again.
	if defaultPath, pathErr := storage.DefaultPathForChannel(a.channel); pathErr == nil {
		stateDir := filepath.Dir(defaultPath)
		if migrated, mErr := configsync.MigrateInPlaceBack(ctx, store, stateDir, filepath.Base(defaultPath)); mErr != nil {
			a.log.Error("migrate config-sync data back", "err", mErr)
		} else if migrated {
			a.log.Info("migrated config-sync in-place data back to the default folder")
		}
	}

	// open the search index and bring it up to date in the background so startup
	// is not blocked by a large backfill. a failure here only disables search.
	if idx, err := openSearchIndex(dataDir); err != nil {
		a.log.Error("open search index", "err", err)
	} else {
		a.index = idx
		goSafe("catching the search index up", a.backfillSearch)
	}

	// one pass over the cache for mail stored before charset detection existed.
	// backgrounded: it reads every message body once, which is not something to
	// hold a window open for.
	goSafe("checking cached mail for broken text", a.markMangledMail)

	a.startBackgroundServices()

	// off by default; only runs at all if the user turned on a check
	// frequency in settings. backgrounded so a slow/unreachable network never
	// delays startup.
	goSafe("checking for updates", func() { a.maybeAutoCheckForUpdates(ctx) })

	// if a bulk offline download was still running when the app last closed,
	// pick it back up; planDownload skips anything that already has its body so
	// this is cheap when most of the range was already fetched.
	a.ResumePendingDownload()
}

// domReady is the wails OnDomReady hook. Native window calls (like the theme
// setter) need the webview up first, so the initial theme is applied here
// rather than in startup. It can run concurrently with startup (a large
// mailbox can still be opening when the webview signals dom-ready), so it
// waits for storeReady first to avoid racing on store.
func (a *App) domReady(ctx context.Context) {
	<-a.storeReady
	a.applyNativeTheme(a.stringSetting(storage.SettingTheme, defaultTheme))
}

// shutdown is the wails OnShutdown hook. It closes the store so the sqlite wal
// is checkpointed cleanly.
func (a *App) shutdown(ctx context.Context) {
	// before the store closes, and before anything else can fail on the way out.
	a.saveGeometry()
	a.stopTray()
	a.stopMCP()
	if a.index != nil {
		if err := a.index.Close(); err != nil {
			a.log.Error("close search index", "err", err)
		}
	}
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			a.log.Error("close store", "err", err)
		}
	}
	// last, so anything the closes above logged still reaches the file.
	a.logWriter.Disable()
}

// openStore opens the database and applies migrations, returning the
// directory it opened from. That is normally the default per-OS app-support
// directory (the same path the cli tools use, so accounts they created are
// visible here), but configsync's in-place mode can redirect it to a folder
// the user chose instead - see configsync.ActiveDataDir. A nightly build opens
// its own directory, so it never sees or touches a stable install's mail.
func openStore(ctx context.Context, channel string) (*storage.DB, string, error) {
	defaultPath, err := storage.DefaultPathForChannel(channel)
	if err != nil {
		return nil, "", err
	}
	defaultDir := filepath.Dir(defaultPath)
	dbFileName := filepath.Base(defaultPath)

	dataDir, err := configsync.ActiveDataDir(defaultDir, defaultDir)
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(dataDir, dbFileName)
	store, err := storage.Open(path)
	if err != nil {
		return nil, "", err
	}
	if err := store.RunMigrations(ctx); err != nil {
		store.Close()
		return nil, "", err
	}
	return store, dataDir, nil
}

// AppVersion returns the build version string for the about section. It is set
// at build time via ldflags and defaults to "dev" in a plain build or dev run.
func (a *App) AppVersion() string {
	return a.version
}

// ready reports whether the store opened. Bound methods call this first so a
// failed startup yields a clear error instead of a nil pointer panic.
func (a *App) ready() error {
	if a.store == nil {
		return errStoreUnavailable
	}
	return nil
}
