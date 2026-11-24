import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import TimeGrid from './TimeGrid.svelte'
import { placeOccurrence, toRFC3339, weekDays } from '../../lib/caldate'
import { blockOccurrence, proposalOccurrence } from './timeblocks'
import { armTaskDrag } from './taskdrag'
import type { Task, TimeBlock } from '../../lib/types'
import { expectAxeClean } from '../../test/axe'

const days = weekDays(new Date(2026, 10, 14), 1)

function block(over: Partial<TimeBlock> = {}): TimeBlock {
  return {
    id: 5,
    title: 'Write the report',
    start: toRFC3339(new Date(2026, 10, 11, 10)),
    end: toRFC3339(new Date(2026, 10, 11, 11)),
    taskId: 3,
    taskUid: 't3',
    messageId: 0,
    accountId: 0,
    folderId: 0,
    hasMail: false,
    masked: false,
    done: false,
    calendarId: 0,
    eventUid: '',
    eventTitle: '',
    linkState: '',
    pending: false,
    ...over,
  }
}

// each day column is 100 wide and 1440 tall: a pixel down is a minute.
let restore: () => void = () => {}
beforeEach(() => {
  const original = Element.prototype.getBoundingClientRect
  Element.prototype.getBoundingClientRect = function (this: Element): DOMRect {
    if (this.classList.contains('col') || this.classList.contains('lane-cell')) {
      const index = Array.from(this.parentElement?.querySelectorAll(`:scope > .${this.classList[0]}`) ?? []).indexOf(this)
      const left = index * 100
      return { x: left, y: 0, left, right: left + 100, top: 0, bottom: 1440, width: 100, height: 1440, toJSON: () => ({}) } as DOMRect
    }
    return original.call(this)
  }
  restore = () => {
    Element.prototype.getBoundingClientRect = original
  }
})

afterEach(() => restore())

function mount(list: ReturnType<typeof blockOccurrence>[], events: Record<string, (e: CustomEvent) => void> = {}) {
  return render(TimeGrid, {
    props: { days, placed: list.map(placeOccurrence), colors: new Map(), now: new Date(2026, 10, 14, 12), timeFormat: '24' as const },
    events,
  })
}

describe('time blocks in the grid', () => {
  it('draws a block apart from events and names it with its time', async () => {
    mount([blockOccurrence(block())])
    const el = screen.getByRole('button', { name: /^Time block: Write the report/ })
    expect(el.classList.contains('block')).toBe(true)
    expect(el.getAttribute('aria-label')).toContain('10:00')
    expect(el.getAttribute('aria-label')).toContain('Alt and an arrow key')
    await expectAxeClean()
  })

  it('shows a block for a mail in a locked vault only as busy', () => {
    mount([blockOccurrence(block({ title: '', masked: true, hasMail: true, taskId: 0, taskUid: '' }))])
    const el = screen.getByRole('button', { name: /^Time block: Busy/ })
    expect(el.textContent).toContain('Busy')
    expect(el.classList.contains('masked')).toBe(true)
  })

  it('marks a block that is out of step with its calendar event', () => {
    mount([blockOccurrence(block({ calendarId: 2, linkState: 'event_gone' }))])
    expect(screen.getByRole('button', { name: /calendar event was deleted/ })).toBeTruthy()
  })

  it('moves, resizes, copies and deletes a block from the keyboard', async () => {
    const change = vi.fn()
    const blockAction = vi.fn()
    mount([blockOccurrence(block())], { change, blockAction })
    const el = screen.getByRole('button', { name: /Write the report/ })

    await fireEvent.keyDown(el, { key: 'ArrowDown', altKey: true })
    expect(change.mock.calls[0][0].detail.start).toEqual(new Date(2026, 10, 11, 10, 15))
    expect(change.mock.calls[0][0].detail.end).toEqual(new Date(2026, 10, 11, 11, 15))

    await fireEvent.keyDown(el, { key: 'ArrowUp', altKey: true, shiftKey: true })
    expect(change.mock.calls[1][0].detail.start).toEqual(new Date(2026, 10, 11, 10))
    expect(change.mock.calls[1][0].detail.end).toEqual(new Date(2026, 10, 11, 10, 45))

    await fireEvent.keyDown(el, { key: 'PageDown', altKey: true })
    expect(change.mock.calls[2][0].detail.start).toEqual(new Date(2026, 10, 12, 10))

    await fireEvent.keyDown(el, { key: 'd', ctrlKey: true })
    await fireEvent.keyDown(el, { key: 'Delete' })
    await fireEvent.keyDown(el, { key: 'F10', shiftKey: true })
    expect(blockAction.mock.calls.map((c) => c[0].detail.action)).toEqual(['duplicate', 'delete', 'menu'])
  })

  it('never shrinks a block below a quarter hour from the keyboard', async () => {
    const change = vi.fn()
    const short = block({ end: toRFC3339(new Date(2026, 10, 11, 10, 15)) })
    mount([blockOccurrence(short)], { change })
    await fireEvent.keyDown(screen.getByRole('button', { name: /Write the report/ }), { key: 'ArrowUp', altKey: true, shiftKey: true })
    expect(change.mock.calls[0][0].detail.end).toEqual(new Date(2026, 10, 11, 10, 15))
  })

  it('draws proposals as proposed and moves them from the keyboard', async () => {
    const change = vi.fn()
    const proposal = proposalOccurrence({
      key: '1',
      taskId: 3,
      taskUid: 't3',
      title: 'Report',
      start: toRFC3339(new Date(2026, 10, 12, 9)),
      end: toRFC3339(new Date(2026, 10, 12, 10)),
      part: 1,
      parts: 2,
    })
    mount([proposal], { change })
    const el = screen.getByRole('button', { name: /^Proposed block: Report/ })
    expect(el.getAttribute('aria-label')).toContain('part 1 of 2')
    expect(el.textContent).toContain('Proposed')
    await fireEvent.keyDown(el, { key: 'ArrowUp', altKey: true })
    expect(change.mock.calls[0][0].detail.start).toEqual(new Date(2026, 10, 12, 8, 45))
  })

  it('takes a task dragged in from the plan pane', async () => {
    const taskDrop = vi.fn()
    mount([], { taskDrop })
    const task = { id: 3, uid: 't3', title: 'Report' } as Task
    const row = document.createElement('div')
    document.body.append(row)
    row.addEventListener('pointerdown', (e) => armTaskDrag(e as PointerEvent, task, 45))
    await fireEvent.pointerDown(row, { clientX: 900, clientY: 10, button: 0, pointerType: 'mouse' })
    await fireEvent.pointerMove(window, { clientX: 250, clientY: 600, pointerType: 'mouse' })
    await fireEvent.pointerMove(window, { clientX: 250, clientY: 610, pointerType: 'mouse' })
    expect(document.querySelector('.ghost.drop')).not.toBeNull()
    await fireEvent.pointerUp(window, { clientX: 250, clientY: 610, pointerType: 'mouse' })
    expect(taskDrop).toHaveBeenCalledTimes(1)
    const { start, end } = taskDrop.mock.calls[0][0].detail
    expect(start).toEqual(new Date(2026, 10, 11, 10, 0))
    expect(end).toEqual(new Date(2026, 10, 11, 10, 45))
    row.remove()
  })
})
