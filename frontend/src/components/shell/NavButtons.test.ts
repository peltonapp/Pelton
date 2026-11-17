import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

import NavButtons from './NavButtons.svelte'
import { space } from '../../stores/space'
import { contextMenu } from '../../stores/contextmenu'
import { startNavHistory } from '../../stores/navhistory'

const settle = (): Promise<void> => new Promise((r) => setTimeout(r, 0))

let stop: () => void

beforeEach(async () => {
  space.set('mail')
  contextMenu.set(null)
  stop = startNavHistory()
  await settle()
})

afterEach(() => stop())

describe('NavButtons', () => {
  it('is disabled with nowhere to go', () => {
    render(NavButtons)
    expect(screen.getByRole('button', { name: 'Back' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Forward' })).toBeDisabled()
  })

  it('steps back and forward', async () => {
    const user = userEvent.setup()
    render(NavButtons)
    space.set('calendar')
    await settle()
    await user.click(screen.getByRole('button', { name: 'Back' }))
    await settle()
    await new Promise((r) => setTimeout(r, 50))
    expect(get(space)).toBe('mail')
    expect(screen.getByRole('button', { name: 'Forward' })).toBeEnabled()
  })

  it('lists the places behind on right-click', async () => {
    render(NavButtons)
    space.set('calendar')
    await settle()
    space.set('notes')
    await settle()
    await fireEvent.contextMenu(screen.getByRole('button', { name: 'Back' }))
    const labels = get(contextMenu)?.entries.map((e) => (typeof e === 'object' && 'label' in e ? e.label : e))
    expect(labels).toEqual(['Calendar', 'Mail'])
  })
})
