import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  setSetting: vi.fn(),
  defaultMailClientStatus: vi.fn(),
  appVersion: vi.fn(),
  listUserLocales: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../lib/browser', () => ({
  openURL: vi.fn(),
}))

import Onboarding from './Onboarding.svelte'
import { prefs } from '../../stores/prefs'

beforeEach(() => {
  api.setSetting.mockResolvedValue(undefined)
  api.defaultMailClientStatus.mockResolvedValue({ known: false, isDefault: false })
  api.appVersion.mockResolvedValue('test')
  api.listUserLocales.mockResolvedValue([])
  prefs.update((p) => ({ ...p, design: 'new' }))
})

async function pastLiability(): Promise<void> {
  await userEvent.click(screen.getByRole('button', { name: 'Get started' }))
  await userEvent.click(screen.getByRole('checkbox'))
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
}

describe('onboarding', () => {
  it('will not pass the acknowledgement until it is given, and stores it', async () => {
    render(Onboarding)
    await userEvent.click(screen.getByRole('button', { name: 'Get started' }))

    expect(screen.getByRole('heading', { name: 'Before you start' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Continue' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Skip setup' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('checkbox'))
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(screen.getByRole('heading', { name: 'Choose a design' })).toBeInTheDocument()
    expect(screen.getByText('3 of 8')).toBeInTheDocument()
    expect(api.setSetting).toHaveBeenCalledWith('liability_accepted', expect.stringContaining('"terms"'))
  })

  it('drops the layout and navigation steps for the classic design', async () => {
    render(Onboarding)
    await pastLiability()

    await userEvent.click(screen.getByRole('radio', { name: /Classic/ }))
    expect(screen.getByText('3 of 6')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }))

    expect(screen.getByRole('heading', { name: 'Set the look' })).toBeInTheDocument()
  })

  it('moves on with Enter and back with Escape', async () => {
    render(Onboarding)
    await pastLiability()

    await userEvent.keyboard('{Enter}')
    expect(screen.getByRole('heading', { name: 'Mail layout' })).toBeInTheDocument()

    await userEvent.keyboard('{Escape}')
    expect(screen.getByRole('heading', { name: 'Choose a design' })).toBeInTheDocument()
  })

  it('finishes from Skip setup', async () => {
    const finish = vi.fn()
    render(Onboarding, { events: { finish } })
    await pastLiability()

    await userEvent.click(screen.getByRole('button', { name: 'Skip setup' }))
    expect(finish).toHaveBeenCalled()
  })
})
