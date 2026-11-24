import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const run = vi.hoisted(() => vi.fn())

vi.mock('../stores/scripts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../stores/scripts')>()),
  runPlaced: run,
}))

import { placedScripts } from '../stores/scripts'
import { closeContextMenu, contextMenu, openContextMenu, type MenuGroup } from '../stores/contextmenu'
import { messageItems, placed, runAt } from './scriptplacements'
import { openScriptMenu, placedLabel } from './scriptmenu'
import { placedRowActions, placedSwipeAction, rowActionView, scriptRowAction, scriptRowActionId, parseRowActions, isRowAction } from './rowactions'
import { runRowAction } from './rowactionrun'
import type { PlacedScript } from './scripting'
import type { MessageSummary } from './types'

function script(scriptId: number, name: string, options: Record<string, unknown> = {}): PlacedScript {
  return { scriptId, name, description: '', options }
}

beforeEach(() => {
  run.mockReset().mockResolvedValue(null)
  placedScripts.set({})
  closeContextMenu()
})

describe('context menu sections', () => {
  it('appends the placed scripts after a separator, in their own Scripts group', () => {
    placedScripts.set({ 'mail.row': [script(4, 'Tag invoices')] })
    openContextMenu(1, 2, [{ label: 'Open', action: () => {} }], { place: 'mail.row', items: [{ id: '7' }] })
    const entries = get(contextMenu)!.entries
    expect(entries[1]).toBe('separator')
    expect(entries[2]).toMatchObject({ kind: 'group', label: 'Scripts', items: [{ label: 'Tag invoices', script: 'Tag invoices' }] })
  })

  it('runs the script with the place and the items', () => {
    placedScripts.set({ 'tasks.task': [script(4, 'Plan')] })
    openContextMenu(0, 0, [], { place: 'tasks.task', items: [{ id: '9' }] })
    const group = get(contextMenu)!.entries[0] as MenuGroup
    expect(group.items[0]).toMatchObject({ label: 'Plan' })
    group.items[0].action()
    expect(run).toHaveBeenCalledWith(4, 'tasks.task', [{ id: '9' }])
  })

  it('takes lookalikes of the script mark out of a script name', () => {
    placedScripts.set({ 'mail.row': [script(4, '⚡ Tag ☇ invoices')] })
    openContextMenu(0, 0, [], { place: 'mail.row', items: [{ id: '7' }] })
    expect((get(contextMenu)!.entries[0] as MenuGroup).items[0].label).toBe('Tag invoices')
  })

  it('opens nothing without entries and without scripts', () => {
    openContextMenu(0, 0, [], { place: 'contacts.contact', items: [{ id: '1' }] })
    expect(get(contextMenu)).toBeNull()
  })

  it('offers only the scripts placed at the place', () => {
    placedScripts.set({ 'mail.row': [script(4, 'Row only')] })
    openContextMenu(0, 0, [], { place: 'notes.note', items: [{ id: 'a.md' }] })
    expect(get(contextMenu)).toBeNull()
  })
})

describe('openScriptMenu', () => {
  const event = () => new MouseEvent('contextmenu', { clientX: 5, clientY: 6, cancelable: true })

  it('leaves the event alone when no script is placed', () => {
    const e = event()
    expect(openScriptMenu(e, 'calendar.event', { id: '3' })).toBe(false)
    expect(e.defaultPrevented).toBe(false)
    expect(get(contextMenu)).toBeNull()
  })

  it('opens a menu of the placed scripts at the pointer', () => {
    placedScripts.set({ 'calendar.event': [script(8, 'Prep')] })
    const e = event()
    expect(openScriptMenu(e, 'calendar.event', { id: '3' })).toBe(true)
    expect(e.defaultPrevented).toBe(true)
    expect(get(contextMenu)).toMatchObject({ x: 5, y: 6 })
  })
})

describe('placedLabel', () => {
  it('prefers the label option over the name', () => {
    expect(placedLabel(script(1, 'Name', { label: ' Send ' }))).toBe('Send')
    expect(placedLabel(script(1, 'Name', { label: '' }))).toBe('Name')
    expect(placedLabel(script(1, 'Name'))).toBe('Name')
  })

  it('cannot carry a lookalike of the script mark', () => {
    expect(placedLabel(script(1, 'Name', { label: '⚡️ Send' }))).toBe('Send')
    expect(placedLabel(script(1, '\u{1F5F2}Name', { label: '⚡' }))).toBe('Name')
  })
})

describe('placed', () => {
  it('follows the placement', () => {
    const store = placed('toolbar.list')
    expect(get(store)).toEqual([])
    placedScripts.set({ 'toolbar.list': [script(2, 'Go')] })
    expect(get(store)).toHaveLength(1)
  })
})

describe('script row actions', () => {
  const plain = { seen: true, flagged: false, pinned: false, muted: false, inSpam: false }

  it('names a script action by its id', () => {
    expect(scriptRowAction(12)).toBe('script:12')
    expect(scriptRowActionId('script:12')).toBe(12)
    expect(scriptRowActionId('archive')).toBeNull()
    expect(isRowAction('script:3')).toBe(true)
    expect(isRowAction('script:x')).toBe(false)
  })

  it('keeps script ids when reading the stored list', () => {
    expect(parseRowActions('archive,script:5,bogus,script:5')).toEqual(['archive', 'script:5'])
  })

  it('describes a script from the placed ones, with its icon and name', () => {
    const view = rowActionView('script:5', plain, [script(5, 'Summarize', { icon: 'star' })])
    expect(view.label).toBe('Summarize')
    expect(view.script).toBe('Summarize')
    expect(rowActionView('archive', plain).script).toBeUndefined()
    expect(view.icon).toContain('<svg')
    expect(view.danger).toBe(false)
  })

  it('drops scripts that are no longer placed', () => {
    const scripts = [script(5, 'Summarize')]
    expect(placedRowActions(['archive', 'script:5', 'script:6'], scripts)).toEqual(['archive', 'script:5'])
    expect(placedSwipeAction('script:6', scripts)).toBe('none')
    expect(placedSwipeAction('script:5', scripts)).toBe('script:5')
    expect(placedSwipeAction('archive', scripts)).toBe('archive')
  })

  it('runs a hover button at row.hover on the row message', async () => {
    await runRowAction('script:5', { id: 41 } as MessageSummary)
    expect(run).toHaveBeenCalledWith(5, 'row.hover', messageItems([41]))
  })

  it('runs a swipe at row.swipe on the row message', async () => {
    await runRowAction('script:5', { id: 41 } as MessageSummary, 'swipe')
    expect(run).toHaveBeenCalledWith(5, 'row.swipe', [{ id: '41' }])
  })
})

describe('runAt', () => {
  it('hands the script, the place and the items to the store', () => {
    runAt('shortcut', 3, [{ id: '1' }])
    expect(run).toHaveBeenCalledWith(3, 'shortcut', [{ id: '1' }])
  })
})
