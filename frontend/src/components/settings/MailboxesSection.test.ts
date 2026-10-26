import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Account } from '../../lib/types'
import { blankAccountProxy } from '../../lib/proxyroute'

const api = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  accountsNeedingPassword: vi.fn(),
  accountOAuthProvider: vi.fn(),
  reauthorizeOAuthAccount: vi.fn(),
  previewArchiveExportName: vi.fn(),
  discoverConfig: vi.fn(),
  testConnection: vi.fn(),
  addPasswordAccount: vi.fn(),
  listFolders: vi.fn(),
  setFolderSyncExcluded: vi.fn(),
  startAccountSync: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/accounts', () => ({
  refreshSidebar: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../../../wailsjs/runtime/runtime', () => ({
  BrowserOpenURL: vi.fn(),
}))

import '../wizard/AddMailboxWizard.svelte'
import MailboxesSection from './MailboxesSection.svelte'
import { account as wizardAccount, fillImapFormAndAdd } from '../../test/fixtures'

// the editor opens in a modal with a transition, and jsdom has no web
// animations. A finished stub lets svelte run the transition to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

const account: Account = {
  id: 7,
  email: 'student@uni.example',
  displayName: '',
  localLabel: '',
  useLocalLabel: false,
  username: '',
  imapHost: 'imap.gmail.com',
  imapPort: 993,
  smtpHost: 'smtp.gmail.com',
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
}

async function openEditor(): Promise<void> {
  render(MailboxesSection)
  await userEvent.click(await screen.findByRole('button', { name: `Edit ${account.email}` }))
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.listAccounts.mockResolvedValue([account])
  api.accountsNeedingPassword.mockResolvedValue([])
  api.previewArchiveExportName.mockResolvedValue('')
  api.reauthorizeOAuthAccount.mockResolvedValue(undefined)
})

describe('mailbox editor sign-in', () => {
  it('offers signing in again instead of a password for an oauth mailbox', async () => {
    api.accountOAuthProvider.mockResolvedValue('google')
    await openEditor()

    const again = await screen.findByRole('button', { name: 'Sign in again' })
    expect(screen.getByText(/signs in with Google/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument()

    await userEvent.click(again)
    expect(api.reauthorizeOAuthAccount).toHaveBeenCalledWith(account.id)
  })

  it('keeps the password field for a password mailbox', async () => {
    api.accountOAuthProvider.mockResolvedValue('')
    await openEditor()

    await vi.waitFor(() => expect(api.accountOAuthProvider).toHaveBeenCalledWith(account.id))
    expect(screen.getByLabelText('Password')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Sign in again' })).not.toBeInTheDocument()
  })
})

describe('add mailbox from settings', () => {
  // closing on 'added' would skip the folder picker and the done step.
  it('keeps the wizard open through the folder picker and reloads the list', async () => {
    api.listFolders.mockResolvedValue([
      { id: 1, accountId: 7, name: 'INBOX' },
      { id: 2, accountId: 7, name: 'Archive' },
    ])
    api.testConnection.mockResolvedValue({ untrusted: [] })
    api.addPasswordAccount.mockResolvedValue(wizardAccount())
    api.startAccountSync.mockResolvedValue(undefined)
    const user = userEvent.setup()
    render(MailboxesSection)
    await user.click(await screen.findByRole('button', { name: 'Add mailbox…' }))
    await user.click(await screen.findByRole('button', { name: /Add a mailbox/i }))

    await fillImapFormAndAdd(user, api)
    await screen.findByText('Archive')
    await waitFor(() => expect(api.listAccounts).toHaveBeenCalledTimes(2))

    await user.click(screen.getByRole('button', { name: /^Sync everything$/i }))
    await user.click(await screen.findByRole('button', { name: /^Done$/i }))
    expect(api.startAccountSync).toHaveBeenCalledTimes(1)
    await waitFor(() => expect(screen.queryByRole('button', { name: /^Done$/i })).not.toBeInTheDocument())
  })
})
