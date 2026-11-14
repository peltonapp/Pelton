// providers.ts holds the wizard's provider presets. each preset seeds the server
// settings and auth method so the user usually only types an address and signs
// in or enters a password. custom uses autodiscovery instead of fixed hosts.

import type { TLSMode } from './types'

export type AuthKind = 'oauth' | 'password'

export interface ProviderPreset {
  id: string
  label: string
  kind: AuthKind
  // oauthProvider is the backend provider key for oauth presets.
  oauthProvider?: 'google' | 'microsoft'
  imapHost?: string
  imapPort?: number
  smtpHost?: string
  smtpPort?: number
  // the connection security each leg needs, stated rather than read back out of
  // the port. outlook and icloud submit over STARTTLS on 587 while the rest use
  // implicit TLS, and that is a fact about the provider, not about the number.
  imapTls?: TLSMode
  smtpTls?: TLSMode
  // labelKey, when set, is the i18n key for a label that needs translating
  // (brand names stay as label).
  labelKey?: string
  // noteKey is the i18n key of the note shown under the form (e.g.
  // app-specific password guidance).
  noteKey?: string
  // appPasswordUrl, when set, replaces the plain note with a warning box that
  // spells out that the provider's regular password will not work and links to
  // where an app-specific password is generated.
  appPasswordUrl?: string
  // custom means "use autodiscovery and let the user edit servers".
  custom?: boolean
  // oauthOptional marks a password-first preset that can still switch to the
  // oauth sign-in form (for users who bring their own client id).
  oauthOptional?: boolean
  // allowClientSecret reveals an optional oauth client-secret field, for
  // providers whose app registration may be a confidential client.
  allowClientSecret?: boolean
  // requireClientSecret shows the client-secret field up front and requires
  // it: Google's token endpoint rejects a Desktop app client that omits it.
  requireClientSecret?: boolean
  // domains are the address domains the provider hosts, so an address typed
  // into the wizard picks its preset without asking.
  domains?: string[]
}

export const providerPresets: ProviderPreset[] = [
  {
    // password-first: an app password works today on consumer accounts with
    // no Google Cloud Console involved. oauth stays reachable behind
    // oauthOptional for users who registered their own client (#56).
    id: 'gmail',
    label: 'Gmail / Google Workspace',
    kind: 'password',
    oauthProvider: 'google',
    imapHost: 'imap.gmail.com',
    imapPort: 993,
    imapTls: 'ssl',
    smtpHost: 'smtp.gmail.com',
    smtpPort: 465,
    smtpTls: 'ssl',
    appPasswordUrl: 'https://myaccount.google.com/apppasswords',
    oauthOptional: true,
    requireClientSecret: true,
    domains: ['gmail.com', 'googlemail.com'],
  },
  {
    id: 'outlook',
    label: 'Outlook / Microsoft 365',
    kind: 'oauth',
    oauthProvider: 'microsoft',
    imapHost: 'outlook.office365.com',
    imapPort: 993,
    imapTls: 'ssl',
    smtpHost: 'smtp.office365.com',
    smtpPort: 587,
    smtpTls: 'starttls',
    allowClientSecret: true,
    domains: [
      'outlook.com', 'outlook.de', 'outlook.fr', 'hotmail.com', 'hotmail.de', 'hotmail.fr', 'hotmail.it', 'hotmail.es',
      'hotmail.co.uk', 'live.com', 'live.de', 'live.fr', 'live.nl', 'live.co.uk', 'msn.com',
    ],
  },
  {
    id: 'icloud',
    label: 'iCloud',
    kind: 'password',
    imapHost: 'imap.mail.me.com',
    imapPort: 993,
    imapTls: 'ssl',
    smtpHost: 'smtp.mail.me.com',
    smtpPort: 587,
    smtpTls: 'starttls',
    appPasswordUrl: 'https://appleid.apple.com',
    domains: ['icloud.com', 'me.com', 'mac.com'],
  },
  {
    id: 'yahoo',
    label: 'Yahoo',
    kind: 'password',
    imapHost: 'imap.mail.yahoo.com',
    imapPort: 993,
    imapTls: 'ssl',
    smtpHost: 'smtp.mail.yahoo.com',
    smtpPort: 465,
    smtpTls: 'ssl',
    noteKey: 'wizard.provider.yahoo.note',
    domains: ['yahoo.com', 'yahoo.de', 'yahoo.fr', 'yahoo.es', 'yahoo.it', 'yahoo.co.uk', 'ymail.com', 'rocketmail.com'],
  },
  {
    id: 'fastmail',
    label: 'Fastmail',
    kind: 'password',
    imapHost: 'imap.fastmail.com',
    imapPort: 993,
    imapTls: 'ssl',
    smtpHost: 'smtp.fastmail.com',
    smtpPort: 465,
    smtpTls: 'ssl',
    noteKey: 'wizard.provider.fastmail.note',
    domains: ['fastmail.com', 'fastmail.fm'],
  },
  {
    id: 'purelymail',
    label: 'Purelymail',
    kind: 'password',
    imapHost: 'imap.purelymail.com',
    imapPort: 993,
    imapTls: 'ssl',
    smtpHost: 'smtp.purelymail.com',
    smtpPort: 465,
    smtpTls: 'ssl',
    noteKey: 'wizard.provider.purelymail.note',
    domains: ['purelymail.com'],
  },
  {
    id: 'custom',
    label: 'Other (IMAP / SMTP)',
    labelKey: 'wizard.provider.custom.label',
    kind: 'password',
    custom: true,
    noteKey: 'wizard.provider.custom.note',
  },
]

/** isEmailAddress reports whether text looks like a whole address: a local part, an @ and a dotted domain. */
export function isEmailAddress(text: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@.]{2,}$/.test(text.trim())
}

/** presetForEmail returns the preset hosting the address's domain, or null when no preset names it. */
export function presetForEmail(email: string): ProviderPreset | null {
  const at = email.trim().lastIndexOf('@')
  if (at < 0) {
    return null
  }
  const domain = email.trim().slice(at + 1).toLowerCase()
  return providerPresets.find((p) => p.domains?.includes(domain)) ?? null
}

/** presetById returns the preset with that id, or null. */
export function presetById(id: string): ProviderPreset | null {
  return providerPresets.find((p) => p.id === id) ?? null
}
