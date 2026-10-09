package desktop

import (
	"errors"
	"strconv"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/peltonapp/Pelton/internal/logging"
	"github.com/peltonapp/Pelton/internal/storage"
)

// ui setting keys. theme and editor mode already exist in storage; the rest are
// ui-only and defined here. raw strings keep the contract in one place next to
// the defaults.
const (
	settingAccent       = "accent"
	settingDensity      = "density"
	settingShowBadge    = "show_mailbox_badge"
	settingShowDateTime = "show_datetime"
	settingShowPGP      = "show_pgp"
	// settingIndexDecrypted lets search see inside encrypted mail. Off by
	// default: the search index is an ordinary file on disk, so indexing
	// decrypted text writes the plaintext there and gives up much of what the
	// encryption was for. Toggling it rebuilds the index either way, so turning
	// it off actually removes the plaintext rather than merely stopping additions.
	settingIndexDecrypted = "search_index_decrypted"
	// the sort order search results come back in, remembered separately for the
	// three shapes a query takes (#404). "auto" lets the ui pick from the query:
	// free text has scores worth ranking by, a query built only from chips does
	// not, and a date window says the user is thinking chronologically. Splitting
	// the memory three ways is what keeps a choice made for one kind of search
	// from following the user onto a search where it makes no sense.
	settingSearchSortText     = "search_sort_text"
	settingSearchSortDated    = "search_sort_dated"
	settingSearchSortFiltered = "search_sort_filtered"
	settingShowAuth           = "show_auth"
	settingToastPosition      = "toast_position"
	settingPaneLocked         = "pane_locked"
	settingSidebarWidth       = "sidebar_width"
	settingListWidth          = "list_width"
	settingSendDelay          = "send_delay_seconds"
	settingFlagHighlight      = "flag_highlight"
	settingShortcutHints      = "show_shortcut_hints"
	settingAccountEmail       = "show_account_email"
	settingRemoteAlways       = "remote_images_always"
	// settingBlockTrackers keeps images that look like tracking pixels blocked
	// even once remote content is loaded, so seeing a newsletter's pictures does
	// not also confirm the open to the sender (#205). Off by default, since the
	// detection is a heuristic that will sometimes be wrong.
	settingBlockTrackers = "block_tracking_pixels"
	// settingSenderFonts keeps the font families a message asks for. On by
	// default: every other mail client honours them, and a named family cannot
	// fetch anything (the reading pane's csp limits font-src to data:). Off puts
	// every message in the reader font.
	settingSenderFonts = "sender_fonts"
	// settingHarvestAddresses keeps learning addresses from mail for compose
	// autocomplete. On by default, since it is what autocomplete was before
	// there were contacts. Off leaves only the synced address books, which is
	// what someone who maintains a real one asked for (#168).
	settingHarvestAddresses = "harvest_addresses"
	settingAvatarSource     = "avatar_source"
	settingAvatarStyle      = "avatar_style"
	settingMultiSelect      = "multi_select_enabled"
	settingSelectedCount    = "show_selected_count"
	// settingSelectAllScope is how far select-all reaches. The default offers
	// the rest rather than taking it: a mailbox holds more than the pages that
	// were scrolled to, and silently selecting all of it is not what a click on
	// a checkbox looks like it does.
	settingSelectAllScope = "select_all_scope"
	// settingSelectAllUnified offers select-all in the unified views too. Off by
	// default: those span every account, so "everything in this list" is a much
	// bigger claim there than in one mailbox.
	settingSelectAllUnified = "select_all_unified"
	settingIndentGuides     = "sidebar_indent_guides"
	settingRowTemplate      = "row_template"
	settingRowAvatar        = "row_show_avatar"
	settingRowSnippet       = "row_show_snippet"
	settingPreviewLines     = "preview_lines"
	settingUIScale          = "ui_scale"
	settingMessageFont      = "message_font_size"
	settingFlaggedCount     = "show_flagged_count"
	settingViewsPlacement   = "views_placement"
	// newer feature settings.
	settingFlagColorSync       = "flag_color_sync"
	settingShowOffline         = "show_offline_indicator"
	settingShowUnsyncedFolder  = "show_unsynced_folder"
	settingRestoreTabs         = "restore_reading_tabs"
	settingPaletteProfiles     = "palette_profiles"
	settingSwipeEnabled        = "swipe_enabled"
	settingSwipeLeft           = "swipe_left_action"
	settingSwipeRight          = "swipe_right_action"
	settingVimMode             = "compose_vim_mode"
	settingDownloadAtts        = "download_include_attachments"
	settingDownloadPending     = "download_pending_since"
	settingAppVimMode          = "app_vim_mode"
	settingLanguage            = "language"
	settingLowPower            = "low_power_mode"
	settingAutoSync            = "auto_sync_interval_seconds"
	settingDefaultEditor       = "default_editor_mode"
	settingComposeAutocomplete = "compose_autocomplete"
	settingComposeChips        = "compose_chips"
	settingEmptyStateImage     = "empty_state_image"
	settingEmptyStateFull      = "empty_state_fullscreen"
	// menu bar settings only matter on macOS: Windows/Linux always use the
	// in-app bar (no native menu is created there at all).
	settingMenuBarInApp         = "menu_bar_in_app"
	settingMenuBarNativeMinimal = "menu_bar_native_minimal"
	// icons in the in-app menu bar's dropdowns; off keeps the classic
	// text-only native look.
	settingMenuBarIcons = "menu_bar_icons"
	// clock preference for rendered times: auto (locale), 12, or 24.
	settingTimeFormat = "time_format"
	// disable ui transitions and animations (the os-level preference is
	// honored by the frontend css regardless).
	settingReduceMotion = "reduce_motion"
	// show the browser hand over clickable chrome instead of the native arrow.
	// hyperlinks keep the hand either way.
	settingHandCursor = "hand_cursor"
	// show the unread count on the dock icon on macOS, or the unread dot on
	// the tray icon on Windows and Linux. Stored under its original name, from
	// when only the dock showed it, so nobody who turned it off gets it back.
	settingUnreadBadge = "dock_badge"
	// dark window bounds ("HH:MM") for the schedule theme mode.
	settingThemeDarkStart = "theme_dark_start"
	settingThemeDarkEnd   = "theme_dark_end"
	// reader fallback font for mail bodies (a key from the frontend's curated
	// list; mail that declares its own fonts keeps them).
	settingBodyFont = "body_font"
	// interface and monospace fonts (#58): keys from the frontend's curated
	// lists or "sys:<family>" for an installed font, overriding the --font-ui
	// and --font-mono tokens.
	settingUIFont   = "ui_font"
	settingMonoFont = "mono_font"
	// corner style for controls and cards: default, square, or round (#60).
	settingCornerStyle = "corner_style"
	// general new-mail OS notifications (#126). VIP senders are stored
	// separately in bind_vip.go and notify even when this is off.
	settingNotifyNewMail = "notify_new_mail"
	// verbose sync surfaces the mailbox currently being synced in the status
	// line instead of a plain "Syncing" (#128). Off by default.
	settingVerboseSync = "verbose_sync"
	// settingSyncProgress shows a progress bar with real message counts while a
	// sync or a backfill runs, rather than a spinner (#313).
	settingSyncProgress = "sync_progress_bar"
	// what the window's close button does: "background" (default) or "quit".
	// See close.go.
	settingCloseAction = "close_button_action"
	// how many of a folder's newest message bodies a first sync fetches, and
	// whether reaching the end of the list pulls the next batch automatically
	// (#175). 0 messages means no limit: sync the whole mailbox as older
	// versions did.
	settingSyncMessageLimit = "sync_message_limit"
	settingSyncAutoBackfill = "sync_auto_backfill"
	// how many sync connections (IMAP sessions) one account may use at once.
	// Sending and push stay outside this pool. Clamped to
	// [minSyncMaxParallel, maxSyncMaxParallel] on write and on read.
	settingSyncMaxParallel = "sync_max_parallel"
	// how many days a folder may go without a full reconcile before the
	// startup sync re-lists it in full in the background. 0 means only manual
	// Sync does that. Clamped to [0, maxSyncFullReconcileDays].
	settingSyncFullReconcileDays = "sync_full_reconcile_days"
	// the order of the unified views block in the sidebar, as a comma-separated
	// list of view keys (#187). The views have no rows of their own, so unlike
	// folders and accounts their order lives here. Empty means the built-in
	// order.
	settingUnifiedViewOrder = "sidebar_unified_view_order"
	// what Pelton opens on launch: "view:<key>", "folder:<id>", or "last" to
	// restore whatever was open when the app last closed. settingLastSelection
	// holds that remembered selection in the same "view:"/"folder:" form.
	settingStartupSelection = "startup_selection"
	settingLastSelection    = "last_selection"
)

