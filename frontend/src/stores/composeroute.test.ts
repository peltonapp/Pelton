import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  openComposeWindow: vi.fn(),
  setSetting: vi.fn(),
  getSetting: vi.fn(async () => ({ value: '', found: false })),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import { placementFor, routeComposeSessions } from './composeroute'
import { closeCompose, composeSessions, getSession, openCompose } from './compose'
import { activeComposeId, activeTabId, closeTab, composeTabId, tabs } from './tabs'
import { closeRequest } from './compose'
import { prefs } from './prefs'

let stop: (() => void) | null = null

beforeEach(() => {
  api.openComposeWindow.mockReset()
  api.openComposeWindow.mockResolvedValue(undefined)
  composeSessions.set([])
  tabs.set([])
  activeTabId.set(null)
})

afterEach(() => {
  stop?.()
  stop = null
})

describe('placementFor', () => {
  it('follows the pref when it can', () => {
    expect(placementFor('window', true, true)).toBe('window')
    expect(placementFor('tab', true, true)).toBe('tab')
    expect(placementFor('docked', true, true)).toBe('docked')
  })

  it('falls back to docked without windows or a tab bar', () => {
    expect(placementFor('window', false, true)).toBe('docked')
    expect(placementFor('tab', true, false)).toBe('docked')
  })
})

describe('routeComposeSessions', () => {
  it('moves a new session into a window of its own', async () => {
    prefs.update((p) => ({ ...p, design: 'new', composeOpenIn: 'window' }))
    stop = routeComposeSessions({ tabs: true })
    const id = openCompose(1, 'wysiwyg')

    expect(getSession(id)?.placement).toBe('leaving')
    await vi.waitFor(() => expect(getSession(id)).toBeUndefined())
    expect(api.openComposeWindow).toHaveBeenCalledOnce()
  })

  it('docks a session whose window could not open', async () => {
    api.openComposeWindow.mockRejectedValue(new Error('no window'))
    prefs.update((p) => ({ ...p, design: 'new', composeOpenIn: 'window' }))
    stop = routeComposeSessions({ tabs: true })
    const id = openCompose(1, 'wysiwyg')

    await vi.waitFor(() => expect(getSession(id)?.placement).toBe('docked'))
  })

  it('opens a tab and closes it with the session', () => {
    prefs.update((p) => ({ ...p, design: 'new', composeOpenIn: 'tab' }))
    stop = routeComposeSessions({ tabs: true })
    const id = openCompose(1, 'wysiwyg')

    expect(get(activeComposeId)).toBe(id)
    expect(get(tabs)[0]).toMatchObject({ id: composeTabId(id), compose: id })

    // closing the tab asks the composer, which owns the unsaved text
    closeTab(composeTabId(id))
    expect(get(closeRequest)).toBe(id)
    expect(get(tabs)).toHaveLength(1)

    closeCompose(id)
    expect(get(tabs)).toHaveLength(0)
    expect(get(activeTabId)).toBeNull()
  })

  it('leaves sessions alone in the classic design', () => {
    prefs.update((p) => ({ ...p, design: 'classic', composeOpenIn: 'window' }))
    stop = routeComposeSessions({ tabs: true })
    const id = openCompose(1, 'wysiwyg')

    expect(getSession(id)?.placement).toBeUndefined()
    expect(api.openComposeWindow).not.toHaveBeenCalled()
  })
})
