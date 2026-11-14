import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'layout', id: 'layout', labelKey: 'layout.label', keywords: 'three column stacked panes vertical horizontal reading pane' },
  { page: 'layout', id: 'nav', labelKey: 'nav.label', keywords: 'rail sidebar footer tabs switcher mail contacts calendar hide' },
  { page: 'layout', id: 'rail-side', labelKey: 'nav.side.label', keywords: 'rail left right' },
  { page: 'layout', id: 'pane-lock', labelKey: 'settings.lockPanes', keywords: 'resize width lock' },
  { page: 'layout', id: 'native-titlebar', labelKey: 'layout.nativeTitlebar', keywords: 'title bar window frame native system' },
]

export default entries
