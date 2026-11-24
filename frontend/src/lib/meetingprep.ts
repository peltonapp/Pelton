// meetingprep.ts is the small amount of logic around the Prepare panel: what
// "write to everyone" addresses, the task made for a meeting, the next meeting
// of the day that is worth preparing for, and the one-line description of a
// person. The gathering itself is the backend's.

import { dayKey, parseEventTime } from './caldate'
import { formatAddress } from './mailcompose'
import { emptyDraft } from '../stores/tasks'
import type { MeetingPrep, Occurrence, PrepAttendee, TaskDraft } from './types'

/** What a person is called: the address book's name, the invitation's, or the address. */
export function attendeeName(p: PrepAttendee): string {
  return p.contact?.fullName || p.name || p.email
}

/** The others invited, leaving out the user. */
export function otherAttendees(prep: Pick<MeetingPrep, 'attendees'>): PrepAttendee[] {
  return prep.attendees.filter((p) => !p.self)
}

/** The recipients and subject of a message to everyone else invited. */
export function composeFor(prep: Pick<MeetingPrep, 'attendees' | 'title'>): { to: string; subject: string } {
  return {
    to: otherAttendees(prep)
      .map((p) => formatAddress({ name: attendeeName(p) === p.email ? '' : attendeeName(p), email: p.email }))
      .join(', '),
    subject: prep.title,
  }
}

/**
 * The task made for preparing a meeting: titled with the meeting, due when it
 * starts, tied to the event and to the people invited so it shows up with them.
 * titleFor says how the title reads, such as "Prepare: {title}".
 */
export function prepTaskDraft(prep: MeetingPrep, listId: number, titleFor: string, timeZone: string): TaskDraft {
  const start = parseEventTime(prep.start, prep.allDay)
  return {
    ...emptyDraft(listId),
    title: titleFor.replace('{title}', prep.title),
    due: prep.allDay ? dayKey(start) : start.toISOString(),
    timeZone: prep.allDay ? '' : timeZone,
    eventUids: prep.uid ? [prep.uid] : [],
    contacts: otherAttendees(prep).map((p) => ({ name: attendeeName(p) === p.email ? '' : attendeeName(p), email: p.email })),
  }
}

/**
 * The next meeting worth preparing for among a day's occurrences: a timed
 * event with others invited that has not ended, the one starting first, with a
 * meeting under way counting as next until it ends. Cancelled events and
 * tasks are never it.
 */
export function nextMeeting(list: Occurrence[], now: Date): Occurrence | null {
  let best: { occ: Occurrence; start: number } | null = null
  for (const occ of list) {
    if (occ.task || occ.allDay || !occ.hasAttendees || occ.status === 'CANCELLED') {
      continue
    }
    const end = parseEventTime(occ.end, false)
    if (end.getTime() <= now.getTime()) {
      continue
    }
    const start = parseEventTime(occ.start, false).getTime()
    if (!best || start < best.start) {
      best = { occ, start }
    }
  }
  return best?.occ ?? null
}

/** The detail line under a person: their title and organisation, else their address. */
export function attendeeDetail(p: PrepAttendee): string {
  const parts = [p.contact?.title, p.contact?.organization].filter((x): x is string => !!x && x.trim() !== '')
  return parts.length > 0 ? parts.join(', ') : p.email
}
