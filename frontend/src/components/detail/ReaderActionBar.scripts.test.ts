import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const run = vi.hoisted(() => vi.fn())

vi.mock('../../stores/scripts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/scripts')>()),
  runPlaced: run,
}))

import ReaderActionBar from './ReaderActionBar.svelte'
import { placedScripts } from '../../stores/scripts'

// the bar flies in, and jsdom has no web animations. A finished stub lets svelte run it to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

beforeEach(() => {
  run.mockReset().mockResolvedValue(null)
  placedScripts.set({})
})

describe('ReaderActionBar scripts', () => {
  it('shows no script button when none is placed', () => {
    render(ReaderActionBar, { messageId: 9 })
    expect(screen.queryByRole('button', { name: 'Send to CRM' })).toBeNull()
  })

  it('renders a button for each script placed on the message toolbar', async () => {
    placedScripts.set({
      'toolbar.message': [{ scriptId: 3, name: 'CRM', description: '', options: { icon: 'star', label: 'Send to CRM' } }],
      'toolbar.list': [{ scriptId: 4, name: 'Elsewhere', description: '', options: {} }],
    })
    render(ReaderActionBar, { messageId: 9 })
    expect(screen.queryByRole('button', { name: /Elsewhere/ })).toBeNull()
    const button = screen.getByRole('button', { name: 'Send to CRM, Script: CRM' })
    expect(button.getAttribute('title')).toBe('Send to CRM, Script: CRM')
    expect(button.querySelector('[data-script-mark]')).not.toBeNull()
    await userEvent.setup().click(button)
    expect(run).toHaveBeenCalledWith(3, 'toolbar.message', [{ id: '9' }])
  })

  it('keeps the mark when the script tries to draw or hide it in its label', () => {
    placedScripts.set({
      'toolbar.message': [{ scriptId: 3, name: '⚡ Reply', description: '', options: { icon: 'lightning', label: 'Reply ⚡️' } }],
    })
    render(ReaderActionBar, { messageId: 9 })
    const button = screen.getByRole('button', { name: 'Script: Reply' })
    expect(button.querySelectorAll('[data-script-mark]')).toHaveLength(1)
    expect(button.textContent).not.toContain('⚡')
  })

  it('marks only script buttons', () => {
    render(ReaderActionBar, { messageId: 9 })
    expect(document.querySelector('[data-script-mark]')).toBeNull()
  })
})
