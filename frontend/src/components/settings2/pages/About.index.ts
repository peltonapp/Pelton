import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  {
    page: 'about',
    id: 'about',
    labelKey: 'settingsPanel.category.about',
    keywords: 'version license licenses website github issue report contributors warranty liability logo',
  },
  {
    page: 'about',
    id: 'about-updates',
    labelKey: 'about.update.label',
    keywords: 'update updates check release new version weekly monthly',
  },
  {
    page: 'about',
    id: 'about-diagnostics',
    labelKey: 'about.diagnostics.label',
    keywords: 'diagnostics log folder logs copy bug report',
  },
  {
    page: 'about',
    id: 'about-tour',
    labelKey: 'setup.runAgain',
    keywords: 'onboarding tour welcome rerun setup again',
  },
]

export default entries
