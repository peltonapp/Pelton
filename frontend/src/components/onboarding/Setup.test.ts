import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  getSetting: vi.fn(),
  setSetting: vi.fn(),
  listCalendarAccounts: vi.fn(),
  listAddressBooks: vi.fn(),
  discoverCalendars: vi.fn(),
  discoverAddressBooks: vi.fn(),
  defaultMailClientStatus: vi.fn(),
  listUserLocales: vi.fn(),
  appVersion: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../lib/browser', () => ({
  openURL: vi.fn(),
}))

import Setup from './Setup.svelte'
import { prefs } from '../../stores/prefs'
import { sidebar } from '../../stores/accounts'
import { serializeProgress, startProgress, type SetupProgress } from '../../lib/setupplan'
import type { Account } from '../../lib/types'

let stored: Record<string, string> = {}

function storeProgress(p: Partial<SetupProgress>): void {
  stored.onboarding_progress = serializeProgress({ ...startProgress(false, []), ...p })
}

beforeEach(() => {
  stored = {}
  api.getSetting.mockImplementation(async (key: string) => (key in stored ? { found: true, value: stored[key] } : { found: false, value: '' }))
  api.setSetting.mockImplementation(async (key: string, value: string) => {
    stored[key] = value
  })
  api.listCalendarAccounts.mockResolvedValue([])
  api.listAddressBooks.mockResolvedValue([])
  api.discoverCalendars.mockReset()
  api.discoverAddressBooks.mockReset()
  api.defaultMailClientStatus.mockResolvedValue({ known: false, isDefault: false })
  api.listUserLocales.mockResolvedValue([])
  api.appVersion.mockResolvedValue('test')
  prefs.update((p) => ({ ...p, design: 'new', hiddenSpaces: '', chatEnabled: false, notesVault: '' }))
  sidebar.set({ status: 'idle', data: null, error: null } as never)
})

async function heading(name: string): Promise<HTMLElement> {
  return screen.findByRole('heading', { level: 2, name })
}

