import type { SettingsIndexEntry } from '../../../lib/settings2'
import { isMac } from '../../../lib/i18n'

const entries: SettingsIndexEntry[] = [
  { page: 'notifications', id: 'toast-position', labelKey: 'settings.toastPosition', keywords: 'notification toast position corner popup' },
  { page: 'notifications', id: 'notify-new-mail', labelKey: 'vip.notifyNewMail', keywords: 'new mail notification alert desktop' },
  {
    page: 'notifications',
    id: 'unread-badge',
    labelKey: isMac ? 'settingsPanel.toggle.dockBadge' : 'settingsPanel.toggle.trayBadge',
    keywords: isMac ? 'dock badge unread count icon' : 'tray badge dot unread count icon',
  },
  { page: 'notifications', id: 'focus-schedule', labelKey: 'focus.schedule.label', keywords: 'focus quiet do not disturb schedule hold notifications' },
  { page: 'notifications', id: 'focus-days', labelKey: 'focus.schedule.days', keywords: 'focus schedule weekdays' },
  { page: 'notifications', id: 'focus-hours', labelKey: 'focus.schedule.hours', keywords: 'focus schedule hours time' },
  { page: 'notifications', id: 'vip-senders', labelKey: 'vip.manageLabel', keywords: 'vip senders important star notify always' },
]

export default entries