// defaultStartupSelection is the unified inbox, which is where every version
// before #187 opened.
const defaultStartupSelection = "view:" + viewInbox

// settingUpdateCheckFreq, settingLastUpdateCheck and defaultUpdateCheckFrequency
// are defined in bind_update.go, next to the rest of the update-check logic.

// defaults for the ui preferences, applied server side so the frontend always
// receives a complete object on startup.
const (
	defaultTheme         = "system"
	defaultAccent        = "#465AF2"
	defaultDensity       = "medium"
	defaultToastPosition = "bottom-right"
	defaultSidebarWidth  = 264
	defaultListWidth     = 380
	// flag highlight styles: flag (icon only), left, both, off.
	defaultFlagHighlight = "flag"
	// avatar source preference (fallback chain): bimi_gravatar, gravatar_bimi, bimi, or
	// pfp (generated only, no network).
	defaultAvatarSource = "bimi_gravatar"
	// generated placeholder style: initials, mono, pixel, geometric.
	defaultAvatarStyle = "initials"
	// search result order: auto, relevance, newest, oldest, subjectAsc,
	// subjectDesc. "auto" reads the order off the query rather than fixing one.
	defaultSearchSort = "auto"
	// list row template: relaxed (avatar + 3 lines), comfortable (3 lines),
	// compact (2 lines), single (1 line).
	defaultRowTemplate = "relaxed"
	// how many snippet preview lines to show where the template allows it.
	defaultPreviewLines = 1
	// interface zoom factor as a string multiplier ("1" = 100%).
	defaultUIScale = "1"
	// base font size (px) for rendered email content.
	defaultMessageFont = 14
	// how many of a folder's newest message bodies a first sync fetches. Older
	// bodies stay on the server until asked for. 0 means no limit.
	defaultSyncMessageLimit = 100
	// parallel sync connections per account: IMAP sync sessions. Sending and
	// push do not consume a slot.
	defaultSyncMaxParallel = 3
	minSyncMaxParallel     = 1
	maxSyncMaxParallel     = 5
	// days between background full reconciles of a folder, and the largest
	// value the setting accepts.
	defaultSyncFullReconcileDays = 7
	maxSyncFullReconcileDays     = 365
)

