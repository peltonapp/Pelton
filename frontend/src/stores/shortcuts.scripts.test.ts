import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({ setSetting: vi.fn(), getSetting: vi.fn() }))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  setSetting: api.setSetting,
  getSetting: api.getSetting,
}))

import { activeBindings, bindings, conflictsFor, resetAll, scriptShortcuts, setBinding } from './shortcuts'
import { placedScripts } from './scripts'
import { scriptAction } from '../lib/shortcuts'

beforeEach(() => {
  api.setSetting.mockReset()
  resetAll()
  placedScripts.set({})
})

describe('script shortcut actions', () => {
  it('exists for the scripts placed as shortcuts only, without a default key', () => {
    placedScripts.set({
      shortcut: [{ scriptId: 7, name: 'Archive invoices', description: '', options: {} }],
      'mail.row': [{ scriptId: 8, name: 'Other', description: '', options: {} }],
    })
    expect(get(scriptShortcuts)).toEqual([{ action: 'script:7', combo: '', labelKey: '', label: 'Archive invoices' }])
  })

  it('stops existing when the script is no longer placed, and its key goes dormant', () => {
    placedScripts.set({ shortcut: [{ scriptId: 7, name: 'A', description: '', options: {} }] })
    setBinding(scriptAction(7), 'mod+shift+9')
    expect(get(activeBindings)['script:7']).toBe('mod+shift+9')
    placedScripts.set({})
    expect(get(scriptShortcuts)).toEqual([])
    expect(get(activeBindings)['script:7']).toBeUndefined()
    expect(get(bindings)['script:7']).toBe('mod+shift+9')
  })

  it('persists a script key and drops it once cleared', () => {
    setBinding(scriptAction(7), 'mod+shift+9')
    expect(JSON.parse(api.setSetting.mock.calls.at(-1)![1])).toMatchObject({ 'script:7': 'mod+shift+9' })
    setBinding(scriptAction(7), '')
    expect(JSON.parse(api.setSetting.mock.calls.at(-1)![1])['script:7']).toBeUndefined()
  })

  it('detects a clash with a placed script and ignores a dormant one', () => {
    placedScripts.set({ shortcut: [{ scriptId: 7, name: 'A', description: '', options: {} }] })
    setBinding(scriptAction(7), 'mod+shift+9')
    expect(conflictsFor('compose', 'mod+shift+9')).toBe('script:7')
    placedScripts.set({})
    expect(conflictsFor('compose', 'mod+shift+9')).toBeNull()
  })
})
