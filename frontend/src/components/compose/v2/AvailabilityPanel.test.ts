import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Calendar, Occurrence } from '../../../lib/types'

const api = vi.hoisted(() => ({
  listOccurrences: vi.fn(),
  listCalendars: vi.fn(),
  listCalendarAccounts: vi.fn(),
  getCalendarSettings: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  ...api,
}))

import AvailabilityPanel from './AvailabilityPanel.svelte'
import { calendars, calendarSettings, calendarSettingsLoaded, defaultCalendarSettings } from '../../../stores/calendar'
import { prefs } from '../../../stores/prefs'
import { toRFC3339 } from '../../../lib/caldate'

const cal = (id: number, name: string, visible = true): Calendar => ({
  id,
  accountId: 0,
  accountName: '',
  name,
  description: '',
  color: '',
  local: true,
  readOnly: false,
  visible,
  lastSync: '',
  lastError: '',
  default: id === 1,
})

const busy = (calendarId: number, start: Date, end: Date): Occurrence => ({
  eventId: calendarId * 10,
  calendarId,
  calendarColor: '',
  uid: 'u',
  summary: 'Busy',
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
})

// Monday 16 November 2026, 08:00.
const now = (): Date => new Date(2026, 10, 16, 8)

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  calendars.set([cal(1, 'Work'), cal(2, 'Birthdays', false)])
  calendarSettings.set({ ...defaultCalendarSettings, weekStart: 1, workDayStart: 9 * 60, workDayEnd: 17 * 60 })
  calendarSettingsLoaded.set(true)
  prefs.update((p) => ({ ...p, timeFormat: '24', reduceMotion: true }))
  api.listOccurrences.mockImplementation(async (_from: string, _to: string, ids: number[]) =>
    [busy(1, new Date(2026, 10, 16, 10), new Date(2026, 10, 16, 16)), busy(2, new Date(2026, 10, 17, 9), new Date(2026, 10, 17, 17))].filter((o) => ids.includes(o.calendarId)),
  )
})

describe('AvailabilityPanel', () => {
  it('shows the free times of this week as toggles and inserts the ones picked', async () => {
    const user = userEvent.setup()
    const inserted = vi.fn()
    render(AvailabilityPanel, { props: { now }, events: { insert: (e: CustomEvent<string>) => inserted(e.detail) } })
    await screen.findByText('6 of 6 times picked')
    const slots = screen.getAllByRole('button', { name: /\d to \d/ })
    // Monday: 9 to 10 and 16 to 17, then four whole days.
    expect(slots).toHaveLength(6)
    expect(api.listOccurrences).toHaveBeenCalledWith(expect.any(String), expect.any(String), [1])
    expect(screen.getByText('6 of 6 times picked')).toBeInTheDocument()

    await user.click(slots[0])
    expect(slots[0]).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByText('5 of 6 times picked')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Insert' }))
    const text: string = inserted.mock.calls[0][0]
    const lines = text.split('\n')
    expect(lines).toHaveLength(6)
    expect(lines[0]).toMatch(/16.*: 16:00 to 17:00$/)
    expect(lines[1]).toMatch(/17.*: 0?9:00 to 17:00$/)
    expect(lines[5]).toMatch(/^Times are in .+\.$/)
  })

  it('counts the visible calendars as busy, and others when picked', async () => {
    const user = userEvent.setup()
    render(AvailabilityPanel, { props: { now } })
    await screen.findByText('6 of 6 times picked')
    expect(screen.getByRole('button', { name: 'Birthdays' })).toHaveAttribute('aria-pressed', 'false')
    await user.click(screen.getByRole('button', { name: 'Birthdays' }))
    await screen.findByText('5 of 5 times picked')
    expect(api.listOccurrences).toHaveBeenLastCalledWith(expect.any(String), expect.any(String), [1, 2])
    await user.click(screen.getByRole('button', { name: 'Work' }))
    await user.click(screen.getByRole('button', { name: 'Birthdays' }))
    await screen.findByText('5 of 5 times picked')
    expect(screen.getAllByRole('button', { name: /\d to \d/ })).toHaveLength(5)
  })

  it('finds nothing to insert in a fully booked range', async () => {
    api.listOccurrences.mockResolvedValue([busy(1, new Date(2026, 10, 16), new Date(2026, 10, 30))])
    render(AvailabilityPanel, { props: { now } })
    await screen.findByText('No free time in this range.')
    expect(screen.getByRole('button', { name: 'Insert' })).toBeDisabled()
  })
})