// UIPrefsDTO is the complete set of user-facing preferences this step exposes:
// theme, accent, density and the per-row technical-info toggles. Other settings
// come later.
type UIPrefsDTO struct {
	Theme            string `json:"theme"`
	Accent           string `json:"accent"`
	Density          string `json:"density"`
	ShowMailboxBadge bool   `json:"showMailboxBadge"`
	ShowDateTime     bool   `json:"showDateTime"`
	ShowPGP          bool   `json:"showPgp"`
	ShowAuth         bool   `json:"showAuth"`
	// layout and chrome preferences.
	ToastPosition string `json:"toastPosition"`
	PaneLocked    bool   `json:"paneLocked"`
	SidebarWidth  int    `json:"sidebarWidth"`
	ListWidth     int    `json:"listWidth"`
	// SendDelaySeconds holds an outgoing message for this many seconds so the user
	// can undo. 0 disables the delay (send immediately).
	SendDelaySeconds int `json:"sendDelaySeconds"`
	// FlagHighlight controls how flagged messages stand out in the list:
	// flag (icon only), left, right, both (colored edge bars), or off.
	FlagHighlight string `json:"flagHighlight"`
	// ShowShortcutHints shows inline keyboard shortcut chips in the ui. Off by
	// default to keep the interface clean.
	ShowShortcutHints bool `json:"showShortcutHints"`
	// HarvestAddresses keeps learning addresses from the mail that passes
	// through, for compose autocomplete. Off leaves only synced contacts.
	HarvestAddresses bool `json:"harvestAddresses"`
	// ShowAccountEmail shows the account email instead of its display name in the
	// sidebar account header.
	ShowAccountEmail bool `json:"showAccountEmail"`
	// AlwaysLoadImages disables remote-image blocking globally. Off by default;
	// the ui guards turning it on with a tracking warning.
	AlwaysLoadImages bool `json:"alwaysLoadImages"`
	// BlockTrackingPixels keeps detected tracking pixels blocked even when the
	// rest of a message's remote content is loaded. Off by default; the private
	// preset in onboarding turns it on.
	BlockTrackingPixels bool `json:"blockTrackingPixels"`
	// AvatarSource selects the sender-photo fallback chain: bimi_gravatar,
	// gravatar_bimi, or pfp (generated only). AvatarStyle picks the generated
	// placeholder look: initials, mono, pixel, or geometric.
	AvatarSource string `json:"avatarSource"`
	AvatarStyle  string `json:"avatarStyle"`
	// MultiSelectEnabled allows selecting several messages at once for bulk
	// actions. ShowSelectedCount toggles the "N selected" count text in the
	// selection bar; both are independent so the count can be hidden while
	// multi-select stays on.
	MultiSelectEnabled bool `json:"multiSelectEnabled"`
	ShowSelectedCount  bool `json:"showSelectedCount"`
	// SelectAllScope is how far select-all reaches: "offer" selects the loaded
	// messages and offers the rest, "all" takes the whole list at once, "loaded"
	// stops at what is on screen.
	SelectAllScope string `json:"selectAllScope"`
	// SelectAllUnified offers select-all in the unified views as well. Off by
	// default: a unified list spans every account.
	SelectAllUnified bool `json:"selectAllUnified"`
	// SearchSortText/Dated/Filtered are the remembered sort order for each shape
	// of search: words typed, a date window set, and chips only. "auto" (the
	// default) leaves the choice to the query.
	SearchSortText     string `json:"searchSortText"`
	SearchSortDated    string `json:"searchSortDated"`
	SearchSortFiltered string `json:"searchSortFiltered"`
	// SidebarIndentGuides draws vertical guide lines for nested folders.
	SidebarIndentGuides bool `json:"sidebarIndentGuides"`
	// RowTemplate selects the message-list row layout: relaxed, comfortable,
	// compact, or single. RowShowAvatar/RowShowSnippet are per-field overrides and
	// PreviewLines clamps the snippet to that many lines.
	RowTemplate    string `json:"rowTemplate"`
	RowShowAvatar  bool   `json:"rowShowAvatar"`
	RowShowSnippet bool   `json:"rowShowSnippet"`
	PreviewLines   int    `json:"previewLines"`
	// UIScale zooms the whole interface (a string multiplier, "1" = 100%).
	// MessageFontSize sets the base font size in px for rendered email content.
	UIScale         string `json:"uiScale"`
	MessageFontSize int    `json:"messageFontSize"`
	// ViewsPlacement controls how saved Views (preset searches) are surfaced:
	// "hidden" (default, feature off), "sidebar" (a group in the mailbox
	// sidebar), or "tab" (a separate Views tab/rail).
	ViewsPlacement string `json:"viewsPlacement"`
	// ShowFlaggedCount shows the count and bold styling on the sidebar Flagged
	// view. Off keeps the entry but renders it plain.
	ShowFlaggedCount bool `json:"showFlaggedCount"`
	// FlagColorSync pushes color labels to the server as imap keywords so they
	// show in other clients. Off keeps colors local only.
	FlagColorSync bool `json:"flagColorSync"`
	// ShowOfflineIndicator shows the little downloaded/offline badge on pinned
	// messages. On by default; can be hidden.
	ShowOfflineIndicator bool `json:"showOfflineIndicator"`
	// ShowUnsyncedFolder marks folders the user excluded from sync in the
	// sidebar, so a folder that stopped receiving new mail says why.
	ShowUnsyncedFolder bool `json:"showUnsyncedFolder"`
	// PaletteProfiles lists every profile directly in the command palette, so
	// one is a keystroke away rather than behind the switcher step. Off by
	// default: with a single profile the entries would be noise.
	PaletteProfiles bool `json:"paletteProfiles"`
	// RestoreTabs brings back the reading-pane tabs that were open at quit.
	// Off by default: a tab is a temporary place to park a message, and most
	// of them are finished with by the time the app closes.
	RestoreTabs bool `json:"restoreTabs"`
	// Swipe gestures on message rows (trackpad only). SwipeEnabled turns them on;
	// SwipeLeftAction/SwipeRightAction pick what each direction does
	// (delete, unread, read, flag, archive, snooze, none).
	SwipeEnabled     bool   `json:"swipeEnabled"`
	SwipeLeftAction  string `json:"swipeLeftAction"`
	SwipeRightAction string `json:"swipeRightAction"`
	// ComposeVimMode enables vim keybindings in the compose editor.
	ComposeVimMode bool `json:"composeVimMode"`
	// DownloadIncludeAttachments is the remembered default for the bulk range
	// download's per-run attachment choice.
	DownloadIncludeAttachments bool `json:"downloadIncludeAttachments"`
	// AppVimMode enables global vim-style navigation (h/j/k/l and friends) for
	// moving around the app window itself, outside of compose.
	AppVimMode bool `json:"appVimMode"`
	// Language is the ui locale code (en, de, fr, nl, es, etc). Defaults to English;
	// the frontend only ever picks something else on an explicit user choice.
	Language string `json:"language"`
	// LowPowerMode pauses periodic auto-sync, blocks starting new bulk offline
	// downloads, and skips the post-sync address-book rescan.
	LowPowerMode bool `json:"lowPowerMode"`
	// AutoSyncIntervalSeconds is how often every account gets a full sync pass,
	// on top of the always-on imap idle push. 0 disables it.
	AutoSyncIntervalSeconds int `json:"autoSyncIntervalSeconds"`
	// DefaultEditorMode is the editor a new compose session starts in:
	// plaintext, markdown, or wysiwyg.
	DefaultEditorMode string `json:"defaultEditorMode"`
	// ComposeAutocomplete offers address-book suggestions while typing a
	// recipient. On by default.
	ComposeAutocomplete bool `json:"composeAutocomplete"`
	// ComposeChips renders recipients as removable chips. When off, the
	// recipient fields fall back to a plain comma-separated text input.
	ComposeChips bool `json:"composeChips"`
	// UpdateCheckFrequency controls the automatic GitHub-releases update
	// check: off (default), startup (every launch), weekly, or monthly. A
	// manual check ("Check now" in settings) always runs regardless.
	UpdateCheckFrequency string `json:"updateCheckFrequency"`
	// EmptyStateImage is a data-uri image shown in the reading pane when no
	// message is open. Empty means the bundled Pelton logo.
	EmptyStateImage string `json:"emptyStateImage"`
	// EmptyStateFullscreen shows the empty-state image as a full-bleed cover
	// background instead of a small centered mark. Off by default.
	EmptyStateFullscreen bool `json:"emptyStateFullscreen"`
	// CornerStyle picks the corner radius look: default, square, or round.
	CornerStyle string `json:"cornerStyle"`
	// ThemeID selects an installed custom theme (see bind_themes.go). Empty
	// means the built-in default themes driven by the Theme setting.
	ThemeID string `json:"themeId"`
	// MenuBarInApp shows the in-app menu bar on macOS (it is always shown on
	// Windows/Linux regardless of this). MenuBarNativeMinimal then reduces the
	// native macOS menu to the app menu, dropping the duplicated submenus.
	MenuBarInApp         bool `json:"menuBarInApp"`
	MenuBarNativeMinimal bool `json:"menuBarNativeMinimal"`
	// MenuBarIcons shows icons next to the in-app menu bar's dropdown items.
	MenuBarIcons bool `json:"menuBarIcons"`
	// TimeFormat picks the clock for rendered times: auto (locale), 12, or 24.
	TimeFormat string `json:"timeFormat"`
	// ReduceMotion disables ui transitions and animations.
	ReduceMotion bool `json:"reduceMotion"`
	// HandCursor shows the browser hand over clickable chrome instead of the
	// native arrow.
	HandCursor bool `json:"handCursor"`
	// UnreadBadge shows the unread count on the dock icon on macOS, or the unread
	// dot on the tray icon on Windows and Linux.
	UnreadBadge bool `json:"unreadBadge"`
	// ThemeDarkStart/ThemeDarkEnd bound the dark window ("HH:MM") for the
	// schedule theme mode.
	ThemeDarkStart string `json:"themeDarkStart"`
	ThemeDarkEnd   string `json:"themeDarkEnd"`
	// BodyFont is the reader fallback font for mail bodies.
	BodyFont string `json:"bodyFont"`
	// UIFont and MonoFont override the interface and monospace font tokens.
	UIFont   string `json:"uiFont"`
	MonoFont string `json:"monoFont"`
	// SenderFonts lets a message use the font families it asks for. Off renders
	// every message in BodyFont instead.
	SenderFonts bool `json:"senderFonts"`
	// NotifyNewMail raises a native OS notification when new mail lands in an
	// inbox. Off by default. VIP-sender notifications fire regardless of this
	// (see bind_vip.go), so important senders cut through when it is off.
	NotifyNewMail bool `json:"notifyNewMail"`
	// VerboseSync shows which mailbox is currently syncing in the status line,
	// with the account and server it is talking to. SyncProgressBar puts a real
	// progress bar next to it, counting message bodies rather than mailboxes.
	VerboseSync     bool `json:"verboseSync"`
	SyncProgressBar bool `json:"syncProgressBar"`
	// CloseAction is what the window's close button does: "background" keeps
	// Pelton running and syncing with the window hidden, "quit" exits.
	CloseAction string `json:"closeAction"`
	// SyncMessageLimit caps how many of a folder's newest message bodies the
	// first sync fetches; older bodies stay on the server until asked for. 0
	// means no limit.
	// SyncAutoBackfill fetches the next batch automatically on reaching the end
	// of the list; off puts it behind a button instead.
	SyncMessageLimit int  `json:"syncMessageLimit"`
	SyncAutoBackfill bool `json:"syncAutoBackfill"`
	// SyncMaxParallel is how many sync connections one account may use at once:
	// IMAP sync sessions. Clamped to 1–5. Sending and new-mail push do not consume a slot.
	SyncMaxParallel int `json:"syncMaxParallel"`
	// SyncFullReconcileDays is how many days a folder may go without a full
	// reconcile before startup re-checks it in the background; 0 means only
	// manual Sync does.
	SyncFullReconcileDays int `json:"syncFullReconcileDays"`
	// StartupSelection is what the sidebar selects on launch: "view:<key>" for a
	// unified view, "folder:<id>" for one account folder, or "last" to restore
	// the previous session. A target that no longer exists falls back to the
	// unified inbox.
	StartupSelection string `json:"startupSelection"`
	// LogToFile writes the app's own log to a rotating file in the data
	// directory, at LogLevel. LogMessageMetadata additionally allows subjects
	// and senders into it for debugging sync. CrashLogs leaves a stack behind
	// when the app panics. All off by default on a stable build; a nightly
	// defaults the file log and crash reports on. Nothing is ever uploaded.
	LogToFile          bool   `json:"logToFile"`
	LogLevel           string `json:"logLevel"`
	LogMessageMetadata bool   `json:"logMessageMetadata"`
	CrashLogs          bool   `json:"crashLogs"`
}

