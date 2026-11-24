import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { get, writable } from 'svelte/store'
import { locationKey, registerLocationProvider, type Location, type NotesLocation } from '../lib/locations'
import { space } from './space'
import {
  backVisits,
  canGoBack,
  canGoForward,
  dropVisit,
  emptyNav,
  forwardVisits,
  goBack,
  goForward,
  goToVisit,
  navHistory,
  pushVisit,
  startNavHistory,
  visitLabel,
  type NavState,
  type Visit,
} from './navhistory'

const visit = (loc: Location, label = ''): Visit => ({ loc, key: locationKey(loc), label })
const note = (path?: string): NotesLocation => ({ space: 'notes', note: path })
const keys = (s: NavState): string[] => s.visits.map((v) => (v.loc.space === 'notes' ? (v.loc.note ?? '-') : v.loc.space))

describe('pushVisit', () => {
  it('adds visits and moves to the newest', () => {
    let s = pushVisit(emptyNav, visit(note('a')))
    s = pushVisit(s, visit(note('b')))
    expect(keys(s)).toEqual(['a', 'b'])
    expect(s.index).toBe(1)
  })

  it('does not repeat the place it is on, but takes a better label', () => {
    let s = pushVisit(emptyNav, visit(note('a')))
    const same = pushVisit(s, visit(note('a')))
    expect(same).toBe(s)
    s = pushVisit(s, visit(note('a'), 'Alpha'))
    expect(s.visits).toHaveLength(1)
    expect(s.visits[0].label).toBe('Alpha')
    expect(pushVisit(s, visit(note('a'), '')).visits[0].label).toBe('Alpha')
  })

  it('keeps at most the cap, dropping the oldest', () => {
    let s = emptyNav
    for (const p of ['a', 'b', 'c', 'd', 'e']) {
      s = pushVisit(s, visit(note(p)), undefined, 3)
    }
    expect(keys(s)).toEqual(['c', 'd', 'e'])
    expect(s.index).toBe(2)
  })

  it('drops the places ahead when moving on from the middle', () => {
    let s = emptyNav
    for (const p of ['a', 'b', 'c']) {
      s = pushVisit(s, visit(note(p)))
    }
    s = { ...s, index: 0 }
    s = pushVisit(s, visit(note('d')))
    expect(keys(s)).toEqual(['a', 'd'])
    expect(s.index).toBe(1)
  })

  it('replaces the current visit when the move only refines it, keeping those ahead', () => {
    const refines = (a: Location, b: Location): boolean => a.space === 'notes' && b.space === 'notes' && (!a.note || !b.note)
    let s = pushVisit(emptyNav, visit(note()))
    s = pushVisit(s, visit({ space: 'mail' }))
    s = { ...s, index: 0 }
    s = pushVisit(s, visit(note('a')), refines)
    expect(keys(s)).toEqual(['a', 'mail'])
    expect(s.index).toBe(0)
    s = pushVisit(s, visit(note('b')), refines)
    expect(keys(s)).toEqual(['a', 'b'])
  })
})

describe('dropVisit', () => {
  it('keeps the current visit current', () => {
    let s = emptyNav
    for (const p of ['a', 'b', 'c']) {
      s = pushVisit(s, visit(note(p)))
    }
    const before = dropVisit(s, 0)
    expect(keys(before)).toEqual(['b', 'c'])
    expect(before.index).toBe(1)
    const after = dropVisit({ ...s, index: 0 }, 2)
    expect(after.index).toBe(0)
    expect(dropVisit(s, 7)).toBe(s)
  })
})

describe('visitLabel', () => {
  it('names the space and what was open', () => {
    const t = (key: string): string => ({ 'space.notes': 'Notes' })[key] ?? key
    expect(visitLabel(visit(note('a'), 'Alpha'), t)).toBe('Notes: Alpha')
    expect(visitLabel(visit(note()), t)).toBe('Notes')
  })
})

