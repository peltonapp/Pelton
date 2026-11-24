import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'filters', id: 'filters-mailbox', labelKey: 'settings2.page.filters', keywords: 'server filters rules sieve managesieve mail server sort move' },
  { page: 'filters', id: 'filters-rules', labelKey: 'sieve.filters.add', keywords: 'new rule filter move folder forward redirect delete flag mark read' },
  { page: 'filters', id: 'filters-mailbox', labelKey: 'sieve.restore', keywords: 'undo backup previous script restore' },
  { page: 'filters', id: 'filters-mailbox', labelKey: 'sieve.filters.view.script', keywords: 'sieve script raw text edit code' },
]

export default entries
