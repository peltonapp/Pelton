// tasks.ts holds the task lists, the tasks, the task preferences and which
// mails have a task. it is loaded once the new design starts, because the rail
// badge, the mail markers and the calendar read it as well as the Tasks
// screen. a sync or an edit anywhere fires calendar:changed, which reloads it.

import { derived, get, writable } from 'svelte/store'
import {
  createTask,
  deleteTask,
  getTaskSettings,
  listTaskLists,
  listTaskMailMarks,
  listTasks,
  resolveTaskConflict,
  restoreTasks,
  setTaskDone,
  setTaskDue,
  setTaskSettings,
  updateTask,
} from '../lib/api'
import { onCalendarChanged, type Unsubscribe } from '../lib/events'
import { badgeCount } from '../lib/tasklogic'
import { errorMessage, pushActions, toastError, toastInfo } from './toast'
import { t } from '../lib/i18n'
import type { RemovedTask, Task, TaskDraft, TaskList, TaskSaveResult, TaskSettings } from '../lib/types'

/** Every task list, local ones first. */
export const taskLists = writable<TaskList[]>([])

/** Every task the settings keep listed. */
export const tasks = writable<Task[]>([])

/** True once the tasks have loaded the first time. */
export const tasksLoaded = writable<boolean>(false)

/** The settings the app works with until the stored ones have loaded. */
export const defaultTaskSettings: TaskSettings = {
  defaultListId: 0,
  defaultDueMinutes: -1,
  badge: 'today',
  showInCalendar: true,
  completedDays: 30,
  sort: 'manual',
}

/** The task preferences. */
export const taskSettings = writable<TaskSettings>(defaultTaskSettings)

/** Message ids that have a task, mapped to true when every such task is done. */
export const taskMailMarks = writable<Map<number, boolean>>(new Map())

/** A save the server refused because its copy moved on, or null. */
export const taskConflict = writable<TaskSaveResult | null>(null)

/** Messages to make tasks from, set by a drop on the Tasks entry. */
export const taskSeed = writable<number[] | null>(null)

/**
 * Asks the app to make tasks from the open or selected mail, for callers that
 * do not know which mail that is, such as the command palette.
 */
export const taskFromMailRequest = writable<boolean>(false)

/** A task to show when the Tasks screen opens, set from elsewhere in the app. */
export const taskFocus = writable<number | null>(null)

/** A person to start a new task for, set from the contacts screen. */
export const taskForContact = writable<{ name: string; email: string } | null>(null)

/** The current minute, so the badge and the smart lists move on at midnight. */
export const taskClock = writable<Date>(new Date())

/** How many tasks the rail badge counts. */
export const taskBadge = derived([tasks, taskSettings, taskClock], ([$tasks, $settings, $now]) => badgeCount($tasks, $settings.badge, $now))

/** The list a new task goes to: the default while it can take one, else the first writable list. */
export function defaultListId(lists: TaskList[], settings: TaskSettings): number {
  const writable = lists.filter((l) => !l.readOnly)
  return (writable.find((l) => l.id === settings.defaultListId) ?? writable.find((l) => l.default) ?? writable[0])?.id ?? 0
}

/** A fresh draft for a task in a list. */
export function emptyDraft(listId: number): TaskDraft {
  return {
    id: 0,
    listId,
    uid: '',
    title: '',
    notes: '',
    url: '',
    due: '',
    start: '',
    timeZone: '',
    status: '',
    percent: 0,
    priority: 0,
    rrule: '',
    reminders: [],
    parent: '',
    categories: [],
    mailLinks: [],
    contacts: [],
    eventUids: [],
    sortOrder: 0,
    force: false,
  }
}

/** The draft that saves a task unchanged, to edit from. */
export function draftOf(task: Task): TaskDraft {
  return {
    id: task.id,
    listId: task.listId,
    uid: task.uid,
    title: task.title,
    notes: task.notes,
    url: task.url,
    due: task.due,
    start: task.start,
    timeZone: task.timeZone || localZone(),
    status: task.status,
    percent: task.percent,
    priority: task.priority,
    rrule: task.rrule,
    reminders: task.reminders.map((r) => ({ ...r })),
    parent: task.parent,
    categories: [...task.categories],
    mailLinks: [...task.mailLinks],
    contacts: task.contacts.map((c) => ({ ...c })),
    eventUids: [...task.eventUids],
    sortOrder: task.sortOrder,
    force: false,
  }
}

/** The IANA zone the app runs in. */
export function localZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  } catch {
    return ''
  }
}

