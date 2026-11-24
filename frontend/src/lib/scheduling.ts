// scheduling.ts is the arithmetic behind the event editor's scheduling view:
// reading the busy time the backend reports, which attendees a proposed time
// runs into, and the next free times for everyone whose calendar is known. It
// is pure, and compares instants, so the zone a time is shown in never
// changes the answer. Free time itself comes from freeslots.ts, which the
// composer's availability panel uses too.

import { freeSlots, type Span } from './freeslots'
import type { BusyKind, BusySpan } from './types'

/** A busy stretch, times read. */
export interface BusyBlock extends Span {
  kind: BusyKind
}

/** One person's row: whether their time is known, and when they are busy. */
export interface Availability {
  email: string
  known: boolean
  busy: BusyBlock[]
}

/** Reads the backend's busy spans, dropping any that end before they start. */
export function toBlocks(spans: BusySpan[]): BusyBlock[] {
  return spans
    .map((s) => ({ start: new Date(s.start), end: new Date(s.end), kind: s.kind }))
    .filter((b) => b.end > b.start)
    .sort((a, b) => a.start.getTime() - b.start.getTime())
}

/** The blocks that overlap [start, end). */
export function overlapping(busy: BusyBlock[], start: Date, end: Date): BusyBlock[] {
  return busy.filter((b) => b.start < end && b.end > start)
}

/** Who a proposed time runs into, by email, in row order. */
export interface Conflicts {
  // busy or away for part of the time.
  busy: string[]
  // only tentatively busy.
  tentative: string[]
  // nothing is known of their time.
  unknown: string[]
}

/** Sorts the rows by what they have on during [start, end). */
export function conflictsAt(rows: Availability[], start: Date, end: Date): Conflicts {
  const out: Conflicts = { busy: [], tentative: [], unknown: [] }
  for (const row of rows) {
    if (!row.known) {
      out.unknown.push(row.email)
      continue
    }
    const hits = overlapping(row.busy, start, end)
    if (hits.some((b) => b.kind !== 'tentative')) {
      out.busy.push(row.email)
    } else if (hits.length > 0) {
      out.tentative.push(row.email)
    }
  }
  return out
}

/** Where and how to look for suggested times. */
export interface SuggestOptions {
  /** The first day looked at, and the day after the last. */
  from: Date
  to: Date
  /** The event's length in minutes. */
  minutes: number
  /** Working hours, minutes after midnight in the zone the times are shown in. */
  workStart: number
  workEnd: number
  /** Saturdays and Sundays count. */
  weekends?: boolean
  /** Nothing earlier than this is offered. */
  now?: Date
  /** How many times to offer. */
  limit?: number
}

/**
 * The next times of the given length within working hours when everyone whose
 * time is known is free, tentative holds counting as busy. Rows with nothing
 * known do not hold anything up. Within a long free stretch a time is offered
 * every event length, and at least every half hour, on the quarter hour.
 */
export function suggestTimes(rows: Availability[], opts: SuggestOptions): Span[] {
  const minutes = Math.max(15, Math.round(opts.minutes))
  const limit = opts.limit ?? 5
  const stride = Math.ceil(Math.max(minutes, 30) / 15) * 15 * 60000
  const length = minutes * 60000
  const busy = rows.filter((r) => r.known).flatMap((r) => r.busy)
  const stretches = freeSlots(busy, {
    from: opts.from,
    to: opts.to,
    minutes,
    workStart: opts.workStart,
    workEnd: opts.workEnd,
    weekends: opts.weekends ?? false,
    now: opts.now,
    step: 15,
  })
  const out: Span[] = []
  for (const s of stretches) {
    for (let at = s.start.getTime(); at + length <= s.end.getTime(); at += stride) {
      out.push({ start: new Date(at), end: new Date(at + length) })
      if (out.length >= limit) {
        return out
      }
    }
  }
  return out
}
