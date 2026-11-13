import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Discovered } from '../../lib/types'

const api = vi.hoisted(() => ({
  discoverConfig: vi.fn(),
  addOAuthAccount: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  discoverConfig: api.discoverConfig,
  addOAuthAccount: api.addOAuthAccount,
}))

vi.mock('../../lib/browser', () => ({
  openURL: vi.fn(),
}))

import AddMailboxWizard from './AddMailboxWizard.svelte'

function discovered(over: Partial<Discovered>): Discovered {
  return {
    imapHost: 'imap.uni.example',
    imapPort: 993,
    smtpHost: 'smtp.uni.example',
    smtpPort: 465,
    imapTls: 'ssl',
    smtpTls: 'ssl',
    oauth: false,
    oauthProvider: '',
    source: 'guess',
    ...over,
  }
}

function open(providerId: string): void {
  render(AddMailboxWizard, { props: { initialProviderId: providerId, offerImport: false } })
}

beforeEach(() => {
  api.discoverConfig.mockReset()
  api.addOAuthAccount.mockReset()
  // never resolves: the tests stop at the moment sign-in is requested.
  api.addOAuthAccount.mockReturnValue(new Promise(() => {}))
})

describe('google workspace detection (#445)', () => {
  it('offers google sign-in for a google-hosted address typed under Other', async () => {
    api.discoverConfig.mockResolvedValue(
      discovered({ imapHost: 'imap.gmail.com', smtpHost: 'smtp.gmail.com', oauth: true, oauthProvider: 'google', source: 'mx' }),
    )
    open('custom')

    await userEvent.type(screen.getByLabelText('Email'), 'student@uni.example')
    await userEvent.tab()

    await userEvent.click(await screen.findByRole('button', { name: 'Set up with Google sign-in' }))

    expect(screen.getByRole('heading', { name: 'Sign in to Gmail / Google Workspace' })).toBeInTheDocument()
    expect(screen.getByLabelText('Email')).toHaveValue('student@uni.example')
  })

  it('says nothing for an address that is not hosted by google', async () => {
    api.discoverConfig.mockResolvedValue(discovered({}))
    open('custom')

    await userEvent.type(screen.getByLabelText('Email'), 'me@uni.example')
    await userEvent.tab()

    await vi.waitFor(() => expect(api.discoverConfig).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: 'Set up with Google sign-in' })).not.toBeInTheDocument()
  })
})

describe('oauth client secret', () => {
  it('requires the secret for google and sends it with the sign-in', async () => {
    open('gmail')
    await userEvent.click(screen.getByRole('button', { name: /Sign in with Google instead/ }))

    await userEvent.type(screen.getByLabelText('Email'), 'student@uni.example')
    await userEvent.type(screen.getByLabelText('OAuth client id'), 'id.apps.googleusercontent.com')
    const signIn = screen.getByRole('button', { name: 'Sign in with Gmail / Google Workspace' })
    expect(signIn).toBeDisabled()

    await userEvent.type(screen.getByLabelText('OAuth client secret'), 'GOCSPX-secret')
    expect(signIn).toBeEnabled()

    await userEvent.click(signIn)
    expect(api.addOAuthAccount).toHaveBeenCalledWith(
      expect.objectContaining({ provider: 'google', clientId: 'id.apps.googleusercontent.com', clientSecret: 'GOCSPX-secret' }),
    )
  })

  it('shows the google client setup steps', async () => {
    open('gmail')
    await userEvent.click(screen.getByRole('button', { name: /Sign in with Google instead/ }))

    expect(screen.getByText('Create your Google OAuth client')).toBeInTheDocument()
    for (const link of ['Create project', 'Enable Gmail API', 'Open consent screen', 'Create client']) {
      expect(screen.getByRole('button', { name: link })).toBeInTheDocument()
    }
  })

  it('keeps the secret optional for microsoft', async () => {
    open('outlook')

    await userEvent.type(screen.getByLabelText('Email'), 'me@contoso.example')
    await userEvent.type(screen.getByLabelText('OAuth client id'), 'entra-client-id')

    expect(screen.getByRole('button', { name: 'Sign in with Outlook / Microsoft 365' })).toBeEnabled()
    expect(screen.queryByText('Create your Google OAuth client')).not.toBeInTheDocument()
  })
})