// GetUIPrefs returns all ui preferences with defaults filled in, so startup is a
// single round trip and the frontend never has to know the default values.
func (a *App) GetUIPrefs() (UIPrefsDTO, error) {
	if err := a.ready(); err != nil {
		return UIPrefsDTO{}, err
	}
	return UIPrefsDTO{
		Theme:               a.stringSetting(storage.SettingTheme, defaultTheme),
		Accent:              a.stringSetting(settingAccent, defaultAccent),
		Density:             a.stringSetting(settingDensity, defaultDensity),
		ShowMailboxBadge:    a.boolSetting(settingShowBadge, true),
		ShowDateTime:        a.boolSetting(settingShowDateTime, true),
		ShowPGP:             a.boolSetting(settingShowPGP, true),
		ShowAuth:            a.boolSetting(settingShowAuth, true),
		ToastPosition:       a.stringSetting(settingToastPosition, defaultToastPosition),
		PaneLocked:          a.boolSetting(settingPaneLocked, false),
		SidebarWidth:        a.intSetting(settingSidebarWidth, defaultSidebarWidth),
		ListWidth:           a.intSetting(settingListWidth, defaultListWidth),
		SendDelaySeconds:    a.intSetting(settingSendDelay, 0),
		FlagHighlight:       a.stringSetting(settingFlagHighlight, defaultFlagHighlight),
		ShowShortcutHints:   a.boolSetting(settingShortcutHints, true),
		HarvestAddresses:    a.harvestAddresses(),
		ShowAccountEmail:    a.boolSetting(settingAccountEmail, false),
		AlwaysLoadImages:    a.boolSetting(settingRemoteAlways, false),
		BlockTrackingPixels: a.blockTrackers(),
		AvatarSource:        a.stringSetting(settingAvatarSource, defaultAvatarSource),
		AvatarStyle:         a.stringSetting(settingAvatarStyle, defaultAvatarStyle),
		MultiSelectEnabled:  a.boolSetting(settingMultiSelect, true),
		ShowSelectedCount:   a.boolSetting(settingSelectedCount, true),
		SelectAllScope:      a.stringSetting(settingSelectAllScope, "offer"),
		SelectAllUnified:    a.boolSetting(settingSelectAllUnified, false),
		SearchSortText:      a.stringSetting(settingSearchSortText, defaultSearchSort),
		SearchSortDated:     a.stringSetting(settingSearchSortDated, defaultSearchSort),
		SearchSortFiltered:  a.stringSetting(settingSearchSortFiltered, defaultSearchSort),
		SidebarIndentGuides: a.boolSetting(settingIndentGuides, false),
		RowTemplate:         a.stringSetting(settingRowTemplate, defaultRowTemplate),
		RowShowAvatar:       a.boolSetting(settingRowAvatar, true),
		RowShowSnippet:      a.boolSetting(settingRowSnippet, true),
		PreviewLines:        a.intSetting(settingPreviewLines, defaultPreviewLines),
		UIScale:             a.stringSetting(settingUIScale, defaultUIScale),
		MessageFontSize:     a.intSetting(settingMessageFont, defaultMessageFont),
		ShowFlaggedCount:    a.boolSetting(settingFlaggedCount, true),
		ViewsPlacement:      a.stringSetting(settingViewsPlacement, "hidden"),

		FlagColorSync:              a.boolSetting(settingFlagColorSync, false),
		ShowOfflineIndicator:       a.boolSetting(settingShowOffline, true),
		ShowUnsyncedFolder:         a.boolSetting(settingShowUnsyncedFolder, true),
		RestoreTabs:                a.boolSetting(settingRestoreTabs, false),
		PaletteProfiles:            a.boolSetting(settingPaletteProfiles, false),
		SwipeEnabled:               a.boolSetting(settingSwipeEnabled, true),
		SwipeLeftAction:            a.stringSetting(settingSwipeLeft, "delete"),
		SwipeRightAction:           a.stringSetting(settingSwipeRight, "unread"),
		ComposeVimMode:             a.boolSetting(settingVimMode, false),
		DownloadIncludeAttachments: a.boolSetting(settingDownloadAtts, true),
		AppVimMode:                 a.boolSetting(settingAppVimMode, false),
		Language:                   a.stringSetting(settingLanguage, "en"),
		LowPowerMode:               a.boolSetting(settingLowPower, false),
		AutoSyncIntervalSeconds:    a.intSetting(settingAutoSync, 900),
		DefaultEditorMode:          a.stringSetting(settingDefaultEditor, "plaintext"),
		ComposeAutocomplete:        a.boolSetting(settingComposeAutocomplete, true),
		ComposeChips:               a.boolSetting(settingComposeChips, true),
		UpdateCheckFrequency:       a.stringSetting(settingUpdateCheckFreq, defaultUpdateCheckFrequency),
		EmptyStateImage:            a.stringSetting(settingEmptyStateImage, ""),
		EmptyStateFullscreen:       a.boolSetting(settingEmptyStateFull, false),
		CornerStyle:                a.stringSetting(settingCornerStyle, "default"),
		ThemeID:                    a.stringSetting(settingThemeID, ""),
		MenuBarInApp:               a.boolSetting(settingMenuBarInApp, false),
		MenuBarNativeMinimal:       a.boolSetting(settingMenuBarNativeMinimal, false),
		MenuBarIcons:               a.boolSetting(settingMenuBarIcons, false),
		TimeFormat:                 a.stringSetting(settingTimeFormat, "auto"),
		ReduceMotion:               a.boolSetting(settingReduceMotion, false),
		HandCursor:                 a.boolSetting(settingHandCursor, false),
		UnreadBadge:                a.boolSetting(settingUnreadBadge, true),
		ThemeDarkStart:             a.stringSetting(settingThemeDarkStart, "19:00"),
		ThemeDarkEnd:               a.stringSetting(settingThemeDarkEnd, "07:00"),
		BodyFont:                   a.stringSetting(settingBodyFont, "default"),
		UIFont:                     a.stringSetting(settingUIFont, "default"),
		MonoFont:                   a.stringSetting(settingMonoFont, "default"),
		SenderFonts:                a.senderFonts(),
		NotifyNewMail:              a.boolSetting(settingNotifyNewMail, false),
		VerboseSync:                a.boolSetting(settingVerboseSync, false),
		SyncProgressBar:            a.boolSetting(settingSyncProgress, true),
		CloseAction:                a.stringSetting(settingCloseAction, closeActionBackground),
		SyncMessageLimit:           a.syncMessageLimit(),
		SyncAutoBackfill:           a.boolSetting(settingSyncAutoBackfill, true),
		SyncMaxParallel:            a.syncMaxParallel(),
		SyncFullReconcileDays:      a.fullReconcileDays(),
		StartupSelection:           a.stringSetting(settingStartupSelection, defaultStartupSelection),
		LogToFile:                  a.logsOn(),
		LogLevel:                   logging.LevelName(a.logLevel()),
		LogMessageMetadata:         a.boolSetting(settingLogMessageMetadata, false),
		CrashLogs:                  a.crashLogsOn(),
	}, nil
}

