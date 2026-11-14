import type { SettingsIndexEntry } from '../../../lib/settings2'
import { isMac } from '../../../lib/i18n'

const entries: SettingsIndexEntry[] = [
  { page: 'layout', id: 'layout', labelKey: 'layout.label', keywords: 'three column stacked panes vertical horizontal reading pane' },
  { page: 'layout', id: 'nav', labelKey: 'nav.label', keywords: 'rail sidebar footer tabs switcher mail contacts calendar hide' },
  { page: 'layout', id: 'rail-side', labelKey: 'nav.side.label', keywords: 'rail left right' },
  { page: 'layout', id: 'pane-lock', labelKey: 'settings.lockPanes', keywords: 'resize width lock' },
  { page: 'layout', id: 'native-titlebar', labelKey: 'layout.nativeTitlebar', keywords: 'title bar window frame native system' },
  ...(isMac
    ? [
        { page: 'layout', id: 'menubar-in-app', labelKey: 'settingsPanel.toggle.menuBarInApp', keywords: 'menu bar in-app top window' },
        { page: 'layout', id: 'menubar-native-minimal', labelKey: 'settingsPanel.toggle.menuBarNativeMinimal', keywords: 'menu bar native macos reduce minimal' },
      ]
    : []),
  { page: 'layout', id: 'menubar-icons', labelKey: 'settingsPanel.toggle.menuBarIcons', keywords: 'menu bar icons' },
  { page: 'layout', id: 'menubar-edit', labelKey: 'menuBar.editButton', keywords: 'menu bar edit rearrange reorder custom items submenu' },
  { page: 'layout', id: 'menubar-new-items', labelKey: 'menuBar.newItems.label', keywords: 'menu bar new items update hidden' },
  { page: 'layout', id: 'status-clock', labelKey: 'settingsPanel.toggle.statusClock', keywords: 'status bar clock time date' },
  { page: 'layout', id: 'status-unread', labelKey: 'settingsPanel.toggle.statusUnread', keywords: 'status bar unread count new mail' },
  { page: 'layout', id: 'sync-progress-bar', labelKey: 'settingsPanel.toggle.syncProgressBar', keywords: 'status bar sync progress bar percent count' },
  { page: 'layout', id: 'indent-guides', labelKey: 'settingsPanel.toggle.indentGuides', keywords: 'sidebar folders nested lines' },
  { page: 'layout', id: 'account-email', labelKey: 'settingsPanel.toggle.showMailboxEmail', keywords: 'sidebar account email name mailbox' },
  { page: 'layout', id: 'flagged-count', labelKey: 'settingsPanel.toggle.flaggedCount', keywords: 'sidebar flagged count starred' },
  { page: 'layout', id: 'offline-indicator', labelKey: 'settingsPanel.toggle.offlineIndicator', keywords: 'sidebar offline downloaded badge' },
  { page: 'layout', id: 'unsynced-folder', labelKey: 'settingsPanel.toggle.unsyncedFolder', keywords: 'sidebar folder not syncing icon' },
  { page: 'layout', id: 'views-placement', labelKey: 'views.setting.label', keywords: 'views saved searches preset sidebar tab' },
]

export default entries
