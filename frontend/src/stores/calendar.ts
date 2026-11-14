// calendar.ts holds the calendars, the CalDAV logins they sync through, and the
// occurrences of the window the calendar screen shows.
//
// Occurrences arrive already expanded from the backend, with their calendar's
// color, so a view only has to place them. A sync or an edit anywhere fires
// calendar:changed and the store reloads the window it holds.

import { get, writable } from 'svelte/store'
import {
  listCalendars,
  listCalendarAccounts,
  listOccurrences,
  syncCalendars as syncCalendarsAPI,
  createEvent,
  updateEvent,
  deleteEvent,
  resolveEventConflict,
  setCalendarVisible,
  setCalendarColor,
  getCalendarSettings,
  setCalendarSettings,
} from '../lib/api'
import { onCalendarChanged, onCalendarSync, type CalendarSyncEvent, type Unsubscribe } from '../lib/events'
import { errorMessage, toastError, toastInfo, toastSuccess } from './toast'
import { t } from '../lib/i18n'
import type {
  Calendar,
  CalendarAccount,
  CalendarSettings,
  CalendarView,
  EventDraft,
  EventSaveResult,
  EventScope,
  Occurrence,
} from '../lib/types'

/** Every calendar, local ones first. */
export const calendars = writable<Calendar[]>([])

/** Every CalDAV login. */
export const calendarAccounts = writable<CalendarAccount[]>([])

/** The window the calendar screen shows, as rfc3339 bounds; empty until set. */
export const calendarRange = writable<{ from: string; to: string }>({ from: '', to: '' })

/** The expanded occurrences in calendarRange, sorted by start. */
export const occurrences = writable<Occurrence[]>([])

/** True while a load is in flight. */
export const calendarLoading = writable<boolean>(false)

/** The running sync's progress, or null when none is running. */
export const calendarSync = writable<CalendarSyncEvent | null>(null)

/** The last sync's error, empty when it went through. */
export const calendarSyncError = writable<string>('')

/**
 * The result of a save or delete the server refused because its copy moved on,
 * or null. It carries both versions; resolveConflict settles it.
 */
export const eventConflict = writable<EventSaveResult | null>(null)

/** The settings the screen works with until the stored ones have loaded. */
export const defaultCalendarSettings: CalendarSettings = {
  syncIntervalMinutes: 15,
  remindersEnabled: true,
  defaultReminderMinutes: 15,
  defaultCalendarId: 0,
  defaultView: 'week',
  weekStart: -1,
  workDayStart: 9 * 60,
  workDayEnd: 17 * 60,
  timeZoneDisplay: 'off',
  secondaryTimeZone: '',
}

/** The calendar preferences. */
export const calendarSettings = writable<CalendarSettings>(defaultCalendarSettings)

/** True once the stored calendar preferences have loaded. */
export const calendarSettingsLoaded = writable<boolean>(false)

/** The view the calendar screen shows. */
export const calendarView = writable<CalendarView>('week')

/** The day the calendar screen is on; its view shows the page holding it. */
export const calendarFocus = writable<Date>(new Date())

/**
 * Where mail was dropped on the calendar: on a time slot (the event starts
 * there), on a day (it keeps the suggested time of day on that day), or in the
 * all-day lane (it becomes an all-day event).
 */
export type MailDropMode = 'time' | 'day' | 'allDay'

/** Mail dropped somewhere on the calendar, as the views report it. */
export interface MailDropAt {
  messageId: number
  at: Date
  mode: MailDropMode
}

/**
 * A message to start a new event from, set by mail dropped on the Calendar
 * entry or on the calendar itself and taken by the calendar screen. at is null
 * for the Calendar entry, where the suggested time stays.
 */
export const calendarSeed = writable<{ messageId: number; at: Date | null; mode: MailDropMode } | null>(null)

