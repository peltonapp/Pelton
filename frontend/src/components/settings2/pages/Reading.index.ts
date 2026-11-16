import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'reading', id: 'message-font-size', labelKey: 'onboarding.extras.fontSize', keywords: 'message font size text zoom larger smaller reading' },
  { page: 'reading', id: 'body-font', labelKey: 'settingsPanel.label.bodyFont', keywords: 'font typeface message body serif sans' },
  { page: 'reading', id: 'sender-fonts', labelKey: 'settingsPanel.toggle.senderFonts', keywords: 'font email typeface sender' },
  { page: 'reading', id: 'charset-fallback', labelKey: 'settingsPanel.label.charsetFallback', keywords: 'encoding charset unicode gibberish mojibake utf8 latin' },
  { page: 'reading', id: 'mailbox-badge', labelKey: 'settingsPanel.mailboxBadge', keywords: 'technical info account badge mailbox' },
  { page: 'reading', id: 'pgp-status', labelKey: 'settingsPanel.pgpStatus', keywords: 'technical info pgp gpg signature badge encrypted' },
  { page: 'reading', id: 'auth-status', labelKey: 'settingsPanel.authStatus', keywords: 'technical info spf dkim dmarc authentication badge' },
  { page: 'reading', id: 'mark-read', labelKey: 'settingsPanel.toggle.markReadOnOpen', keywords: 'mark read seen unread open automatically' },
  { page: 'reading', id: 'sender-panel', labelKey: 'senderPanel.setting', keywords: 'sender panel contact person side info vip files conversations' },
  { page: 'reading', id: 'project-suggestions', labelKey: 'projects.settings.suggestions', keywords: 'projects suggest add file people sender board' },
  { page: 'reading', id: 'mail-dates', labelKey: 'reading.mailDates', keywords: 'dates times calendar hover preview event detect highlight' },
  { page: 'reading', id: 'open-on-arrow', labelKey: 'settingsPanel.toggle.openOnArrow', keywords: 'arrow keys keyboard open select navigate enter' },
]

export default entries
