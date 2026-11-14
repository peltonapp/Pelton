import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'general', id: 'language', labelKey: 'settings.language', keywords: 'locale translation interface' },
  { page: 'general', id: 'language-custom', labelKey: 'language.custom', keywords: 'translation language file folder template json' },
  { page: 'general', id: 'time-format', labelKey: 'settingsPanel.label.timeFormat', keywords: 'clock 12 24 hour am pm' },
  { page: 'general', id: 'startup-selection', labelKey: 'settingsPanel.label.startupSelection', keywords: 'startup launch open default folder view last used sidebar' },
  { page: 'general', id: 'restore-tabs', labelKey: 'settingsPanel.toggle.restoreTabs', keywords: 'reopen tabs launch restore' },
  { page: 'general', id: 'start-hidden', labelKey: 'settingsPanel.toggle.startHidden', keywords: 'start minimized hidden tray background launch login autostart' },
  { page: 'general', id: 'close-action', labelKey: 'settingsPanel.label.closeAction', keywords: 'close button tray background quit exit' },
]

export default entries
