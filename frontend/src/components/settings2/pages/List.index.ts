import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'list', id: 'avatar-placement', labelKey: 'list.avatarPlacement.label', keywords: 'avatar picture photo sender column inline hidden show hide' },
  { page: 'list', id: 'row-template', labelKey: 'settingsPanel.label.rowLayout', keywords: 'rows template relaxed comfortable compact single line density height' },
  { page: 'list', id: 'snippet', labelKey: 'settingsPanel.toggle.showMessagePreview', keywords: 'preview snippet excerpt text body' },
  { page: 'list', id: 'preview-lines', labelKey: 'settingsPanel.label.previewLines', keywords: 'preview snippet lines one two three' },
  { page: 'list', id: 'account-email', labelKey: 'settingsPanel.toggle.showMailboxEmail', keywords: 'account mailbox email address' },
  { page: 'list', id: 'flag-highlight', labelKey: 'settingsPanel.label.flaggedHighlight', keywords: 'flag flagged highlight bar icon star mark' },
  { page: 'list', id: 'date-time', labelKey: 'settingsPanel.dateTime', keywords: 'date time timestamp show hide' },
  { page: 'list', id: 'time-format', labelKey: 'settingsPanel.label.timeFormat', keywords: 'clock 12 24 hour am pm' },
  { page: 'list', id: 'multi-select', labelKey: 'settingsPanel.toggle.multiSelect', keywords: 'select multiple bulk checkbox' },
  { page: 'list', id: 'selected-count', labelKey: 'settingsPanel.toggle.showSelectedCount', keywords: 'selection count number selected' },
  { page: 'list', id: 'select-all-scope', labelKey: 'settingsPanel.label.selectAllScope', keywords: 'select all bulk selection loaded folder' },
  { page: 'list', id: 'select-all-unified', labelKey: 'settingsPanel.toggle.selectAllUnified', keywords: 'select all unified inbox' },
  { page: 'list', id: 'search-sort-text', labelKey: 'settingsPanel.searchSort.text', keywords: 'search sort order relevance newest oldest alphabetical subject' },
  { page: 'list', id: 'search-sort-dated', labelKey: 'settingsPanel.searchSort.dated', keywords: 'search sort order date range relevance newest oldest' },
  { page: 'list', id: 'search-sort-filtered', labelKey: 'settingsPanel.searchSort.filtered', keywords: 'search sort order filter relevance newest oldest' },
  { page: 'list', id: 'indent-guides', labelKey: 'settingsPanel.toggle.indentGuides', keywords: 'sidebar folders tree lines indent' },
  { page: 'list', id: 'unsynced-folder', labelKey: 'settingsPanel.toggle.unsyncedFolder', keywords: 'sidebar folder not synced unsynced' },
  { page: 'list', id: 'flagged-count', labelKey: 'settingsPanel.toggle.flaggedCount', keywords: 'sidebar flagged count number' },
  { page: 'list', id: 'startup-selection', labelKey: 'settingsPanel.label.startupSelection', keywords: 'sidebar startup launch open default folder view last used' },
  { page: 'list', id: 'views-placement', labelKey: 'views.setting.label', keywords: 'views saved searches sidebar tab hidden' },
  { page: 'list', id: 'avatar-source', labelKey: 'settingsPanel.label.senderPhotos', keywords: 'avatar gravatar bimi sender photos logo generated' },
  { page: 'list', id: 'avatar-style', labelKey: 'settingsPanel.label.generatedStyle', keywords: 'avatar style initials mono pixel geometric generated' },
  { page: 'list', id: 'swipe-enabled', labelKey: 'settingsPanel.toggle.swipeEnabled', keywords: 'swipe gesture trackpad' },
  { page: 'list', id: 'swipe-left', labelKey: 'settingsPanel.label.swipeLeft', keywords: 'swipe left gesture trackpad action' },
  { page: 'list', id: 'swipe-right', labelKey: 'settingsPanel.label.swipeRight', keywords: 'swipe right gesture trackpad action' },
]

export default entries
