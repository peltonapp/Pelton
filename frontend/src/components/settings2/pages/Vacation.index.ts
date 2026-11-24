import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'vacation', id: 'vacation-reply', labelKey: 'sieve.vacation.enabled', keywords: 'out of office vacation away holiday auto reply autoresponder automatic answer' },
  { page: 'vacation', id: 'vacation-dates', labelKey: 'sieve.vacation.dates', keywords: 'out of office start end first last day date schedule' },
  { page: 'vacation', id: 'vacation-message', labelKey: 'sieve.vacation.message', keywords: 'out of office subject message text body reply' },
  { page: 'vacation', id: 'vacation-days', labelKey: 'sieve.vacation.days', keywords: 'out of office how often same sender interval days' },
  { page: 'vacation', id: 'vacation-mailbox', labelKey: 'sieve.connection', keywords: 'managesieve sieve server host port 4190 starttls' },
]

export default entries