// GetSetting returns a single setting's raw value. found is false when the key
// has never been written, so the frontend can fall back to its own default.
func (a *App) GetSetting(key string) (SettingResult, error) {
	if err := a.ready(); err != nil {
		return SettingResult{}, err
	}
	value, err := a.store.Get(a.ctx, key)
	if isSettingMissing(err) {
		return SettingResult{Found: false}, nil
	}
	if err != nil {
		return SettingResult{}, err
	}
	return SettingResult{Value: value, Found: true}, nil
}

// SetSetting writes a single setting. The frontend uses the known keys above
// (theme/accent/density/show_*) plus editor_mode.
func (a *App) SetSetting(key, value string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if key == settingSyncMaxParallel {
		n, err := strconv.Atoi(value)
		if err != nil {
			n = defaultSyncMaxParallel
		}
		value = strconv.Itoa(clampSyncMaxParallel(n))
	}
	if key == settingSyncFullReconcileDays {
		n, err := strconv.Atoi(value)
		if err != nil {
			n = defaultSyncFullReconcileDays
		}
		value = strconv.Itoa(clampFullReconcileDays(n))
	}
	var limitRaisedFrom, limitRaisedTo int
	var raiseMessageLimit bool
	if key == settingSyncMessageLimit {
		limitRaisedFrom = a.syncMessageLimit()
		n, err := strconv.Atoi(value)
		if err != nil {
			n = defaultSyncMessageLimit
		}
		if n < 0 {
			n = 0
		}
		limitRaisedTo = n
		value = strconv.Itoa(n)
		raiseMessageLimit = syncMessageLimitExpanded(limitRaisedFrom, limitRaisedTo)
	}
	if err := a.store.Set(a.ctx, key, value); err != nil {
		return err
	}
	if raiseMessageLimit {
		from, to := limitRaisedFrom, limitRaisedTo
		goSafe("expanding sync window", func() { a.enqueueSyncLimitDelta(from, to) })
	}
	if key == settingSyncMaxParallel {
		a.reconfigureSyncPools()
	}
	if key == storage.SettingTheme {
		a.applyNativeTheme(value)
	}
	if key == settingLanguage || key == settingMenuBarInApp || key == settingMenuBarNativeMinimal {
		a.RebuildMenu()
	}
	if key == settingLogToFile || key == settingLogLevel || key == settingLogMessageMetadata || key == settingCrashLogs {
		a.applyLogSettings()
	}
	if key == settingCharsetFallback {
		a.applyCharsetFallback()
	}
	if key == settingUnreadBadge || key == settingLanguage {
		a.applyUnreadBadge()
	}
	if key == settingIndexDecrypted {
		// rebuilt from scratch rather than re-indexed in place: switching this
		// off has to remove the plaintext already written, and overwriting
		// documents would leave it in the index's older segments.
		goSafe("rebuilding the search index", func() { _ = a.rebuildSearchIndex() })
	}
	return nil
}

