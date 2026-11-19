<script lang="ts">
  // the application shell: the three-column layout (resizable, lockable) plus the
  // compose panes, the settings screen, the outbox status bar and the toast
  // stack. it loads initial data, applies preferences, subscribes to backend and
  // menu events, and handles the app-wide keyboard shortcuts.
  import { onMount, onDestroy, type ComponentType } from 'svelte'
  import { get } from 'svelte/store'
  import { t } from './lib/i18n'
  import { isEditableTarget } from './lib/editable'

  import Sidebar from './components/sidebar/Sidebar.svelte'
  import MenuBar from './components/common/MenuBar.svelte'
  import MessageList from './components/list/MessageList.svelte'
  import MessageDetail from './components/detail/MessageDetail.svelte'
  import Toasts from './components/common/Toasts.svelte'
  import StatusBar from './components/common/StatusBar.svelte'
  import ContextMenu from './components/common/ContextMenu.svelte'
  import Resizer from './components/common/Resizer.svelte'
  import TopBar from './components/shell/TopBar.svelte'
  import Rail from './components/shell/Rail.svelte'
  import SpaceSwitcher from './components/shell/SpaceSwitcher.svelte'
  import { space, openSpace, runSpaceAction, isMailOnlyAction } from './stores/space'
  import { meetingPrepTarget, openMeetingPrep, closeMeetingPrep } from './stores/meetingprep'
  import { mailScreen, openMailScreen, closeMailScreen } from './stores/mailscreen'
  import { IconMailX, IconChartBar } from '@tabler/icons-svelte'
  import { orderedSpaces, visibleSpaces } from './lib/spaces'
  import { startChat, stopChat } from './stores/chat'
  import { popupIsOpen } from './lib/popups'
  import { openMenuFromKeyboard } from './lib/keymenu'
  import { calendarFocus, calendarImport, calendarQuickAdd } from './stores/calendar'
  import { formatTime } from './lib/calformat'
  import type { TimeFormat } from './lib/format'
  import { frameIntegrated } from './stores/windowframe'
  import { setDraft, submitSearch, searchOpenedId } from './stores/search'
  import DragStack from './components/list/DragStack.svelte'
  import ConfirmDialog from './components/common/ConfirmDialog.svelte'
  import { loadSyncStates, watchSyncStates } from './stores/syncfailures'

  import { initPrefs, prefs, setPaneWidths, setListHeight, setLowPowerMode } from './stores/prefs'
  import { applyScale } from './theme/theme'
  import { loadSidebar, refreshSidebar, sidebar } from './stores/accounts'
  import { refreshCountsSoon } from './stores/sidebarcounts'
  import { initSidebarState } from './stores/sidebarstate'
  import { loadSignatures } from './stores/signatures'
  import { loadVIPSenders } from './stores/vip'
  import { loadVirusTotalConfig } from './stores/virustotal'
  import { loadOutbox, syncing, lastSynced, syncFolder, syncServer, syncAccount, syncCounts, emptySyncCounts } from './stores/outbox'
  import { selection, applyStartupSelection, searchQuery } from './stores/selection'
  import { loadList, messageList } from './stores/messages'
  import { initProgress } from './stores/progress'
  import { composeSessions, openCompose, openComposeWith, initComposePrefs, openReply, openForward, requestComposeClose, setForcedComposeMode, updateCompose } from './stores/compose'
  import { routeComposeSessions } from './stores/composeroute'
  import { loadAttachmentsBrowser, loadClassicCompose, loadComposer, loadMenuBarEditor, loadEncryptionIndicator, loadPromiseStrip, warmUp } from './lib/lazyload'
  import ComposeTabLayer from './components/compose/ComposeTabLayer.svelte'
  import { openSnooze, openSnoozeMany } from './stores/snooze'
  import { patchInList } from './stores/messages'
  import {
    triggerSync,
    getSetting,
    setSetting,
    SettingKeys,
    exportMessagePrintView,
    setWindowTitle,
    deleteMessage,
    getMessage,
    downloadMessageOffline,
    archiveMessage,
    setMailActionsEnabled,
    isDemoMode,
    isNightly,
    unsubscribeMessage,
    consumePendingMailto,
    consumePendingICS,
    closeWindow,
    getLogStatus,
    openCrashReport,
    titleBarDoubleClick,
    applyTrafficLights,
    setUnreadBadge,
    quitApp,
    hideApp,
    lockNow,
    isFullscreen,
    toggleFullscreen as toggleWindowFullscreen,
  } from './lib/api'
  import { openURL } from './lib/browser'
  import { liabilityAccepted } from './lib/liability'
  import { setDemoActive } from './lib/demo'
  import { recordArchived } from './stores/undoarchive'
  import { onMailNew, onMailRepaired, onSyncState, onSyncProgress, onOutboxChanged, onMenu, onViewsChanged, onFoldersChanged, onProfileChanged, onMailtoCompose, onAgentProposals, onOpenMessage, onReplyMessage, onArchiveAfterSend, onFeedsOpen, onCalendarReminder, onTaskReminder, onICSOpen, onAssistantScript, type Unsubscribe, type MailtoDraft, type CalendarReminderEvent, type TaskReminderEvent } from './lib/events'
  import { assistantToastKey } from './lib/assistantScripts'
  import { selectScript } from './stores/scripts'
  import { loadViews, editingView, closeViewEditor, openViewEditor, views as savedViews } from './stores/views'
  import { selectSavedView } from './stores/selection'
  import { isMac } from './lib/i18n'
  import {
    matchShortcut,
    comboTypes,
    isComposeAction,
    chipActions,
    sendActions,
    spaceNumber,
    scriptOfAction,
    shortcuts as shortcutRegistry,
    type ShortcutAction,
  } from './lib/shortcuts'
  import { runComposeCommand } from './stores/composecommands'
  import CoachLayer from './components/shell/CoachLayer.svelte'
  import FirstTipLayer from './components/shell/FirstTipLayer.svelte'
  import { initCoach, toggleSheet } from './stores/coach'
  import { initFirstTips, setTipsSuspended } from './stores/firsttips'
  import { initWhatsNew, markWhatsNewSeen, whatsNewOpen, whatsNewUnseen } from './stores/whatsnew'
  import { setupRequested } from './stores/setuprun'
  import './lib/palette-setup'
  import { noteAction, noteShortcut } from './lib/coach'
  import { initFocus, focusMenuOpen, refreshFocus } from './stores/focus'
  import './lib/palette-focus'
  import { cyclePane, focusPane as focusPaneArea } from './lib/panes'
  import { bindings, activeBindings, recording, initShortcuts } from './stores/shortcuts'
  import { currentMessageItems, runAt } from './lib/scriptplacements'
  import { initMenuBar } from './stores/menubar'
  import { recordDeleted } from './stores/undodelete'
  import { historyOpen, redoOrSay, undoOrSay } from './stores/undohistory'
  import './lib/palette-history'
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
  import { profiles, loadProfiles, switchTo, switchRelative } from './stores/profiles'
  import { loadProposals } from './stores/agent'
  import { settingsRequest, clearSettingsRequest, openSettingsAt } from './stores/settingsnav'
  import { errorMessage, toastError, toastInfo, pushAction, pushActions, withBusyToast } from './stores/toast'
  import { friendlyError } from './lib/errors'
  import { setOnline } from './stores/network'
  import { moveTargets } from './stores/move'
  import { snoozeTarget } from './stores/snooze'
  import { previewTarget } from './stores/preview'
  import { openMove, openMoveMany } from './stores/move'
  import {
    selectedIds,
    clearSelection,
    anchorAt,
    ensureAnchor,
    selectRange,
    rangeCursor,
    startVisual,
    endVisual,
    inVisual,
  } from './stores/listselect'
  import { selectFolder, selectView, revealMessage } from './stores/selection'
  import { setTheme, setThemeId, setMyDayPanel } from './stores/prefs'
  import MyDayPanel from './components/myday/MyDayPanel.svelte'
  import { myDayFits } from './lib/myday'
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
    exportEml,
    reportArchiveExport,
    dropDeleted,
    leaveList,
    toggleSpam,
  } from './lib/messageactions'
  import { markAnsweredDone, markDone } from './lib/triage'
  import { watchFollowUps } from './stores/followups'
  import { loadSnippets } from './stores/snippets'
  import { editNote, toggleMute, togglePin } from './lib/organize'
  import { buildCommands, mailCommands, type CommandContext, type MessageOp, type FolderOp } from './lib/commands'
  import { extraPaletteCommands, registerPaletteSource } from './lib/palettesources'
  import './lib/palette-calendar'
  import './lib/palette-contacts'
  import './lib/palette-attachments'
  import './lib/palette-sieve'
  import { attachmentsOpen, toggleAttachments } from './stores/attachments'
  import { setSenderPanel } from './stores/prefs'
  import { toggleReadingView } from './stores/readingview'
  import './lib/palette-tasks'
  import './lib/palette-promises'
  import { startPromises } from './stores/promises'
  import './lib/palette-notes'
  import { reloadVault, requestNotes, startNotes } from './stores/notes'
  import './lib/palette-chat'
  import './lib/palette-chatshare'
  import './lib/palette-availability'
  import './lib/palette-projects'
  import { startProjects, fileMailRequest } from './stores/projects'
  import { pickProjectFor } from './lib/projectpicker'
  import './lib/palette-feeds'
  import './lib/palette-automations'
  import AutomationsHost from './components/automations/AutomationsHost.svelte'
  import './lib/palette-navhistory'
  import './lib/location-mail'
  import './lib/location-calendar'
  import './lib/location-tasks'
  import './lib/location-projects'
  import './lib/location-contacts'
  import './lib/location-notes'
  import './lib/location-feeds'
  import './lib/location-chat'
  import './lib/location-automations'
  import { startNavHistory, clearNavHistory, goBack, goForward } from './stores/navhistory'
  import { startFeeds, feedFocus } from './stores/feeds'
  import { startTasks, toggleDone, tasks as taskStore, taskFromMailRequest } from './stores/tasks'
  import { createTasksFromMail, openTask } from './lib/taskmail'
  import { getTask } from './lib/api'
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
  import DevOverlays from './components/dev/DevOverlays.svelte'
  import peltonLogo from './assets/images/icons/pelton-logo.png'
  import {
    passwordPrompt,
    answerPasswordPrompt,
    promptForMissingPasswords,
    refreshMissingPasswords,
  } from './stores/passwordprompt'
  import type { EditorMode, MessageSummary, ThemePref, ThemeInfo, Folder, Space } from './lib/types'

  let settingsOpen = false
  // the settings screen is code-split and loaded on first open. A load that
  // fails used to leave settingsOpen stuck true with nothing on screen, so every
  // later attempt set it to true again and nothing happened until a restart. It
  // now closes again and says so, and the next attempt starts a fresh load.
  // each design has its own settings screen. switching design from inside
  // settings swaps the screen and reopens it on the page that switch lives on.
  let settingsPanel: Promise<{ default: ComponentType } | null> | null = null
  let settingsDesign = ''
  $: if (settingsOpen && (!settingsPanel || settingsDesign !== $prefs.design)) {
    if (settingsPanel) {
      settingsCategory = $prefs.design === 'classic' ? 'appearance' : 'design'
    }
    settingsDesign = $prefs.design
    const load: Promise<{ default: ComponentType }> =
      $prefs.design === 'classic'
        ? import('./components/settings/SettingsPanel.svelte')
        : import('./components/settings2/SettingsScreen.svelte')
    settingsPanel = load.catch((err) => {
      settingsPanel = null
      settingsOpen = false
      toastError(errorMessage(err))
      return null
    })
  }
  // the contacts screen (#168), opened over the mail view and code-split the
  // same way settings is.
  let contactsOpen = false
  // the settings category to open on; set by menu actions that deep-link into a
  // specific section (e.g. Manage Mailboxes), null opens the default section.
  let settingsCategory: string | null = null
  let wizardOpen = false
  let onboardingOpen = false
  // which flow is showing, fixed when it opens: the classic design keeps its
  // own onboarding, the new one runs the setup flow, and a re-run treats what
  // exists as set up.
  let onboardingClassic = false
  let onboardingRerun = false
  // the setup flow opens on the acknowledgement while it is owed.
  let setupNeedsLegal = false
  // installs that predate the onboarding liability step have to acknowledge it
  // once before they can carry on.
  let liabilityOpen = false
  // a nightly build warns on every launch, before anything else including
  // onboarding, and the acknowledgement is deliberately never remembered.
  let nightlyOpen = false
  const unsubscribers: Unsubscribe[] = []

  // set when the app mounts again after the app lock was opened. The stores
  // still hold the session, so the launch-only steps (the startup folder,
  // restored tabs, the nightly and crash notices) are skipped.
  export let resumed = false

  // live pane widths. they track the persisted prefs unless the user is mid-drag,
  // so a resize feels immediate and only commits on release.
  let sidebarW = 264
  let listW = 380
  let dragging = false
  let listH = 320
  $: if (!dragging) {
    sidebarW = $prefs.sidebarWidth
    listW = $prefs.listWidth
    listH = $prefs.listHeight
  }
  $: locked = $prefs.paneLocked

  // the new design. classic keeps the shell below it exactly as it was.
  $: classic = $prefs.design === 'classic'
  $: setForcedComposeMode(classic ? null : $prefs.composeVimMode ? 'plaintext' : 'wysiwyg')
  unsubscribers.push(routeComposeSessions({ tabs: true }))
  $: stacked = $prefs.layoutMode === 'stacked'
  // below this window width the rail folds into the top bar as tabs.
  const railFoldWidth = 760
  let windowWidth = window.innerWidth
  $: railFolded = $prefs.navStyle === 'rail' && windowWidth < railFoldWidth
  $: showRail = $prefs.navStyle === 'rail' && !railFolded

  // the My Day panel sits beside the mail surface only while the reader keeps
  // a readable column next to it; narrower, it folds away.
  let mailWidth = 0
  let myDayWidth = 0
  $: if ($prefs.myDayPanel && myDayWidth === 0) {
    myDayWidth = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--myday-width')) || 0
  }
  $: showMyDay = $prefs.myDayPanel && myDayFits(mailWidth, sidebarW, listW, stacked, myDayWidth)
  $: inboxUnread = $sidebar.data?.views?.find((v) => v.key === 'inbox')?.unreadCount ?? 0
  // classic shows contacts as an overlay; the new design shows it as a space.
  function openContacts(): void {
    if (classic) {
      contactsOpen = true
    } else {
      openSpace('contacts')
    }
  }
  // an overlay covers the top bar, so the window needs another handle to be
  // dragged by while one is open.
  // classic shows automations as an overlay too.
  let automationsOpen = false
  $: overlayOpen = settingsOpen || onboardingOpen || wizardOpen || contactsOpen || automationsOpen
  $: setTipsSuspended(overlayOpen || nightlyOpen || liabilityOpen || $whatsNewOpen)
  $: if ($setupRequested) {
    setupRequested.set(false)
    rerunOnboarding()
  }

  // the in-app menu bar is the only menu on Windows/Linux (no native menu is
  // created there); on macOS the native bar stays and the in-app one is opt-in.
  $: showMenuBar = !isMac || $prefs.menuBarInApp

  // the macOS window has no native title bar (mac.TitleBarHiddenInset), so the
  // ui paints to the top edge and keeps a strip clear for the traffic lights.
  // fullscreen hides the lights, and the strip goes with them.
  let fullscreen = false
  $: macTitlebar = isMac && !fullscreen && $frameIntegrated
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
    fullscreen = await isFullscreen().catch(() => false)
    // where the traffic lights sit differs between macOS releases, so what is
    // drawn beside them is lined up with where they really are.
    await applyTrafficLights()
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

  // the toast line of a calendar reminder: the title and when it starts.
  function reminderText(e: CalendarReminderEvent): string {
    const title = e.summary || $t('calendar.untitled')
    if (e.allDay) {
      return `${title}, ${$t('calendar.allDay')}`
    }
    return `${title}, ${formatTime(new Date(e.start), $prefs.timeFormat as TimeFormat)}`
  }

  function showReminder(e: CalendarReminderEvent): void {
    calendarFocus.set(e.allDay ? new Date(`${e.start}T00:00:00`) : new Date(e.start))
    openSpace('calendar')
  }

  // the toast line of a task reminder: the title and when it is due.
  function taskReminderText(e: TaskReminderEvent): string {
    const title = e.summary || $t('tasks.untitled')
    if (e.due.length > 10) {
      return `${title}, ${formatTime(new Date(e.due), $prefs.timeFormat as TimeFormat)}`
    }
    return title
  }

  async function completeFromReminder(e: TaskReminderEvent): Promise<void> {
    try {
      const task = get(taskStore).find((x) => x.id === e.taskId) ?? (await getTask(e.taskId))
      await toggleDone(task, true)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // tasks: create-task acts on the selection or the open message.
  function createTaskFromMail(): void {
    const ids = selectedMessages.length > 1 ? selectedMessages.map((m) => m.id) : [currentMessage()?.id].filter((id): id is number => id !== undefined)
    if (ids.length === 0) {
      toastInfo(get(t)('app.toast.openMessageFirst'))
      return
    }
    void createTasksFromMail(ids)
  }

  // files the open conversation, or the selected ones, under a project.
  function addToProject(): void {
    const items = selectedMessages.length > 1 ? selectedMessages : [currentMessage()].filter((m): m is MessageSummary => m !== null)
    pickProjectFor(
      items.map((m) => m.id),
      items.length === 1 ? items[0].threadId : 0,
    )
  }

  $: if ($fileMailRequest) {
    fileMailRequest.set(false)
    if (!classic) {
      addToProject()
    }
  }

  $: if ($taskFromMailRequest) {
    taskFromMailRequest.set(false)
    if (!classic) {
      createTaskFromMail()
    }
  }

  // the tasks store feeds the rail badge, the mail markers and the calendar,
  // so it runs for as long as the new design is on.
  let stopTasks: (() => void) | undefined
  $: if (!classic && !stopTasks) {
    stopTasks = startTasks()
  } else if (classic && stopTasks) {
    stopTasks()
    stopTasks = undefined
  }

  // the promises found in mail feed the reader's suggestions and the task
  // lists "I owe" and "Owed to me", in the new design only.
  let stopPromises: (() => void) | undefined
  $: if ($prefs.design === 'new' && !stopPromises) {
    stopPromises = startPromises()
  } else if ($prefs.design !== 'new' && stopPromises) {
    stopPromises()
    stopPromises = undefined
  }
  onDestroy(() => stopTasks?.())

  // the projects feed the mail chips, the reader's suggestions and the
  // composer's picker as well as their own space.
  let stopProjects: (() => void) | undefined
  $: if (!classic && !stopProjects) {
    stopProjects = startProjects()
  } else if (classic && stopProjects) {
    stopProjects()
    stopProjects = undefined
  }
  onDestroy(() => stopProjects?.())

  // the notes index feeds the palette and the notes listed beside mail,
  // events, contacts and tasks. It loads the folder the backend opened at
  // startup, if any, without creating one.
  let stopNotes: (() => void) | undefined
  $: if (!classic && !stopNotes) {
    stopNotes = startNotes()
    void reloadVault()
  } else if (classic && stopNotes) {
    stopNotes()
    stopNotes = undefined
  }
  onDestroy(() => stopNotes?.())

  // back and forward follow the places visited in the new design.
  let stopNav: (() => void) | undefined
  $: if (!classic && !stopNav) {
    stopNav = startNavHistory()
  } else if (classic && stopNav) {
    stopNav()
    stopNav = undefined
  }
  onDestroy(() => stopNav?.())

  // the side buttons of a mouse go back and forward, where the webview
  // reports them; the press is taken so the webview does not act on it too.
  function onNavMouseDown(event: MouseEvent): void {
    if (!classic && (event.button === 3 || event.button === 4)) {
      event.preventDefault()
    }
  }

  function onNavMouseUp(event: MouseEvent): void {
    if (classic || (event.button !== 3 && event.button !== 4)) {
      return
    }
    event.preventDefault()
    if (!overlayOpen) {
      void (event.button === 3 ? goBack() : goForward())
    }
  }

  // the feeds store feeds the rail's unread dot, so it runs alongside.
  let stopFeeds: (() => void) | undefined
  $: if (!classic && !stopFeeds) {
    stopFeeds = startFeeds()
  } else if (classic && stopFeeds) {
    stopFeeds()
    stopFeeds = undefined
  }
  onDestroy(() => stopFeeds?.())

  // shareFromFeeds opens a new message carrying a feed entry's title and link.
  function shareFromFeeds(mail: { subject: string; body: string }): void {
    const accountId = composeAccountId()
    if (accountId === null) {
      toastError(get(t)('app.toast.addMailboxFirst'))
      return
    }
    openComposeWith(accountId, editorMode, { to: '', cc: '', bcc: '', subject: mail.subject, body: mail.body })
  }

  // chat runs only while the user has it turned on, in the new design. Off
  // again, the space closes and its state is dropped.
  let chatRunning = false
  $: chatWanted = !classic && $prefs.chatEnabled
  $: if (chatWanted && !chatRunning) {
    chatRunning = true
    void startChat()
  } else if (!chatWanted && chatRunning) {
    chatRunning = false
    stopChat()
  }
  $: if (!chatWanted && $space === 'chat') {
    openSpace('mail')
  }
  onDestroy(() => {
    if (chatRunning) {
      stopChat()
    }
  })

  // the newsletter manager and the insights page in the palette, with their
  // live shortcut as the hint.
  registerPaletteSource('mailtools', (tr) => {
    const hint = (action: 'open-newsletters' | 'open-insights'): string => {
      const combo = get(bindings)[action]
      return combo ? shortcutLabel(combo) : ''
    }
    return [
      {
        id: 'open-newsletters',
        group: 'navigate',
        label: tr('newsletters.title'),
        keywords: tr('newsletters.keywords'),
        hint: hint('open-newsletters'),
        icon: IconMailX,
        run: () => openMailScreen('newsletters'),
      },
      {
        id: 'open-insights',
        group: 'navigate',
        label: tr('settings2.page.insights'),
        keywords: tr('insights.keywords'),
        hint: hint('open-insights'),
        icon: IconChartBar,
        run: () => openSettingsAt('insights'),
      },
    ]
  })

  // the newsletter manager or the attachments browser fills the mail surface
  // in place of the list and the reader.
  $: screenInPlace = !classic && ($mailScreen !== null || $attachmentsOpen)

  // picking a folder, view or search in the sidebar puts the list back.
  $: closeScreenOn($selection)
  function closeScreenOn(_sel: unknown): void {
    closeMailScreen()
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
    nightlyOpen = !demo && !resumed && (await isNightly().catch(() => false))

    await initPrefs()
    await initSidebarState()
    await initComposePrefs()
    warmUp([
      get(prefs).design === 'classic' ? loadClassicCompose : loadComposer,
      ...(get(prefs).design === 'classic' ? [] : [loadAttachmentsBrowser]),
      ...(showMenuBar ? [loadMenuBarEditor] : []),
      loadEncryptionIndicator,
      ...(get(prefs).design === 'classic' ? [] : [loadPromiseStrip]),
    ])
    void initShortcuts()
    void initCoach().then((stop) => unsubscribers.push(stop))
    void initFirstTips()
    void initWhatsNew()
    void initFocus().then((stop) => unsubscribers.push(stop))
    void initMenuBar()
    void initPalette()
    void loadSignatures()
    void loadSnippets()
    void loadVIPSenders()
    void loadVirusTotalConfig()
    // the sidebar marks mailboxes with no stored password, so the markers have
    // to be known before the first sync raises any prompt.
    void refreshMissingPasswords()
    // a mailbox that failed its last sync is marked from the first paint, not
    // only once another sync run happens to end.
    void loadSyncStates()
    if (!resumed) {
      void restoreTabs(get(prefs).restoreTabs)
    }
    void loadProfiles()
    initProgress()
    await loadSidebar()
    // the sidebar data has to be here first: a configured or remembered folder
    // is only honoured if it still exists (#187).
    const sidebarData = get(sidebar).data
    if (sidebarData && !resumed) {
      await applyStartupSelection(get(prefs).startupSelection, sidebarData)
    }
    // the history starts where the window opened, not on the default list.
    clearNavHistory()
    void loadViews()
    await loadOutbox()

    // in demo mode, skip onboarding and show a sync in progress for the screenshot.
    if (demo) {
      onboardingOpen = false
      syncing.set(true)
    } else {
      // show the first-run onboarding until it has been completed once. the
      // new design's setup flow also comes back when a run was left half way.
      onboardingClassic = get(prefs).design === 'classic'
      try {
        const r = await getSetting(SettingKeys.onboarded)
        const done = r.found && r.value === 'true'
        let resume = false
        if (done && !onboardingClassic) {
          const p = await getSetting(SettingKeys.onboardingProgress)
          resume = p.found && p.value !== ''
        }
        onboardingOpen = !done || resume
        onboardingRerun = done && resume
      } catch {
        // if the lookup fails, do not block the app with onboarding.
        onboardingOpen = false
      }
      // both onboarding flows collect the acknowledgement as their first step,
      // so only ask separately when neither is going to run.
      const accepted = await liabilityAccepted()
      setupNeedsLegal = !accepted
      liabilityOpen = !onboardingOpen && !accepted
      if (!resumed) {
        void offerCrashReport()
      }
    }

    unsubscribers.push(
      onMailNew(() => {
        void refreshSidebar()
        void loadList(get(selection))
      }),
    )
    unsubscribers.push(
      onMailRepaired(() => {
        // the subjects and bodies changed underneath the list, so what is on
        // screen is stale rather than merely incomplete. the unread counts
        // beside it can be too, after a notification button archived or read
        // a message.
        void refreshSidebar()
        void loadList(get(selection))
      }),
    )
    unsubscribers.push(
      onSyncState((e) => {
        syncing.set(e.running)
        // record the moment a sync finishes for the status bar's last-synced time.
        if (!e.running) {
          lastSynced.set(Date.now())
          syncFolder.set('')
          syncServer.set('')
          syncAccount.set('')
          syncCounts.set(emptySyncCounts)
          // a password the server refuses is only discovered by trying, so the
          // markers can only be right after a sync has run.
          void refreshMissingPasswords()
        }
      }),
    )
    unsubscribers.push(
      onSyncProgress((e) => {
        // the closing event carries no folder name; that is what says the run
        // is over, rather than the counts, which for a resync of a cached
        // mailbox can be 0 of 0 the whole way through.
        const running = e.folder !== ''
        syncFolder.set(running ? e.folder : '')
        syncServer.set(running ? e.server : '')
        syncAccount.set(running ? e.accountEmail : '')
        syncCounts.set(
          running
            ? {
                done: e.done,
                total: e.total,
                folderDone: e.folderDone,
                folderTotal: e.folderTotal,
                foldersDone: e.foldersDone,
                foldersTotal: e.foldersTotal,
              }
            : emptySyncCounts,
        )
      }),
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
    unsubscribers.push(onFoldersChanged(() => void refreshSidebar()))
    unsubscribers.push(onProfileChanged(() => void reloadForProfile()))
    unsubscribers.push(onAgentProposals(() => void loadProposals()))
    unsubscribers.push(
      onAssistantScript((e) => {
        const key = assistantToastKey(e)
        if (key) {
          pushAction('info', $t(key).replace('{name}', e.name), { label: $t('scripts.assistant.review'), run: () => reviewScript(e.scriptId) }, 12000)
        }
      }),
    )
    // the system notification already went out; in the new design the reminder
    // also shows here with a way to the event.
    unsubscribers.push(
      onCalendarReminder((e) => {
        if (!classic) {
          const actions = [{ label: $t('calendar.show'), run: () => showReminder(e) }]
          if (e.hasAttendees) {
            actions.push({ label: $t('prep.button'), run: () => openMeetingPrep(e.eventId, e.recurrenceId) })
          }
          pushActions('info', reminderText(e), actions, 10000)
        }
      }),
    )
    unsubscribers.push(
      onTaskReminder((e) => {
        if (!classic) {
          pushActions(
            'info',
            taskReminderText(e),
            [
              { label: $t('tasks.reminder.done'), run: () => void completeFromReminder(e) },
              { label: $t('tasks.open'), run: () => openTask(e.taskId) },
            ],
            10000,
          )
        }
      }),
    )
    void loadProposals()
    unsubscribers.push(watchFollowUps())
    unsubscribers.push(onMenu(handleMenu))
    // a mailto: link opened while the app is already running.
    unsubscribers.push(onMailtoCompose((draft) => openMailtoDraft(draft)))
    // a clicked new-mail notification. the window is already back up by the
    // time this arrives; all that is left is to go to the message.
    unsubscribers.push(
      onOpenMessage((e) => revealMessage(e.messageId, e.accountId, e.folderId, get(sidebar).data ?? null)),
    )
    unsubscribers.push(
      onReplyMessage(async (e) => {
        try {
          openReply(await getMessage(e.messageId), editorMode, false)
        } catch (err) {
          toastError(errorMessage(err))
        }
      }),
    )
    // a clicked notification about a new feed entry.
    unsubscribers.push(
      onFeedsOpen((id) => {
        if (!classic) {
          feedFocus.set(id)
          openSpace('feeds')
        }
      }),
    )
    // a send and archive from a compose window, which closes as it sends.
    unsubscribers.push(
      onArchiveAfterSend((e) => {
        setTimeout(() => void markAnsweredDone(e.messageId), e.delaySeconds * 1000)
      }),
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
      // .ics files opened with Pelton, at launch and while it runs.
      unsubscribers.push(onICSOpen(() => void openPendingICS()))
      void openPendingICS()
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
  function startComposeTo(email: string, subject = '', projectId = 0): void {
    const accountId = composeAccountId()
    if (accountId === null) {
      toastError(get(t)('app.toast.addMailboxFirst'))
      return
    }
    const id = openComposeWith(accountId, editorMode, { to: email, cc: '', bcc: '', subject, body: '' })
    if (projectId) {
      updateCompose(id, { projectId })
    }
  }

  // openPendingICS takes the .ics files the system asked Pelton to open and
  // hands them to the calendar's import. Classic has no calendar to take them.
  async function openPendingICS(): Promise<void> {
    try {
      const paths = await consumePendingICS()
      if (paths.length > 0 && !classic) {
        calendarImport.set(paths)
        openSpace('calendar')
      }
    } catch {
      // nothing to open is the same as a failed lookup here.
    }
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

  function onMailboxAdded(): void {
    wizardOpen = false
    void refreshSidebar().then(flushPendingMailto)
    toastInfo(get(t)('app.toast.mailboxAdded'))
  }

  // onboarding completion is persisted so it shows only once. re-run clears it
  // from settings and reopens the flow.
  function finishOnboarding(): void {
    onboardingOpen = false
    setupNeedsLegal = false
    void setSetting(SettingKeys.onboarded, 'true')
    flushPendingMailto()
  }

  // the setup flow reports where to land; its first run also counts as having
  // seen what is new, since all of it is new then.
  function finishSetup(event: CustomEvent<{ landing: Space; rerun: boolean }>): void {
    finishOnboarding()
    if (!event.detail.rerun) {
      void markWhatsNewSeen()
    }
    openSpace(event.detail.landing)
  }

  function rerunOnboarding(): void {
    settingsOpen = false
    onboardingClassic = classic
    onboardingRerun = true
    onboardingOpen = true
  }

  function onboardingAddedMailbox(): void {
    void refreshSidebar().then(flushPendingMailto)
  }

  function focusSearch(): void {
    const input = document.querySelector<HTMLInputElement>(classic ? 'input[type="search"]' : 'input[data-top-search]')
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
    withBusyToast(get(t)('export.working'), () => exportMessagePrintView(id)).catch((err) => toastError(errorMessage(err)))
  }

  // export the currently open message as an .eml file, or tell the user to open
  // one first.
  function exportOpenEml(): void {
    const id = get(visibleMessageId)
    if (id === null) {
      toastInfo(get(t)('app.toast.exportOpenFirst'))
      return
    }
    void exportEml([id])
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
      case 'done':
        void markDone(items)
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
          await markSeen(msg, true)
          break
        case 'mark-unread':
          await markSeen(msg, false)
          break
        case 'flag':
          await markFlagged(msg, !msg.flagged)
          break
        case 'snooze':
          openSnooze(msg.id, msg.subject)
          break
        case 'download-offline':
          patchInList(msg.id, { offline: true })
          await downloadMessageOffline(msg.id)
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
            openURL(detail.unsubscribe.target)
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
          closeTab(msg.id)
          leaveList([msg.id])
          break
        }
        case 'toggle-spam':
          if (await toggleSpam(msg)) {
            closeTab(msg.id)
          }
          break
        case 'done':
          await markDone([msg])
          break
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // MenuAction covers the actions only menus emit, on top of the shortcuts.
  type MenuAction =
    | ShortcutAction
    | 'about'
    | 'toggle-low-power'
    | 'open-mailboxes'
    | 'open-contacts'
    | 'hide-window'
    | 'export-eml'

  // a mail action with no mail panes on screen, under a screen that fills the
  // mail surface or in another space, does nothing the coach should hear of.
  function swallowedMailAction(action: string): boolean {
    return !classic && isMailOnlyAction(action) && (screenInPlace || $space !== 'mail')
  }

  // dispatch maps an action (from a shortcut or a menu item) to its handler.
  function dispatchAction(action: MenuAction): void {
    // the coach tells a click from a key press here, for every action a menu,
    // the palette or a shortcut runs.
    if (!swallowedMailAction(action)) {
      noteAction(action as ShortcutAction)
    }
    // the space on screen gets the first say, so the same key makes a new
    // event in the calendar and a new message in mail.
    if (!classic && !overlayOpen && runSpaceAction($space, action)) {
      return
    }
    if (isComposeAction(action)) {
      runComposeCommand(action)
      return
    }
    // a screen in place of the list and the reader leaves no message on show
    // for a message shortcut to act on.
    if (screenInPlace && isMailOnlyAction(action)) {
      return
    }
    const place = spaceNumber(action)
    if (place !== null) {
      if (!classic) {
        goToSpaceNumber(place)
      }
      return
    }
    switch (action) {
      case 'open-newsletters':
        if (!classic) {
          openMailScreen('newsletters')
        }
        break
      case 'open-insights':
        if (!classic) {
          openSettingsAt('insights')
        }
        break
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
      case 'export-eml':
        exportOpenEml()
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
        openContacts()
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
      case 'done':
      case 'toggle-spam':
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
      case 'next-pane':
        cyclePane(1)
        break
      case 'prev-pane':
        cyclePane(-1)
        break
      case 'focus-sidebar':
        focusPaneArea('sidebar')
        break
      case 'focus-list':
        focusPaneArea('list')
        break
      case 'focus-reader':
        focusPaneArea('reader')
        break
      case 'calendar-quick-add':
        // from another space: go to the calendar, which opens quick add.
        if (!classic) {
          calendarQuickAdd.set(true)
          openSpace('calendar')
        }
        break
      case 'go-tasks':
        if (!classic) {
          openSpace('tasks')
        }
        break
      case 'go-notes':
        goToSpace('notes')
        break
      case 'notes-new':
        if (!classic && !overlayOpen) {
          requestNotes({ kind: 'new' })
        }
        break
      case 'notes-today':
        if (!classic && !overlayOpen) {
          requestNotes({ kind: 'today' })
        }
        break
      case 'notes-switcher':
        if (!classic && !overlayOpen) {
          requestNotes({ kind: 'switcher' })
        }
        break
      case 'notes-graph':
        if (!classic && !overlayOpen) {
          requestNotes({ kind: 'graph' })
        }
        break
      case 'notes-toggle-mode':
        // the editor's mode only means something with a note on screen.
        break
      case 'go-feeds':
        goToSpace('feeds')
        break
      case 'create-task':
        if (!classic) {
          createTaskFromMail()
        }
        break
      case 'go-projects':
        if (!classic) {
          goToSpace('projects')
        }
        break
      case 'add-to-project':
        if (!classic) {
          addToProject()
        }
        break
      case 'toggle-my-day':
        // from another space it brings mail back with the panel open.
        if (!classic) {
          if ($space !== 'mail') {
            openSpace('mail')
            setMyDayPanel(true)
          } else {
            setMyDayPanel(!$prefs.myDayPanel)
          }
        }
        break
      case 'undo':
        void undoOrSay()
        break
      case 'redo':
        void redoOrSay()
        break
      case 'undo-history':
        historyOpen.update((open) => !open)
        break
      case 'nav-back':
        if (!classic && !overlayOpen) {
          void goBack()
        }
        break
      case 'nav-forward':
        if (!classic && !overlayOpen) {
          void goForward()
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
      case 'go-mail':
        goToSpace('mail')
        break
      case 'go-calendar':
        goToSpace('calendar')
        break
      case 'go-contacts':
        goToSpace('contacts')
        break
      case 'go-chat':
        goToSpace('chat')
        break
      case 'go-automations':
        if (classic) {
          automationsOpen = !settingsOpen && !onboardingOpen && !wizardOpen
        } else {
          goToSpace('automations')
        }
        break
      case 'open-attachments':
        if (!classic && !overlayOpen) {
          openSpace('mail')
          toggleAttachments()
        }
        break
      case 'toggle-sender-panel':
        if (!classic) {
          setSenderPanel(!$prefs.senderPanel)
        }
        break
      case 'reading-view':
        if (!classic && get(visibleMessageId) !== null) {
          toggleReadingView()
        }
        break
      case 'shortcut-sheet':
        if (!classic && !overlayOpen) {
          toggleSheet()
        }
        break
      case 'focus-mode':
        if (!classic && !overlayOpen) {
          focusMenuOpen.update((open) => !open)
        }
        break
      case 'hide-window':
        // the app, not just the window: on macOS ordering the window out
        // strands it, and an app hidden whole comes back from the dock icon.
        hideApp()
        break
      case 'close-window':
        closeFrontmost()
        break
      case 'quit':
        quitApp()
        break
      case 'lock-app':
        if ($prefs.appLock) {
          void lockNow()
        } else {
          toastInfo($t('applock.offHint'))
          openSettingsAt('privacy')
        }
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
      case 'toggle-pin':
      case 'toggle-mute':
      case 'edit-note': {
        const msg = currentMessage()
        if (!msg) {
          toastInfo(get(t)('app.toast.openMessageFirst'))
        } else if (action === 'toggle-pin') {
          void togglePin(msg)
        } else if (action === 'toggle-mute') {
          void toggleMute(msg)
        } else {
          editNote(msg)
        }
        break
      }
      case 'next-split':
      case 'prev-split':
        break
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
      case 'done':
        void markDone([item])
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
      case 'done':
        void markDone(items)
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
  $: paletteCommands = $paletteOpen
    ? [...buildCommands(commandContext), ...(classic ? [] : extraPaletteCommands($t))]
    : []

  // opening a hit also puts the query in the search bar, so the list behind the
  // palette holds the same results the reading pane is showing rather than
  // whatever folder happened to be open.
  function openMailHit(item: MessageSummary): void {
    const query = parseQuery(get(paletteQuery)).text
    searchQuery.set(query)
    if (!classic && query.trim() !== '') {
      setDraft({ text: query, chips: [] })
      submitSearch()
      openSpace('mail')
      searchOpenedId.set(item.id)
    }
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
    await toggleWindowFullscreen()
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
  // navigation instead of just the compose editor. v starts a visual selection
  // the motions extend, and shift plus an arrow extends one directly. it never
  // fires in a text field, while a dialog/panel is open, or with cmd, ctrl or
  // alt held (so cmd+j etc. still reach the normal shortcut path).
  let lastVimKey = ''
  let lastVimKeyAt = 0

  function vimNavList(): { id: number }[] {
    return get(messageList).data?.items ?? []
  }

  // vimCursorIndex is the row motions start from: the range cursor while one is
  // out, the open message otherwise.
  function vimCursorIndex(items: { id: number }[]): number {
    const id = get(rangeCursor) ?? get(openMessageId)
    return id === null ? -1 : items.findIndex((m) => m.id === id)
  }

  // vimCursorTo moves the cursor to a row. Extending carries the selection
  // along from the anchor and leaves the reading pane alone, so a range does
  // not open and mark read every message it passes. A plain move opens the row
  // and makes it the anchor, so a selection started from it includes it.
  function vimCursorTo(items: { id: number }[], index: number, extend: boolean): void {
    const id = items[index].id
    if (extend) {
      rangeCursor.set(id)
      selectRange(items.map((m) => m.id), id)
    } else {
      rangeCursor.set(null)
      openMessageId.set(id)
      anchorAt(id)
    }
  }

  function vimMove(delta: number, extend: boolean): void {
    const items = vimNavList()
    if (items.length === 0) {
      return
    }
    const idx = vimCursorIndex(items)
    if (extend && idx !== -1) {
      ensureAnchor(items[idx].id)
    }
    const next = idx === -1 ? (delta > 0 ? 0 : items.length - 1) : Math.min(Math.max(idx + delta, 0), items.length - 1)
    vimCursorTo(items, next, extend)
  }

  function vimJump(toLast: boolean): void {
    const items = vimNavList()
    if (items.length === 0) {
      return
    }
    vimCursorTo(items, toLast ? items.length - 1 : 0, inVisual())
  }

  // vimVisual toggles visual selection at the cursor, putting the cursor on the
  // first row when it is nowhere yet.
  function vimVisual(): void {
    if (inVisual()) {
      endVisual()
      return
    }
    const items = vimNavList()
    if (items.length === 0) {
      return
    }
    const { id } = items[Math.max(vimCursorIndex(items), 0)]
    rangeCursor.set(id)
    startVisual(items.map((m) => m.id), id)
  }

  // vimEscape unwinds one layer at a time: visual mode first, keeping what it
  // selected, then the selection, then the reading pane.
  function vimEscape(): void {
    if (inVisual()) {
      endVisual()
    } else if (get(selectedIds).size > 0) {
      clearSelection()
    } else {
      openMessageId.set(null)
    }
  }

  // goToSpaceNumber shows the nth space of the navigation, from 1.
  function goToSpaceNumber(n: number): void {
    const entry = get(visibleSpaces)[n - 1]
    if (entry) {
      goToSpace(entry.key)
    }
  }

  // reviewScript opens a script an assistant created or proposed a change to.
  function reviewScript(id: number): void {
    selectScript(id)
    if (classic) {
      automationsOpen = !settingsOpen && !onboardingOpen && !wizardOpen
    } else {
      goToSpace('automations')
    }
  }

  // goToSpace shows one of the spaces from its shortcut. classic has no spaces
  // but does have a contacts screen; a space this build lacks is left alone.
  function goToSpace(key: Space): void {
    if (classic) {
      if (key === 'contacts') {
        openContacts()
      }
      return
    }
    // a space left out of the navigation still opens; only one switched off
    // does not.
    if (overlayOpen || !get(orderedSpaces).some((s) => s.key === key)) {
      return
    }
    openSpace(key)
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
    if (!$prefs.appVimMode || event.metaKey || event.ctrlKey || event.altKey) {
      return false
    }
    if (isEditableTarget(event.target) || inSidebar(event.target) || anyDialogOpen() || screenInPlace) {
      return false
    }
    // shift is part of G itself; with anything else it can only be extending
    // the selection.
    if (event.shiftKey && event.key !== 'G') {
      return tryVimExtend(event)
    }
    const now = Date.now()
    switch (event.key) {
      case 'j':
        vimMove(1, inVisual())
        break
      case 'k':
        vimMove(-1, inVisual())
        break
      case 'l':
      case 'Enter':
        if (get(openMessageId) === null) {
          vimMove(1, inVisual())
        }
        break
      case 'h':
        openMessageId.set(null)
        break
      case 'Escape':
        vimEscape()
        break
      case 'v':
        if (!$prefs.multiSelectEnabled) {
          lastVimKey = ''
          return false
        }
        vimVisual()
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

  // tryVimExtend handles shift plus an arrow, which extends the selection
  // without entering visual mode. A binding the user put on the same keys wins.
  function tryVimExtend(event: KeyboardEvent): boolean {
    if ((event.key !== 'ArrowDown' && event.key !== 'ArrowUp') || !$prefs.multiSelectEnabled) {
      return false
    }
    if (matchShortcut(event, $activeBindings) !== null) {
      return false
    }
    vimMove(event.key === 'ArrowDown' ? 1 : -1, true)
    lastVimKey = ''
    return true
  }

  const newDesignOnly = new Set<ShortcutAction>(shortcutRegistry.filter((s) => s.newDesignOnly).map((s) => s.action))

  function onKeydown(event: KeyboardEvent): void {
    // while the settings panel is capturing a new binding, let it have the keys.
    if ($recording) {
      return
    }
    // Escape that has something of ours to close is ours alone. left to the
    // system as well, it would also take a full screen window out of full
    // screen, which should only happen from the plain main view.
    if (event.key === 'Escape' && (anyDialogOpen() || overlayOpen || popupIsOpen())) {
      event.preventDefault()
    }
    if ($prefs.design !== 'classic' && openMenuFromKeyboard(event)) {
      return
    }
    if (tryVimNav(event)) {
      event.preventDefault()
      return
    }
    const action = matchShortcut(event, $activeBindings)
    // undo and redo belong to a text field, a composer or an editor while one
    // has focus, and are the history's everywhere else.
    if ((action === 'undo' || action === 'redo') && isEditableTarget(event.target)) {
      return
    }
    // the split keys belong to the message list, which takes them itself while
    // it has focus; anywhere else Tab keeps moving focus.
    if (action === 'next-split' || action === 'prev-split') {
      return
    }
    // an editor that already used the keys (indenting on mod+[, say) keeps
    // them, and alt with an arrow moves through a text field's words.
    if ((action === 'nav-back' || action === 'nav-forward') && (event.defaultPrevented || (event.altKey && isEditableTarget(event.target)))) {
      return
    }
    if (action) {
      // a binding that types a character must not hijack typing in a field.
      if (comboTypes($bindings[action]) && isEditableTarget(event.target)) {
        return
      }
      const scriptId = scriptOfAction(action)
      if (scriptId !== null) {
        event.preventDefault()
        runAt('shortcut', scriptId, currentMessageItems())
        return
      }
      if (classic && newDesignOnly.has(action)) {
        if (selectTabByNumber(event)) {
          event.preventDefault()
        }
        return
      }
      // a recipient move only means something on a focused recipient chip;
      // anywhere else its keys keep their own meaning.
      if ((chipActions as readonly string[]).includes(action) &&!(event.target as HTMLElement | null)?.closest?.('[data-chip]')) {
        return
      }
      // like send, a send variant only answers inside a composer, which takes
      // the key before it gets here.
      if ((sendActions as readonly string[]).includes(action)) {
        return
      }
      event.preventDefault()
      if (!swallowedMailAction(action)) {
        noteShortcut(action)
      }
      dispatchAction(action)
      return
    }
    // cmd/ctrl+1 to 9 jump between the reading-pane chips, the way they jump
    // between tabs in a browser: 1 is the pane, 9 is the last tab. Checked after
    // the bindings, so a key the user assigned themselves still wins, and only
    // while tabs exist, so the combination is otherwise left alone. The new
    // design gives those keys to the spaces, so there the chips also answer
    // with alt added.
    if (selectTabByNumber(event)) {
      event.preventDefault()
    }
  }

  // selectTabByNumber handles cmd/ctrl+1 to 9, and cmd/ctrl+alt+1 to 9 in the
  // new design outside a text field, and reports whether it acted. The chips
  // are the untabbed pane followed by the tabs, so 1 is the pane and the
  // numbers after it count along the bar. 9 is the last chip however many there
  // are, which is what a browser does.
  function selectTabByNumber(event: KeyboardEvent): boolean {
    if (!(event.metaKey || event.ctrlKey) || event.shiftKey) {
      return false
    }
    if (event.altKey && (classic || isEditableTarget(event.target))) {
      return false
    }
    // alt turns a digit into a symbol on some layouts, so its key code counts.
    const slot = event.altKey ? Number(/^Digit([1-9])$/.exec(event.code)?.[1]) : Number(event.key)
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
    // the places of the profile left behind are not this one's.
    clearNavHistory()
    void loadSignatures()
    void loadVIPSenders()
    void loadViews()
    void refreshFocus()
    await loadList(get(selection))
  }

  // anything deep in the tree can ask for the settings screen on a category;
  // the panel itself is owned here. See stores/settingsnav.ts.
  $: if ($settingsRequest) {
    settingsCategory = $settingsRequest
    settingsOpen = true
    clearSettingsRequest()
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

  function resizeListHeight(event: CustomEvent<number>): void {
    dragging = true
    listH = clamp(listH + event.detail, 140, Math.max(140, window.innerHeight * 0.7))
  }

  function commitListHeight(): void {
    dragging = false
    setListHeight(listH)
  }

  function commitPanes(): void {
    dragging = false
    setPaneWidths(sidebarW, listW)
  }
</script>

<svelte:window on:keydown={onKeydown} on:mousedown={onNavMouseDown} on:mouseup={onNavMouseUp} on:contextmenu={onContextMenu} on:resize={refreshFullscreen} bind:innerWidth={windowWidth} />

<!-- macOS with no in-app menu bar: an empty top row for the traffic lights, and
     a matching strip above every overlay so the window can still be dragged
     while settings or onboarding is open. with the menu bar on, the bar itself
     is that row and handles both. -->
{#if macTitlebar && (classic ? !showMenuBar : overlayOpen)}
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="titlebar-drag"
    style="--wails-draggable:drag"
    aria-hidden="true"
    on:dblclick={titleBarDoubleClick}
  >
    <!-- the app mark sits where a native title bar would put the title, just
         past the traffic lights. the new design's overlays have headers of
         their own there. -->
    {#if classic}
      <img class="titlebar-logo" src={peltonLogo} alt="" draggable="false" />
    {/if}
  </div>
{/if}

{#if !classic}
<div class="frame" class:rail-right={$prefs.railSide === 'right'} class:has-rail={showRail} data-file-drop-target="app">
  <TopBar tabs={$prefs.navStyle === 'tabs' || railFolded}>
    <svelte:fragment slot="menu">
      {#if showMenuBar}
        <MenuBar on:action={(e) => handleMenu(e.detail)} />
      {/if}
    </svelte:fragment>
  </TopBar>

  <div class="body">
    {#if showRail}
      <Rail
        side={$prefs.railSide}
        unread={inboxUnread}
        on:search={focusSearch}
        on:settings={() => { settingsCategory = $whatsNewUnseen ? 'about#about-whats-new' : null; settingsOpen = true }}
      />
    {/if}

    {#if $space === 'mail'}
      <div
        class="mail"
        class:with-myday={showMyDay}
        role="main"
        aria-label={$t('space.mail')}
        style={`grid-template-columns: ${sidebarW}px 0 minmax(0, 1fr)${showMyDay ? ' var(--myday-width)' : ''}`}
        bind:clientWidth={mailWidth}
      >
        <div class="side-col">
          <Sidebar
            on:compose={startCompose}
            on:sync={runSync}
            on:addMailbox={addMailbox}
          />
          {#if $prefs.navStyle === 'footer'}
            <div class="side-foot">
              <SpaceSwitcher />
            </div>
          {/if}
        </div>
        <Resizer disabled={locked} label={$t('app.pane.resizeSidebar')} on:resize={resizeSidebar} on:end={commitPanes} />
        {#if $mailScreen === 'newsletters'}
          <div class="screen">
            {#await import('./components/newsletters/NewsletterManager.svelte') then m}
              <svelte:component this={m.default} on:close={closeMailScreen} />
            {/await}
          </div>
        {:else}
        <div
          class="surface"
          class:stacked={stacked && !$attachmentsOpen}
          class:browsing={$attachmentsOpen}
          style={$attachmentsOpen
            ? ''
            : stacked
              ? `grid-template-rows: ${listH}px 0 minmax(0, 1fr)`
              : `grid-template-columns: ${listW}px 0 minmax(0, 1fr)`}
        >
          {#if $attachmentsOpen}
            {#await loadAttachmentsBrowser() then m}
              <svelte:component this={m.default} />
            {/await}
          {:else}
            <MessageList />
            {#if stacked}
              <Resizer orientation="horizontal" disabled={locked} label={$t('app.pane.resizeMessageList')} on:resize={resizeListHeight} on:end={commitListHeight} />
            {:else}
              <Resizer disabled={locked} label={$t('app.pane.resizeMessageList')} on:resize={resizeList} on:end={commitPanes} />
            {/if}
            <MessageDetail />
          {/if}
          <ComposeTabLayer />
        </div>
        {/if}
        {#if showMyDay}
          <MyDayPanel on:close={() => setMyDayPanel(false)} />
        {/if}
      </div>
    {:else if $space === 'contacts'}
      <div class="space-host" role="main" aria-label={$t(`space.${$space}`)}>
        {#await import('./components/contacts/ContactsSpace.svelte') then m}
          <svelte:component
            this={m.default}
            on:compose={(e) => { openSpace('mail'); startComposeTo(e.detail) }}
          />
        {/await}
      </div>
    {:else if $space === 'calendar'}
      <div class="space-host" role="main" aria-label={$t(`space.${$space}`)}>
        {#await import('./components/calendar/CalendarScreen.svelte') then m}
          <svelte:component this={m.default} />
        {/await}
      </div>
    {:else if $space === 'tasks'}
      <div class="space-host" role="main" aria-label={$t(`space.${$space}`)}>
        {#await import('./components/tasks/TasksScreen.svelte') then m}
          <svelte:component this={m.default} />
        {/await}
      </div>
    {:else if $space === 'projects'}
      <div class="space-host" role="main" aria-label={$t(`space.${$space}`)}>
        {#await import('./components/projects/ProjectsScreen.svelte') then m}
          <svelte:component
            this={m.default}
            on:compose={(e) => { openSpace('mail'); startComposeTo(e.detail.email, '', e.detail.projectId) }}
          />
        {/await}
      </div>
    {:else if $space === 'notes'}
      <div class="space-host" role="main" aria-label={$t(`space.${$space}`)}>
        {#await import('./components/notes/NotesScreen.svelte') then m}
          <svelte:component this={m.default} />
        {/await}
      </div>
    {:else if $space === 'feeds'}
      <div class="space-host" role="main" aria-label={$t(`space.${$space}`)}>
        {#await import('./components/feeds/FeedsScreen.svelte') then m}
          <svelte:component this={m.default} on:compose={(e) => shareFromFeeds(e.detail)} />
        {/await}
      </div>
    {:else if $space === 'chat' && chatWanted}
      <div class="space-host" role="main" aria-label={$t(`space.${$space}`)}>
        {#await import('./components/chat/ChatSpace.svelte') then m}
          <svelte:component this={m.default} />
        {/await}
      </div>
    {:else if $space === 'automations'}
      <div class="space-host" role="main" aria-label={$t(`space.${$space}`)}>
        {#await import('./components/automations/AutomationsSpace.svelte') then m}
          <svelte:component this={m.default} />
        {/await}
      </div>
    {/if}
  </div>

  <StatusBar />
</div>
{:else}
<div class="shell" class:with-topbar={showMenuBar || macTitlebar} data-file-drop-target="app">
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
{/if}

{#if $composeSessions.length > 0}
  <div class="compose-layer">
    {#each $composeSessions as session (session.id)}
      {#if classic}
        {#await loadClassicCompose() then m}
          <svelte:component this={m.default} {session} />
        {/await}
      {:else if session.placement === 'docked'}
        {#await loadComposer() then m}
          <svelte:component this={m.default} {session} layout="docked" mainWindow />
        {/await}
      {/if}
    {/each}
  </div>
{/if}

<!-- settings and the wizard are code-split: their js/css load only when opened,
     so they cost nothing at startup. compose is split too, and loaded once the
     window has settled so it is ready before it is first opened. -->
{#if automationsOpen && classic}
  {#await import('./components/automations/AutomationsOverlay.svelte') then m}
    <svelte:component this={m.default} on:close={() => (automationsOpen = false)} />
  {/await}
{/if}

<AutomationsHost />

{#if contactsOpen}
  {#await import('./components/contacts/ContactsScreen.svelte') then m}
    <svelte:component
      this={m.default}
      on:close={() => (contactsOpen = false)}
      on:compose={(e) => { contactsOpen = false; startComposeTo(e.detail) }}
    />
  {/await}
{/if}

{#if settingsOpen && settingsPanel}
  {#await settingsPanel then m}
    {#if m}
      <svelte:component
        this={m.default}
        initialCategory={settingsCategory}
        on:close={() => (settingsOpen = false)}
        on:rerunOnboarding={rerunOnboarding}
      />
    {/if}
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
    <svelte:component this={m.default} on:accepted={() => { liabilityOpen = false; setupNeedsLegal = false }} />
  {/await}
{/if}

<!-- onboarding is code-split too: shown only on first run or when re-run. -->
{#if onboardingOpen && onboardingClassic}
  {#await import('./components/onboarding/Onboarding.svelte') then m}
    <svelte:component this={m.default} on:finish={finishOnboarding} on:added={onboardingAddedMailbox} />
  {/await}
{:else if onboardingOpen}
  {#await import('./components/onboarding/Setup.svelte') then m}
    <svelte:component
      this={m.default}
      rerun={onboardingRerun}
      blocked={nightlyOpen}
      legal={setupNeedsLegal}
      on:finish={finishSetup}
      on:added={onboardingAddedMailbox}
      on:classic={() => (onboardingClassic = true)}
    />
  {/await}
{/if}

{#if !classic && $whatsNewOpen}
  {#await import('./components/whatsnew/WhatsNew.svelte') then m}
    <svelte:component this={m.default} on:close={() => whatsNewOpen.set(false)} />
  {/await}
{/if}

{#if wizardOpen}
  {#await import('./components/wizard/AddMailboxWizard.svelte') then m}
    <svelte:component this={m.default} on:close={() => (wizardOpen = false)} on:added={onMailboxAdded} />
  {/await}
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

{#if !classic && $meetingPrepTarget}
  {#await import('./components/calendar/MeetingPrep.svelte') then m}
    <svelte:component
      this={m.default}
      target={$meetingPrepTarget}
      on:close={closeMeetingPrep}
      on:compose={(e) => { closeMeetingPrep(); openSpace('mail'); startComposeTo(e.detail.to, e.detail.subject) }}
    />
  {/await}
{/if}

<Toasts />
<ContextMenu />
<ConfirmDialog />
<DragStack />
<!-- the dialogs below show only when asked for, so their code loads after the
     window's first paint. -->
{#if classic}
  {#await import('./components/detail/classic/SnoozeDialog.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
  {#await import('./components/sidebar/classic/FolderDialog.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
{:else}
  {#await import('./components/detail/SnoozeDialog.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
  {#await import('./components/sidebar/FolderDialog.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
{/if}
{#await import('./components/detail/AttachmentPreview.svelte') then m}
  <svelte:component this={m.default} />
{/await}
{#if classic}
  {#await import('./components/detail/classic/MoveDialog.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
{:else}
  {#await import('./components/detail/MoveDialog.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
{/if}

{#await import('./components/common/CommandPalette.svelte') then m}
  <svelte:component this={m.default} commands={paletteCommands} mail={paletteMailCommands} />
{/await}

{#if !classic}
  <CoachLayer suspended={overlayOpen} />
  <FirstTipLayer />
{/if}

{#if chatWanted}
  {#await import('./components/chat/ShareToChatDialog.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
  {#await import('./components/chat/VerificationDialog.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
{/if}

{#await import('./components/settings/AccountPasswordDialog.svelte') then m}
  <svelte:component this={m.default} account={$passwordPrompt} onDone={answerPasswordPrompt} />
{/await}

{#await import('./components/common/SyncFailureDialog.svelte') then m}
  <svelte:component this={m.default} />
{/await}

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

  /* the new design's window: top bar, then the rail beside the current space,
     then the status bar, all on the chrome surface. */
  .frame {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto;
    height: 100%;
    width: 100%;
    overflow: hidden;
    background: var(--chrome-bg);
  }

  :global(html.window-material) .frame {
    background: transparent;
  }

  .body {
    display: flex;
    min-height: 0;
    padding-inline-end: var(--pane-gap);
  }

  .frame.rail-right .body {
    flex-direction: row-reverse;
    padding-inline-end: 0;
  }

  .frame.rail-right:not(.has-rail) .body {
    padding-inline-end: var(--pane-gap);
  }

  .mail {
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    flex: 1;
    min-width: 0;
    min-height: 0;
  }

  .side-col {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
    min-width: 0;
    min-height: 0;
  }

  .side-foot {
    padding: var(--space-3);
  }

  /* the list and the reader. hybrid raises the two as one rounded surface. */
  .surface {
    position: relative;
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    background: var(--surface-raised);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--pane-radius);
  }

  .surface.stacked,
  .surface.browsing {
    grid-template-columns: minmax(0, 1fr);
  }

  :global(:root[data-panes='soft']) .surface.browsing > :global(.browser) {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--pane-radius);
    overflow: hidden;
  }

  /* a mail-level screen in place of the list and the reader. */
  .screen {
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--pane-radius);
    background: var(--surface-base);
  }

  :global(:root[data-panes='flat']) .screen {
    border-width: var(--hairline) 0 var(--hairline) var(--hairline);
  }

  .surface.stacked > :global(.list-col) {
    border-inline-end: none;
    border-bottom: var(--hairline) solid var(--border-default);
  }

  /* flat: edge to edge, split by hairlines only. */
  :global(:root[data-panes='flat']) .surface {
    border-width: var(--hairline) 0 var(--hairline) var(--hairline);
  }

  .frame .side-col {
    position: relative;
    background: var(--sidebar-bg);
    border-start-start-radius: var(--pane-radius);
    border-end-start-radius: var(--pane-radius);
  }

  /* the folder pane's color runs on under the rounded corners of the surface
     beside it, so no chrome shows through between the two. */
  .frame .side-col::after {
    content: '';
    position: absolute;
    inset-block: 0;
    inset-inline-start: 100%;
    width: var(--pane-radius);
    background: var(--sidebar-bg);
    pointer-events: none;
  }

  :global(:root[data-panes='soft']) .frame .side-col {
    border-radius: var(--pane-radius);
  }

  :global(:root[data-panes='soft']) .frame .side-col::after {
    display: none;
  }

  /* soft: every pane is a rounded panel of its own, with a gap between. */
  :global(:root[data-panes='soft']) .surface {
    gap: var(--pane-gap);
    background: transparent;
    border: none;
    border-radius: 0;
    overflow: visible;
  }

  :global(:root[data-panes='soft']) .surface > :global(.list-col),
  :global(:root[data-panes='soft']) .surface > :global(.detail),
  :global(:root[data-panes='soft']) .side-col {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--pane-radius);
    overflow: hidden;
  }

  :global(:root[data-panes='soft']) .side-col {
    background: var(--surface-raised);
    margin-inline-end: var(--pane-gap);
  }

  :global(:root[data-panes='soft']) .frame:not(.has-rail) .side-col,
  :global(:root[data-panes='soft']) .frame.rail-right .side-col {
    margin-inline-start: var(--pane-gap);
  }

  /* a space other than mail fills the room beside the rail. its screen was
     written as a fixed overlay; containing layout makes this box, not the
     window, what it fills, and it has no traffic lights to keep clear of. */
  .space-host {
    position: relative;
    flex: 1;
    min-width: 0;
    contain: layout;
    overflow: hidden;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--pane-radius);
    --titlebar-lights: 0px;
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

  .titlebar-logo {
    position: absolute;
    left: var(--titlebar-inset);
    top: var(--titlebar-lights-center);
    height: calc(16px / var(--ui-scale, 1));
    width: auto;
    transform: translateY(-50%);
    opacity: 0.85;
    pointer-events: none;
    user-select: none;
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