describe('following the app', () => {
  // a notes space whose open note lives in a store, where some notes are gone.
  const open = writable<string | null>(null)
  const gone = new Set<string>()
  let failing = false
  registerLocationProvider('notes', {
    watch: [open],
    describe: () => ({ space: 'notes', note: get(open) ?? undefined }),
    label: (loc) => (loc.note ? loc.note.toUpperCase() : ''),
    restore: async (loc) => {
      if (failing) {
        throw new Error('boom')
      }
      if (loc.note && gone.has(loc.note)) {
        return false
      }
      open.set(loc.note ?? null)
      space.set('notes')
      return true
    },
    refines: (a, b) => !a.note || !b.note,
  })

  let stop: () => void
  const settle = async (): Promise<void> => {
    await Promise.resolve()
    await new Promise((r) => setTimeout(r, 0))
  }
  const where = (): string => {
    const s = get(navHistory)
    const v = s.visits[s.index]
    return v.loc.space === 'notes' ? `notes:${v.loc.note ?? ''}` : v.loc.space
  }

  beforeEach(async () => {
    gone.clear()
    failing = false
    open.set(null)
    space.set('mail')
    stop = startNavHistory()
    await settle()
  })

  afterEach(() => stop())

  async function visitNote(path: string): Promise<void> {
    space.set('notes')
    open.set(path)
    await settle()
  }

  it('records the space it starts on, then each place, with labels', async () => {
    expect(get(navHistory).visits.map((v) => v.loc.space)).toEqual(['mail'])
    await visitNote('a')
    await visitNote('b')
    const s = get(navHistory)
    expect(s.visits.map((v) => v.label)).toEqual(['', 'A', 'B'])
    expect(get(canGoBack)).toBe(true)
    expect(get(canGoForward)).toBe(false)
  })

  it('records a space change and a detail opened right after it as one visit', async () => {
    space.set('notes')
    open.set('a')
    await settle()
    expect(get(navHistory).visits).toHaveLength(2)
  })

  it('goes back and forward', async () => {
    await visitNote('a')
    await visitNote('b')
    expect(await goBack()).toBe(true)
    expect(where()).toBe('notes:a')
    expect(get(open)).toBe('a')
    expect(get(forwardVisits).map((x) => x.visit.label)).toEqual(['B'])
    expect(await goBack()).toBe(true)
    expect(where()).toBe('mail')
    expect(get(space)).toBe('mail')
    expect(await goBack()).toBe(false)
    expect(await goForward()).toBe(true)
    expect(await goForward()).toBe(true)
    expect(where()).toBe('notes:b')
    expect(get(navHistory).visits).toHaveLength(3)
  })

  it('drops what is gone and goes on to the nearest place that exists', async () => {
    await visitNote('a')
    await visitNote('b')
    await visitNote('c')
    gone.add('b')
    expect(await goBack()).toBe(true)
    expect(where()).toBe('notes:a')
    expect(get(navHistory).visits.map((v) => v.label)).toEqual(['', 'A', 'C'])
  })

  it('treats a place whose restore throws as gone', async () => {
    await visitNote('a')
    await visitNote('b')
    failing = true
    expect(await goBack()).toBe(true)
    expect(where()).toBe('mail')
    expect(get(navHistory).visits.map((v) => v.label)).toEqual(['', 'B'])
    expect(get(canGoForward)).toBe(true)
  })

  it('stays put when there is nowhere to go', async () => {
    await visitNote('a')
    expect(await goForward()).toBe(false)
    gone.add('a')
    await goBack()
    expect(await goBack()).toBe(false)
    expect(where()).toBe('mail')
  })

  it('jumps to a place from the menu', async () => {
    await visitNote('a')
    await visitNote('b')
    await visitNote('c')
    const target = get(backVisits).find((x) => x.visit.label === 'A')
    expect(await goToVisit(target!.index)).toBe(true)
    expect(where()).toBe('notes:a')
    expect(get(forwardVisits)).toHaveLength(2)
  })

  it('cuts the places ahead when moving on after going back', async () => {
    await visitNote('a')
    await visitNote('b')
    await goBack()
    space.set('mail')
    await settle()
    expect(get(navHistory).visits.map((v) => v.loc.space)).toEqual(['mail', 'notes', 'mail'])
    expect(get(canGoForward)).toBe(false)
  })
})
