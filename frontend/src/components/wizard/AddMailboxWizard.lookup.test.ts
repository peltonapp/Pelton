import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  discoverConfig: vi.fn(),
  setSetting: vi.fn(),
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
import { get } from 'svelte/store'

beforeEach(() => {
  api.discoverConfig.mockReset()
  api.discoverConfig.mockResolvedValue({
    imapHost: 'imap.uni.example', imapPort: 993, smtpHost: 'smtp.uni.example', smtpPort: 465,
    imapTls: 'ssl', smtpTls: 'ssl', oauth: false, oauthProvider: '', source: 'autoconfig',
  })
  api.setSetting.mockReset()
  api.setSetting.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, autoconfigLookup: 'ask' }))
})

async function typeAndContinue(address: string): Promise<void> {
  await userEvent.type(screen.getByLabelText('Email'), address)
  await new Promise((r) => setTimeout(r, 700))
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
}

describe('asking before a server lookup', () => {
  it('looks nothing up while typing, then names what would be asked', async () => {
    render(AddMailboxWizard)
    await typeAndContinue('me@uni.example')

    expect(api.discoverConfig).not.toHaveBeenCalled()
    expect(screen.getByText('Look up the server settings for uni.example?')).toBeInTheDocument()
    expect(screen.getByText('autoconfig.thunderbird.net')).toBeInTheDocument()
    expect(screen.getByText('autoconfig.uni.example')).toBeInTheDocument()
    expect(screen.getByText(/Only the domain uni.example is sent/)).toBeInTheDocument()
  })

  it('looks up once allowed, and remembers the answer', async () => {
    render(AddMailboxWizard)
    await typeAndContinue('me@uni.example')
    await userEvent.click(screen.getByRole('button', { name: 'Allow lookups' }))

    expect(api.setSetting).toHaveBeenCalledWith('autoconfig_lookup', 'allow')
    await vi.waitFor(() => expect(api.discoverConfig).toHaveBeenCalledWith('me@uni.example'))
    expect(get(prefs).autoconfigLookup).toBe('allow')
  })

  it('goes to the server fields by hand without any lookup', async () => {
    render(AddMailboxWizard)
    await typeAndContinue('me@uni.example')
    await userEvent.click(screen.getByRole('button', { name: 'Enter settings by hand' }))

    expect(api.setSetting).toHaveBeenCalledWith('autoconfig_lookup', 'manual')
    expect(await screen.findByLabelText('IMAP host')).toBeInTheDocument()
    expect(api.discoverConfig).not.toHaveBeenCalled()
  })

  it('never asks again once the answer is to type by hand', async () => {
    prefs.update((p) => ({ ...p, autoconfigLookup: 'manual' }))
    render(AddMailboxWizard)
    await typeAndContinue('me@uni.example')

    expect(screen.queryByText(/Look up the server settings/)).not.toBeInTheDocument()
    expect(api.discoverConfig).not.toHaveBeenCalled()
  })

  it('needs no question for a built-in provider', async () => {
    render(AddMailboxWizard)
    await typeAndContinue('me@gmail.com')

    expect(screen.queryByText(/Look up the server settings/)).not.toBeInTheDocument()
    expect(api.discoverConfig).not.toHaveBeenCalled()
  })
})
