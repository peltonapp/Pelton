// timeblocks.ts holds what the calendar shows of time blocking: the blocks of
// the range on screen, how much time each task has planned, the time
// blocking settings, and the proposals of a day plan that is being reviewed.
// blocks and proposals are drawn by the time grid as entries like events,
// each carrying its block or proposal, which the screen acts on.

import { get, writable } from 'svelte/store'
import {
  createTimeBlocks,
  deleteTimeBlocks,
  getTimeBlockSettings,
  listTaskPlans,
  listTimeBlocks,
  setTimeBlockSettings,
  updateTimeBlock,
} from '../../lib/api'
import { onCalendarChanged, onTimeBlocksChanged, type Unsubscribe } from '../../lib/events'
import { toRFC3339 } from '../../lib/caldate'
import { errorMessage, toastError } from '../../stores/toast'
import type { BlockProposal, Calendar, Occurrence, TaskPlan, TimeBlock, TimeBlockRequest, TimeBlockSettings } from '../../lib/types'

/** The settings before the stored ones have loaded. */
export const defaultBlockSettings: TimeBlockSettings = {
  defaultMinutes: 30,
  minSlotMinutes: 15,
  bufferMinutes: 5,
  workDays: [1, 2, 3, 4, 5],
  searchDays: 7,
  writeToCalendar: false,
  calendarId: 0,
  eventTitle: 'Focus time',
  includeTitle: false,
}

/** The blocks of the range on screen. */
export const timeBlocks = writable<TimeBlock[]>([])

/** The time blocking preferences. */
export const blockSettings = writable<TimeBlockSettings>(defaultBlockSettings)

/** How much time each task has in blocks, by task UID. */
export const taskPlans = writable<Map<string, TaskPlan>>(new Map())

/** A proposal of a day plan, with the key that names it until it is accepted or dismissed. */
export type Proposal = BlockProposal & { key: string }

/** The proposals of the day plan under review; empty when there is none. */
export const proposals = writable<Proposal[]>([])

/** Whether the plan pane shows beside the day and week views. */
export const planPaneOpen = writable<boolean>(true)

let range = { from: '', to: '' }