describe('setup flow', () => {
  it('opens on the acknowledgement when it is owed, and lets nothing past it unaccepted', async () => {
    storeProgress({ step: 'keys', purpose: 'mail', custom: ['mail'] })
    render(Setup, { props: { legal: true } })
    await heading('Before you start')
    expect(screen.queryByRole('button', { name: 'Skip setup' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Back' })).not.toBeInTheDocument()
    const go = screen.getByRole('button', { name: 'Continue' })
    expect(go).toBeDisabled()
    await userEvent.keyboard('{Enter}')
    expect(screen.getByRole('heading', { level: 2, name: 'Before you start' })).toBeVisible()

    await userEvent.click(screen.getByRole('checkbox'))
    await userEvent.click(go)
    await heading('What will you use Pelton for?')
    expect(api.setSetting).toHaveBeenCalledWith('liability_accepted', expect.stringContaining('"terms"'))
    expect(screen.getByText('2 of 6')).toBeInTheDocument()
  })

  it('leaves the acknowledgement out once it was given', async () => {
    render(Setup, { props: { rerun: true } })
    await heading('What will you use Pelton for?')
    expect(screen.queryByRole('heading', { name: 'Before you start' })).not.toBeInTheDocument()
  })

  it('asks what Pelton is for first, with everything picked', async () => {
    render(Setup)
    await heading('What will you use Pelton for?')
    expect(screen.getByRole('radio', { name: /Everything/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByText('1 of 8')).toBeInTheDocument()
  })

  it('counts only the steps that apply and writes the hidden spaces on Continue', async () => {
    render(Setup)
    await heading('What will you use Pelton for?')
    await userEvent.click(screen.getByRole('radio', { name: /Mail only/ }))
    expect(screen.getByText('1 of 5')).toBeInTheDocument()
    expect(api.setSetting).not.toHaveBeenCalledWith('hidden_spaces', expect.anything())

    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await heading('Choose your language')
    expect(api.setSetting).toHaveBeenCalledWith('hidden_spaces', 'calendar,tasks,projects,contacts,notes,feeds')
    expect(api.setSetting).not.toHaveBeenCalledWith('chat_enabled', expect.anything())
  })

  it('keeps the choice when going back', async () => {
    render(Setup)
    await heading('What will you use Pelton for?')
    await userEvent.click(screen.getByRole('radio', { name: /Mail only/ }))
    await userEvent.click(screen.getByRole('radio', { name: /Let me choose/ }))
    expect(screen.getByRole('checkbox', { name: /Feeds/ })).not.toBeChecked()
    await userEvent.click(screen.getByRole('checkbox', { name: /Feeds/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await heading('Choose your language')
    await userEvent.click(screen.getByRole('button', { name: 'Back' }))
    await heading('What will you use Pelton for?')
    expect(screen.getByRole('radio', { name: /Let me choose/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('checkbox', { name: /Feeds/ })).toBeChecked()
  })

  it('moves on with Enter and leaves Escape to the step', async () => {
    render(Setup)
    await heading('What will you use Pelton for?')
    screen.getByRole('radio', { name: /Everything/ }).focus()
    await userEvent.keyboard('{Enter}')
    await heading('Choose your language')
    await userEvent.keyboard('{Escape}')
    expect(screen.getByRole('heading', { level: 2, name: 'Choose your language' })).toBeVisible()
  })

  it('announces the step', async () => {
    render(Setup)
    await heading('What will you use Pelton for?')
    await waitFor(() => expect(screen.getByText('Step 1 of 8: What will you use Pelton for?')).toBeInTheDocument())
  })

  it('stores where it stands after every move', async () => {
    render(Setup)
    await heading('What will you use Pelton for?')
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await heading('Choose your language')
    expect(JSON.parse(stored.onboarding_progress)).toMatchObject({ step: 'language', outcomes: { purpose: 'done' } })
  })

  it('resumes on the stored step', async () => {
    storeProgress({ step: 'keys', purpose: 'mail', custom: ['mail'] })
    render(Setup)
    await heading('Keyboard')
    expect(screen.getByText('5 of 5')).toBeInTheDocument()
  })

  it('offers each skipped piece again from the summary', async () => {
    storeProgress({ step: 'summary', purpose: 'mail', custom: ['mail'], outcomes: { mailbox: 'skipped' } })
    render(Setup)
    await heading('Pelton is ready')
    expect(screen.getByText('Skipped')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Set up now' }))
    await heading('Set up your mail')
  })

  it('finishes with where to land and forgets the progress', async () => {
    storeProgress({ step: 'summary', purpose: 'custom', custom: ['mail', 'calendar'], outcomes: { purpose: 'done' } })
    const finish = vi.fn()
    render(Setup, { events: { finish } })
    await heading('Pelton is ready')
    await userEvent.click(screen.getByRole('button', { name: 'Open Pelton' }))
    expect(finish.mock.calls[0][0].detail).toEqual({ landing: 'calendar', rerun: false })
    expect(stored.onboarding_progress).toBe('')
  })

  it('skips the whole setup into mail without touching the spaces', async () => {
    const finish = vi.fn()
    render(Setup, { events: { finish } })
    await heading('What will you use Pelton for?')
    await userEvent.click(screen.getByRole('radio', { name: /Mail only/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Skip setup' }))
    expect(finish.mock.calls[0][0].detail).toEqual({ landing: 'mail', rerun: false })
    expect(api.setSetting).not.toHaveBeenCalledWith('hidden_spaces', expect.anything())
  })

  it('treats existing mailboxes as set up on a re-run', async () => {
    sidebar.set({ status: 'ready', data: { accounts: [{ id: 1, email: 'ada@example.org' } as Account] }, error: null } as never)
    storeProgress({ step: 'mailbox', purpose: 'mail', custom: ['mail'], rerun: true })
    render(Setup, { props: { rerun: true } })
    await heading('Set up your mail')
    expect(screen.getByText('ada@example.org')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Continue' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Skip for now' })).not.toBeInTheDocument()
  })

  it('preselects the purpose that matches the spaces shown on a re-run', async () => {
    prefs.update((p) => ({ ...p, hiddenSpaces: 'projects,notes,feeds' }))
    render(Setup, { props: { rerun: true } })
    await heading('What will you use Pelton for?')
    expect(screen.getByRole('radio', { name: /Mail and calendar/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.queryByRole('button', { name: 'Use the Classic design' })).not.toBeInTheDocument()
  })

  it('has no feeds step, while the Feeds space stays chosen', async () => {
    render(Setup)
    await heading('What will you use Pelton for?')
    await userEvent.click(screen.getByRole('radio', { name: /Mail only/ }))
    await userEvent.click(screen.getByRole('radio', { name: /Let me choose/ }))
    await userEvent.click(screen.getByRole('checkbox', { name: /Feeds/ }))
    expect(screen.getByText('1 of 5')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await heading('Choose your language')
    expect(api.setSetting).toHaveBeenCalledWith('hidden_spaces', 'calendar,tasks,projects,contacts,notes')
  })

  it('checks a calendar server before asking it, and contacts nothing on entering the step', async () => {
    storeProgress({ step: 'dav', purpose: 'mailcal' })
    render(Setup)
    await heading('Calendar and contacts')
    expect(api.discoverCalendars).not.toHaveBeenCalled()
    expect(api.discoverAddressBooks).not.toHaveBeenCalled()
    await userEvent.type(screen.getByLabelText('Server'), 'nodot')
    await userEvent.click(screen.getByRole('button', { name: 'Look for calendars and contacts' }))
    expect(screen.getByText('This does not look like a web address.')).toBeInTheDocument()
    expect(api.discoverCalendars).not.toHaveBeenCalled()
  })

  it('keeps going when the calendar server cannot be reached', async () => {
    api.discoverCalendars.mockRejectedValue(new Error('no network'))
    api.discoverAddressBooks.mockRejectedValue(new Error('no network'))
    storeProgress({ step: 'dav', purpose: 'mailcal' })
    render(Setup)
    await heading('Calendar and contacts')
    await userEvent.type(screen.getByLabelText('Server'), 'cloud.example.com')
    await userEvent.click(screen.getByRole('button', { name: 'Look for calendars and contacts' }))
    expect(await screen.findByText(/No calendars found/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Skip for now' }))
    await heading('Keyboard')
  })

  it('leaves chat off until its step turns it on', async () => {
    storeProgress({ step: 'chat', purpose: 'everything' })
    render(Setup)
    await heading('Chat')
    expect(api.setSetting).not.toHaveBeenCalledWith('chat_enabled', 'true')
    await userEvent.click(screen.getByRole('button', { name: 'Turn on chat' }))
    expect(api.setSetting).toHaveBeenCalledWith('chat_enabled', 'true')
    expect(screen.queryByRole('button', { name: 'Skip for now' })).not.toBeInTheDocument()
  })

  it('hands over to the classic onboarding on request', async () => {
    const classic = vi.fn()
    render(Setup, { events: { classic } })
    await heading('What will you use Pelton for?')
    await userEvent.click(screen.getByRole('button', { name: 'Use the Classic design' }))
    expect(classic).toHaveBeenCalled()
    expect(api.setSetting).toHaveBeenCalledWith('design', 'classic')
  })
})
