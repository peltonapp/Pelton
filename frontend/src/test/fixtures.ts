// Shared builders for component tests.
import { expect, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/svelte'
import type userEvent from '@testing-library/user-event'
import type { Account, MessageDetail } from '../lib/types'
import { blankAccountProxy } from '../lib/proxyroute'

// account is a stored IMAP mailbox, id 7, user@example.com.
export function account(overrides: Partial<Account> = {}): Account {
  return {
    id: 7,
    email: 'user@example.com',
    displayName: '',
    localLabel: '',
    useLocalLabel: false,
    username: '',
    imapHost: 'imap.example.com',
    imapPort: 993,
    smtpHost: 'smtp.example.com',
    smtpPort: 465,
    local: false,
    imapTls: 'ssl',
    smtpTls: 'ssl',
    exportOnArchive: false,
    exportDir: '',
    exportSubfolders: 'none',
    exportNameTemplate: '',
    pgpDefault: '',
    passwordPromptDismissed: false,
    trustedCerts: [],
    caSubjects: [],
    proxy: blankAccountProxy(),
    ...overrides,
  }
}

// messageDetail is message 7 in INBOX of account 1: a plain-text stub whose
// body is still being fetched (bodyComplete false), preview text "preview".
export function messageDetail(overrides: Partial<MessageDetail> = {}): MessageDetail {
  return {
    id: 7,
    accountId: 1,
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject: 'Hello',
    fromName: 'Ada',
    fromAddress: 'ada@example.com',
    snippet: '',
    date: '2026-09-21T12:00:00Z',
    seen: true,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    toAddresses: '',
    ccAddresses: '',
    bodyPlain: 'preview',
    bodyQuote: 'preview',
    bodyHtmlSafe: '',
    isHtml: false,
    hasRemoteContent: false,
    remoteAllowed: false,
    remoteHosts: [],
    trackingPixels: [],
    attachments: [],
    phishing: { level: 'none' },
    unsubscribe: null,
    charsetGuess: '',
    pgpState: '',
    bodyComplete: false,
    ...overrides,
  }
}

// fillImapFormAndAdd picks "Other (IMAP / SMTP)" in an open add-mailbox wizard,
// fills in user@example.com, passes the connection test and clicks Add mailbox.
// api holds the mocked calls; discovery is stubbed to find nothing.
export async function fillImapFormAndAdd(
  user: ReturnType<typeof userEvent.setup>,
  api: { testConnection: ReturnType<typeof vi.fn>; discoverConfig: ReturnType<typeof vi.fn> },
): Promise<void> {
  const { testConnection } = api
  api.discoverConfig.mockRejectedValue(new Error('no settings found'))
  await user.click(await screen.findByRole('button', { name: /Other \(IMAP \/ SMTP\)/i }))
  await user.type(screen.getByLabelText(/^Email$/i), 'user@example.com')
  await user.type(screen.getByLabelText(/^Password$/i), 'secret')
  const imap = screen.getByLabelText(/IMAP host/i)
  await user.clear(imap)
  await user.type(imap, 'imap.example.com')
  await user.click(screen.getByRole('button', { name: /Test connection/i }))
  await waitFor(() => expect(testConnection).toHaveBeenCalled())
  await user.click(screen.getByRole('button', { name: /^Add mailbox$/i }))
}
