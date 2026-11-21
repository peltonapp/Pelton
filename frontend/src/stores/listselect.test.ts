import { beforeEach, describe, expect, it } from 'vitest'
import { get } from 'svelte/store'

import {
  selectedIds,
  clearSelection,
  anchorAt,
  ensureAnchor,
  selectAll,
  selectRange,
  rangeCursor,
  startVisual,
  endVisual,
  inVisual,
} from './listselect'

const ids = [1, 2, 3, 4, 5, 6, 7]

function selected(): number[] {
  return [...get(selectedIds)].sort((a, b) => a - b)
}

beforeEach(() => {
  clearSelection()
})

describe('visual selection', () => {
  it('starts at the cursor row and follows the cursor both ways', () => {
    startVisual(ids, 3)
    expect(selected()).toEqual([3])
    selectRange(ids, 5)
    expect(selected()).toEqual([3, 4, 5])
    selectRange(ids, 4)
    expect(selected()).toEqual([3, 4])
    selectRange(ids, 2)
    expect(selected()).toEqual([2, 3])
  })

  it('keeps what it picked when it ends', () => {
    startVisual(ids, 2)
    selectRange(ids, 3)
    endVisual()
    expect(inVisual()).toBe(false)
    expect(selected()).toEqual([2, 3])
  })

  it('adds a second range on top of the first, skipping the rows between', () => {
    startVisual(ids, 1)
    selectRange(ids, 2)
    endVisual()
    anchorAt(5)
    startVisual(ids, 5)
    selectRange(ids, 6)
    expect(selected()).toEqual([1, 2, 5, 6])
  })

  it('ends when the selection is cleared', () => {
    startVisual(ids, 4)
    rangeCursor.set(4)
    clearSelection()
    expect(inVisual()).toBe(false)
    expect(get(rangeCursor)).toBeNull()
    expect(selected()).toEqual([])
  })
})

describe('extending from a moved cursor', () => {
  it('measures the range from the row the cursor last moved to', () => {
    anchorAt(2)
    anchorAt(3)
    selectRange(ids, 4)
    selectRange(ids, 5)
    expect(selected()).toEqual([3, 4, 5])
  })

  it('starts at the highlighted row after the selection was cleared', () => {
    anchorAt(3)
    selectRange(ids, 4)
    clearSelection()
    ensureAnchor(4)
    selectRange(ids, 5)
    expect(selected()).toEqual([4, 5])
  })

  it('keeps everything selected when extending after select all', () => {
    selectAll(ids)
    ensureAnchor(2)
    selectRange(ids, 3)
    expect(selected()).toEqual(ids)
  })

  it('leaves an existing anchor where it is', () => {
    anchorAt(2)
    ensureAnchor(5)
    selectRange(ids, 4)
    expect(selected()).toEqual([2, 3, 4])
  })
})
