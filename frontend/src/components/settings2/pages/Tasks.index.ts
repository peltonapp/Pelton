import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'tasks', id: 'tasks-default-list', labelKey: 'taskSettings.defaultList', keywords: 'tasks todo default list new' },
  { page: 'tasks', id: 'tasks-due-time', labelKey: 'taskSettings.dueTime', keywords: 'tasks todo due time default hour' },
  { page: 'tasks', id: 'tasks-badge', labelKey: 'taskSettings.badge', keywords: 'tasks todo badge count rail overdue today' },
  { page: 'tasks', id: 'tasks-in-calendar', labelKey: 'taskSettings.inCalendar', keywords: 'tasks todo calendar show due' },
  { page: 'tasks', id: 'tasks-completed', labelKey: 'taskSettings.completed', keywords: 'tasks todo completed done hide keep days' },
  { page: 'tasks', id: 'tasks-sort', labelKey: 'taskSettings.sort', keywords: 'tasks todo sort order manual due priority title' },
]

export default entries
