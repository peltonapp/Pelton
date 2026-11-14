import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  discoverConfig: vi.fn(),
  testConnection: vi.fn(),
  addOAuthAccount: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../lib/browser', () => ({
  openURL: vi.fn(),
}))

import AddMailboxWizard from './AddMailboxWizard.svelte'

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.discoverConfig.mockResolvedValue({
    imapHost: 'imap.example.com', imapPort: 993, smtpHost: 'smtp.example.com', smtpPort: 465,
    imapTls: 'ssl', smtpTls: 'ssl', oauth: false, source: 'guess',
  })
  api.testConnection.mockResolvedValue({ untrusted: [] })
  // never resolves: the test stops at the moment sign-in is requested.
  api.addOAuthAccount.mockReturnValue(new Promise(() => {}))
})

// a mailbox only reachable through its own proxy has to be testable, and
// addable, before it exists (#457).
describe('route in the add-mailbox wizard', () => {
  it('tests the connection along the route picked for the new mailbox', async () => {
    render(AddMailboxWizard, { props: { initialProviderId: 'custom', offerImport: false } })

    await userEvent.type(screen.getByLabelText('Email'), 'me@example.com')
    await userEvent.tab()
    await userEvent.type(screen.getByLabelText('Password'), 'mail-pass')
    await userEvent.click(screen.getByRole('button', { name: /connection settings/i }))
    await userEvent.click(screen.getByRole('button', { name: 'Direct' }))
    await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Test connection' })).toBeEnabled())
    await userEvent.click(screen.getByRole('button', { name: 'Test connection' }))

    expect(api.testConnection).toHaveBeenCalledWith(expect.objectContaining({ proxy: expect.objectContaining({ mode: 'off' }) }))
  })

  it('signs in along a custom proxy', async () => {
    render(AddMailboxWizard, { props: { initialProviderId: 'outlook', offerImport: false } })

    await userEvent.type(screen.getByLabelText('Email'), 'me@contoso.example')
    await userEvent.type(screen.getByLabelText('OAuth client id'), 'entra-client-id')
    await userEvent.click(screen.getByRole('button', { name: 'Advanced' }))
    await userEvent.click(screen.getByRole('button', { name: 'Custom proxy' }))
    await userEvent.type(screen.getByLabelText('Proxy host'), 'own.example')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in with Outlook / Microsoft 365' }))

    expect(api.addOAuthAccount).toHaveBeenCalledWith(
      expect.objectContaining({ proxy: expect.objectContaining({ mode: 'manual', host: 'own.example' }) }),
    )
  })
})
