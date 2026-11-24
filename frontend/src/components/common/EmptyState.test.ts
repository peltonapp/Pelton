import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import EmptyState from './EmptyState.svelte'
import { bindings } from '../../stores/shortcuts'
import { prefs } from '../../stores/prefs'
import { shortcutLabel } from '../../lib/i18n'
import { get } from 'svelte/store'

const defaults = get(bindings)

afterEach(() => {
  bindings.set(defaults)
  prefs.update((p) => ({ ...p, showShortcutHints: true }))
})

describe('EmptyState semantics', () => {
  it('announces an empty place politely with its title as a heading', () => {
    render(EmptyState, { props: { title: 'No notes yet', detail: 'Notes you write land here.' } })

    const region = screen.getByRole('status')
    expect(region).toHaveTextContent('No notes yet')
    expect(screen.getByRole('heading', { level: 3, name: 'No notes yet' })).toBeInTheDocument()
    expect(screen.getByText('Notes you write land here.')).toBeInTheDocument()
  })

  it('announces an error assertively', () => {
    render(EmptyState, { props: { title: 'Could not load', kind: 'error' } })

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load')
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })

  it('takes the heading level it is given', () => {
    render(EmptyState, { props: { title: 'Nothing here', level: 2 } })

    expect(screen.getByRole('heading', { level: 2, name: 'Nothing here' })).toBeInTheDocument()
  })

  it('renders a compact title as plain text under its section heading', () => {
    render(EmptyState, { props: { title: 'No backlinks', size: 'compact' } })

    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('No backlinks')
  })

  it('marks which case it shows', () => {
    render(EmptyState, { props: { title: 'No matches', kind: 'filtered' } })

    expect(screen.getByRole('status')).toHaveAttribute('data-kind', 'filtered')
  })
})

describe('EmptyState actions', () => {
  it('runs the primary and the secondary action', async () => {
    const onAction = vi.fn()
    const onSecondary = vi.fn()
    render(EmptyState, {
      props: { title: 'No feeds', actionLabel: 'Add feed', onAction, secondaryLabel: 'Import OPML', onSecondary },
    })

    await userEvent.click(screen.getByRole('button', { name: /Add feed/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Import OPML' }))

    expect(onAction).toHaveBeenCalledOnce()
    expect(onSecondary).toHaveBeenCalledOnce()
  })

  it('places the primary action after the secondary one', () => {
    render(EmptyState, {
      props: { title: 'x', actionLabel: 'Primary', onAction: () => {}, secondaryLabel: 'Secondary', onSecondary: () => {} },
    })

    const names = screen.getAllByRole('button').map((b) => b.textContent?.trim())
    expect(names).toEqual(['Secondary', 'Primary'])
  })

  it('shows no button without a handler', () => {
    render(EmptyState, { props: { title: 'x', actionLabel: 'Orphan' } })

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('offers retry on an error without accent styling', () => {
    render(EmptyState, { props: { title: 'x', kind: 'error', actionLabel: 'Retry', onAction: () => {} } })

    expect(screen.getByRole('button', { name: 'Retry' })).not.toHaveClass('primary')
  })
})

describe('EmptyState shortcut hint', () => {
  it('shows the current binding of the primary action', () => {
    render(EmptyState, { props: { title: 'x', actionLabel: 'New note', onAction: () => {}, shortcut: 'compose' } })

    const combo = get(bindings).compose
    expect(screen.getByRole('button')).toHaveTextContent(shortcutLabel(combo))
  })

  it('follows a rebinding without remounting', async () => {
    render(EmptyState, { props: { title: 'x', actionLabel: 'New note', onAction: () => {}, shortcut: 'notes-new' } })
    expect(screen.getByRole('button').querySelector('kbd')).toBeNull()

    bindings.update((map) => ({ ...map, 'notes-new': 'mod+shift+9' }))
    await Promise.resolve()

    expect(screen.getByRole('button')).toHaveTextContent(shortcutLabel('mod+shift+9'))
  })

  it('hides the hint when shortcut hints are turned off', async () => {
    render(EmptyState, { props: { title: 'x', actionLabel: 'Compose', onAction: () => {}, shortcut: 'compose' } })

    prefs.update((p) => ({ ...p, showShortcutHints: false }))
    await Promise.resolve()

    expect(screen.getByRole('button').querySelector('kbd')).toBeNull()
  })
})
