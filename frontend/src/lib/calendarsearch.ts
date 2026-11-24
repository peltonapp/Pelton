// calendarsearch.ts picks the events a top bar search shows: those whose
// title or place hold every word, one row per event, what comes next first.

import { parseEventTime } from './caldate'
import { matchesQuery } from './spacesearch'
import type { Occurrence } from './types'

/**
 * The occurrences matching a query. A repeating event shows once, as its next
 * occurrence, or its last one when all are over. Upcoming events come first,
 * soonest on top, then past ones, most recent on top.
 */
export function searchOccurrences(list: readonly Occurrence[], query: string, now: Date, limit = 20): Occurrence[] {
  const at = now.getTime()
  const best = new Map<number, { occ: Occurrence; start: number }>()
  for (const occ of list) {
    if (!matchesQuery(query, [occ.summary, occ.location])) {
      continue
    }
    const start = parseEventTime(occ.start, occ.allDay).getTime()
    const held = best.get(occ.eventId)
    if (!held || better(start, held.start, at)) {
      best.set(occ.eventId, { occ, start })
    }
  }
  return [...best.values()]
    .sort((a, b) => {
      const aNext = a.start >= at
      const bNext = b.start >= at
      if (aNext !== bNext) {
        return aNext ? -1 : 1
      }
      return aNext ? a.start - b.start : b.start - a.start
    })
    .slice(0, limit)
    .map((entry) => entry.occ)
}

// an upcoming start beats a past one; among upcoming the sooner wins, among
// past the later.
function better(candidate: number, held: number, at: number): boolean {
  const candidateNext = candidate >= at
  const heldNext = held >= at
  if (candidateNext !== heldNext) {
    return candidateNext
  }
  return candidateNext ? candidate < held : candidate > held
}
