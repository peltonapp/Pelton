import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import MonthView from './MonthView.svelte'
import { monthGrid, placeOccurrence } from '../../lib/caldate'
import { formatLongDay } from '../../lib/calformat'
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

const focus = new Date(2026, 10, 14)

// the day numbers, named by the long date in the system locale.
function dayButtons(): HTMLElement[] {
  const names = monthGrid(focus, 1).flat().map((d) => formatLongDay(d))
  return screen.getAllByRole('button').filter((b) => names.includes(b.getAttribute('aria-label') ?? ''))
}

function mount(list: Occurrence[], events: Record<string, (e: CustomEvent) => void> = {}) {
  return render(MonthView, {
    props: {
      weeks: monthGrid(focus, 1),
      month: focus.getMonth(),
      placed: list.map(placeOccurrence),
      colors: new Map([[1, 'var(--cal-1)']]),
      now: focus,
      weekStart: 1,
      timeFormat: '24' as const,
      focus,
      selectedKey: '',
    },
    events,
  })
}

describe('MonthView', () => {
  it('draws every day of the weeks covering the month', () => {
    mount([])
    // November 2026 starts on a Sunday, so a Monday week shows six rows.
    const numbers = dayButtons()
    expect(numbers).toHaveLength(42)
    expect(numbers[0].textContent?.trim()).toBe('26')
    expect(numbers[41].textContent?.trim()).toBe('6')
    expect(numbers[0].getAttribute('aria-label')).toBe(formatLongDay(new Date(2026, 9, 26)))
  })

  it('shows an event and opens it on click', async () => {
    const open = vi.fn()
    mount([occ(1, 'Standup', '2026-11-12T09:00:00+01:00', '2026-11-12T09:15:00+01:00')], { open })
    await userEvent.click(screen.getByRole('button', { name: /Standup/ }))
    expect(open).toHaveBeenCalledTimes(1)
    expect(open.mock.calls[0][0].detail.occ.summary).toBe('Standup')
  })

  it('folds what does not fit into a more link', () => {
    // jsdom lays nothing out, so a row fits one line: a day with two events
    // shows neither and offers both behind the link.
    mount([
      occ(1, 'Alpha', '2026-11-18T09:00:00+01:00', '2026-11-18T10:00:00+01:00'),
      occ(2, 'Beta', '2026-11-18T11:00:00+01:00', '2026-11-18T12:00:00+01:00'),
      occ(3, 'Solo', '2026-11-26', '2026-11-27', true),
    ])
    expect(screen.getByRole('button', { name: '2 more' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Alpha/ })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Solo/ })).toBeInTheDocument()
  })

  it('opens a day on its number and starts an event on a double click', async () => {
    const day = vi.fn()
    const create = vi.fn()
    mount([], { day, create })
    const number = screen.getByRole('button', { name: formatLongDay(new Date(2026, 10, 18)) })
    await userEvent.click(number)
    expect(day.mock.calls[0][0].detail.getDate()).toBe(18)
    await userEvent.dblClick(number.parentElement as HTMLElement)
    expect(create).toHaveBeenCalled()
    expect(create.mock.calls[0][0].detail.day.getDate()).toBe(18)
  })
})