// applyNativeTheme pushes the app's theme choice onto the native window
// chrome (title bar/menu strip on Windows; no-op on macOS/Linux).
func (a *App) applyNativeTheme(theme string) {
	switch theme {
	case "dark":
		wailsruntime.WindowSetDarkTheme(a.ctx)
	case "light":
		wailsruntime.WindowSetLightTheme(a.ctx)
	default:
		wailsruntime.WindowSetSystemDefaultTheme(a.ctx)
	}
}

// SettingResult is a setting lookup that distinguishes unset from empty.
type SettingResult struct {
	Value string `json:"value"`
	Found bool   `json:"found"`
}

// stringSetting reads a string setting, returning def when unset, on error, or
// if the store hasn't opened yet (domReady can fire before startup finishes
// opening a large store, see app.go) so startup never fails on a missing
// preference.
func (a *App) stringSetting(key, def string) string {
	if a.store == nil {
		return def
	}
	value, err := a.store.Get(a.ctx, key)
	if err != nil || value == "" {
		return def
	}
	return value
}

// boolSetting reads a bool setting, returning def when unset, unparsable, or
// if the store hasn't opened yet.
func (a *App) boolSetting(key string, def bool) bool {
	if a.store == nil {
		return def
	}
	value, err := a.store.GetBool(a.ctx, key)
	if err != nil {
		return def
	}
	return value
}