/** Loads the lists, the tasks and the mail markers. */
export async function loadTasks(): Promise<void> {
  try {
    const [lists, list, marks] = await Promise.all([listTaskLists(), listTasks(), listTaskMailMarks()])
    taskLists.set(lists)
    tasks.set(list)
    taskMailMarks.set(new Map(marks.map((m) => [m.messageId, m.done])))
    tasksLoaded.set(true)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Loads the task preferences. */
export async function loadTaskSettings(): Promise<void> {
  try {
    taskSettings.set({ ...defaultTaskSettings, ...(await getTaskSettings()) })
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Changes some task preferences and saves them. A refused save puts the old ones back. */
export async function saveTaskSettings(patch: Partial<TaskSettings>): Promise<void> {
  const before = get(taskSettings)
  const next = { ...before, ...patch }
  taskSettings.set(next)
  try {
    await setTaskSettings(next)
  } catch (err) {
    taskSettings.set(before)
    toastError(errorMessage(err))
  }
}

function replaceTask(task: Task): void {
  tasks.update((list) => (list.some((x) => x.id === task.id) ? list.map((x) => (x.id === task.id ? task : x)) : [...list, task]))
}

async function settle(result: TaskSaveResult): Promise<TaskSaveResult | null> {
  if (result.conflict) {
    taskConflict.set(result)
    return null
  }
  replaceTask(result.task)
  if (result.queued) {
    toastInfo(get(t)('tasks.queued'))
  }
  return result
}

/** Saves a task: creates it for id 0, else updates it. Returns null when it did not go through. */
export async function saveTask(draft: TaskDraft): Promise<TaskSaveResult | null> {
  try {
    const result = draft.id === 0 ? await createTask(draft) : await updateTask(draft)
    return await settle(result)
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/**
 * Completes a task or opens it again. The row changes at once; a repeating
 * task that moved on to its next date says so.
 */
export async function toggleDone(task: Task, done = !task.done): Promise<TaskSaveResult | null> {
  replaceTask({ ...task, done })
  try {
    const result = await setTaskDone(task.id, done)
    const settled = await settle(result)
    if (settled?.advanced) {
      toastInfo(get(t)('tasks.advanced').replace('{title}', task.title || get(t)('tasks.untitled')))
    }
    return settled
  } catch (err) {
    replaceTask(task)
    toastError(errorMessage(err))
    return null
  }
}

/** Moves a task's due date, keeping its time of day. Empty clears it. */
export async function moveTaskDue(task: Task, due: string): Promise<boolean> {
  replaceTask({ ...task, due })
  try {
    return (await settle(await setTaskDue(task.id, due))) !== null
  } catch (err) {
    replaceTask(task)
    toastError(errorMessage(err))
    return false
  }
}

/**
 * Deletes a task with its subtasks and offers to bring them back. Returns
 * what was deleted, or null when it did not go through.
 */
export async function removeTask(task: Task): Promise<RemovedTask[] | null> {
  const before = get(tasks)
  tasks.update((list) => list.filter((x) => x.id !== task.id && !(x.listId === task.listId && x.parent === task.uid)))
  try {
    const result = await deleteTask(task.id)
    await loadTasks()
    const tr = get(t)
    pushActions('success', tr('tasks.deleted'), [{ label: tr('tasks.undo'), run: () => void undoRemove(result.removed) }], 8000)
    return result.removed
  } catch (err) {
    tasks.set(before)
    toastError(errorMessage(err))
    return null
  }
}

/** Brings deleted tasks back. */
export async function undoRemove(removed: RemovedTask[]): Promise<void> {
  try {
    await restoreTasks(removed)
  } catch (err) {
    toastError(errorMessage(err))
  }
  await loadTasks()
}

/** Settles the open conflict: keepMine writes the local version, else the server's wins. */
export async function resolveConflict(keepMine: boolean): Promise<void> {
  const conflict = get(taskConflict)
  if (!conflict) {
    return
  }
  try {
    replaceTask(await resolveTaskConflict(conflict.task.id, keepMine))
    taskConflict.set(null)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

let started = false

/**
 * Loads the tasks and keeps them current: a change anywhere reloads them, and
 * a clock ticks each minute so what is due today moves on at midnight.
 * Returns the function that stops it.
 */
export function startTasks(): Unsubscribe {
  if (started) {
    return () => undefined
  }
  started = true
  void loadTaskSettings()
  void loadTasks()
  const off = onCalendarChanged(() => {
    void loadTasks()
  })
  const clock = setInterval(() => taskClock.set(new Date()), 60000)
  return () => {
    started = false
    off()
    clearInterval(clock)
  }
}
