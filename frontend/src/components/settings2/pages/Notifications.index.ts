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
  { page: 'notifications', id: 'vip-senders', labelKey: 'vip.manageLabel', keywords: 'vip senders important star notify always' },
]

export default entries
