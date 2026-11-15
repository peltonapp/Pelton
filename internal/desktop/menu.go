package desktop

import (
	goruntime "runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The native menu bar exists only on macOS. Windows and Linux use the in-app
// menu bar rendered by the frontend (MenuBar.svelte): the native bar could not
// follow the app theme there (Windows has no public dark-menubar API), never
// live-updated labels on Linux, and rebuilding it on Linux crashed inside GTK
// (see RebuildMenu). On macOS the system menu bar is mandatory, so it stays
// native; the in-app bar is an opt-in setting there, and enabling it can
// additionally reduce the native menu to the minimal app menu via
// menu_bar_native_minimal so items are not duplicated across two bars.

// initialMenu returns the menu to install at startup: the native menu on
// macOS, none elsewhere.
func (a *App) initialMenu() *application.Menu {
	if goruntime.GOOS != "darwin" {
		return nil
	}
	return a.buildMenu()
}

// buildMenu builds the native application menubar in the current language
// setting. Menu items that map to ui actions emit the menu event with a short
// action string; the frontend listens and performs the action (open settings,
// compose, sync, add mailbox). Window level items (hide, quit) act directly.
func (a *App) buildMenu() *application.Menu {
	s := menuStringsFor(a.stringSetting(settingLanguage, "en"))
	root := application.NewMenu()

	// the app menu, folded under the bold "Pelton" menu next to the apple logo.
	appMenu := root.AddSubmenu(s.appMenu)
	appMenu.Add(s.about).OnClick(a.menuAction("about"))
	appMenu.AddSeparator()
	appMenu.Add(s.preferences).SetAccelerator("CmdOrCtrl+,").OnClick(a.menuAction("preferences"))
	// profiles (#270). No accelerator: switching ships unbound so the user picks
	// their own key under Settings, Shortcuts.
	appMenu.Add(s.switchProfile).OnClick(a.menuAction("switch-profile"))
	appMenu.AddSeparator()
	appMenu.Add(s.hide).SetAccelerator("CmdOrCtrl+H").OnClick(func(*application.Context) {
		a.hideApp()
	})
	appMenu.Add(s.quit).SetAccelerator("CmdOrCtrl+Q").OnClick(func(*application.Context) {
		a.quitApp()
	})

	// with the in-app menu bar enabled and the native menu reduced, the
	// duplicated submenus are dropped: the app menu above stays (macOS always
	// shows one) and the edit menu keeps the native text-editing bindings.
	minimal := a.boolSetting(settingMenuBarInApp, false) && a.boolSetting(settingMenuBarNativeMinimal, false)

	if minimal {
		// the reduced menu carries no message items; drop stale pointers so
		// SetMailActionsEnabled does not update items of a discarded menu.
		a.mailMenuItems = nil
		// the view menu survives here, carrying only the fullscreen toggle, for
		// the same reason the edit menu below does: the accelerator exists only
		// while its item does. the in-app bar cannot pick it up either, since
		// its combo grammar has no way to say ctrl on macOS.
		a.addFullscreenItem(root.AddSubmenu(s.viewMenu), s)
	} else {
		// file menu: composing new mail and exporting the open message.
		fileMenu := root.AddSubmenu(s.fileMenu)
		fileMenu.Add(s.compose).SetAccelerator("CmdOrCtrl+N").OnClick(a.menuAction("compose"))
		fileMenu.AddSeparator()
		fileMenu.Add(s.exportPDF).SetAccelerator("CmdOrCtrl+P").OnClick(a.menuAction("export-pdf"))
		fileMenu.AddSeparator()
		// dispatched to the frontend rather than hiding the window here: what
		// Cmd+W closes depends on what is on screen, and only the frontend
		// knows that.
		fileMenu.Add(s.closeWindow).SetAccelerator("CmdOrCtrl+W").OnClick(a.menuAction("close-window"))

		// mailbox menu: mailbox-level operations - syncing and managing accounts.
		mailboxMenu := root.AddSubmenu(s.mailboxMenu)
		mailboxMenu.Add(s.syncNow).SetAccelerator("CmdOrCtrl+R").OnClick(a.menuAction("sync"))
		mailboxMenu.AddSeparator()
		mailboxMenu.Add(s.addMailbox).SetAccelerator("CmdOrCtrl+M").OnClick(a.menuAction("add-mailbox"))
		mailboxMenu.Add(s.manageMailboxes).OnClick(a.menuAction("open-mailboxes"))
		mailboxMenu.Add(s.contacts).OnClick(a.menuAction("open-contacts"))

		// mail menu: actions on the open message. Undo stays enabled (it undoes
		// the last send/delete/archive, which needs no open message), but the
		// message-level items below start disabled and are only enabled while a
		// message is open (SetMailActionsEnabled, driven by the frontend's
		// selection). Undo has no accelerator here since Cmd+Z is already
		// handled by the app's own keydown handler; binding it again would
		// double-fire.
		mailMenu := root.AddSubmenu(s.mailMenu)
		mailMenu.Add(s.undo).OnClick(a.menuAction("undo"))
		mailMenu.AddSeparator()
		a.mailMenuItems = []*application.MenuItem{
			mailMenu.Add(s.markRead).OnClick(a.menuAction("mark-read")),
			mailMenu.Add(s.markUnread).OnClick(a.menuAction("mark-unread")),
			mailMenu.Add(s.flagUnflag).OnClick(a.menuAction("flag")),
			mailMenu.Add(s.archive).OnClick(a.menuAction("archive")),
			mailMenu.Add(s.deleteMessage).OnClick(a.menuAction("delete-message")),
		}
		for _, item := range a.mailMenuItems {
			item.SetEnabled(a.mailActionsEnabled)
		}

		// view menu: a reliable fullscreen toggle (the native green button can
		// be inconsistent in some setups) plus the low-power mode toggle.
		viewMenu := root.AddSubmenu(s.viewMenu)
		a.addFullscreenItem(viewMenu, s)
		viewMenu.AddSeparator()
		viewMenu.Add(s.lowPowerMode).OnClick(a.menuAction("toggle-low-power"))
		// reading-pane tabs. No accelerators: they ship unbound so the user
		// picks their own under Settings, Shortcuts.
		viewMenu.AddSeparator()
		viewMenu.Add(s.openInTab).OnClick(a.menuAction("open-in-tab"))
		viewMenu.Add(s.closeTab).OnClick(a.menuAction("close-tab"))
	}

	// the standard edit menu gives copy/paste/select-all their native bindings,
	// which the webview needs on macos to work in inputs and the mail body.
	// the role ships with its own fixed English labels (Cut/Copy/Paste/Select
	// All/Undo/Redo), which there is no way to translate.
	root.AddRole(application.EditMenu)

	return root
}

// addFullscreenItem adds the fullscreen toggle and its accelerator to a menu.
// Shared so the reduced menu keeps the binding without duplicating the toggle.
func (a *App) addFullscreenItem(m *application.Menu, s menuStrings) {
	m.Add(s.toggleFullscreen).SetAccelerator("Ctrl+CmdOrCtrl+F").OnClick(func(*application.Context) {
		a.ToggleFullscreen()
	})
}

// RebuildMenu rebuilds and applies the native menubar in the current language
// and menu-bar settings. SetSetting triggers it when a language or menu-bar
// setting is written, so the native menu updates immediately instead of at the
// next launch. Only macOS has a native menu to rebuild; Windows/Linux use the
// in-app bar, which re-renders itself.
func (a *App) RebuildMenu() {
	if a.wapp == nil || !a.runtimeReady.Load() || goruntime.GOOS != "darwin" {
		return
	}
	menu := a.buildMenu()
	application.InvokeSync(func() {
		a.wapp.Menu.Set(menu)
	})
}

// SetMailActionsEnabled greys out or restores the native Mail menu's message-
// level items. The frontend calls it as its open message changes, so those
// actions are only selectable while a message is actually open. The chosen
// state is kept on the App so a later menu rebuild restores it instead of
// resetting every item back to disabled. It only applies where the full native
// menu exists (macOS without the reduced-menu setting); the in-app bar drives
// its item state from frontend selection directly.
func (a *App) SetMailActionsEnabled(enabled bool) {
	a.mailActionsEnabled = enabled
	if goruntime.GOOS != "darwin" || len(a.mailMenuItems) == 0 {
		return
	}
	items := a.mailMenuItems
	application.InvokeSync(func() {
		for _, item := range items {
			item.SetEnabled(enabled)
		}
	})
}

// menuAction returns a menu callback that emits the menu event with the given
// action string.
func (a *App) menuAction(action string) func(*application.Context) {
	return func(*application.Context) {
		// Cmd+W in a message window closes that window. Every other action
		// belongs to the main window, which is the only one listening.
		if action == "close-window" {
			if w := a.focusedSecondaryWindow(); w != nil {
				w.Close()
				return
			}
		}
		a.emit(EventMenu, action)
	}
}
