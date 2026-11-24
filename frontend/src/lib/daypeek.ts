// daypeek.ts reads a day of the calendar around a proposed time: what else is
// on that day, what the time clashes with, what comes right before and after
// it, and which hours a small slice of the day should show. It is pure, so the
// preview a date in a mail opens can be tested without rendering it.

import { addDays, dayMinutes, isLaneEvent, minutesOfDay, startOfDay, type Placed } from './caldate'
import type { Occurrence } from './types'

/** A proposed time, as a date in a mail names it. */
export interface Slot {
  start: Date
  end: Date
  allDay: boolean
}

/** What a day holds around a slot. */
export interface DayPeek {
  /** All-day events and events running a day or more that touch the day. */
  allDay: Placed<Occurrence>[]
  /** The timed events of the day, by start. */
  timed: Placed<Occurrence>[]
  /** The timed events that keep the slot from being free. */
  clashes: Placed<Occurrence>[]
  /** The last busy event ending before the slot, and the first starting after it. */
  before: Placed<Occurrence> | null
  after: Placed<Occurrence> | null
  /** The hours a slice of the day shows, minutes after midnight. */
  from: number
  to: number
}

/** True for an event that takes up time: not marked free, not cancelled, not a task. */
export function isBusy(occ: Pick<Occurrence, 'transparency' | 'status' | 'task'>): boolean {
  return !occ.task && occ.transparency !== 'TRANSPARENT' && occ.status !== 'CANCELLED'
}

function overlaps(a: { start: Date; end: Date }, b: { start: Date; end: Date }): boolean {
  return a.start < b.end && b.start < a.end
}

/**
 * Reads the day a slot falls on. Hours of the slice run from two hours before
 * the slot to two after it, on whole hours inside the day; a slot without a
 * time shows the working day.
 */
export function peekDay(placed: Placed<Occurrence>[], slot: Slot, workStart = 9 * 60, workEnd = 17 * 60): DayPeek {
  const day = startOfDay(slot.start)
  const next = addDays(day, 1)
  const onDay = placed.filter((p) => p.start < next && (p.end > day || p.start.getTime() === day.getTime()))
  const allDay = onDay.filter((p) => isLaneEvent(p)).sort((a, b) => a.start.getTime() - b.start.getTime())
  const timed = onDay.filter((p) => !isLaneEvent(p)).sort((a, b) => a.start.getTime() - b.start.getTime() || a.end.getTime() - b.end.getTime())
  const busy = timed.filter((p) => isBusy(p.occ))
  const clashes = slot.allDay ? [] : busy.filter((p) => overlaps(p, slot))
  let before: Placed<Occurrence> | null = null
  let after: Placed<Occurrence> | null = null
  if (!slot.allDay) {
    for (const p of busy) {
      if (p.end <= slot.start && (!before || p.end > before.end)) {
        before = p
      }
      if (p.start >= slot.end && !after) {
        after = p
      }
    }
  }
  let from: number
  let to: number
  if (slot.allDay) {
    from = workStart
    to = workEnd
  } else {
    const startMin = minutesOfDay(slot.start)
    const endMin = slot.end >= next ? dayMinutes : Math.max(startMin + 30, minutesOfDay(slot.end))
    from = Math.max(0, Math.floor((startMin - 120) / 60) * 60)
    to = Math.min(dayMinutes, Math.ceil((endMin + 120) / 60) * 60)
  }
  return { allDay, timed, clashes, before, after, from, to }
}
