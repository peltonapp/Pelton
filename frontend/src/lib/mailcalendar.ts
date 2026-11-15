// mailcalendar.ts is what a time picked from mail turns into: an event in the
// editor prefilled from the mail, an event made at once with the mail linked,
// a task due then, or the day opened in the calendar. The date previews in the
// reader and the My Day panel both run these.

import { get } from 'svelte/store'
import { mailEventSource } from './api'
import { dayKey } from './caldate'
import { t } from './i18n'
import { mailLinkLines } from './maillink'
import { createTasksFromMail } from './taskmail'
import type { Slot } from './daypeek'
import { calendarFocus, calendarSeed, calendarSettings, calendarView, calendars, defaultCalendarId, loadCalendars, removeEvent, saveEvent } from '../stores/calendar'
import { openSpace } from '../stores/space'
import { errorMessage, pushActions, toastError } from '../stores/toast'
import { newDraft } from '../components/calendar/occurrence'

/** Shows a day in the calendar's day view. */
export function openDayInCalendar(day: Date): void {
  calendarFocus.set(day)
  calendarView.set('day')
  openSpace('calendar')
}

/**
 * Opens the event editor in the calendar for a slot, titled with the mail's
 * subject and with a link back to it in the notes: the calendar's linking drop.
 */
export async function draftEventFromMail(messageId: number, slot: Slot): Promise<void> {
  if (get(calendars).length === 0) {
    await loadCalendars()
  }
  calendarSeed.set({ messageIds: [messageId], at: slot.start, end: slot.allDay ? undefined : slot.end, mode: slot.allDay ? 'allDay' : 'time', shift: true })
  openSpace('calendar')
}

/** The due value a task gets for a slot: its day, or its start time. */
export function dueOfSlot(slot: Slot): string {
  return slot.allDay ? dayKey(slot.start) : slot.start.toISOString()
}

/** Makes a task from a mail, due when the slot starts. */
export function taskFromMailAt(messageIds: number[], slot: Slot): Promise<number[]> {
  return createTasksFromMail(messageIds, dueOfSlot(slot))
}

/**
 * Makes an event for each mail at a slot without opening the editor, titled
 * with the mail's subject and linked back to it, then says so with Undo and
 * Open. Returns the ids of the events made.
 */
export async function eventFromMailAt(messageIds: number[], slot: Slot): Promise<number[]> {
  const tr = get(t)
  if (get(calendars).length === 0) {
    await loadCalendars()
  }
  const calendarId = defaultCalendarId(get(calendars), get(calendarSettings))
  if (calendarId === 0) {
    toastError(tr('calendar.noWritable'))
    return []
  }
  const reminder = get(calendarSettings).defaultReminderMinutes
  const made: number[] = []
  for (const id of messageIds) {
    try {
      const mail = await mailEventSource(id)
      const draft = { ...newDraft(calendarId, slot.start, slot.end, slot.allDay, reminder), summary: mail.subject, description: mailLinkLines(mail, tr) }
      const result = await saveEvent(draft)
      if (result) {
        made.push(result.event.id)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
  if (made.length === 0) {
    return made
  }
  const message = made.length > 1 ? tr('quickAdd.createdMany').replace('{n}', String(made.length)) : tr('quickAdd.created')
  pushActions(
    'success',
    message,
    [
      {
        label: tr('quickAdd.undo'),
        run: () =>
          void (async () => {
            for (const id of made) {
              await removeEvent(id)
            }
          })(),
      },
      { label: tr('quickAdd.open'), run: () => openDayInCalendar(slot.start) },
    ],
    8000,
  )
  return made
}
