import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'privacy', id: 'always-load-images', labelKey: 'settingsPanel.toggle.alwaysLoadImages', keywords: 'remote images tracking block load' },
  { page: 'privacy', id: 'block-tracking-pixels', labelKey: 'settingsPanel.toggle.blockTrackingPixels', keywords: 'tracking pixel spy pixel read receipt open tracking' },
  { page: 'privacy', id: 'allowlist', labelKey: 'settingsPanel.label.manageWhitelist', keywords: 'trusted senders allowlist whitelist domains remote images' },
  { page: 'privacy', id: 'clean-links', labelKey: 'links.clean.label', keywords: 'links clean tracking utm fbclid redirect unwrap parameters url' },
  { page: 'privacy', id: 'autoconfig-lookup', labelKey: 'privacy.lookup.label', keywords: 'autoconfig autodiscover ispdb thunderbird mozilla mx dns lookup server settings mailbox' },
  { page: 'privacy', id: 'privacy-counters', labelKey: 'privacy.counters.label', keywords: 'tracking report trackers counters history shield' },
  { page: 'privacy', id: 'unsubscribe-grace', labelKey: 'privacy.grace.label', keywords: 'unsubscribe still sending grace days' },
  { page: 'privacy', id: 'privacy-report', labelKey: 'privacy.report.title', keywords: 'tracking report trackers senders pixels links' },
  { page: 'privacy', id: 'app-lock',labelKey: 'applock.settings.toggle', keywords: 'lock app lock screen touch id password passphrase security idle away' },
  { page: 'privacy', id: 'app-lock-idle', labelKey: 'applock.settings.idle', keywords: 'lock idle minutes away timeout automatic' },
  { page: 'privacy', id: 'app-lock-launch', labelKey: 'applock.settings.onLaunch', keywords: 'lock start launch open' },
  { page: 'privacy', id: 'app-lock-notifications', labelKey: 'applock.settings.hideNotifications', keywords: 'lock notifications hide sender subject private' },
  { page: 'privacy', id: 'app-lock-now', labelKey: 'applock.settings.lockNow', keywords: 'lock now passphrase change' },
  { page: 'privacy', id: 'proxy', labelKey: 'settingsPanel.category.network', keywords: 'proxy connection tls socks socks5 http network route' },
]

export default entries
