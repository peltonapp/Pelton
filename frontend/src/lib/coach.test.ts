import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  buildSheet,
  emptyCounters,
  filterSheet,
  hintAllowed,
  hintLimit,
  noteAction,
  noteShortcut,
  onCoachAction,
  originWindowMs,
  parseCounters,
  recordInput,
  withLearned,
  withShown,
  type SheetContext,
  type SheetSource,
} from './coach'

describe('telling a click from a key', () => {
  const heard: [string, string][] = []
  let stop: () => void

  beforeEach(() => {
    heard.length = 0
    stop = onCoachAction((action, origin) => heard.push([action, origin]))
  })

  afterEach(() => stop())

  it('reports an action right after a pointer press as done by mouse', () => {
    recordInput('pointer', 1000)
    noteAction('archive', 1100)
    expect(heard).toEqual([['archive', 'pointer']])
  })

  it('reports one click that acts on many messages once', () => {
    recordInput('pointer', 1000)
    noteAction('archive', 1010)
    noteAction('archive', 1020)
    noteAction('archive', 1030)
    expect(heard).toEqual([['archive', 'pointer']])
  })

  it('says nothing for an action after a key press that was not its shortcut', () => {
    recordInput('key', 1000)
    noteAction('archive', 1050)
    expect(heard).toEqual([])
  })

  it('reports the shortcut and not a click when the key fired the action', () => {
    recordInput('key', 1000)
    noteShortcut('archive')
    noteAction('archive', 1050)
    expect(heard).toEqual([['archive', 'shortcut']])
  })

  it('ignores an action long after the last input', () => {
    recordInput('pointer', 1000)
    noteAction('reply', 1000 + originWindowMs + 1)
    expect(heard).toEqual([])
  })
})

describe('hint rules', () => {
  it('shows a bound action until its limit', () => {
    let c = emptyCounters()
    for (let i = 0; i < hintLimit; i++) {
      expect(hintAllowed(c, 'archive', 'e')).toBe(true)
      c = withShown(c, 'archive')
    }
    expect(hintAllowed(c, 'archive', 'e')).toBe(false)
    expect(hintAllowed(c, 'reply', 'r')).toBe(true)
  })

  it('stops once the key has been used', () => {
    const c = withLearned(emptyCounters(), 'archive')
    expect(hintAllowed(c, 'archive', 'e')).toBe(false)
  })

  it('never hints an unbound action', () => {
    expect(hintAllowed(emptyCounters(), 'mark-read', '')).toBe(false)
  })

  it('reads stored counters and survives junk', () => {
    expect(parseCounters('{"shown":{"archive":2},"learned":{"reply":true}}')).toEqual({ shown: { archive: 2 }, learned: { reply: true } })
    expect(parseCounters('nope')).toEqual(emptyCounters())
    expect(parseCounters('{"shown":3}')).toEqual(emptyCounters())
  })
})

describe('the sheet', () => {
  const base: SheetContext = {
    space: 'mail',
    spaces: ['mail', 'contacts'],
    message: false,
    composer: false,
    composerFocused: false,
    vim: false,
    tabs: false,
    spaceActions: [],
  }
  const source: SheetSource = {
    bindings: {
      'command-palette': 'mod+k',
      search: 'mod+f',
      'go-contacts': 'mod+shift+a',
      'go-mail': 'mod+shift+m',
      'shortcut-sheet': 'shift+?',
      reply: 'r',
      archive: 'e',
      'mark-read': '',
      send: 'mod+enter',
      'close-window': 'mod+w',
      compose: 'mod+n',
    },
    labelFor: (action) => `label.${action}`,
    format: (combo) => combo,
  }
  const ids = (ctx: SheetContext): string[] => buildSheet(ctx, source).map((g) => g.id)
  const items = (ctx: SheetContext, id: string): string[] =>
    buildSheet(ctx, source).find((g) => g.id === id)?.items.map((i) => i.id) ?? []

  it('leaves out the message group with no message on screen', () => {
    expect(ids(base)).toEqual(['navigation', 'list', 'space'])
    expect(ids({ ...base, message: true })).toEqual(['navigation', 'message', 'list', 'space'])
  })

  it('lists only bound actions', () => {
    expect(items({ ...base, message: true }, 'message')).toEqual(['reply', 'archive'])
  })

  it('offers the keys to the other spaces only', () => {
    const nav = items(base, 'navigation')
    expect(nav).toContain('go-contacts')
    expect(nav).not.toContain('go-mail')
  })

  it('adds the number key of a space by its place in the navigation', () => {
    const src: SheetSource = { ...source, bindings: { ...source.bindings, 'go-space-1': 'mod+1', 'go-space-2': 'mod+2' } }
    const ctx: SheetContext = { ...base, space: 'calendar', spaces: ['mail', 'contacts', 'calendar'], numbered: ['contacts', 'mail'] }
    const nav = buildSheet(ctx, src).find((g) => g.id === 'navigation')?.items ?? []
    expect(nav.find((i) => i.id === 'go-contacts')?.keys).toEqual(['mod+shift+a', 'mod+1'])
    expect(nav.find((i) => i.id === 'go-mail')?.keys).toEqual(['mod+shift+m', 'mod+2'])
  })

  it('puts the composer first while it has the focus', () => {
    expect(ids({ ...base, composer: true, composerFocused: true })[0]).toBe('composer')
    expect(ids({ ...base, composer: true })).toEqual(['navigation', 'list', 'composer', 'space'])
  })

  it('shows the vim keys only with vim navigation on', () => {
    expect(items(base, 'list')).not.toContain('vim-move')
    expect(items({ ...base, vim: true }, 'list')).toContain('vim-move')
  })

  it('lists what another space answers to under its own label', () => {
    const groups = buildSheet({ ...base, space: 'contacts', spaceActions: [{ action: 'compose', labelKey: 'contacts.new' }] }, source)
    const space = groups.find((g) => g.id === 'space')
    expect(space?.titleKey).toBe('space.contacts')
    expect(space?.items).toEqual([{ id: 'compose', labelKey: 'contacts.new', keys: ['mod+n'] }])
    expect(groups.map((g) => g.id)).not.toContain('list')
  })

  it('filters by label and by key', () => {
    const groups = buildSheet({ ...base, message: true }, source)
    const label = (key: string): string => (key === 'label.archive' ? 'Archive' : key)
    expect(filterSheet(groups, 'archi', label).flatMap((g) => g.items.map((i) => i.id))).toEqual(['archive'])
    expect(filterSheet(groups, 'mod+k', label).flatMap((g) => g.items.map((i) => i.id))).toEqual(['command-palette'])
    expect(filterSheet(groups, '  ', label)).toBe(groups)
  })
})

describe('input tracking', () => {
  it('ignores a bare modifier so holding it never counts as a key press', async () => {
    const { trackInput } = await import('./coach')
    const heard = vi.fn()
    const stopBus = onCoachAction(heard)
    const stop = trackInput(window)
    window.dispatchEvent(new PointerEvent('pointerdown'))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Control' }))
    noteAction('compose')
    expect(heard).toHaveBeenCalledWith('compose', 'pointer')
    stop()
    stopBus()
  })
})
