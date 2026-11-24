import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Discovered } from '../../lib/types'

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
import { prefs } from '../../stores/prefs'

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
    source: 'autoconfig',
    ...over,
  }
}

beforeEach(() => {
  api.discoverConfig.mockReset()
  api.testConnection.mockReset()
  api.discoverConfig.mockResolvedValue(discovered({}))
  // the user allowed lookups before; asking first is covered on its own.
  prefs.update((p) => ({ ...p, autoconfigLookup: 'allow' }))
})

describe('the address step', () => {
  it('opens on a single address field and offers importing', () => {
    render(AddMailboxWizard)

    expect(screen.getByRole('heading', { name: 'Add a mailbox' })).toBeInTheDocument()
    expect(screen.getByLabelText('Email')).toBeInTheDocument()
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Import from another client/ })).toBeInTheDocument()
  })

  it('leaves importing to the host when asked to', () => {
    render(AddMailboxWizard, { props: { offerImport: false } })

    expect(screen.queryByRole('button', { name: /Import from another client/ })).not.toBeInTheDocument()
  })

  it('knows a preset domain without looking it up', async () => {
    render(AddMailboxWizard)

    await userEvent.type(screen.getByLabelText('Email'), 'me@gmail.com')

    expect(screen.getByText('Gmail / Google Workspace')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(screen.getByRole('heading', { name: 'Gmail / Google Workspace' })).toBeInTheDocument()
    expect(screen.getByText('me@gmail.com')).toBeInTheDocument()
    expect(screen.getByLabelText('Password')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Create an app-specific password/ })).toBeInTheDocument()
    expect(screen.queryByLabelText('Email')).not.toBeInTheDocument()
    expect(api.discoverConfig).not.toHaveBeenCalled()
  })

  it('looks up any other domain and carries the servers into the form', async () => {
    api.testConnection.mockResolvedValue({ untrusted: [] })
    render(AddMailboxWizard)

    await userEvent.type(screen.getByLabelText('Email'), 'me@uni.example')
    expect(await screen.findByText('imap.uni.example', {}, { timeout: 2000 })).toBeInTheDocument()
    expect(api.discoverConfig).toHaveBeenCalledWith('me@uni.example')

    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(screen.getByRole('heading', { name: 'Other (IMAP / SMTP)' })).toBeInTheDocument()

    await userEvent.type(screen.getByLabelText('Password'), 'pass')
    await userEvent.click(screen.getByRole('button', { name: 'Test connection' }))

    expect(api.testConnection).toHaveBeenCalledWith(
      expect.objectContaining({ email: 'me@uni.example', imapHost: 'imap.uni.example', smtpHost: 'smtp.uni.example' }),
    )
    expect(await screen.findByText('Connection works.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add mailbox' })).toBeEnabled()
  })

  it('looks up at once when continued before typing has paused', async () => {
    render(AddMailboxWizard)

    await userEvent.type(screen.getByLabelText('Email'), 'me@uni.example{Enter}')

    expect(await screen.findByRole('heading', { name: 'Other (IMAP / SMTP)' })).toBeInTheDocument()
    expect(api.discoverConfig).toHaveBeenCalledTimes(1)
  })

  it('takes a google-hosted domain to the Google form', async () => {
    api.discoverConfig.mockResolvedValue(discovered({ imapHost: 'imap.gmail.com', oauthProvider: 'google', source: 'mx' }))
    render(AddMailboxWizard)

    await userEvent.type(screen.getByLabelText('Email'), 'student@uni.example{Enter}')

    expect(await screen.findByRole('heading', { name: 'Gmail / Google Workspace' })).toBeInTheDocument()
    expect(screen.getByText('student@uni.example')).toBeInTheDocument()
  })

  it('opens the server settings when nothing was found', async () => {
    api.discoverConfig.mockRejectedValue(new Error('nothing found'))
    render(AddMailboxWizard)

    await userEvent.type(screen.getByLabelText('Email'), 'me@nowhere.example{Enter}')

    expect(await screen.findByLabelText('IMAP host')).toHaveValue('')
  })

  it('lets the provider be chosen by hand, keeping the address', async () => {
    render(AddMailboxWizard)

    await userEvent.type(screen.getByLabelText('Email'), 'me@icloud.com')
    await userEvent.click(screen.getByRole('button', { name: 'Choose the provider yourself' }))
    await userEvent.click(screen.getByRole('button', { name: 'Yahoo' }))

    expect(screen.getByRole('heading', { name: 'Yahoo' })).toBeInTheDocument()
    expect(screen.getByText('me@icloud.com')).toBeInTheDocument()
  })

  it('goes back to the address from the form', async () => {
    render(AddMailboxWizard)

    await userEvent.type(screen.getByLabelText('Email'), 'me@gmail.com{Enter}')
    await userEvent.click(screen.getByRole('button', { name: 'Change' }))

    expect(screen.getByLabelText('Email')).toHaveValue('me@gmail.com')
  })
})

describe('a failed connection test', () => {
  it('says what went wrong and how to fix it', async () => {
    api.testConnection.mockRejectedValue(new Error('[AUTHENTICATIONFAILED] Invalid credentials'))
    render(AddMailboxWizard)

    await userEvent.type(screen.getByLabelText('Email'), 'me@gmail.com{Enter}')
    await userEvent.type(screen.getByLabelText('Password'), 'wrong')
    await userEvent.click(screen.getByRole('button', { name: 'Test connection' }))

    expect(await screen.findByText('The server turned the sign-in down.')).toBeInTheDocument()
    expect(screen.getByText('[AUTHENTICATIONFAILED] Invalid credentials')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Edit settings' }))
    expect(screen.getByLabelText('Password')).toHaveValue('wrong')
  })
})
