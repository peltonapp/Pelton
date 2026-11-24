import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import YearView from './YearView.svelte'
import { placeOccurrence } from '../../lib/caldate'
import type { Occurrence } from '../../lib/types'

function occ(id: number, summary: string, start: string, end: string, allDay = false): Occurrence {
  return {
    eventId: id,
    calendarId: 1,
    calendarColor: '#3b6fd4',
    uid: `u${id}`,
    summary,
    location: '',
    start,
    end,
    allDay,
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

// jsdom has no layout: the observer reports the width the test sets.
let boxWidth = 0
Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => boxWidth })
vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    constructor(private run: ResizeObserverCallback) {}
    observe(target: Element): void {
      this.run([{ target } as ResizeObserverEntry], this)
    }
    unobserve(): void {}
    disconnect(): void {}
  },
)

const focus = new Date(2026, 10, 14)

function mount(list: Occurrence[], events: Record<string, (e: CustomEvent) => void> = {}, extra: Record<string, unknown> = {}) {
  return render(YearView, {
    props: {
      year: 2026,
      focus,
      now: focus,
      weekStart: 1,
      placed: list.map(placeOccurrence),
      colors: new Map([[1, 'var(--cal-1)']]),
      timeFormat: '24' as const,
      ...extra,
    },
    events,
  })
}

function day(n: number, month: number): HTMLElement {
  const el = document.querySelector<HTMLElement>(`[data-day="2026-${String(month).padStart(2, '0')}-${String(n).padStart(2, '0')}"]`)
  if (!el) {
    throw new Error('no such day')
  }
  return el
}

describe('YearView', () => {
  it('draws twelve months and every day of the year', () => {
    mount([])
    expect(screen.getAllByRole('region')).toHaveLength(12)
    expect(document.querySelectorAll('[data-day]')).toHaveLength(365)
  })

  it.each([
    [1200, '4'],
    [700, '3'],
    [400, '2'],
  ])('lays the months out in the columns a %i px width allows', (width, cols) => {
    boxWidth = width
    mount([])
    expect(document.querySelector<HTMLElement>('.months')?.style.getPropertyValue('--cols')).toBe(cols)
    boxWidth = 0
  })

  it('leaves the bars out when the days are narrow', () => {
    boxWidth = 300
    mount([occ(1, 'Trip', '2026-06-02', '2026-06-05', true)])
    expect(document.querySelectorAll('.bar')).toHaveLength(0)
    boxWidth = 0
  })

  it('numbers the weeks', () => {
    mount([])
    expect(document.querySelectorAll('.week .wn').length).toBeGreaterThan(50)
  })

  it('shades days by how many events they hold', () => {
    const list = [
      occ(1, 'One', '2026-03-04T09:00:00', '2026-03-04T10:00:00'),
      ...[2, 3, 4, 5, 6, 7].map((i) => occ(i, `Many ${i}`, '2026-03-05T09:00:00', '2026-03-05T10:00:00')),
    ]
    mount(list)
    expect(day(3, 3).className).toContain('lv-0')
    expect(day(4, 3).className).toContain('lv-1')
    expect(day(5, 3).className).toContain('lv-4')
  })

  it('names a day with its event count', () => {
    mount([occ(1, 'One', '2026-03-04T09:00:00', '2026-03-04T10:00:00')])
    expect(day(4, 3).getAttribute('aria-label')).toContain('1 event')
    expect(day(5, 3).getAttribute('aria-label')).toContain('No events')
  })

  it('marks today and the focused day', () => {
    mount([])
    expect(day(14, 11).className).toContain('today')
    expect(day(14, 11).className).toContain('selected')
    expect(day(15, 11).className).not.toContain('selected')
  })

  it('draws a multi-day event as bars over its days', () => {
    mount([occ(1, 'Trip', '2026-06-02', '2026-06-05', true)], {}, { })
    expect(document.querySelectorAll('.bar')).toHaveLength(1)
  })

  it('opens the day on click and the month on its title', async () => {
    const onDay = vi.fn()
    const onMonth = vi.fn()
    mount([], { day: onDay, month: onMonth })
    await userEvent.click(day(9, 5))
    expect(onDay).toHaveBeenCalledTimes(1)
    expect((onDay.mock.calls[0][0] as CustomEvent<Date>).detail.getDate()).toBe(9)
    const march = screen.getAllByRole('region')[2].querySelector('button.title') as HTMLElement
    expect(march.getAttribute('aria-label')).toMatch(/^Open /)
    await userEvent.click(march)
    expect((onMonth.mock.calls[0][0] as CustomEvent<Date>).detail.getMonth()).toBe(2)
  })

  it('lists the events of a day in a popover on focus', async () => {
    mount([occ(1, 'Standup', '2026-03-04T09:30:00', '2026-03-04T10:00:00'), occ(2, 'Offsite', '2026-03-04', '2026-03-05', true)])
    day(4, 3).focus()
    const tip = await screen.findByRole('tooltip')
    expect(tip.textContent).toContain('Standup')
    expect(tip.textContent).toContain('Offsite')
    expect(day(4, 3).getAttribute('aria-describedby')).toBe('year-tip')
    await fireEvent.keyDown(day(4, 3), { key: 'Escape' })
    expect(screen.queryByRole('tooltip')).toBeNull()
  })

  it('moves between days with the arrow keys', async () => {
    mount([])
    day(14, 11).focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(document.activeElement).toBe(day(15, 11))
    await userEvent.keyboard('{ArrowDown}')
    expect(document.activeElement).toBe(day(22, 11))
    await userEvent.keyboard('{ArrowLeft}{ArrowUp}')
    expect(document.activeElement).toBe(day(14, 11))
  })

  it('keeps the arrow keys inside the year', async () => {
    mount([], {}, { focus: new Date(2026, 11, 31) })
    day(31, 12).focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(document.activeElement).toBe(day(31, 12))
  })

  it('has one day in the tab order', () => {
    mount([])
    expect(document.querySelectorAll('[data-day][tabindex="0"]')).toHaveLength(1)
  })
})
