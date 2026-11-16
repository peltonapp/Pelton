import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'privacy', id: 'always-load-images', labelKey: 'settingsPanel.toggle.alwaysLoadImages', keywords: 'remote images tracking block load' },
  { page: 'privacy', id: 'block-tracking-pixels', labelKey: 'settingsPanel.toggle.blockTrackingPixels', keywords: 'tracking pixel spy pixel read receipt open tracking' },
  { page: 'privacy', id: 'allowlist', labelKey: 'settingsPanel.label.manageWhitelist', keywords: 'trusted senders allowlist whitelist domains remote images' },
  { page: 'privacy', id: 'clean-links', labelKey: 'links.clean.label', keywords: 'links clean tracking utm fbclid redirect unwrap parameters url' },
  { page: 'privacy', id: 'proxy', labelKey: 'settingsPanel.category.network', keywords: 'proxy connection tls socks socks5 http network route' },
]

export default entries
