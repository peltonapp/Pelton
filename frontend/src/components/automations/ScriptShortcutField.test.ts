import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  setSetting: vi.fn(),
}))

import ScriptShortcutField from './ScriptShortcutField.svelte'
import { bindings, resetAll } from '../../stores/shortcuts'
import { placedScripts } from '../../stores/scripts'

beforeEach(() => {
  resetAll()
  placedScripts.set({ shortcut: [{ scriptId: 5, name: 'Mine', description: '', options: {} }] })
})

describe('ScriptShortcutField', () => {
  it('shows that no key is set', () => {
    render(ScriptShortcutField, { scriptId: 5 })
    expect(screen.getByText('Not set')).toBeTruthy()
  })

  it('records a key into the bindings store', async () => {
    const user = userEvent.setup()
    render(ScriptShortcutField, { scriptId: 5 })
    await user.click(screen.getByRole('button', { name: 'Change' }))
    await user.keyboard('{Control>}{Shift>}9{/Shift}{/Control}')
    expect(get(bindings)['script:5']).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Clear' })).toBeTruthy()
  })

  it('refuses a key another action uses', async () => {
    const user = userEvent.setup()
    render(ScriptShortcutField, { scriptId: 5 })
    await user.click(screen.getByRole('button', { name: 'Change' }))
    await user.keyboard('r')
    expect(get(bindings)['script:5'] ?? '').toBe('')
    expect(screen.getByText(/Already used by/i)).toBeTruthy()
  })

  it('clears the key', async () => {
    const user = userEvent.setup()
    bindings.update((b) => ({ ...b, 'script:5': 'alt+9' }))
    render(ScriptShortcutField, { scriptId: 5 })
    await user.click(screen.getByRole('button', { name: 'Clear' }))
    expect(get(bindings)['script:5']).toBe('')
  })
})
