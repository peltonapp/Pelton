import type { SettingsIndexEntry } from '../../../lib/settings2'
import { isMac, isWindows } from '../../../lib/i18n'

const entries: SettingsIndexEntry[] = [
  { page: 'theme', id: 'theme-mode', labelKey: 'settingsPanel.label.theme', keywords: 'dark light system schedule night color mode' },
  { page: 'theme', id: 'accent', labelKey: 'onboarding.accentTitle', keywords: 'accent color highlight hex' },
  { page: 'theme', id: 'themes', labelKey: 'settingsPanel.category.themes', keywords: 'theme pack import custom peltontheme gallery editor' },
  { page: 'theme', id: 'density', labelKey: 'settingsPanel.label.density', keywords: 'compact medium luxe spacing tight' },
  { page: 'theme', id: 'corners', labelKey: 'settingsPanel.label.corners', keywords: 'rounded square radius' },
  { page: 'theme', id: 'interface-scale', labelKey: 'settingsPanel.label.interfaceScale', keywords: 'zoom scale size bigger smaller' },
  ...(isMac || isWindows
    ? [{ page: 'theme', id: 'window-material', labelKey: 'settings2.theme.windowMaterial', keywords: 'glass liquid mica material translucent transparent blur' }]
    : []),
  { page: 'theme', id: 'ui-font', labelKey: 'settingsPanel.label.uiFont', keywords: 'font interface typeface' },
  { page: 'theme', id: 'mono-font', labelKey: 'settingsPanel.label.monoFont', keywords: 'font monospace code' },
  { page: 'theme', id: 'empty-image', labelKey: 'settingsPanel.label.emptyStateImage', keywords: 'background image empty reading pane logo' },
  { page: 'theme', id: 'empty-fullscreen', labelKey: 'settingsPanel.toggle.emptyStateFullscreen', keywords: 'background image fill full bleed' },
]

export default entries
