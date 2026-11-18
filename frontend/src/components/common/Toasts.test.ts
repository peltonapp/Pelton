import { afterEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { get } from 'svelte/store'
import Toasts from './Toasts.svelte'
import { push, pushAction, toasts, updateToast } from '../../stores/toast'

// toasts fly in, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

afterEach(() => {
  vi.useRealTimers()
  toasts.set([])
})

describe('Toasts for assistive tech', () => {
  it('reads a toast once and keeps a ticking countdown quiet', async () => {
    render(Toasts)
    const id = pushAction('info', 'Sending in 5s', { label: 'Undo', run: () => {} }, 0)
    await tick()
    expect(screen.getByRole('status')).toHaveTextContent('Sending in 5s. Undo')
    updateToast(id, 'Sending in 4s')
    await tick()
    expect(screen.getByRole('status')).toHaveTextContent('Sending in 5s. Undo')
  })

  it('raises errors as alerts', async () => {
    render(Toasts)
    push('error', 'Could not connect')
    await tick()
    expect(screen.getByRole('alert')).toHaveTextContent('Could not connect')
  })

  it('holds a toast while the pointer rests on it', async () => {
    vi.useFakeTimers()
    render(Toasts)
    push('info', 'Saved')
    await tick()
    const toast = screen.getByText('Saved', { selector: '.message' }).closest('.toast')!
    await fireEvent.mouseEnter(toast)
    vi.advanceTimersByTime(10000)
    expect(get(toasts)).toHaveLength(1)
    await fireEvent.mouseLeave(toast)
    vi.advanceTimersByTime(3500)
    expect(get(toasts)).toHaveLength(0)
  })
})
