import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const setSetting = vi.fn(async () => {})
vi.mock('../../lib/api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../../lib/api')>()), setSetting }))

const { default: SpaceNavEditor } = await import('./SpaceNavEditor.svelte')
const { prefs } = await import('../../stores/prefs')

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  prefs.update((p) => ({ ...p, spaceOrder: '', hiddenSpaces: '', chatEnabled: false }))
})

describe('SpaceNavEditor', () => {
  it('switches a space out of the navigation and back', async () => {
    const user = userEvent.setup()
    render(SpaceNavEditor)
    await user.click(screen.getByRole('switch', { name: 'Show Feeds in the navigation' }))
    expect(get(prefs).hiddenSpaces).toBe('feeds')
    expect(screen.getByText('Hidden')).toBeInTheDocument()
    await user.click(screen.getByRole('switch', { name: 'Show Feeds in the navigation' }))
    expect(get(prefs).hiddenSpaces).toBe('')
  })

  it('never lets mail be hidden', () => {
    render(SpaceNavEditor)
    expect(screen.getByRole('switch', { name: 'Show Mail in the navigation' })).toBeDisabled()
    expect(screen.getByText('Always shown')).toBeInTheDocument()
  })

  it('moves with the buttons and with alt and an arrow on the grip', async () => {
    const user = userEvent.setup()
    render(SpaceNavEditor)
    expect(screen.getByRole('button', { name: 'Move Mail up' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Move Mail down' }))
    expect(get(prefs).spaceOrder.startsWith('calendar,mail')).toBe(true)
    screen.getByRole('button', { name: 'Reorder Mail' }).focus()
    await user.keyboard('{Alt>}{ArrowUp}{/Alt}')
    expect(get(prefs).spaceOrder).toBe('')
    expect(screen.getByRole('button', { name: 'Reorder Mail' })).toHaveFocus()
  })
})
