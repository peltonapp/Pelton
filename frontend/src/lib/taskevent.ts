// taskevent.ts turns a task into an event and an event into a task: the new
// one is made, the old one deleted, and a toast offers to undo both. a task's
// due date becomes the event's day or start, an event's start the task's due
// date; title, notes, repeat and mail links travel along.

import { get } from 'svelte/store'
import { createEvent, deleteEvent, deleteTask, getEvent, restoreTasks, createTask } from './api'
import { addDays, dayKey, defaultSlot, parseEventTime, startOfDay } from './caldate'
import { mailLinks } from './maillink'
import { dueDate, dueHasTime } from './tasklogic'
import { t } from './i18n'
import { newDraft, draftFromEvent } from '../components/calendar/occurrence'
import { calendars, calendarSettings, loadCalendars, loadOccurrences, saveEvent } from '../stores/calendar'
import { defaultListId, emptyDraft, loadTasks, localZone, taskLists, taskSettings } from '../stores/tasks'
import { errorMessage, pushActions, toastError } from '../stores/toast'
import type { CalendarEvent, EventDraft, Task, TaskDraft } from './types'

/** The event a task becomes, in the given calendar. */
export function eventDraftFromTask(task: Task, calendarId: number, reminderMinutes: number, now = new Date()): EventDraft {
  const due = dueDate(task)
  let draft: EventDraft
  if (due && !dueHasTime(task.due)) {
    draft = newDraft(calendarId, due, addDays(due, 1), true, reminderMinutes)
  } else {
    const start = due ?? defaultSlot(startOfDay(now), now)
    draft = newDraft(calendarId, start, new Date(start.getTime() + 3600000), false, reminderMinutes)
  }
  const links = task.mailLinks.filter((l) => !task.notes.includes(l))
  return {
    ...draft,
    summary: task.title,
    description: [task.notes, ...links].filter(Boolean).join('\n'),
    url: task.url,
    categories: [...task.categories],
    rrule: task.rrule,
  }
}

/** The task an event becomes, in the given list. */
export function taskDraftFromEvent(ev: CalendarEvent, listId: number): TaskDraft {
  const start = parseEventTime(ev.start, ev.allDay)
  return {
    ...emptyDraft(listId),
    title: ev.summary,
    notes: ev.description,
    url: ev.url,
    due: ev.allDay ? dayKey(start) : start.toISOString(),
    timeZone: ev.timeZone || localZone(),
    rrule: ev.rrule,
    categories: [...ev.categories],
    mailLinks: mailLinks(ev.description),
  }
}

/** Turns a task into an event and deletes the task. Returns the new event's id, 0 when nothing changed. */
export async function convertTaskToEvent(task: Task): Promise<number> {
  const tr = get(t)
  if (get(calendars).length === 0) {
    await loadCalendars()
  }
  const writable = get(calendars).filter((c) => !c.readOnly)
  const settings = get(calendarSettings)
  const calendarId = (writable.find((c) => c.id === settings.defaultCalendarId) ?? writable.find((c) => c.default) ?? writable[0])?.id ?? 0
  if (calendarId === 0) {
    toastError(tr('calendar.noWritable'))
    return 0
  }
  try {
    const made = await createEvent(eventDraftFromTask(task, calendarId, settings.defaultReminderMinutes))
    const removed = await deleteTask(task.id)
    await Promise.all([loadTasks(), loadOccurrences()])
    pushActions(
      'success',
      tr('tasks.madeEvent'),
      [
        {
          label: tr('tasks.undo'),
          run: () =>
            void (async () => {
              await deleteEvent(made.event.id).catch((err) => toastError(errorMessage(err)))
              await loadOccurrences()
              await restoreTasks(removed.removed).catch((err) => toastError(errorMessage(err)))
              await loadTasks()
            })(),
        },
      ],
      8000,
    )
    return made.event.id
  } catch (err) {
    toastError(errorMessage(err))
    return 0
  }
}

/** Turns an event into a task and deletes the event. Returns the new task's id, 0 when nothing changed. */
export async function convertEventToTask(eventId: number): Promise<number> {
  const tr = get(t)
  if (get(taskLists).length === 0) {
    await loadTasks()
  }
  const listId = defaultListId(get(taskLists), get(taskSettings))
  if (listId === 0) {
    toastError(tr('tasks.noWritable'))
    return 0
  }
  try {
    const ev = await getEvent(eventId, '')
    const made = await createTask(taskDraftFromEvent(ev, listId))
    await deleteEvent(eventId)
    await Promise.all([loadTasks(), loadOccurrences()])
    const restore = { ...draftFromEvent(ev), id: 0 }
    pushActions(
      'success',
      tr('tasks.madeTask'),
      [
        {
          label: tr('tasks.undo'),
          run: () =>
            void (async () => {
              await deleteTask(made.task.id).catch((err) => toastError(errorMessage(err)))
              await saveEvent(restore)
              await loadTasks()
            })(),
        },
      ],
      8000,
    )
    return made.task.id
  } catch (err) {
    toastError(errorMessage(err))
    return 0
  }
}
