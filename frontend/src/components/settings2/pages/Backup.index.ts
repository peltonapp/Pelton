import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'backup', id: 'external', labelKey: 'settingsPanel.category.external', keywords: 'external mcp server ai assistant tools port token' },
  { page: 'backup', id: 'cli', labelKey: 'cli.title', keywords: 'command line terminal shell cli pelton script socket token' },
  { page: 'backup', id: 'import-export', labelKey: 'importExport.exportTitle', keywords: 'export backup settings mail file save' },
  { page: 'backup', id: 'import-export', labelKey: 'import.tb.title', keywords: 'import thunderbird apple mail outlook pst profile migrate move' },
  { page: 'backup', id: 'import-export', labelKey: 'import.files.title', keywords: 'import mbox eml maildir vcard ics mail files contacts calendar' },
  { page: 'backup', id: 'import-export', labelKey: 'importExport.importTitle', keywords: 'import restore backup file settings' },
]

export default entries