/** Loads the calendar preferences once; the screen opens in their default view. */
export async function loadCalendarSettings(): Promise<void> {
  try {
    const s = await getCalendarSettings()
    calendarSettings.set({ ...defaultCalendarSettings, ...s })
    if (!get(calendarSettingsLoaded)) {
      calendarView.set(s.defaultView || 'week')
    }
    calendarSettingsLoaded.set(true)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Changes some calendar preferences and saves them. A refused save puts the old ones back. */
export async function saveCalendarSettings(patch: Partial<CalendarSettings>): Promise<void> {
  const before = get(calendarSettings)
  const next = { ...before, ...patch }
  calendarSettings.set(next)
  try {
    await setCalendarSettings(next)
  } catch (err) {
    calendarSettings.set(before)
    toastError(errorMessage(err))
  }
}

/** Loads the calendars and the CalDAV logins. */
export async function loadCalendars(): Promise<void> {
  try {
    calendars.set(await listCalendars())
    calendarAccounts.set(await listCalendarAccounts())
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Reloads the occurrences of the current range. Nothing happens before a range is set. */
export async function loadOccurrences(): Promise<void> {
  const { from, to } = get(calendarRange)
  if (!from || !to) {
    return
  }
  calendarLoading.set(true)
  try {
    const list = await listOccurrences(from, to)
    // a newer range may have been asked for while this one loaded.
    const now = get(calendarRange)
    if (now.from === from && now.to === to) {
      occurrences.set(list)
    }
  } catch (err) {
    toastError(errorMessage(err))
  } finally {
    calendarLoading.set(false)
  }
}

/** Moves the window to [from, to), both rfc3339, and loads it. */
export async function showRange(from: string, to: string): Promise<void> {
  calendarRange.set({ from, to })
  await loadOccurrences()
}

/** Syncs every calendar account now, then reloads. */
export async function refreshCalendars(): Promise<void> {
  try {
    await syncCalendarsAPI()
  } catch (err) {
    toastError(errorMessage(err))
  }
  await Promise.all([loadCalendars(), loadOccurrences()])
}

/**
 * Saves an event: creates it for id 0, else updates it with the draft's scope.
 * A conflict opens eventConflict and returns null; an unreachable server keeps
 * the change queued and says so. Returns the result otherwise.
 */
export async function saveEvent(draft: EventDraft): Promise<EventSaveResult | null> {
  try {
    const result = draft.id === 0 ? await createEvent(draft) : await updateEvent(draft)
    return await settle(result)
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/**
 * Deletes an event, or for an occurrence of a series the scope named. Like a
 * save it can come back as a conflict. Returns true when it went through or
 * was queued.
 */
export async function removeEvent(
  id: number,
  scope: EventScope | '' = '',
  recurrenceId = '',
  force = false,
): Promise<boolean> {
  try {
    const result = await deleteEvent(id, scope, recurrenceId, force)
    if ((await settle(result)) === null) {
      return false
    }
    if (!result.queued) {
      toastSuccess(get(t)('calendar.eventDeleted'))
    }
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/** Settles the open conflict: keepMine writes the local version, else the server's wins. */
export async function resolveConflict(keepMine: boolean): Promise<void> {
  const conflict = get(eventConflict)
  if (!conflict) {
    return
  }
  try {
    await resolveEventConflict(conflict.event.id, keepMine)
    eventConflict.set(null)
  } catch (err) {
    toastError(errorMessage(err))
  }
  await loadOccurrences()
}

/** Closes the conflict without settling it; the event stays marked. */
export function dismissEventConflict(): void {
  eventConflict.set(null)
}

/** Shows or hides a calendar. */
export async function toggleCalendar(id: number, visible: boolean): Promise<void> {
  calendars.update((list) => list.map((c) => (c.id === id ? { ...c, visible } : c)))
  try {
    await setCalendarVisible(id, visible)
  } catch (err) {
    calendars.update((list) => list.map((c) => (c.id === id ? { ...c, visible: !visible } : c)))
    toastError(errorMessage(err))
  }
}

/** Sets a calendar's color here. */
export async function recolorCalendar(id: number, color: string): Promise<void> {
  try {
    await setCalendarColor(id, color)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/**
 * Subscribes the store to the backend's calendar events: a change reloads the
 * calendars and the window, a sync updates calendarSync. Returns the
 * unsubscribe.
 */
export function watchCalendar(): Unsubscribe {
  const offChanged = onCalendarChanged(() => {
    void loadCalendars()
    void loadOccurrences()
  })
  const offSync = onCalendarSync((e) => {
    calendarSync.set(e.running ? e : null)
    if (!e.running) {
      calendarSyncError.set(e.error)
    }
  })
  return () => {
    offChanged()
    offSync()
  }
}

async function settle(result: EventSaveResult): Promise<EventSaveResult | null> {
  if (result.conflict) {
    eventConflict.set(result)
    return null
  }
  if (result.queued) {
    toastInfo(get(t)('calendar.eventQueued'))
  }
  await loadOccurrences()
  return result
}
