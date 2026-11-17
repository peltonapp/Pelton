import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  getSetting: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import FirstTipLayer from './FirstTipLayer.svelte'
import { activeTip, dismissTip, initFirstTips, resetTips, setTipsSuspended } from '../../stores/firsttips'
import { prefs } from '../../stores/prefs'
import { space } from '../../stores/space'
import { mailDrag } from '../../stores/maildrag'

beforeEach(async () => {
  api.getSetting.mockResolvedValue({ value: '', found: false })
  api.setSetting.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, design: 'new', showTips: true }))
  space.set('mail')
  await initFirstTips()
  setTipsSuspended(false)
  resetTips()
})

afterEach(() => {
  dismissTip()
  mailDrag.set(null)
})

describe('first tips', () => {
  it('shows the space keys the first time another space opens, with live keys', async () => {
    render(FirstTipLayer)
    space.set('calendar')
    const tip = await screen.findByRole('status', { name: 'Tip' })
    expect(tip).toHaveTextContent('jump between spaces')
    expect(tip.querySelectorAll('kbd')).toHaveLength(3)
  })

  it('is put away with Escape', async () => {
    render(FirstTipLayer)
    space.set('notes')
    await screen.findByRole('status', { name: 'Tip' })
    await userEvent.keyboard('{Escape}')
    expect(get(activeTip)).toBeNull()
  })

  it('is put away with one click', async () => {
    render(FirstTipLayer)
    mailDrag.set({} as never)
    await screen.findByRole('status', { name: 'Tip' })
    await userEvent.click(screen.getByRole('button', { name: 'Dismiss tip' }))
    expect(get(activeTip)).toBeNull()
  })

  it('shows nothing with tips switched off', async () => {
    prefs.update((p) => ({ ...p, showTips: false }))
    render(FirstTipLayer)
    space.set('calendar')
    await Promise.resolve()
    expect(screen.queryByRole('status', { name: 'Tip' })).toBeNull()
  })
})
