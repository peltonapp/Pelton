import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({
  startFocus: vi.fn(),
  endFocus: vi.fn(),
  getFocus: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import FocusButton from './FocusButton.svelte'
import { focus, focusMenuOpen } from '../../stores/focus'

const off = { active: false, until: '', scheduled: false, held: 0 }

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.startFocus.mockImplementation(async (until: Date | null) => ({ ...off, active: true, until: until ? until.toISOString() : '' }))
  api.endFocus.mockResolvedValue(undefined)
  api.getFocus.mockResolvedValue(off)
  focus.set(off)
  focusMenuOpen.set(false)
})

describe('FocusButton', () => {
  it('is a quiet button while focus is off', () => {
    render(FocusButton)
    const button = screen.getByRole('button', { name: 'Focus' })
    expect(button).not.toHaveTextContent(/\S/)
  })

  it('shows the time left while focus is on', () => {
    focus.set({ active: true, until: new Date(Date.now() + 24 * 60_000 - 1000).toISOString(), scheduled: false, held: 0 })
    render(FocusButton)
    const button = screen.getByRole('button', { name: 'Focus is on, 24 min left' })
    expect(button).toHaveTextContent('24 min')
  })

  it('says it is on without an end', () => {
    focus.set({ active: true, until: '', scheduled: false, held: 0 })
    render(FocusButton)
    expect(screen.getByRole('button', { name: 'Focus is on' })).toHaveTextContent('On')
  })

  it('starts focus for a chosen length', async () => {
    render(FocusButton)
    await userEvent.click(screen.getByRole('button', { name: 'Focus' }))
    const menu = screen.getByRole('menu', { name: 'Focus' })
    await userEvent.click(within(menu).getByRole('menuitem', { name: 'For 25 minutes' }))

    expect(api.startFocus).toHaveBeenCalledTimes(1)
    const until = api.startFocus.mock.calls[0][0] as Date
    expect(Math.round((until.getTime() - Date.now()) / 60_000)).toBe(25)
    expect(screen.getByRole('button', { name: 'Focus is on, 25 min left' })).toBeInTheDocument()
  })

  it('starts focus until it is turned off', async () => {
    render(FocusButton)
    await userEvent.click(screen.getByRole('button', { name: 'Focus' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Until I turn it off' }))
    expect(api.startFocus).toHaveBeenCalledWith(null)
  })

  it('ends a running focus and says what arrived so far', async () => {
    focus.set({ active: true, until: '', scheduled: false, held: 3 })
    render(FocusButton)
    await userEvent.click(screen.getByRole('button', { name: 'Focus is on' }))
    const dialog = screen.getByRole('dialog', { name: 'Focus' })
    expect(dialog).toHaveTextContent('On until you turn it off')
    expect(dialog).toHaveTextContent('New messages so far: 3')
    await userEvent.click(within(dialog).getByRole('button', { name: 'End focus' }))
    expect(api.endFocus).toHaveBeenCalledTimes(1)
  })

  it('opens from its shortcut', async () => {
    render(FocusButton)
    focusMenuOpen.set(true)
    expect(await screen.findByRole('dialog', { name: 'Focus' })).toBeInTheDocument()
  })
})
