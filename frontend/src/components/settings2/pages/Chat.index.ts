import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'chat', id: 'chat-enabled', labelKey: 'chatSettings.enabled', keywords: 'chat matrix messaging enable turn on off' },
  { page: 'chat', id: 'chat-account', labelKey: 'chatSettings.account', keywords: 'chat matrix account sign in sign out login homeserver server sso' },
  { page: 'chat', id: 'chat-homeserver', labelKey: 'chatSettings.homeserver', keywords: 'chat matrix homeserver server' },
  { page: 'chat', id: 'chat-session', labelKey: 'chatSettings.session', keywords: 'chat matrix session device id' },
  { page: 'chat', id: 'chat-connection', labelKey: 'chatSettings.connection', keywords: 'chat matrix connection online offline syncing status' },
  { page: 'chat', id: 'chat-device-verified', labelKey: 'chatSettings.deviceVerified', keywords: 'chat matrix verify verified device encryption trust' },
  { page: 'chat', id: 'chat-cross-signing', labelKey: 'chatSettings.crossSigning', keywords: 'chat matrix cross-signing encryption keys' },
  { page: 'chat', id: 'chat-secure-backup', labelKey: 'chatSettings.secureBackup', keywords: 'chat matrix secure backup recovery key passphrase encryption' },
  { page: 'chat', id: 'chat-key-backup', labelKey: 'chatSettings.keyBackup', keywords: 'chat matrix key backup message keys encryption' },
  { page: 'chat', id: 'chat-fingerprint', labelKey: 'chatSettings.fingerprint', keywords: 'chat matrix fingerprint key encryption' },
  { page: 'chat', id: 'chat-encryption-actions', labelKey: 'chatSettings.actions', keywords: 'chat matrix verify unlock recovery key set up secure backup' },
  { page: 'chat', id: 'chat-sessions', labelKey: 'chatSettings.sessions', keywords: 'chat matrix sessions devices sign out other last seen' },
  { page: 'chat', id: 'chat-notify', labelKey: 'chatSettings.notify', keywords: 'chat matrix notifications all messages mentions direct off' },
  { page: 'chat', id: 'chat-receipts', labelKey: 'chatSettings.receipts', keywords: 'chat matrix read receipts private privacy' },
  { page: 'chat', id: 'chat-typing', labelKey: 'chatSettings.typing', keywords: 'chat matrix typing notifications privacy' },
  { page: 'chat', id: 'chat-markdown', labelKey: 'chatSettings.markdown', keywords: 'chat matrix markdown format compose messages' },
]

export default entries
