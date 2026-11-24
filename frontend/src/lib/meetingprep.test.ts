import { describe, expect, it } from 'vitest'
import { attendeeDetail, attendeeName, composeFor, nextMeeting, otherAttendees, prepTaskDraft } from './meetingprep'
import { toRFC3339 } from './caldate'
import type { MeetingPrep, Occurrence, PrepAttendee } from './types'

function person(email: string, patch: Partial<PrepAttendee> = {}): PrepAttendee {
  return { email, name: '', role: 'REQ-PARTICIPANT', partStat: 'NEEDS-ACTION', organizer: false, self: false, contact: null, ...patch }
}

function prepFor(attendees: PrepAttendee[], patch: Partial<MeetingPrep> = {}): MeetingPrep {
  return {
    eventId: 7,
    recurrenceId: '',
    uid: 'uid-7',
    title: 'Roadmap review',
    description: '',
    location: '',
    url: '',
    start: toRFC3339(new Date(2026, 10, 17, 14)),
    end: toRFC3339(new Date(2026, 10, 17, 15)),
    allDay: false,
    recurring: false,
    join: null,
    attendees,
    threads: [],
    files: [],
    tasks: [],
    previous: [],
    ...patch,
  }
}

const contact = { id: 1, fullName: 'Ada Lovelace', organization: 'Analytical', title: 'Countess' } as PrepAttendee['contact']

describe('names and details', () => {
  it('names a person by card, invitation or address, in that order', () => {
    expect(attendeeName(person('a@x.example', { contact, name: 'A. Lovelace' }))).toBe('Ada Lovelace')
    expect(attendeeName(person('a@x.example', { name: 'A. Lovelace' }))).toBe('A. Lovelace')
    expect(attendeeName(person('a@x.example'))).toBe('a@x.example')
  })

  it('details a person by title and organisation, else the address', () => {
    expect(attendeeDetail(person('a@x.example', { contact }))).toBe('Countess, Analytical')
    expect(attendeeDetail(person('a@x.example'))).toBe('a@x.example')
  })
})

describe('writing to everyone', () => {
  const prep = prepFor([person('me@x.example', { self: true, organizer: true }), person('ada@x.example', { contact }), person('bob@x.example'), person('cy@x.example', { name: 'Cy, Jr.' })])

  it('addresses everyone but the user', () => {
    expect(otherAttendees(prep).map((p) => p.email)).toEqual(['ada@x.example', 'bob@x.example', 'cy@x.example'])
  })

  it('fills the recipients with names where there are some, quoting where needed, and the subject', () => {
    expect(composeFor(prep)).toEqual({
      to: 'Ada Lovelace <ada@x.example>, bob@x.example, "Cy, Jr." <cy@x.example>',
      subject: 'Roadmap review',
    })
  })
})

describe('prepTaskDraft', () => {
  const prep = prepFor([person('me@x.example', { self: true }), person('ada@x.example', { contact })])

  it('is titled with the meeting, due when it starts and tied to the event and the people', () => {
    const draft = prepTaskDraft(prep, 3, 'Prepare: {title}', 'Europe/Berlin')
    expect(draft).toMatchObject({
      id: 0,
      listId: 3,
      title: 'Prepare: Roadmap review',
      due: new Date(2026, 10, 17, 14).toISOString(),
      timeZone: 'Europe/Berlin',
      eventUids: ['uid-7'],
      contacts: [{ name: 'Ada Lovelace', email: 'ada@x.example' }],
    })
  })

  it('is due on the day for an all-day meeting', () => {
    const draft = prepTaskDraft({ ...prep, allDay: true, start: '2026-11-17', end: '2026-11-18' }, 3, 'Prepare: {title}', 'Europe/Berlin')
    expect(draft.due).toBe('2026-11-17')
    expect(draft.timeZone).toBe('')
  })
})

describe('nextMeeting', () => {
  function occ(summary: string, start: Date, end: Date, patch: Partial<Occurrence> = {}): Occurrence {
    return {
      eventId: 1, calendarId: 1, calendarColor: '', uid: summary, summary, location: '', start: toRFC3339(start), end: toRFC3339(end), allDay: false,
      recurring: false, recurrenceId: '', override: false, status: '', transparency: '', readOnly: false, hasReminders: false, hasAttendees: true,
      pending: false, conflict: false, ...patch,
    }
  }
  const now = new Date(2026, 10, 16, 10)
  const at = (h: number, m = 0) => new Date(2026, 10, 16, h, m)

  it('is the first one that has not ended, meetings under way included', () => {
    const list = [occ('late', at(15), at(16)), occ('over', at(8), at(9)), occ('under way', at(9, 30), at(10, 30)), occ('soon', at(11), at(12))]
    expect(nextMeeting(list, now)?.summary).toBe('under way')
  })

  it('skips events nobody else is invited to, all-day ones, cancelled ones and tasks', () => {
    const list = [
      occ('alone', at(11), at(12), { hasAttendees: false }),
      occ('all day', at(0), at(23), { allDay: true }),
      occ('cancelled', at(11), at(12), { status: 'CANCELLED' }),
      occ('task', at(11), at(12), { task: {} as never }),
      occ('real', at(13), at(14)),
    ]
    expect(nextMeeting(list, now)?.summary).toBe('real')
  })

  it('is null when nothing is left', () => {
    expect(nextMeeting([occ('over', at(8), at(9))], now)).toBeNull()
    expect(nextMeeting([], now)).toBeNull()
  })
})