/** Loads the blocks overlapping [from, to), both rfc3339, and keeps that range for reloads. */
export async function loadBlocks(from = range.from, to = range.to): Promise<void> {
  range = { from, to }
  if (!from || !to) {
    return
  }
  try {
    const list = await listTimeBlocks(from, to)
    if (range.from === from && range.to === to) {
      timeBlocks.set(list)
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Loads how much time each task has planned. */
export async function loadTaskPlans(): Promise<void> {
  try {
    taskPlans.set(new Map((await listTaskPlans()).map((p) => [p.taskUid, p])))
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Loads the time blocking preferences. */
export async function loadBlockSettings(): Promise<void> {
  try {
    blockSettings.set({ ...defaultBlockSettings, ...(await getTimeBlockSettings()) })
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Changes some time blocking preferences and saves them; a refused save puts the old ones back. */
export async function saveBlockSettings(patch: Partial<TimeBlockSettings>): Promise<void> {
  const before = get(blockSettings)
  const next = { ...before, ...patch }
  blockSettings.set(next)
  try {
    await setTimeBlockSettings(next)
  } catch (err) {
    blockSettings.set(before)
    toastError(errorMessage(err))
  }
}

/** Reloads the blocks and the plans whenever blocks or calendars change. Returns the function that stops it. */
export function watchBlocks(): Unsubscribe {
  const reload = (): void => {
    void loadBlocks()
    void loadTaskPlans()
  }
  const offBlocks = onTimeBlocksChanged(reload)
  const offCalendar = onCalendarChanged(reload)
  return () => {
    offBlocks()
    offCalendar()
  }
}

/** The calendar a new block is written to: none unless the settings say so, then the chosen one while it is writable. */
export function blockCalendarId(settings: TimeBlockSettings, calendars: Calendar[], fallback: number): number {
  if (!settings.writeToCalendar) {
    return 0
  }
  return calendars.some((c) => c.id === settings.calendarId && !c.readOnly) ? settings.calendarId : fallback
}

/** The occurrence key a block is drawn under. */
export function blockKey(id: number): string {
  return `0|block:${id}`
}

/** The occurrence key a proposal is drawn under. */
export function proposalKey(key: string): string {
  return `0|proposal:${key}`
}

function entry(start: string, end: string, summary: string): Omit<Occurrence, 'recurrenceId'> {
  return {
    eventId: 0,
    calendarId: 0,
    calendarColor: '',
    uid: '',
    summary,
    location: '',
    start,
    end,
    allDay: false,
    recurring: false,
    override: false,
    status: '',
    transparency: '',
    readOnly: false,
    hasReminders: false,
    pending: false,
    conflict: false,
  }
}

/** A block as the calendar views place it. */
export function blockOccurrence(b: TimeBlock): Occurrence {
  return { ...entry(b.start, b.end, b.title), recurrenceId: `block:${b.id}`, pending: b.pending, conflict: b.linkState !== '', block: b }
}

/** A proposal as the calendar views place it. */
export function proposalOccurrence(p: Proposal): Occurrence {
  return { ...entry(p.start, p.end, p.title), recurrenceId: `proposal:${p.key}`, proposal: p }
}

/** The request that saves a block as it is, to change from. */
export function requestOf(b: TimeBlock): TimeBlockRequest {
  return {
    id: b.id,
    title: b.masked ? '' : b.title,
    start: b.start,
    end: b.end,
    taskUid: b.taskUid,
    messageId: 0,
    done: b.done,
    calendarId: b.calendarId,
    eventTitle: b.eventTitle,
  }
}

function replaceBlock(next: TimeBlock): void {
  timeBlocks.update((list) => list.map((b) => (b.id === next.id ? next : b)))
}

/**
 * Saves a change to a block. The block changes on screen at once and goes
 * back when the save is refused. Returns the saved block, or null.
 */
export async function changeBlock(b: TimeBlock, patch: Partial<TimeBlockRequest>): Promise<TimeBlock | null> {
  const req = { ...requestOf(b), ...patch }
  replaceBlock({ ...b, start: req.start, end: req.end, done: req.done, title: b.masked ? b.title : req.title || b.title })
  try {
    const saved = await updateTimeBlock(req)
    replaceBlock(saved)
    return saved
  } catch (err) {
    replaceBlock(b)
    toastError(errorMessage(err))
    return null
  }
}

/** Moves a block to start and end. */
export function moveBlock(b: TimeBlock, start: Date, end: Date): Promise<TimeBlock | null> {
  return changeBlock(b, { start: toRFC3339(start), end: toRFC3339(end) })
}

/** Makes blocks; returns them, or an empty list when it failed. */
export async function makeBlocks(reqs: TimeBlockRequest[]): Promise<TimeBlock[]> {
  try {
    const made = await createTimeBlocks(reqs)
    timeBlocks.update((list) => [...list, ...made])
    return made
  } catch (err) {
    toastError(errorMessage(err))
    return []
  }
}

/** Deletes blocks. Returns whether it went through. */
export async function removeBlocks(ids: number[]): Promise<boolean> {
  const before = get(timeBlocks)
  timeBlocks.update((list) => list.filter((b) => !ids.includes(b.id)))
  try {
    await deleteTimeBlocks(ids)
    return true
  } catch (err) {
    timeBlocks.set(before)
    toastError(errorMessage(err))
    return false
  }
}

/** The request that makes a copy of a block right after it, on the same calendar. */
export function duplicateRequest(b: TimeBlock): TimeBlockRequest {
  const start = new Date(b.end)
  const end = new Date(start.getTime() + (new Date(b.end).getTime() - new Date(b.start).getTime()))
  return { ...requestOf(b), id: 0, start: toRFC3339(start), end: toRFC3339(end), done: false }
}

let proposalSeq = 0

/** Puts a day plan's proposals up for review. */
export function proposeAll(list: BlockProposal[]): void {
  proposals.set(list.map((p) => ({ ...p, key: String(++proposalSeq) })))
}

/** Moves a proposal. */
export function moveProposal(key: string, start: Date, end: Date): void {
  proposals.update((list) => list.map((p) => (p.key === key ? { ...p, start: toRFC3339(start), end: toRFC3339(end) } : p)))
}

/** Takes proposals out of review, accepted or dismissed. */
export function dropProposals(keys: string[]): void {
  proposals.update((list) => list.filter((p) => !keys.includes(p.key)))
}

/** The request that makes a block of a proposal. */
export function proposalRequest(p: Proposal, calendarId: number): TimeBlockRequest {
  return { id: 0, title: '', start: p.start, end: p.end, taskUid: p.taskUid, messageId: 0, done: false, calendarId, eventTitle: '' }
}

/** Says how long a number of minutes is: "45 min", "2 h", "1 h 30 min". */
export function formatMinutes(minutes: number, tr: (key: string) => string): string {
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  if (h === 0) {
    return tr('tasks.duration.minutes').replace('{n}', String(m))
  }
  if (m === 0) {
    return tr('tasks.duration.hours').replace('{n}', String(h))
  }
  return tr('tasks.duration.hoursMinutes').replace('{h}', String(h)).replace('{m}', String(m))
}
