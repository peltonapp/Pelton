import { describe, expect, it } from 'vitest'
import {
  applyShownOrder,
  canHide,
  hiddenSpaces,
  moveInList,
  moveShown,
  orderSpaces,
  parseKeys,
  serializeHidden,
  serializeOrder,
} from './spaceorder'
import type { Space } from './types'

const all: Space[] = ['mail', 'calendar', 'tasks', 'projects', 'contacts', 'notes', 'feeds', 'chat']

describe('parseKeys', () => {
  it('trims and drops empty parts', () => {
    expect(parseKeys(' mail, ,notes,')).toEqual(['mail', 'notes'])
    expect(parseKeys('')).toEqual([])
  })
})

describe('orderSpaces', () => {
  it('is the default order when nothing is stored', () => {
    expect(orderSpaces(all, '')).toEqual(all)
  })

  it('applies the stored order and appends spaces it does not name', () => {
    expect(orderSpaces(all, 'notes,mail')).toEqual(['notes', 'mail', 'calendar', 'tasks', 'projects', 'contacts', 'feeds', 'chat'])
  })

  it('ignores unknown and repeated keys', () => {
    expect(orderSpaces(all, 'wiki,feeds,feeds,mail')).toEqual(['feeds', 'mail', 'calendar', 'tasks', 'projects', 'contacts', 'notes', 'chat'])
  })

  it('puts a space added in a later version at the end', () => {
    const older: Space[] = ['mail', 'calendar']
    const stored = serializeOrder(older, ['calendar', 'mail'])
    expect(orderSpaces([...older, 'notes'], stored)).toEqual(['calendar', 'mail', 'notes'])
  })
})

describe('hiddenSpaces', () => {
  it('never hides mail and ignores unknown keys', () => {
    expect([...hiddenSpaces(all, 'mail,feeds,bogus,chat')]).toEqual(['feeds', 'chat'])
    expect(canHide('mail')).toBe(false)
    expect(canHide('feeds')).toBe(true)
  })
})

describe('serializing', () => {
  it('stores the default order as empty', () => {
    expect(serializeOrder(all, all)).toBe('')
    expect(serializeOrder(all, moveInList(all, 'notes', -1))).toBe('mail,calendar,tasks,projects,notes,contacts,feeds,chat')
  })

  it('stores hidden spaces in display order without mail', () => {
    expect(serializeHidden(all, new Set<Space>(['chat', 'mail', 'tasks']))).toBe('tasks,chat')
  })
})

describe('moving', () => {
  it('moves within the list and stays put at the ends', () => {
    expect(moveInList(all, 'calendar', -1).slice(0, 2)).toEqual(['calendar', 'mail'])
    expect(moveInList(all, 'mail', -1)).toEqual(all)
    expect(moveInList(all, 'chat', 1)).toEqual(all)
    expect(moveInList(all, 'chat', 0)).toEqual(all)
  })

  it('keeps hidden spaces in their slots when the shown ones are reordered', () => {
    const hidden = new Set<Space>(['tasks', 'contacts'])
    const shown: Space[] = ['notes', 'mail', 'calendar', 'projects', 'feeds', 'chat']
    expect(applyShownOrder(all, hidden, shown)).toEqual(['notes', 'mail', 'tasks', 'calendar', 'contacts', 'projects', 'feeds', 'chat'])
  })

  it('fills slots with shown spaces a partial order leaves out', () => {
    expect(applyShownOrder(['mail', 'calendar', 'tasks'], new Set(), ['tasks'])).toEqual(['tasks', 'mail', 'calendar'])
  })

  it('moves past the neighbour on the rail, skipping hidden spaces', () => {
    const hidden = new Set<Space>(['tasks', 'projects'])
    expect(moveShown(all, hidden, 'contacts', -1)).toEqual(['mail', 'contacts', 'tasks', 'projects', 'calendar', 'notes', 'feeds', 'chat'])
    expect(moveShown(all, hidden, 'mail', -1)).toEqual(all)
  })
})