// intSetting reads an int setting, returning def when unset, unparsable, or if
// the store hasn't opened yet.
func (a *App) intSetting(key string, def int) int {
	if a.store == nil {
		return def
	}
	value, err := a.store.GetInt(a.ctx, key)
	if err != nil {
		return def
	}
	return value
}

// clampSyncMaxParallel keeps a parallel-sync setting inside 1–5.
func clampSyncMaxParallel(n int) int {
	if n < minSyncMaxParallel {
		return minSyncMaxParallel
	}
	if n > maxSyncMaxParallel {
		return maxSyncMaxParallel
	}
	return n
}

// syncMessageLimitExpanded reports whether newLimit admits more local bodies
// than oldLimit. Zero means unlimited (all local bodies per protocol rules).
func syncMessageLimitExpanded(oldLimit, newLimit int) bool {
	if newLimit == oldLimit {
		return false
	}
	if oldLimit == 0 {
		return false
	}
	if newLimit == 0 {
		return true
	}
	return newLimit > oldLimit
}

// syncMaxParallel is how many sync connections one account may use at once.
// An unset or unparsable value is the default; a stored value outside 1–5 is
// clamped so a hand-edited setting cannot open more sessions than the pool
// allows.
func (a *App) syncMaxParallel() int {
	return clampSyncMaxParallel(a.intSetting(settingSyncMaxParallel, defaultSyncMaxParallel))
}

