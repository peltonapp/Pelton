<script lang="ts">
  // the application shell: the three-column layout (resizable, lockable) plus the
  // compose panes, the settings screen, the outbox status bar and the toast
  // stack. it loads initial data, applies preferences, subscribes to backend and
  // menu events, and handles the app-wide keyboard shortcuts.
  import { onMount, onDestroy } from 'svelte'
  import { get } from 'svelte/store'
  import { t } from './lib/i18n'

  import Sidebar from './components/sidebar/Sidebar.svelte'
  import MenuBar from './components/common/MenuBar.svelte'
  import MessageList from './components/list/MessageList.svelte'
  import MessageDetail from './components/detail/MessageDetail.svelte'
  import Compose from './components/compose/Compose.svelte'
  import Toasts from './components/common/Toasts.svelte'
  import StatusBar from './components/common/StatusBar.svelte'
  import ContextMenu from './components/common/ContextMenu.svelte'
  import Resizer from './components/common/Resizer.svelte'
  import SnoozeDialog from './components/detail/SnoozeDialog.svelte'
  import ConfirmDialog from './components/common/ConfirmDialog.svelte'
  import SyncFailureDialog from './components/common/SyncFailureDialog.svelte'
  import { loadSyncStates, watchSyncStates } from './stores/syncfailures'
  import FolderDialog from './components/sidebar/FolderDialog.svelte'
  import AttachmentPreview from './components/detail/AttachmentPreview.svelte'
  import MoveDialog from './components/detail/MoveDialog.svelte'
  import AddMailboxDialog from './components/wizard/AddMailboxDialog.svelte'
  import CommandPalette from './components/common/CommandPalette.svelte'

  import { initPrefs, prefs, setPaneWidths, setLowPowerMode } from './stores/prefs'
  import { applyScale } from './theme/theme'
  import { loadSidebar, refreshSidebar, sidebar } from './stores/accounts'
  import { refreshCountsSoon } from './stores/sidebarcounts'
  import { initSidebarState } from './stores/sidebarstate'
  import { loadSignatures } from './stores/signatures'
  import { loadVIPSenders } from './stores/vip'
  import { loadVirusTotalConfig } from './stores/virustotal'
  import { loadOutbox, syncing, lastSynced, syncFolder, syncServer, syncAccount, syncCounts, syncPhase, emptySyncCounts } from './stores/outbox'
  import { selection, applyStartupSelection, searchQuery } from './stores/selection'
  import { loadList, listIsPaginated, messageList, refreshListHead } from './stores/messages'
  import { shouldReplaceListOnMailNew } from './lib/maillistrefresh'
  import { initProgress } from './stores/progress'
  import { composeSessions, openCompose, openComposeWith, initComposePrefs, openReply, openForward, requestComposeClose } from './stores/compose'
  import { openSnooze, openSnoozeMany } from './stores/snooze'
  import { patchInList, removeFromList } from './stores/messages'
  import {
    triggerSync,
    getSetting,
    setSetting,
    SettingKeys,
    exportMessagePrintView,
    setWindowTitle,
    setSeen,
    setFlagged,
    deleteMessage,
    getMessage,
    archiveMessage,
    setMailActionsEnabled,
    isDemoMode,
    isNightly,
    unsubscribeMessage,
    consumePendingMailto,
    closeWindow,
    getLogStatus,
    openCrashReport,
    titleBarDoubleClick,
    setUnreadBadge,
  } from './lib/api'
  import { BrowserOpenURL } from '../wailsjs/runtime/runtime'
  import { liabilityAccepted } from './lib/liability'
  import { setDemoActive } from './lib/demo'
  import { recordArchived } from './stores/undoarchive'
  import { applyProgressEvent, applySyncState, currentProgress, currentVerify, isSyncing, startProgressSweep, type SyncView } from './lib/syncprogress'
  import { onMailNew, onMailRepaired, onMailUpdated, onMailBodyFailed, onSyncState, onSyncProgress, onOutboxChanged, onMenu, onViewsChanged, onProfileChanged, onMailtoCompose, onAgentProposals, onOpenMessage, type Unsubscribe, type MailtoDraft } from './lib/events'
  import { refreshMessage } from './stores/message'
  import { markBodyFetchFailed, clearBodyFetchFailed } from './stores/bodyfetch'
  import { loadViews, editingView, closeViewEditor, openViewEditor, views as savedViews } from './stores/views'
  import { selectSavedView } from './stores/selection'
  import { isMac } from './lib/i18n'
  import { Quit, Hide, WindowIsFullscreen, WindowFullscreen, WindowUnfullscreen } from '../wailsjs/runtime/runtime'
  import { matchShortcut, comboHasModifier, type ShortcutAction } from './lib/shortcuts'
  import { bindings, recording, initShortcuts } from './stores/shortcuts'
  import { initMenuBar } from './stores/menubar'
  import { triggerUndo } from './stores/undosend'
  import { recordDeleted, triggerUndoDelete } from './stores/undodelete'
  import { triggerUndoArchive } from './stores/undoarchive'
  import { openMessageId, openMessage } from './stores/selection'
  import {
    visibleMessageId,
    tabs,
    activeTabId,
    openInTab,
    closeTab,
    closeActiveTab,
    clearAllTabs,
    focusTab,
    focusPane,
    restoreTabs,
  } from './stores/tabs'
  import { messageDetail } from './stores/message'
  import { profiles, currentProfile, loadProfiles, switchTo, switchRelative } from './stores/profiles'
  import { loadProposals } from './stores/agent'
  import { settingsRequest, clearSettingsRequest } from './stores/settingsnav'
  import { errorMessage, toastError, toastInfo, pushAction } from './stores/toast'
  import { friendlyError } from './lib/errors'
  import { setOnline } from './stores/network'
  import { moveTargets } from './stores/move'
  import { snoozeTarget } from './stores/snooze'
  import { previewTarget } from './stores/preview'
  import { openMove, openMoveMany } from './stores/move'
  import { selectedIds, clearSelection } from './stores/listselect'
  import { selectFolder, selectView, revealMessage } from './stores/selection'
  import { setTheme, setThemeId } from './stores/prefs'
  import { openCreateFolder, openRenameFolder, openDeleteFolder, openEmptyTrash } from './stores/folderdialog'
  import { setFolderPinned, listThemes } from './lib/api'
  import { editViewInEditor } from './stores/views'
  import {
    markSeen,
    markFlagged,
    markColor,
    toggleSenderVIP,
    setOffline,
    trashMessage,
    archive as archiveOne,
    bulkMarkSeen,
    bulkMarkFlagged,
    bulkMarkColor,
    bulkSetOffline,
    bulkTrash,
    bulkArchive,
    reportArchiveExport,
    dropDeleted,
  } from './lib/messageactions'
  import { buildCommands, mailCommands, type CommandContext, type MessageOp, type FolderOp } from './lib/commands'
  import { catalogByAction } from './lib/menuactions'
  import { shortcutLabel } from './lib/i18n'
  import {
    initPalette,
    openPaletteStep,
    togglePalette,
    parseQuery,
    paletteOpen,
    paletteMail,
    paletteQuery,
  } from './stores/palette'
  import AccountPasswordDialog from './components/settings/AccountPasswordDialog.svelte'
  import DevOverlays from './components/dev/DevOverlays.svelte'
  import {
    passwordPrompt,
    answerPasswordPrompt,
    promptForMissingPasswords,
    refreshMissingPasswords,
  } from './stores/passwordprompt'
  import type { EditorMode, MessageSummary, ThemePref, ThemeInfo, Folder, Account } from './lib/types'

  let settingsOpen = false
  // the contacts screen (#168), opened over the mail view and code-split the
  // same way settings is.
  let contactsOpen = false
  // the settings category to open on; set by menu actions that deep-link into a
  // specific section (e.g. Manage Mailboxes), null opens the default section.
  let settingsCategory: string | null = null
  let wizardOpen = false
  let onboardingOpen = false
  // installs that predate the onboarding liability step have to acknowledge it
  // once before they can carry on.
  let liabilityOpen = false
  // a nightly build warns on every launch, before anything else including
  // onboarding, and the acknowledgement is deliberately never remembered.
  let nightlyOpen = false
  const unsubscribers: Unsubscribe[] = []
  let syncView: SyncView = { progress: new Map(), verify: new Map(), running: false }
  // pushes the view into the status bar's stores. A running sync owns the line;
  // the background folder check shows only when nothing else runs.
  function publishSyncView(view: SyncView): void {
    const running = isSyncing(view)
    syncing.set(running)
    const cur = currentProgress(view.progress) ?? (running ? null : currentVerify(view))
    syncFolder.set(cur ? cur.folder : '')
    syncServer.set(cur ? cur.server : '')
    syncAccount.set(cur ? cur.accountEmail : '')
    syncPhase.set(cur?.phase ? cur.phase : '')
    syncCounts.set(
      cur
        ? {
            done: cur.done,
            total: cur.total,
            folderDone: cur.folderDone,
            folderTotal: cur.folderTotal,
            foldersDone: cur.foldersDone,
            foldersTotal: cur.foldersTotal,
          }
        : emptySyncCounts,
    )
  }

  // live pane widths. they track the persisted prefs unless the user is mid-drag,
  // so a resize feels immediate and only commits on release.
  let sidebarW = 264
  let listW = 380
  let dragging = false
  $: if (!dragging) {
    sidebarW = $prefs.sidebarWidth
    listW = $prefs.listWidth
  }
  $: locked = $prefs.paneLocked

  // the in-app menu bar is the only menu on Windows/Linux (no native menu is
  // created there); on macOS the native bar stays and the in-app one is opt-in.
  $: showMenuBar = !isMac || $prefs.menuBarInApp

  // the macOS window has no native title bar (mac.TitleBarHiddenInset), so the
  // ui paints to the top edge and keeps a strip clear for the traffic lights.
  // fullscreen hides the lights, and the strip goes with them.
  let fullscreen = false
  $: macTitlebar = isMac && !fullscreen
  $: applyTitlebar(macTitlebar)
  function applyTitlebar(on: boolean): void {
    if (on) {
      document.documentElement.dataset.titlebar = 'mac'
    } else {
      delete document.documentElement.dataset.titlebar
    }
  }

  // only a window resize can change fullscreen state, and only the window layer
  // knows the answer: wkwebview reports nothing useful about the frame.
  async function refreshFullscreen(): Promise<void> {
    if (!isMac) {
      return
    }
    fullscreen = await WindowIsFullscreen().catch(() => false)
  }

  // the native Mail menu's message actions are only usable while a message is
  // open; keep them greyed in step with the open message.
  $: setMailActionsEnabled($visibleMessageId != null)

  // the dock or tray badge follows the unified inbox, which the sidebar already
  // recomputes after a sync and after anything that changes read state, so
  // there is no second count to keep in step.
  $: setUnreadBadge($sidebar.data?.views?.find((v) => v.key === 'inbox')?.unreadCount ?? 0)

  // keep the native window title in sync with context: "Settings" while the
  // settings screen is open, the open message's subject when reading, otherwise
  // the current folder/view name.
  $: updateWindowTitle(settingsOpen, $visibleMessageId, $selection, $messageList, $t)
  function updateWindowTitle(
    inSettings: boolean,
    id: number | null,
    sel: typeof $selection,
    list: typeof $messageList,
    tFn: (key: string) => string,
  ): void {
    let title = 'Pelton'
    if (inSettings) {
      setWindowTitle(`${tFn('settings.title')} - Pelton`)
      return
    }
    if (id !== null) {
      const item = list.data?.items?.find((m) => m.id === id)
      if (item) {
        title = `${item.subject || tFn('app.noSubject')} - Pelton`
      }
    } else if (sel) {
      title = `${sel.label} - Pelton`
    }
    setWindowTitle(title)
  }

  // offerCrashReport says so when the last run ended in a crash and there is a
  // file about it (#211). It stays up until it is closed, since a crash is not
  // something to notice out of the corner of an eye. Closing it without opening
  // the report leaves the report unread, so the offer comes back next launch.
  async function offerCrashReport(): Promise<void> {
    let status
    try {
      status = await getLogStatus()
    } catch {
      return
    }
    if (!status.crashName) {
      return
    }
    const when = status.crashTime || status.crashName
    pushAction(
      'error',
      $t('crash.toast').replace('{when}', when),
      {
        label: $t('crash.open'),
        run: () => {
          openCrashReport().catch((err) => toastError(errorMessage(err)))
        },
      },
      0,
    )
  }

  onMount(async () => {
    // cosmetic demo mode (--potatoes-are-nice). main.ts has normally answered
    // this before the first component mounted; this is the backstop for a
    // webview where the bindings were not ready that early.
    const demo = await isDemoMode().catch(() => false)
    setDemoActive(demo)

    void refreshFullscreen()

    // a nightly warns before anything else is on screen. demo mode is only used
    // for screenshots, so the dialog would just be in the way there.
    nightlyOpen = !demo && (await isNightly().catch(() => false))

    await initPrefs()
    await initSidebarState()
    await initComposePrefs()
    void initShortcuts()
    void initMenuBar()
    void initPalette()
    void loadSignatures()
    void loadVIPSenders()
    void loadVirusTotalConfig()
    // the sidebar marks mailboxes with no stored password, so the markers have
    // to be known before the first sync raises any prompt.
    void refreshMissingPasswords()
    // a mailbox that failed its last sync is marked from the first paint, not
    // only once another sync run happens to end.
    void loadSyncStates()
    void restoreTabs(get(prefs).restoreTabs)
    void loadProfiles()
    initProgress()
    await loadSidebar()
    // the sidebar data has to be here first: a configured or remembered folder
    // is only honoured if it still exists (#187).
    const sidebarData = get(sidebar).data
    if (sidebarData) {
      await applyStartupSelection(get(prefs).startupSelection, sidebarData)
    }
    void loadViews()
    await loadOutbox()

    // in demo mode, skip onboarding and show a sync in progress for the screenshot.
    if (demo) {
      onboardingOpen = false
      syncing.set(true)
    } else {
      // show the first-run onboarding until it has been completed once.
      try {
        const r = await getSetting(SettingKeys.onboarded)
        onboardingOpen = !(r.found && r.value === 'true')
      } catch {
        // if the lookup fails, do not block the app with onboarding.
        onboardingOpen = false
      }
      // onboarding collects the acknowledgement itself, so only ask separately
      // when the flow is not going to run.
      liabilityOpen = !onboardingOpen && !(await liabilityAccepted())
      void offerCrashReport()
    }

    unsubscribers.push(
      onMailNew(() => {
        void refreshSidebar()
        const sel = get(selection)
        if (
          shouldReplaceListOnMailNew({
            syncPhase: get(syncPhase),
            paginated: listIsPaginated(),
          })
        ) {
          void loadList(sel)
        } else {
          void refreshListHead(sel)
        }
      }),
    )
    unsubscribers.push(
      onMailRepaired(() => {
        // the subjects and bodies changed underneath the list, so what is on
        // screen is stale rather than merely incomplete.
        void loadList(get(selection))
      }),
    )
    unsubscribers.push(
      onMailUpdated((e) => {
        clearBodyFetchFailed(e.messageId)
        const open = get(visibleMessageId)
        if (open === e.messageId) {
          void refreshMessage(e.messageId)
        }
      }),
    )
    unsubscribers.push(
      onMailBodyFailed((e) => {
        if (get(visibleMessageId) === e.messageId) {
          markBodyFetchFailed(e.messageId)
        }
      }),
    )
    unsubscribers.push(
      onSyncState((e) => {
        // fires once per account run, so it cannot clear the status line;
        // only the progress closing events do that.
        syncView = applySyncState(syncView, e.running, Date.now())
        syncing.set(isSyncing(syncView))
        // record the moment a sync finishes for the status bar's last-synced time.
        if (!e.running) {
          lastSynced.set(Date.now())
          // a password the server refuses is only discovered by trying, so the
          // markers can only be right after a sync has run.
          void refreshMissingPasswords()
        }
      }),
    )
    unsubscribers.push(
      onSyncProgress((e) => {
        // accounts sync concurrently, so the line follows whichever is still
        // running instead of going blank when the first one closes.
        syncView = applyProgressEvent(syncView, e, Date.now())
        publishSyncView(syncView)
      }),
    )
    unsubscribers.push(
      startProgressSweep(
        () => syncView,
        (v) => (syncView = v),
        publishSyncView,
      ),
    )
    unsubscribers.push(watchSyncStates())
    unsubscribers.push(
      onOutboxChanged(() => {
        void loadOutbox()
        // a message leaving the outbox lands in Sent, and a queued one that
        // failed goes back. Neither touches the message list, so the badge
        // would otherwise wait for the next sync (#403).
        refreshCountsSoon()
      }),
    )
    unsubscribers.push(onViewsChanged(() => void loadViews()))
    unsubscribers.push(onProfileChanged(() => void reloadForProfile()))
    unsubscribers.push(onAgentProposals(() => void loadProposals()))
    void loadProposals()
    unsubscribers.push(onMenu(handleMenu))
    // a mailto: link opened while the app is already running.
    unsubscribers.push(onMailtoCompose((draft) => openMailtoDraft(draft)))
    // a clicked new-mail notification. the window is already back up by the
    // time this arrives; all that is left is to go to the message.
    unsubscribers.push(
      onOpenMessage((e) => revealMessage(e.messageId, e.accountId, e.folderId, get(sidebar).data ?? null)),
    )

    // a mailto: link that launched the app: the backend stashed it, so pick it
    // up now that the sidebar (and any accounts) have loaded. onboarding, if
    // shown, holds it until a mailbox exists.
    if (!demo) {
      try {
        const pending = await consumePendingMailto()
        if (pending.present) {
          openMailtoDraft(pending.draft)
        }
      } catch {
        // a failed lookup just means no prefilled compose; not worth surfacing.
      }
    }

    // WebKitGTK (Linux) has a known quirk where maximizing the window - a
    // resize driven by the window manager rather than the user dragging an
    // edge - doesn't always make the webview recompute layout against the
    // new size, leaving the interface rendered at the old (smaller)
    // dimensions with blank space along the right/bottom. Reapplying the css
    // `zoom` we already use for interface scale forces a relayout; this is a
    // no-op-looking but effective kick, and harmless on macOS/Windows where
    // resize already reflows correctly on its own. Both a window 'resize'
    // listener and a ResizeObserver on the root are wired up since it's
    // unclear which signal fires reliably for a window-manager-driven
    // maximize on WebKitGTK; either one firing is enough to fix it.
    window.addEventListener('resize', onWindowResize)
    unsubscribers.push(() => window.removeEventListener('resize', onWindowResize))
    const rootResizeObserver = new ResizeObserver(onWindowResize)
    rootResizeObserver.observe(document.documentElement)
    unsubscribers.push(() => rootResizeObserver.disconnect())
  })

  let resizeRelayoutHandle = 0
  function onWindowResize(): void {
    cancelAnimationFrame(resizeRelayoutHandle)
    resizeRelayoutHandle = requestAnimationFrame(() => applyScale(get(prefs).uiScale))
  }

  onDestroy(() => {
    for (const off of unsubscribers) {
      off()
    }
  })

  function composeAccountId(): number | null {
    const data = get(sidebar).data
    if (!data || data.accounts.length === 0) {
      return null
    }
    const sel = get(selection)
    if (sel.kind === 'folder') {
      return sel.accountId
    }
    return data.accounts[0].id
  }

  $: editorMode = $prefs.defaultEditorMode as EditorMode

  function startCompose(): void {
    const accountId = composeAccountId()
    if (accountId === null) {
      toastError(get(t)('app.toast.addMailboxFirst'))
      return
    }
    openCompose(accountId, editorMode)
  }

  // startComposeTo opens a new message addressed to one contact, which is what
  // clicking an address on the contacts screen does (#168).
  function startComposeTo(email: string): void {
    const accountId = composeAccountId()
    if (accountId === null) {
      toastError(get(t)('app.toast.addMailboxFirst'))
      return
    }
    openComposeWith(accountId, editorMode, { to: email, cc: '', bcc: '', subject: '', body: '' })
  }

  // a mailto: link waiting for a mailbox to exist. onboarding-first: if the link
  // arrives before any account is set up, it is held here and opened once
  // onboarding finishes, rather than being dropped.
  let pendingMailto: MailtoDraft | null = null

  // openMailtoDraft opens a prefilled compose from a mailto: link, or defers it
  // until a mailbox exists (see pendingMailto).
  function openMailtoDraft(draft: MailtoDraft): void {
    const accountId = composeAccountId()
    if (accountId === null) {
      pendingMailto = draft
      return
    }
    openComposeWith(accountId, editorMode, draft)
  }

  // flushPendingMailto opens a held mailto draft once a mailbox is available.
  function flushPendingMailto(): void {
    if (pendingMailto && composeAccountId() !== null) {
      const draft = pendingMailto
      pendingMailto = null
      openComposeWith(composeAccountId() as number, editorMode, draft)
    }
  }

  async function runSync(): Promise<void> {
    await promptForMissingPasswords()
    syncing.set(true)
    try {
      await triggerSync()
      await refreshSidebar()
      await loadList(get(selection))
      lastSynced.set(Date.now())
      setOnline(true)
    } catch (err) {
      toastError(friendlyError(err))
    } finally {
      syncing.set(false)
    }
  }

  // add-mailbox opens the fullscreen wizard (lazy-loaded). once a mailbox is
  // added we reload the sidebar so the new account and its folders appear.
  function addMailbox(): void {
    wizardOpen = true
  }

  // onboarding completion is persisted so it shows only once. re-run clears it
  // from settings and reopens the flow.
  function finishOnboarding(): void {
    onboardingOpen = false
    void setSetting(SettingKeys.onboarded, 'true')
    flushPendingMailto()
  }

  function rerunOnboarding(): void {
    settingsOpen = false
    onboardingOpen = true
  }

  function onboardingAddedMailbox(): void {
    void refreshSidebar().then(flushPendingMailto)
  }

  function focusSearch(): void {
    const input = document.querySelector<HTMLInputElement>('input[type="search"]')
    input?.focus()
  }

  // export the currently open message to a print/pdf view, or tell the user to
  // open one first.
  function exportPdf(): void {
    const id = get(visibleMessageId)
    if (id === null) {
      toastInfo(get(t)('app.toast.exportOpenFirst'))
      return
    }
    exportMessagePrintView(id).catch((err) => toastError(errorMessage(err)))
  }

  // currentMessage resolves the message on screen so the message-level shortcuts
  // can act on it: the active tab's, or the one picked in the list.
  //
  // A tab can hold a message from a folder the list is not showing, so the
  // loaded detail stands in when the row is not there. MessageDetail extends
  // MessageSummary, so it is the same shape with more on it.
  function currentMessage(): MessageSummary | null {
    const id = get(visibleMessageId)
    if (id === null) {
      return null
    }
    const row = get(messageList).data?.items?.find((m) => m.id === id)
    if (row) {
      return row
    }
    const detail = get(messageDetail).data
    return detail && detail.id === id ? detail : null
  }

  // bulkAction runs a shortcut against a multi-selection in the list, and reports
  // whether it took the action. Selecting eleven messages and pressing archive
  // means all eleven (#309); the reading pane's own buttons are unaffected,
  // since those are about the message on screen.
  //
  // Actions with no bulk meaning (reply, forward, unsubscribe) are not handled
  // here and fall through to the open message.
  function bulkAction(action: ShortcutAction, items: MessageSummary[]): boolean {
    switch (action) {
      case 'mark-read':
        void bulkMarkSeen(items, true)
        return true
      case 'mark-unread':
        void bulkMarkSeen(items, false)
        return true
      case 'flag':
        // a mixed selection flags rather than unflags: the odd one out joining
        // the rest is recoverable, clearing ten flags is annoying.
        void bulkMarkFlagged(items, items.some((m) => !m.flagged))
        return true
      case 'delete-message':
        void bulkTrash(items)
        return true
      case 'archive':
        void bulkArchive(items)
        return true
      case 'download-offline':
        void bulkSetOffline(items, true)
        return true
      case 'snooze':
        openSnoozeMany(items.map((m) => m.id))
        clearSelection()
        return true
      default:
        return false
    }
  }

  // messageAction runs a message-level shortcut. With more than one message
  // selected in the list it acts on the whole selection; otherwise on the open
  // message, mirroring the right-click menu. it no-ops (with a hint) when
  // neither exists.
  async function messageAction(action: ShortcutAction): Promise<void> {
    // one selected row counts as a selection, not as nothing. Opening a message
    // does not select it, so a selection of one only exists because it was
    // picked deliberately, and the right-click menu already acts on it. Without
    // this, selecting a row and pressing delete answered "open a message
    // first" (#329).
    if (selectedMessages.length >= 1 && bulkAction(action, selectedMessages)) {
      return
    }
    const msg = currentMessage()
    if (!msg) {
      toastInfo(get(t)('app.toast.openMessageFirst'))
      return
    }
    try {
      switch (action) {
        case 'reply':
        case 'reply-all':
          openReply(await getMessage(msg.id), editorMode, action === 'reply-all')
          break
        case 'forward':
          openForward(await getMessage(msg.id), editorMode)
          break
        case 'mark-read':
          patchInList(msg.id, { seen: true })
          await setSeen(msg.id, true)
          break
        case 'mark-unread':
          patchInList(msg.id, { seen: false })
          await setSeen(msg.id, false)
          break
        case 'flag':
          patchInList(msg.id, { flagged: !msg.flagged })
          await setFlagged(msg.id, !msg.flagged)
          break
        case 'snooze':
          openSnooze(msg.id, msg.subject)
          break
        case 'download-offline':
          await setOffline(msg, true)
          break
        case 'delete-message':
          await deleteMessage(msg.id)
          recordDeleted(msg)
          closeTab(msg.id)
          dropDeleted(msg.id)
          break
        case 'unsubscribe': {
          const detail = await getMessage(msg.id)
          if (!detail.unsubscribe) {
            toastInfo(get(t)('detail.unsubscribe.none'))
            break
          }
          if (detail.unsubscribe.kind === 'link') {
            BrowserOpenURL(detail.unsubscribe.target)
            break
          }
          await unsubscribeMessage(msg.id)
          toastInfo(get(t)('detail.unsubscribe.done'))
          break
        }
        case 'archive': {
          const undo = await archiveMessage(msg.id)
          reportArchiveExport(undo)
          recordArchived(msg, undo)
          removeFromList(msg.id)
          closeTab(msg.id)
          if (get(openMessageId) === msg.id) {
            openMessageId.set(null)
          }
          break
        }
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // MenuAction covers the actions only menus emit, on top of the shortcuts.
  type MenuAction =
    | ShortcutAction
    | 'about'
    | 'undo'
    | 'toggle-low-power'
    | 'open-mailboxes'
    | 'open-contacts'
    | 'hide-window'

  // dispatch maps an action (from a shortcut or a menu item) to its handler.
  function dispatchAction(action: MenuAction): void {
    switch (action) {
      case 'compose':
        startCompose()
        break
      case 'send':
        // the compose pane holding focus sends, and takes the key before it
        // gets here. Outside one there is nothing to send.
        break
      case 'export-pdf':
        exportPdf()
        break
      case 'preferences':
        settingsCategory = null
        settingsOpen = true
        break
      case 'open-mailboxes':
        settingsCategory = 'mailboxes'
        settingsOpen = true
        break
      case 'open-contacts':
        contactsOpen = true
        break
      case 'sync':
        void runSync()
        break
      case 'add-mailbox':
        addMailbox()
        break
      case 'search':
        focusSearch()
        break
      case 'about':
        settingsCategory = 'about'
        settingsOpen = true
        break
      case 'reply':
      case 'reply-all':
      case 'forward':
      case 'mark-read':
      case 'mark-unread':
      case 'flag':
      case 'snooze':
      case 'download-offline':
      case 'delete-message':
      case 'archive':
      case 'unsubscribe':
        void messageAction(action)
        break
      case 'open-in-tab': {
        const msg = currentMessage()
        if (msg) {
          openInTab(msg.id, msg.subject)
        } else {
          toastInfo(get(t)('app.toast.openMessageFirst'))
        }
        break
      }
      case 'close-tab':
        closeActiveTab()
        break
      case 'next-profile':
        void switchRelative(1)
        break
      case 'prev-profile':
        void switchRelative(-1)
        break
      case 'undo':
        if (!triggerUndo() && !triggerUndoDelete() && !triggerUndoArchive()) {
          toastInfo(get(t)('app.toast.nothingToUndo'))
        }
        break
      case 'toggle-low-power':
        setLowPowerMode(!$prefs.lowPowerMode)
        break
      case 'new-view':
        openViewEditor()
        break
      case 'next-view':
        cycleView(1)
        break
      case 'prev-view':
        cycleView(-1)
        break
      case 'toggle-fullscreen':
        void toggleFullscreen()
        break
      case 'hide-window':
        // Hide, not WindowHide: on macOS ordering the window out strands it,
        // because wails has no reopen handler for the dock icon to trigger.
        Hide()
        break
      case 'close-window':
        closeFrontmost()
        break
      case 'quit':
        Quit()
        break
      case 'command-palette':
        togglePalette()
        break
      case 'mark-vip': {
        const msg = currentMessage()
        if (msg) {
          void toggleSenderVIP(msg)
        } else {
          toastInfo(get(t)('app.toast.openMessageFirst'))
        }
        break
      }
      case 'move-to': {
        if (selectedMessages.length > 1) {
          const items = selectedMessages
          clearSelection()
          openMoveMany(items)
          break
        }
        const msg = currentMessage()
        if (msg) {
          openMove(msg)
        } else {
          toastInfo(get(t)('app.toast.openMessageFirst'))
        }
        break
      }
      case 'remove-offline': {
        if (selectedMessages.length > 1) {
          void bulkSetOffline(selectedMessages, false)
          break
        }
        const msg = currentMessage()
        if (msg) {
          void setOffline(msg, false)
        } else {
          toastInfo(get(t)('app.toast.openMessageFirst'))
        }
        break
      }
      // these need a target, so they hand off to the palette's picker rather
      // than guessing one. that is also what makes them bindable to a key.
      case 'flag-color':
      case 'empty-trash':
      case 'new-folder':
      case 'rename-folder':
      case 'delete-folder':
      case 'toggle-pin-folder':
      case 'apply-theme':
      case 'edit-view':
      case 'switch-profile':
        openStepFor(action)
        break
    }
  }

  // openStepFor finds the palette entry for an action that needs a target and
  // opens the palette directly on its picker.
  function openStepFor(action: MenuAction): void {
    const entry = buildCommands(commandContext).find((c) => c.id === `action:${action}`)
    if (!entry) {
      toastInfo(get(t)('palette.nothingToPick'))
      return
    }
    const step = entry.run()
    if (step && 'items' in step) {
      openPaletteStep(step)
    }
  }

  // --- command palette (#134) ---

  // installed themes, only fetched while the palette is open so a user who
  // never opens it never pays for the read.
  let paletteThemes: ThemeInfo[] = []
  let paletteThemesLoaded = false
  $: if ($paletteOpen && !paletteThemesLoaded) {
    paletteThemesLoaded = true
    void listThemes()
      .then((list) => (paletteThemes = list))
      .catch(() => (paletteThemes = []))
  }

  $: selectedMessages = ($messageList.data?.items ?? []).filter((m) => $selectedIds.has(m.id))

  function runMessageOp(op: MessageOp, item: MessageSummary, color = 0): void {
    switch (op) {
      case 'mark-read':
        void markSeen(item, true)
        break
      case 'mark-unread':
        void markSeen(item, false)
        break
      case 'flag':
        void markFlagged(item, true)
        break
      case 'unflag':
        void markFlagged(item, false)
        break
      case 'flag-color':
        void markColor(item, color)
        break
      case 'delete':
        void trashMessage(item)
        break
      case 'archive':
        void archiveOne(item)
        break
      case 'download-offline':
        void setOffline(item, true)
        break
      case 'remove-offline':
        void setOffline(item, false)
        break
    }
  }

  function runBulkOp(op: MessageOp, items: MessageSummary[], color = 0): void {
    switch (op) {
      case 'mark-read':
        void bulkMarkSeen(items, true)
        break
      case 'mark-unread':
        void bulkMarkSeen(items, false)
        break
      case 'flag':
        void bulkMarkFlagged(items, true)
        break
      case 'unflag':
        void bulkMarkFlagged(items, false)
        break
      case 'flag-color':
        void bulkMarkColor(items, color)
        break
      case 'delete':
        void bulkTrash(items)
        break
      case 'archive':
        void bulkArchive(items)
        break
      case 'download-offline':
        void bulkSetOffline(items, true)
        break
      case 'remove-offline':
        void bulkSetOffline(items, false)
        break
    }
  }

  async function runFolderOp(op: FolderOp, folder: Folder): Promise<void> {
    switch (op) {
      case 'rename':
        openRenameFolder(folder)
        break
      case 'delete':
        openDeleteFolder(folder)
        break
      case 'new-subfolder':
        openCreateFolder(folder.accountId, folder)
        break
      case 'empty-trash':
        openEmptyTrash(folder)
        break
      case 'toggle-pin':
        try {
          await setFolderPinned(folder.id, !folder.pinned)
          await refreshSidebar()
        } catch (err) {
          toastError(errorMessage(err))
        }
        break
    }
  }

  $: commandContext = {
    t: get(t),
    dispatch: (action) => dispatchAction(action as MenuAction),
    hintFor: (action) => {
      // a catalog entry can pin a fixed combo (the platform menu owns it);
      // otherwise it is the live rebindable one, and menu-only actions like
      // About are in neither.
      const combo = catalogByAction[action]?.hint ?? ($bindings as Record<string, string>)[action] ?? ''
      return combo ? shortcutLabel(combo) : ''
    },
    accounts: $sidebar.data?.accounts ?? [],
    foldersByAccount: $sidebar.data?.foldersByAccount ?? {},
    unifiedViews: $sidebar.data?.views ?? [],
    savedViews: $savedViews,
    themes: paletteThemes,
    profiles: $profiles,
    paletteProfiles: $prefs.paletteProfiles,
    switchProfile: (id: number) => void switchTo(id),
    openMessage: currentMessage(),
    selected: selectedMessages,
    selectFolder: (folder) => {
      selectFolder(folder)
      clearSelection()
    },
    selectUnified: (key, label) => {
      selectView(key as Parameters<typeof selectView>[0], label)
      clearSelection()
    },
    selectSavedView: (id, name) => {
      selectSavedView(id, name)
      clearSelection()
    },
    openSettings: (category) => {
      settingsCategory = category || null
      settingsOpen = true
    },
    onMessage: runMessageOp,
    onBulk: runBulkOp,
    onFolder: (op, folder) => void runFolderOp(op, folder),
    editView: (view) => editViewInEditor(view),
    applyTheme: (themeId) => void setThemeId(themeId).catch((err) => toastError(errorMessage(err))),
    setBaseTheme: (theme) => {
      void setThemeId('')
      setTheme(theme as ThemePref)
    },
  } satisfies CommandContext

  // rebuilt whenever anything the entries depend on moves, so the open palette
  // never offers a folder that was just deleted or a message action with no
  // message behind it.
  $: paletteCommands = $paletteOpen ? buildCommands(commandContext) : []

  // opening a hit also puts the query in the search bar, so the list behind the
  // palette holds the same results the reading pane is showing rather than
  // whatever folder happened to be open.
  function openMailHit(item: MessageSummary): void {
    searchQuery.set(parseQuery(get(paletteQuery)).text)
    openMessage(item.id)
  }

  $: paletteMailCommands = $paletteOpen ? mailCommands(get(t), $paletteMail, openMailHit) : []

  // cycleView moves the selection to the next (dir 1) or previous (dir -1) saved
  // view, wrapping around. From a non-view selection it jumps to the first/last.
  function cycleView(dir: number): void {
    const list = get(savedViews)
    if (list.length === 0) {
      return
    }
    const sel = get(selection)
    const cur = sel.kind === 'savedView' ? list.findIndex((v) => v.id === sel.viewId) : -1
    let next: number
    if (cur === -1) {
      next = dir > 0 ? 0 : list.length - 1
    } else {
      next = (cur + dir + list.length) % list.length
    }
    const v = list[next]
    selectSavedView(v.id, v.name)
  }

  // closeFrontmost is what Cmd+W means on a desktop: it shuts the thing you are
  // looking at, not always the window. the nightly warning, the liability
  // acknowledgement and onboarding are deliberately absent, since they are
  // unskippable by design and this must not become a way around them.
  function closeFrontmost(): void {
    const composeEl = document.activeElement?.closest('[data-compose-id]')
    if (composeEl instanceof HTMLElement && composeEl.dataset.composeId) {
      requestComposeClose(Number(composeEl.dataset.composeId))
      return
    }
    if (settingsOpen) {
      settingsOpen = false
      return
    }
    if (wizardOpen) {
      wizardOpen = false
      return
    }
    // a reading-pane tab is the next thing in, the way a browser tab is: with
    // one showing, this closes it and leaves the window alone. On the untabbed
    // pane, or with no tabs at all, it goes on to close the window.
    if (get(activeTabId) !== null) {
      closeActiveTab()
      return
    }
    // nothing layered on top, so this closes the window itself. the backend owns
    // what that means: it follows the close action setting, and hides through
    // the same call the close button uses, which the dock icon can undo.
    closeWindow()
  }

  async function toggleFullscreen(): Promise<void> {
    if (await WindowIsFullscreen()) {
      WindowUnfullscreen()
    } else {
      WindowFullscreen()
    }
  }

  function handleMenu(action: string): void {
    dispatchAction(action as MenuAction)
  }

  // suppress the webview's default context menu (inspect/reload) everywhere. the
  // one exception is when text is selected, so the native copy menu still works
  // for selected mail text. components that want a real menu (the message list)
  // open the custom one themselves.
  function onContextMenu(event: MouseEvent): void {
    const selected = window.getSelection()?.toString().trim()
    if (selected) {
      return
    }
    event.preventDefault()
  }

  // in-app vim navigation: when enabled, plain h/j/k/l (and gg/G) move around
  // the message list and open/close the reading pane, mirroring mutt-style
  // navigation instead of just the compose editor. it never fires in a text
  // field, while a dialog/panel is open, or with a modifier held (so cmd+j
  // etc. still reach the normal shortcut path).
  let lastVimKey = ''
  let lastVimKeyAt = 0

  function vimNavList(): { id: number }[] {
    return get(messageList).data?.items ?? []
  }

  function vimMove(delta: number): void {
    const items = vimNavList()
    if (items.length === 0) {
      return
    }
    const currentId = get(openMessageId)
    const idx = currentId === null ? -1 : items.findIndex((m) => m.id === currentId)
    const next = idx === -1 ? (delta > 0 ? 0 : items.length - 1) : Math.min(Math.max(idx + delta, 0), items.length - 1)
    openMessageId.set(items[next].id)
  }

  function vimJump(toLast: boolean): void {
    const items = vimNavList()
    if (items.length === 0) {
      return
    }
    openMessageId.set(toLast ? items[items.length - 1].id : items[0].id)
  }

  function anyDialogOpen(): boolean {
    return (
      settingsOpen ||
      wizardOpen ||
      onboardingOpen ||
      $composeSessions.length > 0 ||
      $moveTargets.length > 0 ||
      $snoozeTarget !== null ||
      $previewTarget !== null ||
      $paletteOpen
    )
  }

  // vim navigation is scoped to the list/reading pane: it must not hijack h/l
  // while the user is browsing the sidebar (folders, accounts) or any other
  // chrome, so it only fires when focus is outside the sidebar.
  function inSidebar(target: EventTarget | null): boolean {
    const el = target as HTMLElement | null
    return !!el?.closest?.('.sidebar')
  }

  function tryVimNav(event: KeyboardEvent): boolean {
    if (!$prefs.appVimMode || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) {
      return false
    }
    if (isEditableTarget(event.target) || inSidebar(event.target) || anyDialogOpen()) {
      return false
    }
    const now = Date.now()
    switch (event.key) {
      case 'j':
        vimMove(1)
        break
      case 'k':
        vimMove(-1)
        break
      case 'l':
      case 'Enter':
        if (get(openMessageId) === null) {
          vimMove(1)
        }
        break
      case 'h':
      case 'Escape':
        openMessageId.set(null)
        break
      case 'g':
        if (lastVimKey === 'g' && now - lastVimKeyAt < 500) {
          vimJump(false)
          lastVimKey = ''
          return true
        }
        lastVimKey = 'g'
        lastVimKeyAt = now
        return true
      case 'G':
        vimJump(true)
        break
      default:
        lastVimKey = ''
        return false
    }
    lastVimKey = ''
    return true
  }

  function onKeydown(event: KeyboardEvent): void {
    // while the settings panel is capturing a new binding, let it have the keys.
    if ($recording) {
      return
    }
    if (tryVimNav(event)) {
      event.preventDefault()
      return
    }
    // cmd/ctrl+z undoes a pending delayed send, when one is in its window. it
    // takes priority over other shortcuts and is swallowed only if it acted.
    if ((event.metaKey || event.ctrlKey) && !event.shiftKey && event.key.toLowerCase() === 'z') {
      // undo-send takes priority; otherwise undo the last message deletion.
      if (triggerUndo()) {
        event.preventDefault()
        return
      }
      if (triggerUndoDelete()) {
        event.preventDefault()
        return
      }
      if (triggerUndoArchive()) {
        event.preventDefault()
        return
      }
    }
    const action = matchShortcut(event, $bindings)
    if (action) {
      // a modifier-less custom binding must not hijack typing in a field.
      if (!comboHasModifier($bindings[action]) && isEditableTarget(event.target)) {
        return
      }
      event.preventDefault()
      dispatchAction(action)
      return
    }
    // cmd/ctrl+1 to 9 jump between the reading-pane chips, the way they jump
    // between tabs in a browser: 1 is the pane, 9 is the last tab. Checked after
    // the bindings, so a key the user assigned themselves still wins, and only
    // while tabs exist, so the combination is otherwise left alone.
    if (selectTabByNumber(event)) {
      event.preventDefault()
    }
  }

  // selectTabByNumber handles cmd/ctrl+1 to 9 and reports whether it acted. The
  // chips are the untabbed pane followed by the tabs, so 1 is the pane and the
  // numbers after it count along the bar. 9 is the last chip however many there
  // are, which is what a browser does.
  function selectTabByNumber(event: KeyboardEvent): boolean {
    if (!(event.metaKey || event.ctrlKey) || event.shiftKey || event.altKey) {
      return false
    }
    const slot = Number(event.key)
    if (!Number.isInteger(slot) || slot < 1 || slot > 9) {
      return false
    }
    const open = get(tabs)
    if (open.length === 0) {
      return false
    }
    if (slot === 9) {
      focusTab(open[open.length - 1].id)
      return true
    }
    if (slot === 1) {
      focusPane()
      return true
    }
    const tab = open[slot - 2]
    if (!tab) {
      return false
    }
    focusTab(tab.id)
    return true
  }

  // reloadForProfile rebuilds everything the ui holds after a profile switch.
  // Settings, accounts, signatures and saved views are all scoped to a profile,
  // and the open message almost certainly belongs to an account this profile
  // cannot see, so the pane and the tabs are cleared rather than left showing
  // mail from the profile you just left.
  async function reloadForProfile(): Promise<void> {
    openMessageId.set(null)
    clearAllTabs()
    clearSelection()
    await loadProfiles()
    await initPrefs()
    await loadSidebar()
    const data = get(sidebar).data
    if (data) {
      await applyStartupSelection(get(prefs).startupSelection, data)
    }
    void loadSignatures()
    void loadVIPSenders()
    void loadViews()
    await loadList(get(selection))
  }

  // anything deep in the tree can ask for the settings screen on a category;
  // the panel itself is owned here. See stores/settingsnav.ts.
  $: if ($settingsRequest) {
    settingsCategory = $settingsRequest
    settingsOpen = true
    clearSettingsRequest()
  }

  // isEditableTarget reports whether the event originated in a text field, so
  // plain-key shortcuts do not fire while the user is typing.
  function isEditableTarget(target: EventTarget | null): boolean {
    const el = target as HTMLElement | null
    if (!el) {
      return false
    }
    const tag = el.tagName
    return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
  }

  // pane resize. clamps keep each column usable; widths persist on release.
  function clamp(value: number, min: number, max: number): number {
    return Math.min(Math.max(value, min), max)
  }

  function resizeSidebar(event: CustomEvent<number>): void {
    dragging = true
    sidebarW = clamp(sidebarW + event.detail, 180, 480)
  }

  function resizeList(event: CustomEvent<number>): void {
    dragging = true
    listW = clamp(listW + event.detail, 260, 720)
  }

  function commitPanes(): void {
    dragging = false
    setPaneWidths(sidebarW, listW)
  }
</script>

<svelte:window on:keydown={onKeydown} on:contextmenu={onContextMenu} on:resize={refreshFullscreen} />

<!-- macOS with no in-app menu bar: an empty top row for the traffic lights, and
     a matching strip above every overlay so the window can still be dragged
     while settings or onboarding is open. with the menu bar on, the bar itself
     is that row and handles both. -->
{#if macTitlebar && !showMenuBar}
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="titlebar-drag"
    style="--wails-draggable:drag"
    aria-hidden="true"
    on:dblclick={titleBarDoubleClick}
  ></div>
{/if}

<div class="shell" class:with-topbar={showMenuBar || macTitlebar}>
  {#if showMenuBar}
    <MenuBar on:action={(e) => handleMenu(e.detail)} />
  {:else if macTitlebar}
    <div class="titlebar-space" aria-hidden="true"></div>
  {/if}
  <div class="columns" style={`grid-template-columns: ${sidebarW}px 0 ${listW}px 0 1fr`}>
    <Sidebar
      on:compose={startCompose}
      on:sync={runSync}
      on:addMailbox={addMailbox}
    />
    <Resizer disabled={locked} label={$t('app.pane.resizeSidebar')} on:resize={resizeSidebar} on:end={commitPanes} />
    <MessageList />
    <Resizer disabled={locked} label={$t('app.pane.resizeMessageList')} on:resize={resizeList} on:end={commitPanes} />
    <MessageDetail />
  </div>

  <StatusBar />
</div>

{#if $composeSessions.length > 0}
  <div class="compose-layer">
    {#each $composeSessions as session (session.id)}
      <Compose {session} />
    {/each}
  </div>
{/if}

<!-- settings and the wizard are code-split: their js/css load only when opened,
     so they cost nothing at startup. compose stays eager (used constantly). -->
{#if contactsOpen}
  {#await import('./components/contacts/ContactsScreen.svelte') then m}
    <svelte:component
      this={m.default}
      on:close={() => (contactsOpen = false)}
      on:compose={(e) => { contactsOpen = false; startComposeTo(e.detail) }}
    />
  {/await}
{/if}

{#if settingsOpen}
  {#await import('./components/settings/SettingsPanel.svelte') then m}
    <svelte:component
      this={m.default}
      initialCategory={settingsCategory}
      on:close={() => (settingsOpen = false)}
      on:rerunOnboarding={rerunOnboarding}
    />
  {/await}
{/if}

<!-- the nightly-build warning, above everything else and shown every launch. -->
{#if nightlyOpen}
  {#await import('./components/common/NightlyDialog.svelte') then m}
    <svelte:component this={m.default} on:accepted={() => (nightlyOpen = false)} />
  {/await}
{/if}

<!-- the liability acknowledgement for installs from before the onboarding step. -->
{#if liabilityOpen}
  {#await import('./components/common/LiabilityDialog.svelte') then m}
    <svelte:component this={m.default} on:accepted={() => (liabilityOpen = false)} />
  {/await}
{/if}

<!-- onboarding is code-split too: shown only on first run or when re-run. -->
{#if onboardingOpen}
  {#await import('./components/onboarding/Onboarding.svelte') then m}
    <svelte:component this={m.default} on:finish={finishOnboarding} on:added={onboardingAddedMailbox} />
  {/await}
{/if}

{#if wizardOpen}
  <AddMailboxDialog on:close={() => (wizardOpen = false)} on:added={flushPendingMailto} />
{/if}

{#if $editingView}
  {#await import('./components/settings/ViewEditorModal.svelte') then m}
    <svelte:component
      this={m.default}
      value={$editingView}
      accounts={$sidebar.data?.accounts ?? []}
      on:close={closeViewEditor}
      on:saved={closeViewEditor}
    />
  {/await}
{/if}

<Toasts />
<ContextMenu />
<ConfirmDialog />
<SnoozeDialog />
<FolderDialog />
<AttachmentPreview />
<MoveDialog />

<CommandPalette commands={paletteCommands} mail={paletteMailCommands} />

<AccountPasswordDialog account={$passwordPrompt} onDone={answerPasswordPrompt} />

<SyncFailureDialog />

<DevOverlays />

<style>
  .shell {
    display: grid;
    grid-template-rows: 1fr auto;
    /* percentages, not vh/vw: percentage sizes resolve against the zoomed
       layout size, so they follow the css zoom the interface-scale setting
       puts on the root (vh/vw don't, which is what the old
       calc(100vh / var(--ui-scale)) compensated for), and they stay correct
       on WebKitGTK, where viewport units misresolved and left the interface
       smaller than the window. #app (the parent) is sized the same way in
       style.css. */
    height: 100%;
    width: 100%;
    overflow: hidden;
  }

  /* the menu bar, or the macOS traffic-light row, takes a top auto row. */
  .shell.with-topbar {
    grid-template-rows: auto minmax(0, 1fr) auto;
  }

  .titlebar-space {
    height: var(--titlebar-lights);
  }

  /* above every overlay: a window with no native title bar has nothing else to
     drag it by, and settings or onboarding being open is no reason to lose
     that. only rendered without the menu bar, which would otherwise have its
     titles buried under this. */
  .titlebar-drag {
    position: fixed;
    inset: 0 0 auto 0;
    height: var(--titlebar-lights);
    z-index: 700;
  }

  /* the two zero-width tracks hold the resizer handles, which overhang via
     negative margins so they sit on the column borders without taking space. the
     single row is pinned to the shell height with a 0 minimum so each column's
     own scroll area can shrink and scroll instead of stretching the grid. */
  .columns {
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    min-height: 0;
    overflow: hidden;
  }

  /* the dock spans the viewport width so extra panes wrap upward (wrap-reverse,
     bottom-anchored) instead of cascading off the left edge. it is click-through
     in the empty gaps; each pane re-enables pointer events. */
  .compose-layer {
    position: fixed;
    bottom: var(--space-5);
    right: var(--space-5);
    left: var(--space-5);
    display: flex;
    flex-wrap: wrap-reverse;
    justify-content: flex-end;
    gap: var(--space-4);
    align-items: flex-end;
    z-index: 90;
    pointer-events: none;
  }

  .compose-layer > :global(.compose) {
    pointer-events: auto;
  }
</style>
