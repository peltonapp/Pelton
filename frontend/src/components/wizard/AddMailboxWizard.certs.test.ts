import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { UntrustedCert } from '../../lib/types'

const api = vi.hoisted(() => ({
  discoverConfig: vi.fn(),
  testConnection: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../lib/browser', () => ({
  openURL: vi.fn(),
}))

import AddMailboxWizard from './AddMailboxWizard.svelte'

const bridgeCert: UntrustedCert = {
  server: 'imap',
  host: '127.0.0.1',
  port: 1143,
  fingerprint: 'cd'.repeat(32),
  display: Array(32).fill('CD').join(':'),
  subject: 'CN=127.0.0.1',
  issuer: 'CN=127.0.0.1',
  notBefore: '2026-01-01T00:00:00Z',
  notAfter: '2046-01-01T00:00:00Z',
  names: ['127.0.0.1'],
  selfSigned: true,
  reason: 'x509: certificate signed by unknown authority',
}

beforeEach(() => {
  api.discoverConfig.mockReset()
  api.testConnection.mockReset()
  api.discoverConfig.mockResolvedValue({
    imapHost: '127.0.0.1', imapPort: 1143, smtpHost: '127.0.0.1', smtpPort: 1025,
    imapTls: 'starttls', smtpTls: 'starttls', oauth: false, source: 'guess',
  })
})

describe('untrusted certificate in the connection test (#446)', () => {
  it('shows the certificate, and trusting it tests again with it pinned', async () => {
    api.testConnection
      .mockResolvedValueOnce({ untrusted: [bridgeCert, { ...bridgeCert, server: 'smtp', port: 1025 }] })
      .mockResolvedValueOnce({ untrusted: [] })
    render(AddMailboxWizard, { props: { initialProviderId: 'custom', offerImport: false } })

    await userEvent.type(screen.getByLabelText('Email'), 'me@proton.example')
    await userEvent.tab()
    await userEvent.type(screen.getByLabelText('Password'), 'bridge-pass')
    await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Test connection' })).toBeEnabled())
    await userEvent.click(screen.getByRole('button', { name: 'Test connection' }))

    expect(await screen.findByText(bridgeCert.display)).toBeInTheDocument()
    expect(screen.queryByText('Connection works.')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Trust this certificate' }))

    expect(await screen.findByText('Connection works.')).toBeInTheDocument()
    expect(api.testConnection).toHaveBeenLastCalledWith(
      expect.objectContaining({ trustedCerts: [bridgeCert.fingerprint], smtpHost: '127.0.0.1', smtpPort: 1025 }),
    )
    expect(screen.queryByText(bridgeCert.display)).not.toBeInTheDocument()
  })
})
