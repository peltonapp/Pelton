import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'shortcuts', id: 'shortcut-hints', labelKey: 'settingsPanel.toggle.shortcutHints', keywords: 'keyboard hints tooltips' },
  { page: 'shortcuts', id: 'palette-quick-select', labelKey: 'settingsPanel.toggle.paletteQuickSelect', keywords: 'command palette quick select numbers' },
  { page: 'shortcuts', id: 'app-vim', labelKey: 'onboarding.extras.appVim', keywords: 'vim navigation hjkl keys' },
  { page: 'shortcuts', id: 'bindings', labelKey: 'settings.shortcuts', keywords: 'keyboard keys hotkeys rebind change reset binding' },
]

export default entries
