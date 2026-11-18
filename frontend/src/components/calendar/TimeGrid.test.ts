import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import TimeGrid from './TimeGrid.svelte'
import { placeOccurrence, toRFC3339, weekDays } from '../../lib/caldate'
import type { Calendar, Occurrence } from '../../lib/types'
import { calendars } from '../../stores/calendar'

function occ(id: number, summary: string, start: Date, end: Date): Occurrence {
  return {
    eventId: id,
    calendarId: 1,
    calendarColor: '',
    uid: `u${id}`,
    summary,
    location: '',
    start: toRFC3339(start),
    end: toRFC3339(end),
    allDay: false,
    recurring: false,
    recurrenceId: '',
    override: false,
    status: '',
    transparency: '',
    readOnly: false,
    hasReminders: false,
    pending: false,
    conflict: false,
  }
}

const days = weekDays(new Date(2026, 10, 14), 1)

// jsdom lays nothing out. Each day column is given a box 100 wide and 1440
// tall, side by side, so a pixel down a column is a minute of its day.
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

function mount(list: Occurrence[], events: Record<string, (e: CustomEvent) => void> = {}) {
  return render(TimeGrid, {
    props: {
      days,
      placed: list.map(placeOccurrence),
      colors: new Map([[1, 'var(--cal-1)']]),
      now: new Date(2026, 10, 14, 12),
      timeFormat: '24' as const,
    },
    events,
  })
}

function column(index: number): HTMLElement {
  return document.querySelectorAll<HTMLElement>('.cols > .col')[index]
}

describe('TimeGrid', () => {
  it('heads a column per day and places events in them', () => {
    mount([occ(1, 'Review', new Date(2026, 10, 11, 10), new Date(2026, 10, 11, 11, 30))])
    expect(document.querySelectorAll('.cols > .col')).toHaveLength(7)
    const review = screen.getByRole('button', { name: /Review/ })
    expect(column(2).contains(review)).toBe(true)
    expect(parseFloat(review.style.top)).toBeCloseTo((600 / 1440) * 100, 3)
    expect(parseFloat(review.style.height)).toBeCloseTo((90 / 1440) * 100, 3)
  })

  it('names an event with its time, calendar and the states its color and style show', () => {
    calendars.set([{ id: 1, name: 'Work' } as Calendar])
    const event = { ...occ(1, 'Review', new Date(2026, 10, 11, 10), new Date(2026, 10, 11, 11, 30)), status: 'TENTATIVE', recurring: true }
    mount([event])
    const name = screen.getByRole('button', { name: /^Review,/ }).getAttribute('aria-label') ?? ''
    expect(name).toContain('10:00')
    expect(name).toContain('Work')
    expect(name).toContain('tentative')
    expect(name).toContain('repeats')
    calendars.set([])
  })

  it('draws a new event by dragging over empty time', async () => {
    const create = vi.fn()
    mount([], { create })
    await fireEvent.pointerDown(column(3), { clientX: 350, clientY: 600, button: 0, pointerType: 'mouse' })
    await fireEvent.pointerMove(window, { clientX: 350, clientY: 650, pointerType: 'mouse' })
    await fireEvent.pointerMove(window, { clientX: 350, clientY: 690, pointerType: 'mouse' })
    await fireEvent.pointerUp(window, { clientX: 350, clientY: 690, pointerType: 'mouse' })
    expect(create).toHaveBeenCalledTimes(1)
    const { start, end, allDay } = create.mock.calls[0][0].detail
    expect(allDay).toBe(false)
    expect(start).toEqual(new Date(2026, 10, 12, 10, 0))
    expect(end).toEqual(new Date(2026, 10, 12, 11, 30))
  })

  it('makes an hour-long event from a plain click', async () => {
    const create = vi.fn()
    mount([], { create })
    await fireEvent.pointerDown(column(0), { clientX: 50, clientY: 845, button: 0, pointerType: 'mouse' })
    await fireEvent.pointerUp(window, { clientX: 50, clientY: 845, pointerType: 'mouse' })
    const { start, end } = create.mock.calls[0][0].detail
    expect(start).toEqual(new Date(2026, 10, 9, 14, 0))
    expect(end).toEqual(new Date(2026, 10, 9, 15, 0))
  })

  it('moves an event to another day and time by dragging it', async () => {
    const change = vi.fn()
    mount([occ(1, 'Lunch', new Date(2026, 10, 10, 12), new Date(2026, 10, 10, 13))], { change })
    const lunch = screen.getByRole('button', { name: /Lunch/ })
    await fireEvent.pointerDown(lunch, { clientX: 150, clientY: 735, button: 0, pointerType: 'mouse' })
    await fireEvent.pointerMove(window, { clientX: 450, clientY: 800, pointerType: 'mouse' })
    await fireEvent.pointerUp(window, { clientX: 450, clientY: 800, pointerType: 'mouse' })
    const { start, end } = change.mock.calls[0][0].detail
    expect(start).toEqual(new Date(2026, 10, 13, 13, 0))
    expect(end).toEqual(new Date(2026, 10, 13, 14, 0))
  })

  it('opens an event on a click without a drag', async () => {
    const open = vi.fn()
    const change = vi.fn()
    mount([occ(1, 'Lunch', new Date(2026, 10, 10, 12), new Date(2026, 10, 10, 13))], { open, change })
    const lunch = screen.getByRole('button', { name: /Lunch/ })
    await fireEvent.pointerDown(lunch, { clientX: 150, clientY: 735, button: 0, pointerType: 'mouse' })
    await fireEvent.pointerUp(window, { clientX: 150, clientY: 735, pointerType: 'mouse' })
    expect(open).toHaveBeenCalledTimes(1)
    expect(change).not.toHaveBeenCalled()
  })
})
