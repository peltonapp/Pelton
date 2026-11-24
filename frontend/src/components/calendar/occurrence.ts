// occurrence.ts holds small helpers the calendar views share about one
// occurrence: the key that names it across reloads, and the editor draft an
// event turns into.

import { toRFC3339, dayKey } from '../../lib/caldate'
import type { CalendarEvent, EventDraft, Occurrence } from '../../lib/types'

/** Names an occurrence: its event and, for a series, which occurrence. */
export function occurrenceKey(occ: Pick<Occurrence, 'eventId' | 'recurrenceId'>): string {
  return `${occ.eventId}|${occ.recurrenceId}`
}

/** The editor draft of a stored event, or of one occurrence of a series. */
export function draftFromEvent(ev: CalendarEvent): EventDraft {
  return {
    id: ev.id,
    calendarId: ev.calendarId,
    summary: ev.summary,
    description: ev.description,
    location: ev.location,
    url: ev.url,
    status: ev.status,
    transparency: ev.transparency,
    categories: [...(ev.categories ?? [])],
    start: ev.start,
    end: ev.end,
    allDay: ev.allDay,
    timeZone: ev.timeZone,
    rrule: ev.rrule,
    reminders: (ev.reminders ?? []).map((r) => ({ ...r })),
    organizer: { ...ev.organizer },
    attendees: (ev.attendees ?? []).map((a) => ({ ...a })),
    scope: '',
    recurrenceId: ev.recurrenceId,
    force: false,
  }
}

/** The local zone, which new events are written in so a series keeps its wall time over daylight saving. */
export function localZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? ''
  } catch {
    return ''
  }
}

/** A new event's draft for the given span. */
export function newDraft(calendarId: number, start: Date, end: Date, allDay: boolean, reminderMinutes: number): EventDraft {
  return {
    id: 0,
    calendarId,
    summary: '',
    description: '',
    location: '',
    url: '',
    status: '',
    transparency: '',
    categories: [],
    start: allDay ? dayKey(start) : toRFC3339(start),
    end: allDay ? dayKey(end) : toRFC3339(end),
    allDay,
    timeZone: allDay ? '' : localZone(),
    rrule: '',
    reminders: reminderMinutes >= 0 ? [{ minutesBefore: reminderMinutes, relatedEnd: false, absolute: '', action: '' }] : [],
    organizer: { email: '', name: '', role: '', partStat: '', rsvp: false },
    attendees: [],
    scope: '',
    recurrenceId: '',
    force: false,
  }
}

/** Writes new times into a draft, as dates for an all-day event. */
export function withTimes(draft: EventDraft, start: Date, end: Date): EventDraft {
  return {
    ...draft,
    start: draft.allDay ? dayKey(start) : toRFC3339(start),
    end: draft.allDay ? dayKey(end) : toRFC3339(end),
  }
}
