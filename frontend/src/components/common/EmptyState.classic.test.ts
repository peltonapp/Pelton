import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { createRawSnippet, type ComponentProps } from 'svelte'
import EmptyState from './EmptyState.svelte'
import ErrorState from './ErrorState.svelte'
import { prefs } from '../../stores/prefs'

function useDesign(design: 'new' | 'classic'): void {
  prefs.update((p) => ({ ...p, design }))
}

afterEach(() => useDesign('new'))

const icon = createRawSnippet(() => ({ render: () => '<svg data-testid="slot-icon"></svg>' }))

function renderWithIcon(props: { title: string; detail?: string }) {
  const all = { ...props, children: icon, $$slots: { default: true } }
  return render(EmptyState, { props: all as unknown as ComponentProps<typeof EmptyState> })
}

describe('EmptyState in the classic design', () => {
  it('keeps the plain paragraph title, no live region and no case marker', () => {
    useDesign('classic')
    const { container } = renderWithIcon({ title: 'No messages here', detail: 'This view is empty.' })

    const root = container.querySelector('.empty')
    expect(root).toHaveClass('classic')
    expect(root).not.toHaveAttribute('role')
    expect(root).not.toHaveAttribute('data-kind')
    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
    expect(screen.getByText('No messages here').tagName).toBe('P')
    expect(root?.querySelector('.icon [data-testid="slot-icon"]')).not.toBeNull()
    expect(root?.children[1]).toHaveClass('title')
  })

  it('keeps the icon box even when nothing fills it', () => {
    useDesign('classic')
    const { container } = render(EmptyState, { props: { title: 'Nothing' } })

    const root = container.querySelector('.empty')
    expect(root?.firstElementChild).toHaveClass('icon')
    expect(root?.firstElementChild?.childElementCount).toBe(0)
  })
})

describe('EmptyState in the new design', () => {
  it('uses a heading inside a status region', () => {
    const { container } = renderWithIcon({ title: 'No messages here' })

    const root = container.querySelector('.empty')
    expect(root).not.toHaveClass('classic')
    expect(root).toHaveAttribute('role', 'status')
    expect(root).toHaveAttribute('data-kind', 'empty')
    expect(screen.getByRole('heading', { level: 3, name: 'No messages here' })).toBeInTheDocument()
  })

  it('leaves out the icon box when there is no icon', () => {
    const { container } = render(EmptyState, { props: { title: 'Nothing' } })

    expect(container.querySelector('.empty .icon')).toBeNull()
  })
})

describe('ErrorState', () => {
  it('keeps its own pane and retry button in the classic design', async () => {
    useDesign('classic')
    const onRetry = vi.fn()
    const { container } = render(ErrorState, { props: { message: 'Connection refused', onRetry } })

    const pane = screen.getByRole('alert')
    expect(pane).toHaveClass('error')
    expect(container.querySelector('.empty')).toBeNull()
    expect(screen.queryByRole('heading')).not.toBeInTheDocument()
    expect(screen.getByText('Connection refused')).toHaveClass('detail')
    const retry = screen.getByRole('button')
    expect(retry).toHaveClass('retry')
    expect(retry).not.toHaveClass('cal-btn')

    await userEvent.click(retry)
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it('goes through the shared empty state in the new design', () => {
    const { container } = render(ErrorState, { props: { message: 'Connection refused', onRetry: () => {} } })

    expect(container.querySelector('.empty[data-kind="error"]')).toHaveAttribute('role', 'alert')
    expect(screen.getByRole('heading', { level: 3 })).toBeInTheDocument()
    expect(screen.getByRole('button')).toHaveClass('cal-btn')
  })
})
