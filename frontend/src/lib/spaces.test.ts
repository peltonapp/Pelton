import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const setSetting = vi.fn(async () => {})
vi.mock('./api', async (importOriginal) => ({ ...(await importOriginal<typeof import('./api')>()), setSetting }))

const { prefs } = await import('../stores/prefs')
const { space } = await import('../stores/space')
const { settingsRequest } = await import('../stores/settingsnav')
const spacesModule = await import('./spaces')
const { orderedSpaces, visibleSpaces, navSpaces, setSpaceHidden, moveSpaceShown, moveSpaceListed, reorderShownSpaces, resetSpaceNav, spaceMenu, navSettingsAnchor } = spacesModule

const keys = (list: { key: string }[]): string[] => list.map((s) => s.key)

beforeEach(() => {
  setSetting.mockClear()
  prefs.update((p) => ({ ...p, spaceOrder: '', hiddenSpaces: '', chatEnabled: true }))
  space.set('mail')
  settingsRequest.set(null)
})

describe('the navigation order', () => {
  it('is the default order with everything shown', () => {
    expect(keys(get(visibleSpaces))).toEqual(['mail', 'calendar', 'tasks', 'projects', 'contacts', 'notes', 'feeds', 'chat', 'automations'])
  })

  it('keeps chat out until it is switched on', () => {
    prefs.update((p) => ({ ...p, chatEnabled: false }))
    expect(keys(get(orderedSpaces))).not.toContain('chat')
    expect(keys(get(visibleSpaces))).not.toContain('chat')
  })

  it('applies the stored order and leaves hidden spaces out', () => {
    prefs.update((p) => ({ ...p, spaceOrder: 'notes,mail,bogus', hiddenSpaces: 'tasks,mail,feeds' }))
    expect(keys(get(visibleSpaces))).toEqual(['notes', 'mail', 'calendar', 'projects', 'contacts', 'chat', 'automations'])
    expect(get(orderedSpaces).filter((s) => s.hidden).map((s) => s.key)).toEqual(['tasks', 'feeds'])
  })
})

describe('a hidden space that is open', () => {
  it('shows in the navigation for now, at its place, marked', () => {
    prefs.update((p) => ({ ...p, hiddenSpaces: 'tasks' }))
    expect(keys(get(navSpaces))).not.toContain('tasks')
    space.set('tasks')
    const nav = get(navSpaces)
    expect(keys(nav).slice(0, 3)).toEqual(['mail', 'calendar', 'tasks'])
    expect(nav.find((s) => s.key === 'tasks')?.transient).toBe(true)
    expect(nav.find((s) => s.key === 'mail')?.transient).toBeUndefined()
  })
})

describe('changing the navigation', () => {
  it('hides and shows a space, but never mail', () => {
    setSpaceHidden('feeds', true)
    expect(get(prefs).hiddenSpaces).toBe('feeds')
    expect(setSetting).toHaveBeenLastCalledWith('hidden_spaces', 'feeds')
    setSpaceHidden('mail', true)
    expect(get(prefs).hiddenSpaces).toBe('feeds')
    setSpaceHidden('feeds', false)
    expect(get(prefs).hiddenSpaces).toBe('')
  })

  it('moves among the shown spaces past hidden ones', () => {
    setSpaceHidden('tasks', true)
    moveSpaceShown('projects', -1)
    expect(keys(get(visibleSpaces)).slice(0, 3)).toEqual(['mail', 'projects', 'calendar'])
    expect(get(prefs).spaceOrder).toBe('mail,projects,tasks,calendar,contacts,notes,feeds,chat,automations')
  })

  it('moves in the settings list without passing a switched-off space', () => {
    prefs.update((p) => ({ ...p, chatEnabled: false }))
    moveSpaceListed('automations', 1)
    expect(get(prefs).spaceOrder).toBe('')
    moveSpaceListed('automations', -1)
    expect(keys(get(orderedSpaces)).slice(-2)).toEqual(['automations', 'feeds'])
  })

  it('takes a dragged order and resets to the default', () => {
    reorderShownSpaces(['chat', 'mail', 'calendar', 'tasks', 'projects', 'contacts', 'notes', 'feeds'])
    expect(keys(get(visibleSpaces))[0]).toBe('chat')
    setSpaceHidden('notes', true)
    resetSpaceNav()
    expect(get(prefs).spaceOrder).toBe('')
    expect(get(prefs).hiddenSpaces).toBe('')
  })
})

describe('spaceMenu', () => {
  const entry = (key: string) => get(navSpaces).find((s) => s.key === key)!

  it('offers moves, hiding and the settings row', () => {
    const labels = spaceMenu(entry('calendar'), 'y').map((e) => (typeof e === 'object' && 'label' in e ? e.label : e))
    expect(labels).toEqual(['Move up', 'Move down', 'Hide from navigation', 'separator', 'Customize navigation…'])
  })

  it('cannot hide mail or move the first entry up', () => {
    const labels = spaceMenu(entry('mail'), 'x').map((e) => (typeof e === 'object' && 'label' in e ? e.label : e))
    expect(labels).toEqual(['Move right', 'separator', 'Customize navigation…'])
  })

  it('offers to show a hidden space that is open, and opens settings on the row', () => {
    setSpaceHidden('feeds', true)
    space.set('feeds')
    const items = spaceMenu(entry('feeds'), 'y')
    const first = items[0]
    expect(typeof first === 'object' && 'label' in first && first.label).toBe('Show in navigation')
    const last = items[items.length - 1]
    if (typeof last === 'object' && 'action' in last) {
      last.action()
    }
    expect(get(settingsRequest)).toBe(navSettingsAnchor)
  })
})
