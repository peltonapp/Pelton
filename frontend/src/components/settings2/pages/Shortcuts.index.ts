import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'shortcuts', id: 'shortcut-hints', labelKey: 'settingsPanel.toggle.shortcutHints', keywords: 'keyboard hints tooltips' },
  { page: 'shortcuts', id: 'coach-hints', labelKey: 'coach.settings.hints', keywords: 'coach hint learn keys mouse tip' },
  { page: 'shortcuts', id: 'coach-hold', labelKey: 'coach.settings.hold', keywords: 'hold command control modifier cheat sheet overlay' },
  { page: 'shortcuts', id: 'coach-reset', labelKey: 'coach.settings.reset', keywords: 'reset hints coach start over' },
  { page: 'shortcuts', id: 'tips', labelKey: 'tips.settings.show', keywords: 'tips hints first time learn discover help' },
  { page: 'shortcuts', id: 'tips-reset', labelKey: 'tips.settings.reset', keywords: 'reset tips show again start over' },
  { page: 'shortcuts', id: 'palette-quick-select', labelKey: 'settingsPanel.toggle.paletteQuickSelect', keywords: 'command palette quick select numbers' },
  { page: 'shortcuts', id: 'app-vim', labelKey: 'onboarding.extras.appVim', keywords: 'vim navigation hjkl keys' },
  { page: 'shortcuts', id: 'bindings', labelKey: 'settings.shortcuts', keywords: 'keyboard keys hotkeys rebind change reset binding' },
]

export default entries
