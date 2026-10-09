<script lang="ts">
  // the settings screen. it fills the window and uses a two-column layout: a
  // category nav on the left and the selected category's controls on the right,
  // so the (many) preferences stay übersichtlich instead of one long scroll.
  // every control has a short hint/tooltip. no preference was removed in the
  // reorganization — they are only grouped.
  import {
    IconX,
    IconSearch,
    IconFolderOpen,
    IconFileExport,
    IconRefresh,
  } from '@tabler/icons-svelte'
  import { createEventDispatcher, onMount } from 'svelte'
  import SegmentedSetting from './SegmentedSetting.svelte'
  import StepSlider from './StepSlider.svelte'
  import AccentPicker from './AccentPicker.svelte'
  import TechToggles from './TechToggles.svelte'
  import ToastPositionPicker from './ToastPositionPicker.svelte'
  import ShortcutSettings from './ShortcutSettings.svelte'
  import SignaturesSection from './SignaturesSection.svelte'
  import AddressBookSection from './AddressBookSection.svelte'
  import MailboxesSection from './MailboxesSection.svelte'
  import ProfilesSection from './ProfilesSection.svelte'
  import ContactsSection from './ContactsSection.svelte'
  import NetworkSection from './NetworkSection.svelte'
  import ExternalSection from './ExternalSection.svelte'
  import { setEditing as setMenuBarEditing, menuBarNewItems, setNewItemsMode, type NewItemsMode } from '../../stores/menubar'
  import ImportExportSection from './ImportExportSection.svelte'
  import ThemesSection from './ThemesSection.svelte'
  import ThemedIcon from '../common/ThemedIcon.svelte'
  import RowLayoutPreview from './RowLayoutPreview.svelte'
  import AboutSection from './AboutSection.svelte'
  import EncryptionSection from './EncryptionSection.svelte'
  import PassphraseDialog from './PassphraseDialog.svelte'
  import { settingsCategories, settingsGroups } from '../../lib/settingscategories'
  import { paletteQuickSelect, setPaletteQuickSelect } from '../../stores/palette'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import LanguageSelect from '../common/LanguageSelect.svelte'
  import DateTimePicker from '../common/DateTimePicker.svelte'
  import { pfpDataUri, type PfpStyle } from '../../lib/pfp'
  import { initials } from '../../lib/format'
  import { sidebar } from '../../stores/accounts'
  import {
    prefs,
    setTheme,
    setDensity,
    setCornerStyle,
    setUIScale,
    setMessageFontSize,
    setToastPosition,
    setNotifyNewMail,
    setUnreadBadgeEnabled,
    setPaneLocked,
    setSendDelay,
    setFlagHighlight,
    setShortcutHints,
    setShowAccountEmail,
    setAlwaysLoadImages,
    setBlockTrackingPixels,
    setAvatarSource,
    setAvatarStyle,
    setMultiSelectEnabled,
    setSelectAllScope,
    setSearchSort,
    searchSortPref,
    setSelectAllUnified,
    setShowSelectedCount,
    setSidebarIndentGuides,
    setShowUnsyncedFolder,
    setRestoreTabs,
    setStartupSelection,
    setShowFlaggedCount,
    setViewsPlacement,
    setRowTemplate,
    setRowShowAvatar,
    setRowShowSnippet,
    setPreviewLines,
    setFlagColorSync,
    setShowOfflineIndicator,
    setSwipeEnabled,
    setSwipeLeftAction,
    setSwipeRightAction,
    setComposeVimMode,
    setAppVimMode,
    setDownloadIncludeAttachments,
    setLanguage,
    setLowPowerMode,
    setAutoSyncInterval,
    setVerboseSync,
    setSyncProgressBar,
    setCloseAction,
    setSyncMessageLimit,
    setSyncMaxParallel,
    setSyncFullReconcileDays,
    setSyncAutoBackfill,
    setDefaultEditorMode,
    setComposeAutocomplete,
    setComposeChips,
    setEmptyStateImage,
    setEmptyStateFullscreen,
    setMenuBarInApp,
    setMenuBarNativeMinimal,
    setMenuBarIcons,
    setTimeFormat,
    setReduceMotion,
    setHandCursor,
    setThemeDarkTimes,
    setBodyFont,
    setSenderFonts,
    setUIFont,
    setMonoFont,
    setLogToFile,
    setLogLevel,
    setLogMessageMetadata,
    setCrashLogs,
  } from '../../stores/prefs'
  import peltonLogo from '../../assets/images/icons/pelton-logo.png'
  import { isMac } from '../../lib/i18n'
  import { downloadRange, cancelDownload, openLocalesFolder, saveLocaleTemplate, getLogStatus, openLogFolder, deleteLogs } from '../../lib/api'
  import en from '../../lib/locales/en'
  import { downloadProgress } from '../../stores/progress'
  import { toastInfo, toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { ThemePref, DensityPref, EditorMode, ViewsPlacement, CloseAction, LogLevel, LogStatus, SelectAllScope, SearchKind, SearchSortPref } from '../../lib/types'
  import { searchSorts, automaticSort } from '../../lib/searchsort'
  import Select from '../common/Select.svelte'
  import type { SelectItem } from '../../lib/selectnav'

  // the three kinds of search that remember their own result order (#404).
  const searchSortRows: { kind: SearchKind; label: string }[] = [
    { kind: 'text', label: 'settingsPanel.searchSort.text' },
    { kind: 'dated', label: 'settingsPanel.searchSort.dated' },
    { kind: 'filtered', label: 'settingsPanel.searchSort.filtered' },
  ]

  let editorModeOptions: { key: EditorMode; label: string }[] = []
  $: editorModeOptions = [
    { key: 'plaintext', label: $t('settingsPanel.editorMode.plaintext') },
    { key: 'markdown', label: $t('settingsPanel.editorMode.markdown') },
    { key: 'wysiwyg', label: $t('settingsPanel.editorMode.wysiwyg') },
  ]

  const dispatch = createEventDispatcher<{ close: void; rerunOnboarding: void }>()

  // the unread badge setting is a count on the dock tile on macOS and a dot on
  // the tray icon on Windows and Linux, so its row is labelled for the one at hand.
  const badgeToggleKey = isMac ? 'settingsPanel.toggle.dockBadge' : 'settingsPanel.toggle.trayBadge'
  const badgeHintKey = isMac ? 'settingsPanel.hint.dockBadge' : 'settingsPanel.hint.trayBadge'
  $: currentLocale = $prefs.language

  // languageReload remounts the picker so newly dropped language files show
  // up without leaving settings.
  let languageReload = 0

  async function onOpenLocalesFolder(): Promise<void> {
    try {
      await openLocalesFolder()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // onSaveLocaleTemplate writes a ready-to-translate template: every English
  // string plus the meta fields a language file needs.
  async function onSaveLocaleTemplate(): Promise<void> {
    const template = JSON.stringify({ name: 'My Language', author: '', base: 'en', strings: en }, null, 2)
    try {
      const path = await saveLocaleTemplate(template)
      if (path) {
        toastInfo($t('language.templateSaved'))
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // left-nav categories. each maps to a block rendered on the right. iconName
  // is the themeable icon slot (the tabler name in kebab, see ThemedIcon).
  // group places the category under a top-level heading in the nav; keywords is
  // localized search text (synonyms and notable sub-settings) so the search box
  // can find a category by the toggles it holds, not just its title. Nothing is
  // removed here, only grouped and made searchable.
  $: categories = settingsCategories($t)
  $: navGroups = settingsGroups($t)

  // per-setting search index: each entry is one individual setting, mapped to the
  // category that now holds it. `label` is the control's own localized label, so
  // search finds a setting by name, not just its category. `kw` adds synonyms.
  $: settingsIndex = [
    { cat: 'appearance', label: $t('settingsPanel.label.theme'), kw: 'dark light schedule' },
    { cat: 'appearance', label: $t('onboarding.accentTitle'), kw: 'accent color' },
    { cat: 'appearance', label: $t('settingsPanel.label.density'), kw: '' },
    { cat: 'appearance', label: $t('settingsPanel.label.corners'), kw: 'rounded square' },
    { cat: 'appearance', label: $t('settingsPanel.label.interfaceScale'), kw: 'zoom scale size' },
    { cat: 'appearance', label: $t('settingsPanel.toggle.reduceMotion'), kw: 'animation' },
    { cat: 'appearance', label: $t('settingsPanel.toggle.handCursor'), kw: 'cursor pointer mouse arrow hand' },
    { cat: 'appearance', label: $t('settingsPanel.label.emptyStateImage'), kw: 'background image empty' },
    { cat: 'menubar', label: $t('settingsPanel.toggle.menuBarInApp'), kw: 'menu bar' },
    { cat: 'menubar', label: $t('settingsPanel.toggle.menuBarNativeMinimal'), kw: 'menu bar native' },
    { cat: 'menubar', label: $t('settingsPanel.toggle.menuBarIcons'), kw: 'menu bar icons' },
    { cat: 'menubar', label: $t('menuBar.editButton'), kw: 'menu bar edit' },
    { cat: 'menubar', label: $t('menuBar.newItems.label'), kw: 'menu bar new items' },
    { cat: 'themes', label: $t('settingsPanel.category.themes'), kw: 'theme pack import' },
    { cat: 'language', label: $t('settings.language'), kw: 'locale translation' },
    { cat: 'list', label: $t('settingsPanel.label.rowLayout'), kw: 'rows layout' },
    { cat: 'list', label: $t('settingsPanel.toggle.showSenderAvatar'), kw: 'avatar' },
    { cat: 'list', label: $t('settingsPanel.toggle.showMessagePreview'), kw: 'preview snippet' },
    { cat: 'list', label: $t('settingsPanel.label.previewLines'), kw: 'preview lines' },
    { cat: 'list', label: $t('settingsPanel.label.flaggedHighlight'), kw: 'flag highlight' },
    { cat: 'list', label: $t('settingsPanel.toggle.showMailboxEmail'), kw: 'account email' },
    { cat: 'list', label: $t('settingsPanel.toggle.multiSelect'), kw: 'select multiple' },
    { cat: 'list', label: $t('settingsPanel.toggle.indentGuides'), kw: 'sidebar folders' },
    { cat: 'list', label: $t('settingsPanel.toggle.flaggedCount'), kw: 'sidebar flagged count' },
    { cat: 'list', label: $t('settingsPanel.label.startupSelection'), kw: 'sidebar startup launch open default folder view last used' },
    { cat: 'list', label: $t('views.setting.label'), kw: 'views saved searches' },
    { cat: 'list', label: $t('settingsPanel.label.senderPhotos'), kw: 'avatar gravatar' },
    { cat: 'list', label: $t('settingsPanel.label.generatedStyle'), kw: 'avatar style' },
    { cat: 'display', label: $t('onboarding.extras.fontSize'), kw: 'message font size' },
    { cat: 'display', label: $t('settingsPanel.label.timeFormat'), kw: 'clock 24 hour' },
    { cat: 'display', label: $t('settingsPanel.label.bodyFont'), kw: 'font' },
    { cat: 'display', label: $t('settingsPanel.toggle.senderFonts'), kw: 'font email typeface sender' },
    { cat: 'list', label: $t('settingsPanel.label.selectAllScope'), kw: 'select all bulk selection' },
    { cat: 'list', label: $t('settingsPanel.subhead.searchSort'), kw: 'search sort order relevance newest oldest alphabetical subject' },
    { cat: 'list', label: $t('settingsPanel.toggle.selectAllUnified'), kw: 'select all unified inbox' },
    { cat: 'display', label: $t('settingsPanel.label.uiFont'), kw: 'font interface' },
    { cat: 'display', label: $t('settingsPanel.label.monoFont'), kw: 'font monospace code' },
    { cat: 'display', label: $t('settingsPanel.label.charsetFallback'), kw: 'encoding charset unicode gibberish mojibake utf8 latin' },
    { cat: 'display', label: $t('settings.lockPanes'), kw: 'panes layout lock' },
    { cat: 'composing', label: $t('settingsPanel.label.defaultEditor'), kw: 'editor html plain markdown' },
    { cat: 'composing', label: $t('settingsPanel.toggle.autocomplete'), kw: 'autocomplete recipients' },
    { cat: 'composing', label: $t('settingsPanel.toggle.chipRecipients'), kw: 'chips recipients' },
    { cat: 'composing', label: $t('onboarding.extras.vimEditor'), kw: 'vim' },
    { cat: 'composing', label: $t('settingsPanel.label.undoSendWindow'), kw: 'undo send delay' },
    { cat: 'signatures', label: $t('settingsPanel.category.signatures'), kw: 'signature footer' },
    { cat: 'notifications', label: $t('settings.toastPosition'), kw: 'notification position' },
    { cat: 'notifications', label: $t('vip.notifyNewMail'), kw: 'new mail notification' },
    { cat: 'notifications', label: $t(badgeToggleKey), kw: isMac ? 'dock badge unread count icon' : 'tray badge dot unread count icon' },
    { cat: 'notifications', label: $t('vip.manageLabel'), kw: 'vip senders' },
    { cat: 'gestures', label: $t('settingsPanel.toggle.swipeEnabled'), kw: 'swipe gesture' },
    { cat: 'gestures', label: $t('settingsPanel.label.swipeLeft'), kw: 'swipe left' },
    { cat: 'gestures', label: $t('settingsPanel.label.swipeRight'), kw: 'swipe right' },
    { cat: 'privacy', label: $t('settingsPanel.toggle.alwaysLoadImages'), kw: 'remote images tracking' },
    { cat: 'privacy', label: $t('settingsPanel.toggle.blockTrackingPixels'), kw: 'tracking pixel spy pixel read receipt open tracking' },
    { cat: 'privacy', label: $t('settingsPanel.label.manageWhitelist'), kw: 'trusted senders allowlist' },
    { cat: 'privacy', label: $t('settingsPanel.category.network'), kw: 'proxy connection tls socks' },
    { cat: 'privacy', label: $t('settingsPanel.toggle.logToFile'), kw: 'log logging debug file troubleshooting' },
    { cat: 'privacy', label: $t('settingsPanel.toggle.crashLogs'), kw: 'crash report stack trace panic' },
    { cat: 'mailboxes', label: $t('settingsPanel.category.mailboxes'), kw: 'accounts imap smtp servers' },
    { cat: 'mailboxes', label: $t('settingsPanel.category.contacts'), kw: 'address book people' },
    { cat: 'external', label: $t('settingsPanel.category.external'), kw: 'default mail client links browser' },
    { cat: 'external', label: $t('settingsPanel.category.importExport'), kw: 'backup restore transfer' },
    { cat: 'external', label: $t('import.tb.title'), kw: 'thunderbird migrate switch move' },
    { cat: 'external', label: $t('import.files.title'), kw: 'eml mbox archive file import' },
    { cat: 'external', label: $t('mcp.permissions'), kw: 'mcp agent ai permissions write send delete' },
    { cat: 'external', label: $t('mcp.log'), kw: 'mcp agent ai audit log history' },
    { cat: 'power', label: $t('settingsPanel.toggle.lowPowerMode'), kw: 'battery energy' },
    { cat: 'power', label: $t('settingsPanel.label.autoSyncInterval'), kw: 'sync interval' },
    { cat: 'power', label: $t('settingsPanel.toggle.verboseSync'), kw: 'sync status' },
    { cat: 'power', label: $t('settingsPanel.toggle.syncProgressBar'), kw: 'sync progress bar percent count' },
    { cat: 'power', label: $t('settingsPanel.label.closeAction'), kw: 'close button tray background quit exit' },
    { cat: 'power', label: $t('settingsPanel.label.syncMaxParallel'), kw: 'parallel connections sync concurrent pool' },
    { cat: 'power', label: $t('settingsPanel.label.syncFullReconcileDays'), kw: 'full check reconcile verify days sync' },
    { cat: 'power', label: $t('settingsPanel.label.syncMessageLimit'), kw: 'sync limit messages older initial download bodies' },
    { cat: 'power', label: $t('settingsPanel.toggle.syncAutoBackfill'), kw: 'older mail backfill scroll load more' },
    { cat: 'power', label: $t('settingsPanel.toggle.offlineIndicator'), kw: 'offline' },
    { cat: 'power', label: $t('settingsPanel.toggle.flagColorSync'), kw: 'flag color sync' },
    { cat: 'power', label: $t('settingsPanel.label.downloadOffline'), kw: 'download offline cache' },
    { cat: 'encryption', label: $t('encryption.import'), kw: 'pgp gpg openpgp key import' },
    { cat: 'encryption', label: $t('encryption.signingTitle'), kw: 'pgp sign key account' },
    { cat: 'encryption', label: $t('encryption.revocationTitle'), kw: 'smime revocation ocsp crl certificate withdrawn' },
    { cat: 'shortcuts', label: $t('settingsPanel.toggle.shortcutHints'), kw: 'keyboard hints' },
    { cat: 'shortcuts', label: $t('settingsPanel.toggle.paletteQuickSelect'), kw: 'command palette quick select numbers' },
    { cat: 'shortcuts', label: $t('onboarding.extras.appVim'), kw: 'vim navigation' },
    { cat: 'shortcuts', label: $t('settings.shortcuts'), kw: 'keyboard keys hotkeys' },
    { cat: 'about', label: $t('settingsPanel.category.about'), kw: 'version license update' },
  ]

  // settings search: filter the nav by title, group name or keywords, and build a
  // flat list of individual settings that match, so the user can jump straight to
  // a specific toggle without knowing its category.
  let settingsQuery = ''
  $: settingsQ = settingsQuery.trim().toLowerCase()
  function catLabel(key: string): string {
    return categories.find((c) => c.key === key)?.label ?? key
  }
  $: matchedCategories =
    settingsQ === ''
      ? categories
      : categories.filter(
          (c) =>
            c.label.toLowerCase().includes(settingsQ) ||
            c.keywords.toLowerCase().includes(settingsQ) ||
            (navGroups.find((g) => g.key === c.group)?.label ?? '').toLowerCase().includes(settingsQ),
        )
  $: searchResults =
    settingsQ === ''
      ? []
      : settingsIndex.filter(
          (s) =>
            s.label.toLowerCase().includes(settingsQ) ||
            s.kw.toLowerCase().includes(settingsQ) ||
            catLabel(s.cat).toLowerCase().includes(settingsQ),
        )
  function openResult(cat: string): void {
    active = cat
    settingsQuery = ''
  }

  // auto-sync interval presets, in seconds (0 = off).
  $: autoSyncOptions = [
    { key: '0', label: $t('settingsPanel.unit.off') },
    { key: '30', label: $t('settingsPanel.unit.s30') },
    { key: '300', label: $t('settingsPanel.unit.m5') },
    { key: '900', label: $t('settingsPanel.unit.m15') },
    { key: '1800', label: $t('settingsPanel.unit.m30') },
    { key: '3600', label: $t('settingsPanel.unit.h1') },
    { key: '21600', label: $t('settingsPanel.unit.h6') },
    { key: '43200', label: $t('settingsPanel.unit.h12') },
    { key: '86400', label: $t('settingsPanel.unit.h24') },
  ]
  function onAutoSyncInterval(event: CustomEvent<string>): void {
    setAutoSyncInterval(Number(event.detail))
  }

  // how many of a folder's newest messages a first sync fetches. "all" is the
  // pre-#175 behavior of downloading the whole mailbox up front.
  $: syncLimitOptions = [
    { key: '50', label: '50' },
    { key: '100', label: '100' },
    { key: '250', label: '250' },
    { key: '500', label: '500' },
    { key: '1000', label: '1000' },
    { key: '0', label: $t('settingsPanel.syncLimit.all') },
  ]

  function onSyncMessageLimit(event: CustomEvent<string>): void {
    setSyncMessageLimit(Number(event.detail))
  }

  // parallel sync connections per mailbox: 1–5, default 3.
  $: syncParallelOptions = [
    { key: '1', label: '1' },
    { key: '2', label: '2' },
    { key: '3', label: '3' },
    { key: '4', label: '4' },
    { key: '5', label: '5' },
  ]
  function onSyncMaxParallel(event: CustomEvent<string>): void {
    setSyncMaxParallel(Number(event.detail))
  }

  $: syncFullReconcileOptions = [
    { key: '1', label: '1' },
    { key: '3', label: '3' },
    { key: '7', label: '7' },
    { key: '14', label: '14' },
    { key: '30', label: '30' },
    { key: '0', label: $t('settingsPanel.syncFullReconcile.manual') },
  ]
  function onSyncFullReconcileDays(event: CustomEvent<string>): void {
    setSyncFullReconcileDays(Number(event.detail))
  }

  // what the window's close button does.
  $: closeActionOptions = [
    { key: 'background', label: $t('settingsPanel.closeAction.background') },
    { key: 'quit', label: $t('settingsPanel.closeAction.quit') },
  ]
  function onCloseAction(event: CustomEvent<string>): void {
    setCloseAction(event.detail as CloseAction)
  }

  // swipe gesture actions (trackpad). shown in the two direction dropdowns.
  $: swipeActionOptions = [
    { key: 'none', label: $t('settingsPanel.swipeAction.none') },
    { key: 'delete', label: $t('settingsPanel.swipeAction.delete') },
    { key: 'read', label: $t('settingsPanel.swipeAction.read') },
    { key: 'unread', label: $t('settingsPanel.swipeAction.unread') },
    { key: 'flag', label: $t('settingsPanel.swipeAction.flag') },
    { key: 'archive', label: $t('settingsPanel.swipeAction.archive') },
    { key: 'snooze', label: $t('settingsPanel.swipeAction.snooze') },
  ]

  // offline range download state. the start date defaults to one year ago.
  let downloadStart = defaultDownloadStart()
  function defaultDownloadStart(): string {
    const d = new Date()
    d.setFullYear(d.getFullYear() - 1)
    return d.toISOString().slice(0, 10)
  }

  // quick presets sit next to the native date input so picking a common range
  // (rather than fiddling with the bare calendar widget) is one click.
  $: downloadPresets = [
    { key: '1w', label: $t('settingsPanel.preset.lastWeek'), days: 7 },
    { key: '1m', label: $t('settingsPanel.preset.lastMonth'), days: 30 },
    { key: '3m', label: $t('settingsPanel.preset.last3Months'), days: 90 },
    { key: '6m', label: $t('settingsPanel.preset.last6Months'), days: 180 },
    { key: '1y', label: $t('settingsPanel.preset.lastYear'), days: 365 },
    { key: 'all', label: $t('settingsPanel.preset.allTime'), days: 0 },
  ]
  function applyPreset(days: number): void {
    const d = new Date()
    if (days === 0) {
      d.setFullYear(d.getFullYear() - 20) // effectively "everything"
    } else {
      d.setDate(d.getDate() - days)
    }
    downloadStart = d.toISOString().slice(0, 10)
  }

  async function startDownload(): Promise<void> {
    if (!downloadStart) {
      return
    }
    try {
      await downloadRange(downloadStart, $prefs.downloadIncludeAttachments)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  import { bodyFonts, uiFonts, monoFonts } from '../../lib/fonts'
  import { listSystemFonts, getSetting, setSetting } from '../../lib/api'

  $: bodyFontOptions = bodyFonts.map((f) => ({ key: f.key, label: f.label ?? $t(f.labelKey ?? '') }))
  $: uiFontOptions = uiFonts.map((f) => ({ key: f.key, label: f.label ?? $t(f.labelKey ?? '') }))
  $: monoFontOptions = monoFonts.map((f) => ({ key: f.key, label: f.label ?? $t(f.labelKey ?? '') }))

  // every installed system font, loaded once when the panel opens. the list
  // is backend-cached, so reopening settings is instant.
  let systemFonts: string[] = []
  onMount(() => {
    listSystemFonts()
      .then((fonts) => (systemFonts = fonts))
      .catch(() => (systemFonts = []))
  })

  // how mail that names no encoding, or names one nothing knows, is read. It is
  // a parser setting rather than a ui preference, so it is read from and written
  // to the store directly. 'auto' detects from the bytes and is what almost
  // everyone should leave it on; the named encodings are for someone who
  // receives mail from one system that is always wrong in the same way.
  const charsetKey = 'charset_fallback'
  const charsetOptions = [
    'windows-1252',
    'iso-8859-2',
    'iso-8859-7',
    'iso-8859-9',
    'koi8-r',
    'windows-1251',
    'shift_jis',
    'euc-jp',
    'gb18030',
    'big5',
    'euc-kr',
    'utf-8',
  ]
  let charsetFallback = 'auto'
  onMount(async () => {
    try {
      const stored = await getSetting(charsetKey)
      if (stored.found && stored.value !== '') {
        charsetFallback = stored.value
      }
    } catch {
      // the store is not open yet; the default is what the parser uses anyway.
    }
  })

  function onCharsetFallback(event: CustomEvent<string>): void {
    charsetFallback = event.detail
    void setSetting(charsetKey, charsetFallback)
  }

  // select handlers (the cast lives in script; inline ts casts break the parser).
  // the startup target is stored as an opaque string ('last', 'view:<key>' or
  // 'folder:<id>'), so the select works directly on it.
  function onStartupSelection(event: CustomEvent<string>): void {
    setStartupSelection(event.detail)
  }

  // unified view names are localized by key in the sidebar; reuse that here so
  // the setting and the row it points at read the same.
  function unifiedViewName(key: string, fallback: string): string {
    const lookup = key === 'inbox' ? 'sidebar.unifiedInbox' : `sidebar.view.${key}`
    const translated = $t(lookup)
    return translated === lookup ? fallback : translated
  }

  // the pickers hand over the chosen value itself rather than the event, so
  // nothing here has to reach back into the dom for it.
  function onSelectAllScope(event: CustomEvent<string>): void {
    setSelectAllScope(event.detail as SelectAllScope)
  }
  function onSearchSort(kind: SearchKind, event: CustomEvent<string>): void {
    setSearchSort(kind, event.detail as SearchSortPref)
  }
  function onBodyFont(event: CustomEvent<string>): void {
    setBodyFont(event.detail)
  }
  function onUIFont(event: CustomEvent<string>): void {
    setUIFont(event.detail)
  }
  function onMonoFont(event: CustomEvent<string>): void {
    setMonoFont(event.detail)
  }
  function onSwipeLeft(event: CustomEvent<string>): void {
    setSwipeLeftAction(event.detail)
  }
  function onSwipeRight(event: CustomEvent<string>): void {
    setSwipeRightAction(event.detail)
  }

  // the three font pickers share a shape: a curated list, then whatever the
  // system has, prefixed so the backend can tell the two apart.
  function fontItems(curated: { key: string; label: string }[]): SelectItem[] {
    const items: SelectItem[] = [{ label: $t('settingsPanel.bodyFont.groupCurated'), options: curated.map((o) => ({ value: o.key, label: o.label })) }]
    if (systemFonts.length > 0) {
      items.push({
        label: $t('settingsPanel.bodyFont.groupSystem'),
        options: systemFonts.map((family) => ({ value: `sys:${family}`, label: family })),
      })
    }
    return items
  }

  // "last used", then every unified view and every folder grouped by account.
  $: startupItems = [
    { value: 'last', label: $t('settingsPanel.startup.lastUsed') },
    ...($sidebar.data
      ? [
          {
            label: $t('sidebar.unifiedViews.heading'),
            options: $sidebar.data.views.map((view) => ({
              value: `view:${view.key}`,
              label: unifiedViewName(view.key, view.label),
            })),
          },
          ...$sidebar.data.accounts.map((account) => ({
            label: account.email,
            options: ($sidebar.data?.foldersByAccount[account.id] ?? []).map((folder) => ({
              value: `folder:${folder.id}`,
              label: folder.imapPath,
            })),
          })),
        ]
      : []),
  ] satisfies SelectItem[]
  // initialCategory deep-links the panel to a section (e.g. opened from the
  // "Manage Mailboxes" menu item); null opens the default section.
  export let initialCategory: string | null = null
  let active = initialCategory ?? 'appearance'

  $: themeOptions = [
    { key: 'system', label: $t('onboarding.theme.system') },
    { key: 'light', label: $t('onboarding.theme.light') },
    { key: 'dark', label: $t('onboarding.theme.dark') },
    { key: 'schedule', label: $t('settingsPanel.theme.schedule') },
  ]

  $: densityOptions = [
    { key: 'compact', label: $t('onboarding.density.compact') },
    { key: 'medium', label: $t('onboarding.density.medium') },
    { key: 'luxe', label: $t('onboarding.density.luxe') },
  ]

  $: newItemsOptions = [
    { key: 'visible', label: $t('menuBar.newItems.visible') },
    { key: 'hidden', label: $t('menuBar.newItems.hidden') },
  ]

  // clock preference for rendered times.
  $: timeFormatOptions = [
    { key: 'auto', label: $t('settingsPanel.timeFormat.auto') },
    { key: '12', label: $t('settingsPanel.timeFormat.h12') },
    { key: '24', label: $t('settingsPanel.timeFormat.h24') },
  ]

  $: cornerOptions = [
    { key: 'square', label: $t('settingsPanel.corners.square') },
    { key: 'default', label: $t('settingsPanel.corners.default') },
    { key: 'round', label: $t('settingsPanel.corners.round') },
  ]

  // interface zoom. values are string multipliers applied as css zoom.
  $: scaleOptions = [
    { key: '0.9', label: $t('settingsPanel.scale.90') },
    { key: '1', label: $t('settingsPanel.scale.100') },
    { key: '1.1', label: $t('settingsPanel.scale.110') },
    { key: '1.17', label: $t('settingsPanel.scale.117') },
    { key: '1.25', label: $t('settingsPanel.scale.125') },
    { key: '1.5', label: $t('settingsPanel.scale.150') },
  ]

  // base font size (px) for rendered email content.
  $: messageFontOptions = [
    { key: '12', label: $t('onboarding.font.small') },
    { key: '14', label: $t('onboarding.font.default') },
    { key: '16', label: $t('onboarding.font.large') },
    { key: '18', label: $t('onboarding.font.larger') },
    { key: '20', label: $t('onboarding.font.largest') },
  ]

  $: sendDelayOptions = [
    { key: '0', label: $t('settingsPanel.unit.off') },
    { key: '5', label: $t('settingsPanel.unit.s5') },
    { key: '10', label: $t('settingsPanel.unit.s10') },
    { key: '30', label: $t('settingsPanel.unit.s30') },
    { key: '60', label: $t('settingsPanel.unit.s60') },
  ]

  function onSendDelay(event: CustomEvent<string>): void {
    setSendDelay(Number(event.detail))
  }

  // enabling the global remote-image override is guarded by a tracking warning.
  let confirmImages = false
  function onImagesToggle(checked: boolean): void {
    if (checked && !$prefs.alwaysLoadImages) {
      confirmImages = true
    } else {
      setAlwaysLoadImages(false)
    }
  }
  function confirmEnableImages(): void {
    setAlwaysLoadImages(true)
    confirmImages = false
  }

  // logging (#211). the status comes from the backend rather than the prefs
  // store because it describes the disk (folder, size, an unread crash report)
  // and because --debug can have logging on with the setting off.
  let logStatus: LogStatus | null = null
  let confirmDeleteLogs = false

  let logLevelOptions: { key: LogLevel; label: string }[] = []
  $: logLevelOptions = [
    { key: 'debug', label: $t('settingsPanel.logLevel.debug') },
    { key: 'info', label: $t('settingsPanel.logLevel.info') },
    { key: 'warn', label: $t('settingsPanel.logLevel.warn') },
    { key: 'error', label: $t('settingsPanel.logLevel.error') },
  ]

  async function refreshLogStatus(): Promise<void> {
    try {
      logStatus = await getLogStatus()
    } catch {
      logStatus = null
    }
  }

  // re-read whenever the privacy section is shown, so the size and the folder
  // are current rather than whatever they were at startup.
  $: if (active === 'privacy') {
    void refreshLogStatus()
  }

  function onLogToFile(checked: boolean): void {
    setLogToFile(checked)
    // the backend opens or closes the file as part of saving the setting, so
    // give it a moment before reading the state back.
    setTimeout(refreshLogStatus, 200)
  }

  async function onOpenLogFolder(): Promise<void> {
    try {
      await openLogFolder()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onDeleteLogs(): Promise<void> {
    confirmDeleteLogs = false
    try {
      await deleteLogs()
      await refreshLogStatus()
      toastInfo($t('settingsPanel.logs.deleted'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // formatBytes keeps the size next to the folder readable without pulling in a
  // formatting library for one number.
  function formatBytes(bytes: number): string {
    if (bytes < 1024) {
      return `${bytes} B`
    }
    if (bytes < 1024 * 1024) {
      return `${Math.round(bytes / 1024)} KB`
    }
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  }

  // the remote-image allowlist manager (trusted senders/domains) opens in a modal.
  let allowlistOpen = false
  let vipOpen = false

  // the reading-pane empty-state image is picked from a local file and stored as
  // a data uri. anything past the hard cap is refused; between the soft and hard
  // caps we warn ("here be dragons") but let the user proceed, since a large data
  // uri in settings can slow the ui down.
  let emptyImageInput: HTMLInputElement
  const maxEmptyImageBytes = 50_000_000
  const warnEmptyImageBytes = 3_000_000
  // a data uri awaiting confirmation because the chosen file is large.
  let dragonsPending: string | null = null
  function onPickEmptyImage(event: Event): void {
    const input = event.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file) {
      return
    }
    if (file.size > maxEmptyImageBytes) {
      toastError($t('settingsPanel.error.imageTooLarge'))
      return
    }
    const large = file.size > warnEmptyImageBytes
    const reader = new FileReader()
    reader.onload = () => {
      const uri = String(reader.result)
      if (large) {
        dragonsPending = uri
      } else {
        setEmptyStateImage(uri)
      }
    }
    reader.onerror = () => toastError($t('settingsPanel.error.imageRead'))
    reader.readAsDataURL(file)
  }
  function confirmDragons(): void {
    if (dragonsPending) {
      setEmptyStateImage(dragonsPending)
    }
    dragonsPending = null
  }

  // sender-photo fallback chain. "Generated" never touches the network.
  $: viewsPlacementOptions = [
    { key: 'hidden', label: $t('views.setting.hidden') },
    { key: 'sidebar', label: $t('views.setting.sidebar') },
    { key: 'tab', label: $t('views.setting.tab') },
  ]

  $: avatarSourceOptions = [
    { key: 'bimi_gravatar', label: $t('settingsPanel.avatarSource.bimiGravatar') },
    { key: 'gravatar_bimi', label: $t('settingsPanel.avatarSource.gravatarBimi') },
    { key: 'bimi', label: $t('settingsPanel.avatarSource.bimiOnly') },
    { key: 'pfp', label: $t('settingsPanel.avatarSource.generated') },
  ]

  // generated placeholder styles, previewed with a sample sender so the look is
  // obvious before choosing.
  const sampleEmail = 'potato@pelton.email'
  const sampleInitials = initials('', sampleEmail)
  let avatarStyleOptions: { key: PfpStyle; label: string }[] = []
  $: avatarStyleOptions = [
    { key: 'initials', label: $t('onboarding.avatar.classic') },
    { key: 'mono', label: $t('onboarding.avatar.mono') },
    { key: 'pixel', label: $t('onboarding.avatar.pixel') },
    { key: 'geometric', label: $t('onboarding.avatar.geometric') },
  ]
  function stylePreview(style: PfpStyle): string {
    return pfpDataUri(style, sampleEmail, sampleInitials)
  }

  $: flagOptions = [
    { key: 'flag', label: $t('onboarding.flagopt.icon') },
    { key: 'left', label: $t('onboarding.flagopt.left') },
    { key: 'both', label: $t('onboarding.flagopt.both') },
    { key: 'off', label: $t('onboarding.flagopt.off') },
  ]

  $: rowTemplateOptions = [
    { key: 'relaxed', label: $t('onboarding.row.relaxed') },
    { key: 'comfortable', label: $t('onboarding.row.comfortable') },
    { key: 'compact', label: $t('onboarding.row.compact') },
    { key: 'single', label: $t('onboarding.row.single') },
  ]

  $: previewLineOptions = [
    { key: '1', label: $t('settingsPanel.previewLines.1') },
    { key: '2', label: $t('settingsPanel.previewLines.2') },
    { key: '3', label: $t('settingsPanel.previewLines.3') },
  ]

  $: snippetCapable = $prefs.rowTemplate === 'relaxed' || $prefs.rowTemplate === 'comfortable'

  function onTheme(event: CustomEvent<string>): void {
    setTheme(event.detail as ThemePref)
  }

  function onDensity(event: CustomEvent<string>): void {
    setDensity(event.detail as DensityPref)
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      dispatch('close')
    }
  }
</script>

<svelte:window on:keydown={onKeydown} />

<div class="screen" role="dialog" aria-modal="true" aria-label={$t('settings.title')}>
  <header class="head">
    <h2>{$t('settings.title')}</h2>
    <button type="button" class="close" aria-label={$t('settingsPanel.closeAria')} on:click={() => dispatch('close')}>
      <IconX size={20} stroke={1.8} />
    </button>
  </header>

  <div class="body">
    <nav class="nav" aria-label={$t('settingsPanel.navAria')}>
      <div class="nav-search">
        <IconSearch size={15} stroke={1.7} />
        <input
          type="text"
          bind:value={settingsQuery}
          placeholder={$t('settings.search')}
          aria-label={$t('settings.search')}
        />
        {#if settingsQuery}
          <button type="button" class="nav-search-clear" aria-label={$t('settingsPanel.closeAria')} on:click={() => (settingsQuery = '')}>
            <IconX size={14} stroke={1.8} />
          </button>
        {/if}
      </div>

      {#each navGroups as g (g.key)}
        {@const items = matchedCategories.filter((c) => c.group === g.key)}
        {#if items.length}
          <div class="nav-group">{g.label}</div>
          {#each items as cat (cat.key)}
            <button
              type="button"
              class="nav-item"
              class:active={active === cat.key}
              aria-current={active === cat.key}
              on:click={() => {
                active = cat.key
                settingsQuery = ''
              }}
            >
              <span class="nav-icon"><ThemedIcon name={cat.iconName} icon={cat.icon} size={17} stroke={1.6} /></span>
              <span>{cat.label}</span>
            </button>
          {/each}
        {/if}
      {/each}

      {#if matchedCategories.length === 0}
        <p class="nav-empty">{$t('settings.searchEmpty')}</p>
      {/if}
    </nav>

    <div class="content">
      {#if settingsQ !== ''}
        <section>
          <h3>{$t('settings.search')}</h3>
          {#if searchResults.length === 0}
            <p class="hint">{$t('settings.searchEmpty')}</p>
          {:else}
            <div class="search-results">
              {#each searchResults as r (r.cat + r.label)}
                <button type="button" class="result" on:click={() => openResult(r.cat)}>
                  <span class="result-label">{r.label}</span>
                  <span class="result-cat">{catLabel(r.cat)}</span>
                </button>
              {/each}
            </div>
          {/if}
        </section>
      {:else if active === 'language'}
        <section>
          <h3>{$t('settings.language')}</h3>
          <p class="hint">{$t('settings.languageHint')}</p>
          {#key languageReload}
            <LanguageSelect value={currentLocale} onSelect={setLanguage} />
          {/key}
          <h4 class="lang-tools-heading">{$t('language.custom')}</h4>
          <p class="hint">{$t('language.customHint')}</p>
          <div class="lang-tools">
            <button type="button" class="lang-tool-btn" on:click={onOpenLocalesFolder} title={$t('language.openFolderHint')}>
              <IconFolderOpen size={15} stroke={1.6} />
              {$t('language.openFolder')}
            </button>
            <button type="button" class="lang-tool-btn" on:click={onSaveLocaleTemplate} title={$t('language.templateHint')}>
              <IconFileExport size={15} stroke={1.6} />
              {$t('language.template')}
            </button>
            <button type="button" class="lang-tool-btn" on:click={() => (languageReload += 1)} title={$t('language.reloadHint')}>
              <IconRefresh size={15} stroke={1.6} />
              {$t('language.reload')}
            </button>
          </div>
        </section>
      {:else if active === 'themes'}
        <section>
          <h3>{$t('settingsPanel.category.themes')}</h3>
          <ThemesSection />
        </section>
      {:else if active === 'appearance'}
        <section>
          <h3>{$t('settingsPanel.category.appearance')}</h3>
          {#if $prefs.themeId}
            <p class="hint">{$t('themes.baseLockedHint')}</p>
          {:else}
            <SegmentedSetting label={$t('settingsPanel.label.theme')} value={$prefs.theme} options={themeOptions} on:change={onTheme} />
            {#if $prefs.theme === 'schedule'}
              <div class="schedule-times">
                <label class="schedule-time">
                  <span>{$t('settingsPanel.label.darkFrom')}</span>
                  <input
                    type="time"
                    value={$prefs.themeDarkStart}
                    on:change={(e) => setThemeDarkTimes(e.currentTarget.value, $prefs.themeDarkEnd)}
                  />
                </label>
                <label class="schedule-time">
                  <span>{$t('settingsPanel.label.darkUntil')}</span>
                  <input
                    type="time"
                    value={$prefs.themeDarkEnd}
                    on:change={(e) => setThemeDarkTimes($prefs.themeDarkStart, e.currentTarget.value)}
                  />
                </label>
              </div>
              <p class="hint">{$t('settingsPanel.hint.themeSchedule')}</p>
            {/if}
          {/if}
          <AccentPicker />
          <SegmentedSetting label={$t('settingsPanel.label.density')} value={$prefs.density} options={densityOptions} on:change={onDensity} />
          <SegmentedSetting
            label={$t('settingsPanel.label.corners')}
            value={$prefs.cornerStyle}
            options={cornerOptions}
            on:change={(e) => setCornerStyle(e.detail)}
          />
          <p class="hint">{$t('settingsPanel.hint.corners')}</p>
          <SegmentedSetting
            label={$t('settingsPanel.label.interfaceScale')}
            value={$prefs.uiScale}
            options={scaleOptions}
            on:change={(e) => setUIScale(e.detail)}
          />
          <p class="hint">{$t('settingsPanel.hint.interfaceScale')}</p>
          <div class="toggle">
            <span class="row-label">{$t('settingsPanel.toggle.reduceMotion')}</span>
            <ToggleSwitch
              checked={$prefs.reduceMotion}
              label={$t('settingsPanel.toggle.reduceMotion')}
              on:change={(e) => setReduceMotion(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.reduceMotion')}</p>
          <div class="toggle">
            <span class="row-label">{$t('settingsPanel.toggle.handCursor')}</span>
            <ToggleSwitch
              checked={$prefs.handCursor}
              label={$t('settingsPanel.toggle.handCursor')}
              on:change={(e) => setHandCursor(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.handCursor')}</p>

          <div class="field">
            <span class="row-label">{$t('settingsPanel.label.emptyStateImage')}</span>
            <p class="hint">{$t('settingsPanel.hint.emptyStateImage')}</p>
            <div class="empty-image-row">
              <div class="empty-image-preview">
                <img src={$prefs.emptyStateImage || peltonLogo} alt="" draggable="false" />
              </div>
              <div class="empty-image-actions">
                <button type="button" class="action-btn" on:click={() => emptyImageInput?.click()}>
                  {$t('settingsPanel.button.selectImage')}
                </button>
                <button
                  type="button"
                  class="action-btn"
                  disabled={!$prefs.emptyStateImage}
                  on:click={() => setEmptyStateImage('')}
                >
                  {$t('settingsPanel.button.resetImage')}
                </button>
              </div>
            </div>
            <input
              class="hidden-file"
              type="file"
              accept="image/*"
              bind:this={emptyImageInput}
              on:change={onPickEmptyImage}
            />
            {#if dragonsPending}
              <div class="warn">
                <p>{$t('settingsPanel.warn.imageLarge')}</p>
                <div class="warn-actions">
                  <button type="button" class="ghost-btn" on:click={() => (dragonsPending = null)}>{$t('settingsPanel.button.cancel')}</button>
                  <button type="button" class="danger-btn" on:click={confirmDragons}>{$t('settingsPanel.button.useAnyway')}</button>
                </div>
              </div>
            {/if}
            <div class="toggle" class:disabled={!$prefs.emptyStateImage} title={$t('settingsPanel.hint.emptyStateFullscreen')}>
              <span class="row-label">{$t('settingsPanel.toggle.emptyStateFullscreen')}</span>
              <ToggleSwitch
                checked={$prefs.emptyStateFullscreen}
                disabled={!$prefs.emptyStateImage}
                label={$t('settingsPanel.toggle.emptyStateFullscreen')}
                on:change={(e) => setEmptyStateFullscreen(e.detail)}
              />
            </div>
            <p class="hint">{$t('settingsPanel.hint.emptyStateFullscreen')}</p>
          </div>
        </section>
      {:else if active === 'menubar'}
        <section>
          <h3>{$t('settingsPanel.category.menubar')}</h3>
          {#if isMac}
            <div class="toggle">
              <span class="row-label">{$t('settingsPanel.toggle.menuBarInApp')}</span>
              <ToggleSwitch
                checked={$prefs.menuBarInApp}
                label={$t('settingsPanel.toggle.menuBarInApp')}
                on:change={(e) => setMenuBarInApp(e.detail)}
              />
            </div>
            <p class="hint">{$t('settingsPanel.hint.menuBarInApp')}</p>
            {#if $prefs.menuBarInApp}
              <div class="toggle">
                <span class="row-label">{$t('settingsPanel.toggle.menuBarNativeMinimal')}</span>
                <ToggleSwitch
                  checked={$prefs.menuBarNativeMinimal}
                  label={$t('settingsPanel.toggle.menuBarNativeMinimal')}
                  on:change={(e) => setMenuBarNativeMinimal(e.detail)}
                />
              </div>
              <p class="hint">{$t('settingsPanel.hint.menuBarNativeMinimal')}</p>
            {/if}
          {/if}
          {#if !isMac || $prefs.menuBarInApp}
            <div class="toggle">
              <span class="row-label">{$t('settingsPanel.toggle.menuBarIcons')}</span>
              <ToggleSwitch
                checked={$prefs.menuBarIcons}
                label={$t('settingsPanel.toggle.menuBarIcons')}
                on:change={(e) => setMenuBarIcons(e.detail)}
              />
            </div>
            <p class="hint">{$t('settingsPanel.hint.menuBarIcons')}</p>
            <button
              type="button"
              class="edit-menubar"
              on:click={() => {
                setMenuBarEditing(true)
                dispatch('close')
              }}
            >
              {$t('menuBar.editButton')}
            </button>
            <p class="hint">{$t('menuBar.editHint')}</p>
            <SegmentedSetting
              label={$t('menuBar.newItems.label')}
              value={$menuBarNewItems}
              options={newItemsOptions}
              on:change={(e) => setNewItemsMode(e.detail as NewItemsMode)}
            />
            <p class="hint">{$t('menuBar.newItems.hint')}</p>
          {/if}
        </section>
      {:else if active === 'list'}
        <section>
          <h3>{$t('settingsPanel.category.messageList')}</h3>
          <SegmentedSetting
            label={$t('settingsPanel.label.rowLayout')}
            value={$prefs.rowTemplate}
            options={rowTemplateOptions}
            on:change={(e) => setRowTemplate(e.detail)}
          />
          <RowLayoutPreview />
          <div class="toggle" class:disabled={$prefs.rowTemplate === 'single'} title={$t('settingsPanel.hint.avatarHiddenSingleLine')}>
            <span class="row-label">{$t('settingsPanel.toggle.showSenderAvatar')}</span>
            <ToggleSwitch
              checked={$prefs.rowShowAvatar}
              disabled={$prefs.rowTemplate === 'single'}
              label={$t('settingsPanel.toggle.showSenderAvatar')}
              on:change={(e) => setRowShowAvatar(e.detail)}
            />
          </div>
          <div class="toggle" class:disabled={!snippetCapable} title={$t('settingsPanel.hint.previewShowsOn')}>
            <span class="row-label">{$t('settingsPanel.toggle.showMessagePreview')}</span>
            <ToggleSwitch
              checked={$prefs.rowShowSnippet}
              disabled={!snippetCapable}
              label={$t('settingsPanel.toggle.showMessagePreview')}
              on:change={(e) => setRowShowSnippet(e.detail)}
            />
          </div>
          {#if snippetCapable && $prefs.rowShowSnippet}
            <SegmentedSetting
              label={$t('settingsPanel.label.previewLines')}
              value={String($prefs.previewLines)}
              options={previewLineOptions}
              on:change={(e) => setPreviewLines(Number(e.detail))}
            />
          {/if}
          <SegmentedSetting
            label={$t('settingsPanel.label.flaggedHighlight')}
            value={$prefs.flagHighlight}
            options={flagOptions}
            on:change={(e) => setFlagHighlight(e.detail)}
          />
          <p class="hint">{$t('settingsPanel.hint.barIconFlag')}</p>
          <div class="toggle">
            <span class="row-label">{$t('settingsPanel.toggle.showMailboxEmail')}</span>
            <ToggleSwitch
              checked={$prefs.showAccountEmail}
              label={$t('settingsPanel.toggle.showMailboxEmail')}
              on:change={(e) => setShowAccountEmail(e.detail)}
            />
          </div>
          <div class="toggle" title={$t('settingsPanel.hint.multiSelect')}>
            <span class="row-label">{$t('settingsPanel.toggle.multiSelect')}</span>
            <ToggleSwitch
              checked={$prefs.multiSelectEnabled}
              label={$t('settingsPanel.toggle.multiSelect')}
              on:change={(e) => setMultiSelectEnabled(e.detail)}
            />
          </div>
          <div class="toggle" class:disabled={!$prefs.multiSelectEnabled}>
            <span class="row-label">{$t('settingsPanel.toggle.showSelectedCount')}</span>
            <ToggleSwitch
              checked={$prefs.showSelectedCount}
              disabled={!$prefs.multiSelectEnabled}
              label={$t('settingsPanel.toggle.showSelectedCountAria')}
              on:change={(e) => setShowSelectedCount(e.detail)}
            />
          </div>

          <div class="row" class:disabled={!$prefs.multiSelectEnabled}>
            <span class="row-label">{$t('settingsPanel.label.selectAllScope')}</span>
            <Select
              value={$prefs.selectAllScope}
              disabled={!$prefs.multiSelectEnabled}
              ariaLabel={$t('settingsPanel.label.selectAllScope')}
              items={[
                { value: 'offer', label: $t('settingsPanel.selectAllScope.offer') },
                { value: 'all', label: $t('settingsPanel.selectAllScope.all') },
                { value: 'loaded', label: $t('settingsPanel.selectAllScope.loaded') },
              ]}
              on:change={onSelectAllScope}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.selectAllScope')}</p>
          <div class="toggle" class:disabled={!$prefs.multiSelectEnabled} title={$t('settingsPanel.hint.selectAllUnified')}>
            <span class="row-label">{$t('settingsPanel.toggle.selectAllUnified')}</span>
            <ToggleSwitch
              checked={$prefs.selectAllUnified}
              disabled={!$prefs.multiSelectEnabled}
              label={$t('settingsPanel.toggle.selectAllUnified')}
              on:change={(e) => setSelectAllUnified(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.selectAllUnified')}</p>

          <h4 class="subhead">{$t('settingsPanel.subhead.searchSort')}</h4>
          <p class="hint">{$t('settingsPanel.hint.searchSort')}</p>
          {#each searchSortRows as row (row.kind)}
            <div class="row">
              <span class="row-label">{$t(row.label)}</span>
              <Select
                value={searchSortPref($prefs, row.kind)}
                ariaLabel={$t(row.label)}
                items={[
                  {
                    value: 'auto',
                    label: `${$t('settingsPanel.searchSort.auto')} (${$t(`messageList.search.sort.${automaticSort(row.kind)}`)})`,
                  },
                  ...searchSorts.map((option) => ({
                    value: option,
                    label: $t(`messageList.search.sort.${option}`),
                  })),
                ]}
                on:change={(e) => onSearchSort(row.kind, e)}
              />
            </div>
          {/each}

          <h4 class="subhead">{$t('settingsPanel.category.sidebar')}</h4>
          <div class="toggle" title={$t('settingsPanel.hint.indentGuides')}>
            <span class="row-label">{$t('settingsPanel.toggle.indentGuides')}</span>
            <ToggleSwitch
              checked={$prefs.sidebarIndentGuides}
              label={$t('settingsPanel.toggle.indentGuides')}
              on:change={(e) => setSidebarIndentGuides(e.detail)}
            />
          </div>
          <div class="toggle" title={$t('settingsPanel.hint.unsyncedFolder')}>
            <span class="row-label">{$t('settingsPanel.toggle.unsyncedFolder')}</span>
            <ToggleSwitch
              checked={$prefs.showUnsyncedFolder}
              label={$t('settingsPanel.toggle.unsyncedFolder')}
              on:change={(e) => setShowUnsyncedFolder(e.detail)}
            />
          </div>
          <div class="toggle" title={$t('settingsPanel.hint.flaggedCount')}>
            <span class="row-label">{$t('settingsPanel.toggle.flaggedCount')}</span>
            <ToggleSwitch
              checked={$prefs.showFlaggedCount}
              label={$t('settingsPanel.toggle.flaggedCount')}
              on:change={(e) => setShowFlaggedCount(e.detail)}
            />
          </div>
          <div class="row">
            <span class="row-label">{$t('settingsPanel.label.startupSelection')}</span>
            <Select
              value={$prefs.startupSelection}
              ariaLabel={$t('settingsPanel.label.startupSelection')}
              items={startupItems}
              on:change={onStartupSelection}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.startupSelection')}</p>
          <SegmentedSetting
            label={$t('views.setting.label')}
            value={$prefs.viewsPlacement}
            options={viewsPlacementOptions}
            on:change={(e) => setViewsPlacement(e.detail as ViewsPlacement)}
          />
          <p class="hint">{$t('views.setting.hint')}</p>

          <h4 class="subhead">{$t('settingsPanel.category.avatars')}</h4>
          <SegmentedSetting
            label={$t('settingsPanel.label.senderPhotos')}
            value={$prefs.avatarSource}
            options={avatarSourceOptions}
            on:change={(e) => setAvatarSource(e.detail)}
          />
          <p class="hint">
            {$t('settingsPanel.hint.avatarSource')}
          </p>

          <div class="field">
            <span class="row-label">{$t('settingsPanel.label.generatedStyle')}</span>
            <div class="style-grid">
              {#each avatarStyleOptions as opt (opt.key)}
                <button
                  type="button"
                  class="style-card"
                  class:active={$prefs.avatarStyle === opt.key}
                  aria-pressed={$prefs.avatarStyle === opt.key}
                  on:click={() => setAvatarStyle(opt.key)}
                >
                  <img class="style-img" src={stylePreview(opt.key)} alt="" aria-hidden="true" />
                  <span class="style-label">{opt.label}</span>
                </button>
              {/each}
            </div>
            <p class="hint">{$t('settingsPanel.hint.previewSender')} {sampleEmail}</p>
          </div>
        </section>
      {:else if active === 'signatures'}
        <section>
          <SignaturesSection />
        </section>
      {:else if active === 'privacy'}
        <section>
          <h3>{$t('settingsPanel.category.privacy')}</h3>
          <div class="toggle" title={$t('settingsPanel.hint.remoteImagesToggle')}>
            <span class="row-label">{$t('settingsPanel.toggle.alwaysLoadImages')}</span>
            <ToggleSwitch
              checked={$prefs.alwaysLoadImages}
              label={$t('settingsPanel.toggle.alwaysLoadImages')}
              on:change={(e) => onImagesToggle(e.detail)}
            />
          </div>
          {#if confirmImages}
            <div class="warn">
              <p>
                {$t('settingsPanel.warn.remoteImages')}
              </p>
              <div class="warn-actions">
                <button type="button" class="ghost-btn" on:click={() => (confirmImages = false)}>{$t('settingsPanel.button.cancel')}</button>
                <button type="button" class="danger-btn" on:click={confirmEnableImages}>{$t('settingsPanel.button.enableAnyway')}</button>
              </div>
            </div>
          {/if}

          <div class="toggle" title={$t('settingsPanel.hint.blockTrackingPixels')}>
            <span class="row-label">{$t('settingsPanel.toggle.blockTrackingPixels')}</span>
            <ToggleSwitch
              checked={$prefs.blockTrackingPixels}
              label={$t('settingsPanel.toggle.blockTrackingPixels')}
              on:change={(e) => setBlockTrackingPixels(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.blockTrackingPixelsDetail')}</p>

          <div class="field">
            <span class="row-label">{$t('settingsPanel.label.manageWhitelist')}</span>
            <p class="hint">{$t('settingsPanel.hint.manageWhitelist')}</p>
            <button type="button" class="action-btn" on:click={() => (allowlistOpen = true)}>
              {$t('settingsPanel.button.manageWhitelist')}
            </button>
          </div>

          <h4 class="subhead">{$t('settingsPanel.category.logs')}</h4>
          <p class="hint">{$t('settingsPanel.hint.logsIntro')}</p>
          <div class="toggle" title={$t('settingsPanel.hint.logToFile')}>
            <span class="row-label">{$t('settingsPanel.toggle.logToFile')}</span>
            <ToggleSwitch
              checked={$prefs.logToFile}
              label={$t('settingsPanel.toggle.logToFile')}
              on:change={(e) => onLogToFile(e.detail)}
            />
          </div>
          {#if logStatus?.forced}
            <p class="hint">{$t('settingsPanel.hint.logForced')}</p>
          {/if}
          {#if $prefs.logToFile}
            <SegmentedSetting
              label={$t('settingsPanel.label.logLevel')}
              value={$prefs.logLevel}
              options={logLevelOptions}
              on:change={(e) => setLogLevel(e.detail as LogLevel)}
            />
            <p class="hint">{$t('settingsPanel.hint.logLevel')}</p>
            <div class="toggle" title={$t('settingsPanel.hint.logMessageMetadata')}>
              <span class="row-label">{$t('settingsPanel.toggle.logMessageMetadata')}</span>
              <ToggleSwitch
                checked={$prefs.logMessageMetadata}
                label={$t('settingsPanel.toggle.logMessageMetadata')}
                on:change={(e) => setLogMessageMetadata(e.detail)}
              />
            </div>
            <p class="hint">{$t('settingsPanel.hint.logMessageMetadataDetail')}</p>
          {/if}
          <div class="toggle" title={$t('settingsPanel.hint.crashLogs')}>
            <span class="row-label">{$t('settingsPanel.toggle.crashLogs')}</span>
            <ToggleSwitch
              checked={$prefs.crashLogs}
              label={$t('settingsPanel.toggle.crashLogs')}
              on:change={(e) => setCrashLogs(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.crashLogs')}</p>
          {#if logStatus}
            <div class="field">
              <span class="row-label">{$t('settingsPanel.label.logFolder')}</span>
              <p class="hint path">{logStatus.dir}</p>
              <p class="hint">
                {$t('settingsPanel.hint.logSize').replace('{size}', formatBytes(logStatus.sizeBytes))}
              </p>
              <div class="log-actions">
                <button type="button" class="action-btn" on:click={onOpenLogFolder}>
                  {$t('settingsPanel.button.openLogFolder')}
                </button>
                <button
                  type="button"
                  class="action-btn"
                  disabled={logStatus.sizeBytes === 0}
                  on:click={() => (confirmDeleteLogs = true)}
                >
                  {$t('settingsPanel.button.deleteLogs')}
                </button>
              </div>
            </div>
            {#if confirmDeleteLogs}
              <div class="warn">
                <p>{$t('settingsPanel.warn.deleteLogs')}</p>
                <div class="warn-actions">
                  <button type="button" class="ghost-btn" on:click={() => (confirmDeleteLogs = false)}>{$t('settingsPanel.button.cancel')}</button>
                  <button type="button" class="danger-btn" on:click={onDeleteLogs}>{$t('settingsPanel.button.deleteLogs')}</button>
                </div>
              </div>
            {/if}
          {/if}

          <div class="merged-block">
            <NetworkSection />
          </div>
        </section>
      {:else if active === 'notifications'}
        <section>
          <h3>{$t('settingsPanel.category.notifications')}</h3>
          <div class="row">
            <span class="row-label">{$t('settings.toastPosition')}</span>
            <ToastPositionPicker
              value={$prefs.toastPosition}
              on:change={(e) => setToastPosition(e.detail)}
            />
          </div>
          <div class="toggle" title={$t('vip.notifyNewMailHint')}>
            <span class="row-label">{$t('vip.notifyNewMail')}</span>
            <ToggleSwitch
              checked={$prefs.notifyNewMail}
              label={$t('vip.notifyNewMail')}
              on:change={(e) => setNotifyNewMail(e.detail)}
            />
          </div>
          <div class="toggle">
            <span class="row-label">{$t(badgeToggleKey)}</span>
            <ToggleSwitch
              checked={$prefs.unreadBadge}
              label={$t(badgeToggleKey)}
              on:change={(e) => setUnreadBadgeEnabled(e.detail)}
            />
          </div>
          <p class="hint">{$t(badgeHintKey)}</p>
          <div class="field">
            <span class="row-label">{$t('vip.manageLabel')}</span>
            <p class="hint">{$t('vip.manageHint')}</p>
            <button type="button" class="action-btn" on:click={() => (vipOpen = true)}>
              {$t('vip.manage')}
            </button>
          </div>
        </section>
      {:else if active === 'display'}
        <section>
          <h3>{$t('settingsPanel.category.reading')}</h3>
          <div class="toggle" title={$t('settingsPanel.hint.restoreTabs')}>
            <span class="row-label">{$t('settingsPanel.toggle.restoreTabs')}</span>
            <ToggleSwitch
              checked={$prefs.restoreTabs}
              label={$t('settingsPanel.toggle.restoreTabs')}
              on:change={(e) => setRestoreTabs(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.restoreTabs')}</p>
          <SegmentedSetting
            label={$t('onboarding.extras.fontSize')}
            value={String($prefs.messageFontSize)}
            options={messageFontOptions}
            on:change={(e) => setMessageFontSize(Number(e.detail))}
          />
          <p class="hint">{$t('settingsPanel.hint.fontSize')}</p>
          <SegmentedSetting
            label={$t('settingsPanel.label.timeFormat')}
            value={$prefs.timeFormat}
            options={timeFormatOptions}
            on:change={(e) => setTimeFormat(e.detail)}
          />
          <p class="hint">{$t('settingsPanel.hint.timeFormat')}</p>
          <div class="row">
            <span class="row-label">{$t('settingsPanel.label.bodyFont')}</span>
            <Select
              value={$prefs.bodyFont}
              ariaLabel={$t('settingsPanel.label.bodyFont')}
              items={fontItems(bodyFontOptions)}
              on:change={onBodyFont}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.bodyFont')}</p>
          <div class="toggle" title={$t('settingsPanel.hint.senderFonts')}>
            <span class="row-label">{$t('settingsPanel.toggle.senderFonts')}</span>
            <ToggleSwitch
              checked={$prefs.senderFonts}
              label={$t('settingsPanel.toggle.senderFonts')}
              on:change={(e) => setSenderFonts(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.senderFonts')}</p>
          <div class="row">
            <span class="row-label">{$t('settingsPanel.label.uiFont')}</span>
            <Select
              value={$prefs.uiFont}
              ariaLabel={$t('settingsPanel.label.uiFont')}
              items={fontItems(uiFontOptions)}
              on:change={onUIFont}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.uiFont')}</p>
          <div class="row">
            <span class="row-label">{$t('settingsPanel.label.monoFont')}</span>
            <Select
              value={$prefs.monoFont}
              ariaLabel={$t('settingsPanel.label.monoFont')}
              items={fontItems(monoFontOptions)}
              on:change={onMonoFont}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.monoFont')}</p>
          <div class="row">
            <span class="row-label">{$t('settingsPanel.label.charsetFallback')}</span>
            <Select
              value={charsetFallback}
              ariaLabel={$t('settingsPanel.label.charsetFallback')}
              items={[
                { value: 'auto', label: $t('settingsPanel.charset.auto') },
                ...charsetOptions.map((name) => ({ value: name, label: name })),
              ]}
              on:change={onCharsetFallback}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.charsetFallback')}</p>
          <TechToggles />

          <h4 class="subhead">{$t('settings.panes')}</h4>
          <div class="toggle" title={$t('settingsPanel.hint.lockPanes')}>
            <span class="row-label">{$t('settings.lockPanes')}</span>
            <ToggleSwitch
              checked={$prefs.paneLocked}
              label={$t('settings.lockPanes')}
              on:change={(e) => setPaneLocked(e.detail)}
            />
          </div>
        </section>
      {:else if active === 'gestures'}
        <section>
          <h3>{$t('settingsPanel.heading.gestures')}</h3>
          <div class="toggle" title={$t('settingsPanel.hint.swipeEnable')}>
            <span class="row-label">{$t('settingsPanel.toggle.swipeEnabled')}</span>
            <ToggleSwitch
              checked={$prefs.swipeEnabled}
              label={$t('settingsPanel.toggle.swipeEnabled')}
              on:change={(e) => setSwipeEnabled(e.detail)}
            />
          </div>
          <div class="row" class:disabled={!$prefs.swipeEnabled}>
            <span class="row-label">{$t('settingsPanel.label.swipeLeft')}</span>
            <Select
              disabled={!$prefs.swipeEnabled}
              value={$prefs.swipeLeftAction}
              ariaLabel={$t('settingsPanel.label.swipeLeft')}
              items={swipeActionOptions.map((opt) => ({ value: opt.key, label: opt.label }))}
              on:change={onSwipeLeft}
            />
          </div>
          <div class="row" class:disabled={!$prefs.swipeEnabled}>
            <span class="row-label">{$t('settingsPanel.label.swipeRight')}</span>
            <Select
              disabled={!$prefs.swipeEnabled}
              value={$prefs.swipeRightAction}
              ariaLabel={$t('settingsPanel.label.swipeRight')}
              items={swipeActionOptions.map((opt) => ({ value: opt.key, label: opt.label }))}
              on:change={onSwipeRight}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.swipeWork')}</p>
        </section>
      {:else if active === 'power'}
        <section>
          <h3>{$t('settingsPanel.category.powerSync')}</h3>
          <SegmentedSetting
            label={$t('settingsPanel.label.closeAction')}
            value={$prefs.closeAction}
            options={closeActionOptions}
            on:change={onCloseAction}
          />
          <p class="hint">
            {$t('settingsPanel.hint.closeAction')}
          </p>
          <div class="toggle" title={$t('settingsPanel.hint.lowPowerToggle')}>
            <span class="row-label">{$t('settingsPanel.toggle.lowPowerMode')}</span>
            <ToggleSwitch
              checked={$prefs.lowPowerMode}
              label={$t('settingsPanel.toggle.lowPowerMode')}
              on:change={(e) => setLowPowerMode(e.detail)}
            />
          </div>
          <p class="hint">
            {$t('settingsPanel.hint.lowPowerDetail')}
          </p>
          <StepSlider
            label={$t('settingsPanel.label.autoSyncInterval')}
            value={String($prefs.autoSyncIntervalSeconds)}
            options={autoSyncOptions}
            on:change={onAutoSyncInterval}
          />
          <p class="hint">
            {$t('settingsPanel.hint.autoSyncDetail')}
          </p>
          <div class="toggle" title={$t('settingsPanel.hint.verboseSync')}>
            <span class="row-label">{$t('settingsPanel.toggle.verboseSync')}</span>
            <ToggleSwitch
              checked={$prefs.verboseSync}
              label={$t('settingsPanel.toggle.verboseSync')}
              on:change={(e) => setVerboseSync(e.detail)}
            />
          </div>
          <p class="hint">
            {$t('settingsPanel.hint.verboseSyncDetail')}
          </p>
          <div class="toggle" title={$t('settingsPanel.hint.syncProgressBar')}>
            <span class="row-label">{$t('settingsPanel.toggle.syncProgressBar')}</span>
            <ToggleSwitch
              checked={$prefs.syncProgressBar}
              label={$t('settingsPanel.toggle.syncProgressBar')}
              on:change={(e) => setSyncProgressBar(e.detail)}
            />
          </div>
          <p class="hint">
            {$t('settingsPanel.hint.syncProgressBarDetail')}
          </p>
          <StepSlider
            label={$t('settingsPanel.label.syncMaxParallel')}
            value={String($prefs.syncMaxParallel)}
            options={syncParallelOptions}
            on:change={onSyncMaxParallel}
          />
          <p class="hint">
            {$t('settingsPanel.hint.syncMaxParallel')}
          </p>
          <StepSlider
            label={$t('settingsPanel.label.syncMessageLimit')}
            value={String($prefs.syncMessageLimit)}
            options={syncLimitOptions}
            on:change={onSyncMessageLimit}
          />
          <p class="hint">
            {$t('settingsPanel.hint.syncMessageLimit')}
          </p>
          <StepSlider
            label={$t('settingsPanel.label.syncFullReconcileDays')}
            value={String($prefs.syncFullReconcileDays)}
            options={syncFullReconcileOptions}
            on:change={onSyncFullReconcileDays}
          />
          <p class="hint">
            {$t('settingsPanel.hint.syncFullReconcileDays')}
          </p>
          <div class="toggle" title={$t('settingsPanel.hint.syncAutoBackfill')}>
            <span class="row-label">{$t('settingsPanel.toggle.syncAutoBackfill')}</span>
            <ToggleSwitch
              checked={$prefs.syncAutoBackfill}
              label={$t('settingsPanel.toggle.syncAutoBackfill')}
              on:change={(e) => setSyncAutoBackfill(e.detail)}
            />
          </div>
          <p class="hint">
            {$t('settingsPanel.hint.syncAutoBackfillDetail')}
          </p>

          <h4 class="subhead">{$t('settingsPanel.category.offline')}</h4>
          <div class="toggle" title={$t('settingsPanel.hint.offlineIndicator')}>
            <span class="row-label">{$t('settingsPanel.toggle.offlineIndicator')}</span>
            <ToggleSwitch
              checked={$prefs.showOfflineIndicator}
              label={$t('settingsPanel.toggle.offlineIndicator')}
              on:change={(e) => setShowOfflineIndicator(e.detail)}
            />
          </div>
          <div class="toggle" title={$t('settingsPanel.hint.flagColorSync')}>
            <span class="row-label">{$t('settingsPanel.toggle.flagColorSync')}</span>
            <ToggleSwitch
              checked={$prefs.flagColorSync}
              label={$t('settingsPanel.toggle.flagColorSync')}
              on:change={(e) => setFlagColorSync(e.detail)}
            />
          </div>

          <div class="field">
            <span class="row-label">{$t('settingsPanel.label.downloadOffline')}</span>
            <p class="hint">
              {$t('settingsPanel.hint.downloadOffline')}
            </p>
            <div class="download-presets">
              {#each downloadPresets as p (p.key)}
                <button type="button" class="preset-btn" on:click={() => applyPreset(p.days)}>{p.label}</button>
              {/each}
            </div>
            <div class="download-row">
              <div class="download-date">
                <DateTimePicker mode="date" bind:value={downloadStart} />
              </div>
              {#if $downloadProgress && $downloadProgress.running}
                <button type="button" class="action-btn" on:click={() => cancelDownload()}>
                  {$t('settingsPanel.button.cancelDownload')}
                </button>
              {:else}
                <button type="button" class="action-btn" on:click={startDownload}>
                  {$t('settingsPanel.button.download')}
                </button>
              {/if}
            </div>
            <div class="toggle" title={$t('settingsPanel.hint.includeAttachments')}>
              <span class="row-label">{$t('settingsPanel.toggle.includeAttachments')}</span>
              <ToggleSwitch
                checked={$prefs.downloadIncludeAttachments}
                label={$t('settingsPanel.toggle.includeAttachments')}
                on:change={(e) => setDownloadIncludeAttachments(e.detail)}
              />
            </div>
          </div>
        </section>
      {:else if active === 'mailboxes'}
        <section>
          <MailboxesSection />
          <div class="merged-block">
            <AddressBookSection />
          </div>
        </section>
      {:else if active === 'contacts'}
        <section>
          <ContactsSection />
        </section>
      {:else if active === 'profiles'}
        <section>
          <ProfilesSection />
        </section>
      {:else if active === 'external'}
        <section>
          <ExternalSection />
          <h4 class="subhead">{$t('settingsPanel.category.importExport')}</h4>
          <ImportExportSection />
        </section>
      {:else if active === 'composing'}
        <section>
          <h3>{$t('settingsPanel.category.composingSending')}</h3>
          <SegmentedSetting
            label={$t('settingsPanel.label.defaultEditor')}
            value={$prefs.defaultEditorMode}
            options={editorModeOptions}
            on:change={(e) => setDefaultEditorMode(e.detail as EditorMode)}
          />
          <p class="hint">{$t('settingsPanel.hint.defaultEditor')}</p>
          <div class="toggle" title={$t('settingsPanel.hint.autocomplete')}>
            <span class="row-label">{$t('settingsPanel.toggle.autocomplete')}</span>
            <ToggleSwitch
              checked={$prefs.composeAutocomplete}
              label={$t('settingsPanel.toggle.autocomplete')}
              on:change={(e) => setComposeAutocomplete(e.detail)}
            />
          </div>
          <div class="toggle" title={$t('settingsPanel.hint.chipRecipients')}>
            <span class="row-label">{$t('settingsPanel.toggle.chipRecipients')}</span>
            <ToggleSwitch
              checked={$prefs.composeChips}
              label={$t('settingsPanel.toggle.chipRecipients')}
              on:change={(e) => setComposeChips(e.detail)}
            />
          </div>
          {#if !$prefs.composeChips}
            <p class="hint">{$t('settingsPanel.hint.plainRecipients')}</p>
          {/if}
          <div class="toggle" title={$t('settingsPanel.hint.vimEditor')}>
            <span class="row-label">{$t('onboarding.extras.vimEditor')} <span class="badge-experimental">{$t('common.experimental')}</span></span>
            <ToggleSwitch
              checked={$prefs.composeVimMode}
              label={$t('onboarding.extras.vimEditor')}
              on:change={(e) => setComposeVimMode(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.vimEditorDetail')}</p>

          <h4 class="subhead">{$t('settingsPanel.category.sending')}</h4>
          <SegmentedSetting
            label={$t('settingsPanel.label.undoSendWindow')}
            value={String($prefs.sendDelaySeconds)}
            options={sendDelayOptions}
            on:change={onSendDelay}
          />
          <p class="hint">{$t('settingsPanel.hint.undoSend')}</p>
        </section>
      {:else if active === 'encryption'}
        <section>
          <h3>{$t('settingsPanel.category.encryption')}</h3>
          <EncryptionSection />
        </section>
      {:else if active === 'shortcuts'}
        <section>
          <h3>{$t('settings.shortcuts')}</h3>
          <div class="toggle">
            <span class="row-label">{$t('settingsPanel.toggle.shortcutHints')}</span>
            <ToggleSwitch
              checked={$prefs.showShortcutHints}
              label={$t('settingsPanel.toggle.shortcutHints')}
              on:change={(e) => setShortcutHints(e.detail)}
            />
          </div>
          <div class="toggle" title={$t('settingsPanel.hint.paletteQuickSelect')}>
            <span class="row-label">{$t('settingsPanel.toggle.paletteQuickSelect')}</span>
            <ToggleSwitch
              checked={$paletteQuickSelect}
              label={$t('settingsPanel.toggle.paletteQuickSelect')}
              on:change={(e) => setPaletteQuickSelect(e.detail)}
            />
          </div>
          <div class="toggle" title={$t('settingsPanel.hint.appVim')}>
            <span class="row-label">{$t('onboarding.extras.appVim')} <span class="badge-experimental">{$t('common.experimental')}</span></span>
            <ToggleSwitch
              checked={$prefs.appVimMode}
              label={$t('onboarding.extras.appVim')}
              on:change={(e) => setAppVimMode(e.detail)}
            />
          </div>
          <p class="hint">{$t('settingsPanel.hint.appVimDetail')}</p>
          <ShortcutSettings />
        </section>
      {:else if active === 'about'}
        <section>
          <h3>{$t('settingsPanel.category.about')}</h3>
          <AboutSection on:rerunOnboarding={() => dispatch('rerunOnboarding')} />
        </section>
      {/if}
    </div>
  </div>
</div>

<!-- the allowlist modal is code-split so its list logic loads only on demand. -->
{#if allowlistOpen}
  {#await import('./ImageAllowlistModal.svelte') then m}
    <svelte:component
      this={m.default}
      on:close={() => (allowlistOpen = false)}
      on:openMessage={() => {
        allowlistOpen = false
        dispatch('close')
      }}
    />
  {/await}
{/if}

{#if vipOpen}
  {#await import('./VIPSendersModal.svelte') then m}
    <svelte:component this={m.default} on:close={() => (vipOpen = false)} />
  {/await}
{/if}

<PassphraseDialog />

<style>
  .screen {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: flex;
    flex-direction: column;
    background: var(--surface-base);
    /* covers the whole window, so it has to keep the macOS traffic lights clear
       itself; zero on every other platform. */
    padding-top: var(--titlebar-lights);
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4) var(--space-6);
    border-bottom: var(--hairline) solid var(--border-default);
    flex-shrink: 0;
  }

  .head h2 {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
  }

  .close {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    padding: var(--space-2);
    border-radius: var(--radius-control);
  }

  .close:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  /* two columns: a fixed nav rail and a scrolling content pane. */
  .body {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: 220px 1fr;
  }

  .nav {
    border-inline-end: var(--hairline) solid var(--border-subtle);
    padding: var(--space-3);
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .nav-search {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    margin-bottom: var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
    position: sticky;
    top: 0;
    z-index: 1;
  }

  .nav-search:focus-within {
    border-color: var(--accent);
  }

  .nav-search input {
    flex: 1;
    min-width: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-list);
  }

  .nav-search-clear {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: 0;
  }

  .nav-search-clear:hover {
    color: var(--text-primary);
  }

  .nav-group {
    padding: var(--space-3) var(--space-3) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .nav-group:first-of-type {
    padding-top: var(--space-1);
  }

  .nav-empty {
    padding: var(--space-3);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .nav-item {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: none;
    background: transparent;
    border-radius: var(--radius-control);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    text-align: start;
    font-size: var(--fz-list);
  }

  .nav-item:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .nav-item.active {
    background: var(--selection-bg);
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .nav-icon {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .nav-item.active .nav-icon {
    color: var(--accent);
  }

  .content {
    overflow-y: auto;
    padding: var(--space-5) var(--space-6) var(--space-6);
  }

  section {
    max-width: 720px;
  }

  h3 {
    margin: 0 0 var(--space-4);
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  /* subheading for a merged sub-group inside a category. */
  .subhead {
    margin: var(--space-5) 0 var(--space-2);
    padding-top: var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  /* a merged component sub-section (e.g. Contacts under Accounts) gets a divider
     and its own top spacing since it renders its own heading. */
  .merged-block {
    margin-top: var(--space-5);
    padding-top: var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-2) 0;
  }

  .field {
    padding: var(--space-3) 0;
  }

  /* the log folder path, shown so it can be read and typed somewhere else. */
  .hint.path {
    font-family: var(--font-mono);
    word-break: break-all;
  }

  .log-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }

  .row-label {
    font-size: var(--fz-body);
    color: var(--text-primary);
  }

  .schedule-times {
    display: flex;
    gap: var(--space-5);
    margin: var(--space-2) 0;
  }

  .schedule-time {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .schedule-time input {
    padding: var(--space-1) var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-size: var(--fz-label);
  }

  /* a hint hugs the control it describes (small gap above) and leaves a clear
     gap below so it reads as part of that setting, not the next one. */
  .hint {
    margin: 2px 0 var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  /* flat list of individual settings that match the search query. */
  .search-results {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .result {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-4);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    background: var(--surface-raised);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    text-align: start;
  }

  .result:hover {
    background: var(--surface-hover);
    border-color: var(--border-default);
  }

  .result-label {
    font-size: var(--fz-body);
  }

  .result-cat {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .edit-menubar {
    margin-top: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .edit-menubar:hover {
    background: var(--surface-hover);
  }

  /* a small marker for features that are still rough around the edges. */
  .badge-experimental {
    display: inline-block;
    margin-inline-start: var(--space-2);
    padding: 1px 6px;
    border-radius: var(--radius-control);
    background: var(--warning-bg, var(--surface-sunken));
    color: var(--warning, var(--text-secondary));
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    vertical-align: middle;
  }

  /* generated-style chooser: a small grid of previewed cards. */
  .style-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: var(--space-3);
    margin-top: var(--space-3);
  }

  .style-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    cursor: var(--cursor-action);
  }

  .style-card:hover {
    background: var(--surface-hover);
  }

  .style-card.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  .style-img {
    width: 48px;
    height: 48px;
    border-radius: 999px;
  }

  .style-label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .action-btn {
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .action-btn:hover {
    background: var(--surface-hover);
  }

  .action-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .empty-image-row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    margin-top: var(--space-2);
  }

  .empty-image-preview {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 72px;
    height: 72px;
    flex-shrink: 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .empty-image-preview img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
  }

  .empty-image-actions {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    align-items: flex-start;
  }

  .hidden-file {
    display: none;
  }

  .warn {
    margin-top: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--warning);
    border-radius: var(--radius-card);
    background: var(--warning-bg);
  }

  .warn p {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-primary);
    line-height: 1.5;
  }

  .warn-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
  }

  .ghost-btn,
  .danger-btn {
    padding: var(--space-2) var(--space-4);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    border: var(--hairline) solid var(--border-default);
  }

  .ghost-btn {
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .danger-btn {
    background: var(--danger);
    color: #fff;
    border-color: transparent;
  }

  .toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-2) 0;
    cursor: var(--cursor-action);
  }

  .toggle.disabled {
    opacity: 0.45;
    cursor: default;
  }

  .row.disabled {
    opacity: 0.45;
  }

  /* the picker carries its own look now. The settings rows only cap how wide
     it gets, so a long font or folder name cannot push the row apart. */
  .row :global(.select) {
    max-width: 60%;
  }

  .download-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }

  .download-date {
    width: 160px;
  }

  .download-presets {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }

  .preset-btn {
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: 999px;
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .preset-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .lang-tools-heading {
    margin: var(--space-5) 0 var(--space-1);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .lang-tools {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .lang-tool-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .lang-tool-btn:hover {
    background: var(--surface-hover);
  }
</style>
