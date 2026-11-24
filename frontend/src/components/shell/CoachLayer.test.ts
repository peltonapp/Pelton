import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { tick } from 'svelte'

import CoachLayer from './CoachLayer.svelte'
import { coachHint, sheetMode } from '../../stores/coach'
import { prefs } from '../../stores/prefs'

// jsdom is not a Mac, so the modifier is Control.
function key(type: 'keydown' | 'keyup', name: string, init: KeyboardEventInit = {}): KeyboardEvent {
  const event = new KeyboardEvent(type, { key: name, bubbles: true, cancelable: true, ...init })
  window.dispatchEvent(event)
  return event
}

const sheet = (): HTMLElement | null => screen.queryByRole('region', { name: 'Keyboard shortcuts' })

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  prefs.update((p) => ({ ...p, design: 'new', coachHold: true, toastPosition: 'bottom-right' }))
  sheetMode.set('closed')
  coachHint.set(null)
})

afterEach(() => {
  vi.useRealTimers()
  document.documentElement.removeAttribute('data-reduce-motion')
})

describe('holding the modifier', () => {
  beforeEach(() => vi.useFakeTimers())

  it('shows the sheet after a second and hides it on release', async () => {
    render(CoachLayer)
    key('keydown', 'Control', { ctrlKey: true })
    vi.advanceTimersByTime(999)
    await tick()
    expect(sheet()).toBeNull()

    vi.advanceTimersByTime(1)
    await tick()
    const shown = sheet()
    expect(shown).not.toBeNull()
    expect(within(shown!).getByRole('heading', { name: 'Navigation' })).toBeInTheDocument()
    expect(within(shown!).getByText('Command palette')).toBeInTheDocument()

    key('keyup', 'Control')
    await tick()
    expect(sheet()).toBeNull()
  })

  it('gives way to any other key and never swallows it', async () => {
    render(CoachLayer)
    key('keydown', 'Control', { ctrlKey: true })
    const other = key('keydown', 'k', { ctrlKey: true })
    vi.advanceTimersByTime(1500)
    await tick()
    expect(sheet()).toBeNull()
    expect(other.defaultPrevented).toBe(false)

    key('keyup', 'Control')
    key('keydown', 'Control', { ctrlKey: true })
    vi.advanceTimersByTime(1000)
    await tick()
    expect(sheet()).not.toBeNull()
    const pressed = key('keydown', 'n', { ctrlKey: true })
    await tick()
    expect(sheet()).toBeNull()
    expect(pressed.defaultPrevented).toBe(false)
  })

  it('stays away with the setting off', async () => {
    prefs.update((p) => ({ ...p, coachHold: false }))
    render(CoachLayer)
    key('keydown', 'Control', { ctrlKey: true })
    vi.advanceTimersByTime(2000)
    await tick()
    expect(sheet()).toBeNull()
  })

  it('leaves the modifier with Shift alone', async () => {
    render(CoachLayer)
    key('keydown', 'Control', { ctrlKey: true, shiftKey: true })
    vi.advanceTimersByTime(2000)
    await tick()
    expect(sheet()).toBeNull()
  })
})

describe('the pinned sheet', () => {
  it('filters across every shortcut and closes on Escape', async () => {
    render(CoachLayer)
    sheetMode.set('pinned')
    await tick()
    const dialog = await screen.findByRole('dialog', { name: 'Keyboard shortcuts' })
    const filter = within(dialog).getByRole('searchbox', { name: 'Filter shortcuts' })
    await vi.waitFor(() => expect(filter).toHaveFocus())

    await userEvent.type(filter, 'archi')
    expect(within(dialog).getByText('Archive')).toBeInTheDocument()
    expect(within(dialog).queryByText('Command palette')).toBeNull()

    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Keyboard shortcuts' })).toBeNull()
  })
})

describe('the hint', () => {
  it('names the key and takes no clicks', async () => {
    render(CoachLayer)
    coachHint.set({ id: 1, action: 'archive', labelKey: 'shortcut.archive', combo: 'e' })
    await tick()
    const hint = screen.getByRole('status')
    expect(hint).toHaveTextContent('Archive: E')
  })
})
