import { describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

vi.mock('tabler-nodes-outline', () => ({
  default: {
    'mail-opened': [['path', { d: 'M1 1' }]],
    mail: [['path', { d: 'M2 2' }]],
    folder: [['path', { d: 'M3 3' }]],
    'folder-open': [['path', { d: 'M4 4' }]],
    rocket: [['path', { d: 'M5 5' }]],
  },
}))

import IconPicker from './IconPicker.svelte'

type Pick = { iconName: string; iconNodes?: unknown }

function mount(props: Record<string, unknown> = {}) {
  const select = vi.fn<(e: CustomEvent<Pick>) => void>()
  const clear = vi.fn()
  render(IconPicker, { props: { themeIcons: false, ...props }, events: { select, clear } })
  return { select, clear }
}

describe('the icon picker', () => {
  it('shows a loading state, then the icons', async () => {
    mount()
    expect(screen.getByRole('progressbar', { name: 'Loading icons…' })).toBeTruthy()
    expect(await screen.findAllByRole('option')).toHaveLength(5)
    expect(screen.queryByRole('progressbar')).toBeNull()
  })

  it('searches by name, ignoring separators and case', async () => {
    const user = userEvent.setup()
    mount()
    await screen.findAllByRole('option')
    await user.type(screen.getByRole('combobox', { name: 'Search icons' }), 'Mail Open')
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(1)
    expect(options[0].getAttribute('title')).toBe('mail-opened')
  })

  it('says so when nothing matches', async () => {
    const user = userEvent.setup()
    mount()
    await screen.findAllByRole('option')
    await user.type(screen.getByRole('combobox', { name: 'Search icons' }), 'zzz')
    expect(screen.queryAllByRole('option')).toHaveLength(0)
    expect(screen.getByText('No icons match your search.')).toBeTruthy()
  })

  it('returns the name and geometry of a clicked icon', async () => {
    const user = userEvent.setup()
    const { select } = mount()
    const options = await screen.findAllByRole('option')
    await user.click(options.find((o) => o.getAttribute('title') === 'rocket') as HTMLElement)
    expect(select).toHaveBeenCalledTimes(1)
    expect(select.mock.calls[0][0].detail).toEqual({ iconName: 'rocket', iconNodes: [['path', { d: 'M5 5' }]] })
  })

  it('moves through the results with the arrow keys and picks with Enter', async () => {
    const user = userEvent.setup()
    const { select } = mount()
    await screen.findAllByRole('option')
    const search = screen.getByRole('combobox', { name: 'Search icons' })
    expect(document.activeElement).toBe(search)
    await user.keyboard('{ArrowRight}{ArrowRight}')
    expect(screen.getAllByRole('option')[2].getAttribute('aria-selected')).toBe('true')
    expect(search.getAttribute('aria-activedescendant')).toBe('icon-picker-2')
    await user.keyboard('{ArrowLeft}')
    expect(screen.getAllByRole('option')[1].getAttribute('aria-selected')).toBe('true')
    await user.keyboard('{Enter}')
    expect(select.mock.calls[0][0].detail.iconName).toBe('mail')
  })

  it('picks the first result with Enter straight after searching', async () => {
    const user = userEvent.setup()
    const { select } = mount()
    await screen.findAllByRole('option')
    await user.type(screen.getByRole('combobox', { name: 'Search icons' }), 'folder{Enter}')
    expect(select.mock.calls[0][0].detail.iconName).toBe('folder')
  })

  it('offers the clear choice under the given label', async () => {
    const user = userEvent.setup()
    const { clear } = mount({ clearLabel: 'Default' })
    await user.click(screen.getByRole('button', { name: 'Default' }))
    expect(clear).toHaveBeenCalled()
    await waitFor(() => expect(screen.getAllByRole('option').length).toBeGreaterThan(0))
  })
})