// accountSyncMaxParallel is how many sync connections one account may use: its
// own override when set, otherwise the global setting, clamped to 1–5.
func (a *App) accountSyncMaxParallel(accountID int64) int {
	if a.store != nil {
		if acct, err := a.store.GetAccount(a.ctx, accountID); err == nil && acct.SyncMaxParallel != nil {
			return clampSyncMaxParallel(*acct.SyncMaxParallel)
		}
	}
	return a.syncMaxParallel()
}

// reconfigureSyncPools pushes the current parallelism onto every running pool.
// Accounts with an override keep it; they land on the same value either way.
func (a *App) reconfigureSyncPools() {
	a.syncsMu.Lock()
	ids := make([]int64, 0, len(a.syncs))
	for id := range a.syncs {
		ids = append(ids, id)
	}
	a.syncsMu.Unlock()
	for _, id := range ids {
		a.reconfigureAccountPool(id)
	}
}

// reconfigureAccountPool re-reads one account's parallelism into its running
// pool, if it has one. When the user changed it, the adaptive throttle starts
// over at the new size: SetConfigured alone only ever lowers the effective
// size, so a raise would not take effect until a success streak grew it back.
// A save that leaves the size as it was keeps any throttle in force.
func (a *App) reconfigureAccountPool(accountID int64) {
	n := a.accountSyncMaxParallel(accountID)
	a.syncsMu.Lock()
	rt := a.syncs[accountID]
	a.syncsMu.Unlock()
	if rt == nil || rt.pool == nil {
		return
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.pool.SetConfigured(n)
	if n == rt.userN {
		return
	}
	rt.userN = n
	rt.throttleFails = 0
	rt.throttleSuccesses = 0
	rt.pool.SetEffective(n)
}

// isSettingMissing reports whether err is the not-found sentinel, so callers can
// treat an unset key as "use the default" rather than an error.
func isSettingMissing(err error) bool {
	return errors.Is(err, storage.ErrSettingNotFound)
}

// lowPowerMode reports the current low-power setting, read fresh each call
// (a local sqlite read) so background loops always see live changes without
// needing their own change-notification plumbing.
func (a *App) lowPowerMode() bool {
	return a.boolSetting(settingLowPower, false)
}
