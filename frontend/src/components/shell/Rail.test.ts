import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/svelte'
import { get } from 'svelte/store'

const setSetting = vi.fn(async () => {})
vi.mock('../../lib/api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../lib/api')>()), setSetting }))

const { default: Rail } = await import('./Rail.svelte')
const { prefs } = await import('../../stores/prefs')
const { space } = await import('../../stores/space')
const { contextMenu } = await import('../../stores/contextmenu')

const nav = (): HTMLElement => screen.getByRole('navigation', { name: 'Switch area' })
const names = (): string[] =>
  within(nav())
    .getAllByRole('button')
    .filter((b) => b.hasAttribute('data-space'))
    .map((b) => b.getAttribute('data-space') ?? '')

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  prefs.update((p) => ({ ...p, design: 'new', spaceOrder: '', hiddenSpaces: '', chatEnabled: false }))
  space.set('mail')
  contextMenu.set(null)
})

describe('Rail', () => {
  it('lists the spaces in the stored order without the hidden ones', () => {
    prefs.update((p) => ({ ...p, spaceOrder: 'notes,mail', hiddenSpaces: 'tasks' }))
    render(Rail)
    expect(names()).toEqual(['notes', 'mail', 'calendar', 'projects', 'contacts', 'feeds', 'automations'])
  })

  it('shows a hidden space while it is open, marked as not in the navigation', () => {
    prefs.update((p) => ({ ...p, hiddenSpaces: 'tasks' }))
    space.set('tasks')
    render(Rail)
    const entry = screen.getByRole('button', { name: 'Tasks (not in navigation)' })
    expect(entry).toHaveAttribute('aria-current', 'page')
    expect(entry).not.toHaveAttribute('data-reorder-id')
  })

  it('offers hide, move and customize on right-click', async () => {
    render(Rail)
    await fireEvent.contextMenu(screen.getByRole('button', { name: 'Calendar' }))
    const labels = get(contextMenu)?.entries.map((e) => (typeof e === 'object' && 'label' in e ? e.label : e))
    expect(labels).toEqual(['Move up', 'Move down', 'Hide from navigation', 'separator', 'Customize navigation…'])
  })

  it('moves the focused entry with alt and an arrow', async () => {
    render(Rail)
    await fireEvent.keyDown(screen.getByRole('button', { name: 'Calendar' }), { key: 'ArrowUp', altKey: true })
    expect(names().slice(0, 2)).toEqual(['calendar', 'mail'])
    expect(get(prefs).spaceOrder.startsWith('calendar,mail')).toBe(true)
  })
})
