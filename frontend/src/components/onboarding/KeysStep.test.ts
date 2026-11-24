import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({ setSetting: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import KeysStep from './KeysStep.svelte'
import { prefs } from '../../stores/prefs'

beforeEach(() => {
  api.setSetting.mockResolvedValue(undefined)
})

describe('keyboard step', () => {
  it('preselects arrow keys on a fresh install and stores nothing until a pick', () => {
    prefs.update((p) => ({ ...p, appVimMode: false }))
    render(KeysStep)
    expect(screen.getByRole('radio', { name: /Arrow keys/ })).toHaveAttribute('aria-checked', 'true')
    expect(api.setSetting).not.toHaveBeenCalled()
  })

  it('keeps a stored vim choice', async () => {
    prefs.update((p) => ({ ...p, appVimMode: true }))
    render(KeysStep)
    expect(screen.getByRole('radio', { name: /Vim style/ })).toHaveAttribute('aria-checked', 'true')
    await userEvent.click(screen.getByRole('radio', { name: /Arrow keys/ }))
    expect(api.setSetting).toHaveBeenCalledWith('app_vim_mode', 'false')
  })
})
