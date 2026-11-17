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
    expect(screen.queryByRole('button', { name: 'Elsewhere' })).toBeNull()
    const button = screen.getByRole('button', { name: 'Send to CRM' })
    expect(button.getAttribute('title')).toBe('Send to CRM')
    await userEvent.setup().click(button)
    expect(run).toHaveBeenCalledWith(3, 'toolbar.message', [{ id: '9' }])
  })
})
