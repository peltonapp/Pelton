// tasklogic.ts is what the task screen, the calendar and the rail badge agree
// on: when a task is due, which smart list it falls in, how a list is ordered
// and nested, and what the badge counts. everything here is pure, so the
// Today list and the calendar's day can never disagree about what is due.

import { addDays, dayKey, parseDayKey, startOfDay } from './caldate'
import type { Task, TaskBadge, TaskSort } from './types'

/** The smart lists every install has. */
export type SmartList = 'today' | 'upcoming' | 'overdue' | 'nodate' | 'completed'

/** The smart lists in sidebar order. */
export const smartLists: SmartList[] = ['today', 'upcoming', 'overdue', 'nodate', 'completed']

/** What the task list shows: a smart list or one task list. */
export type TaskScope = { kind: 'smart'; key: SmartList } | { kind: 'list'; id: number }

/** True when the due value carries a time of day. */
export function dueHasTime(due: string): boolean {
  return due.length > 10
}

/** When a task is due as a local Date: midnight for a date, null for none. */
export function dueDate(task: Pick<Task, 'due'>): Date | null {
  if (!task.due) {
    return null
  }
  return dueHasTime(task.due) ? new Date(task.due) : parseDayKey(task.due)
}

/** The local day a task is due on as yyyy-mm-dd, or '' for none. */
export function dueDay(task: Pick<Task, 'due'>): string {
  const d = dueDate(task)
  return d ? dayKey(d) : ''
}

/** An open task whose due date or time has passed. A date is overdue from the next day on. */
export function isOverdue(task: Pick<Task, 'due' | 'done'>, now: Date): boolean {
  const d = dueDate(task)
  if (task.done || !d) {
    return false
  }
  return dueHasTime(task.due) ? d < now : d < startOfDay(now)
}

/** An open task due on the day now falls on. */
export function isDueToday(task: Pick<Task, 'due' | 'done'>, now: Date): boolean {
  return !task.done && dueDay(task) === dayKey(now)
}

/**
 * Whether a task belongs in a smart list. Today holds what is due today,
 * Overdue what is past, Upcoming what is due after today, No date the open
 * tasks without one and Completed the done ones.
 */
export function inSmartList(task: Pick<Task, 'due' | 'done'>, key: SmartList, now: Date): boolean {
  switch (key) {
    case 'today':
      return isDueToday(task, now)
    case 'overdue':
      return isOverdue(task, now)
    case 'upcoming':
      return !task.done && task.due !== '' && dueDay(task) > dayKey(now)
    case 'nodate':
      return !task.done && task.due === ''
    case 'completed':
      return task.done
  }
}

/** The tasks a scope shows. Completed tasks show only in Completed and in their list. */
export function tasksInScope(tasks: Task[], scope: TaskScope, now: Date): Task[] {
  if (scope.kind === 'list') {
    return tasks.filter((t) => t.listId === scope.id)
  }
  return tasks.filter((t) => inSmartList(t, scope.key, now))
}

// a priority's place when sorting: 1 first, 9 last, none after that.
function priorityRank(p: number): number {
  return p > 0 ? p : 10
}

function dueTime(t: Task): number {
  return dueDate(t)?.getTime() ?? Number.POSITIVE_INFINITY
}

/** Compares two tasks for a sort order. */
export function compareTasks(a: Task, b: Task, sort: TaskSort): number {
  const byDue = (): number => dueTime(a) - dueTime(b) || 0
  const byPriority = (): number => priorityRank(a.priority) - priorityRank(b.priority)
  const byManual = (): number => (a.sortOrder || Number.MAX_SAFE_INTEGER) - (b.sortOrder || Number.MAX_SAFE_INTEGER)
  let out = 0
  switch (sort) {
    case 'due':
      out = byDue() || byPriority()
      break
    case 'priority':
      out = byPriority() || byDue()
      break
    case 'title':
      out = a.title.localeCompare(b.title)
      break
    case 'created':
      out = (b.created || '').localeCompare(a.created || '')
      break
    case 'manual':
      out = byManual()
      break
  }
  return out || a.id - b.id
}

/** One line of a nested list: a task and how deep it sits. */
export interface TaskRow {
  task: Task
  depth: number
  /** How many subtasks it has among the tasks listed. */
  children: number
}

/**
 * Orders tasks and nests subtasks under their parent where the parent is in
 * the same set; a subtask whose parent is not shown stands on its own. Done
 * tasks sink below open ones at each level, except in the Completed list.
 */
export function taskRows(tasks: Task[], sort: TaskSort, doneLast = true): TaskRow[] {
  const byKey = new Map(tasks.map((t) => [`${t.listId}|${t.uid}`, t]))
  const kids = new Map<string, Task[]>()
  const roots: Task[] = []
  for (const t of tasks) {
    const parentKey = `${t.listId}|${t.parent}`
    if (t.parent && byKey.has(parentKey) && t.parent !== t.uid) {
      kids.set(parentKey, [...(kids.get(parentKey) ?? []), t])
    } else {
      roots.push(t)
    }
  }
  const order = (list: Task[]): Task[] =>
    [...list].sort((a, b) => (doneLast ? Number(a.done) - Number(b.done) : 0) || compareTasks(a, b, sort))
  const out: TaskRow[] = []
  const seen = new Set<number>()
  const walk = (list: Task[], depth: number): void => {
    for (const t of order(list)) {
      if (seen.has(t.id)) {
        continue
      }
      seen.add(t.id)
      const children = kids.get(`${t.listId}|${t.uid}`) ?? []
      out.push({ task: t, depth, children: children.length })
      walk(children, depth + 1)
    }
  }
  walk(roots, 0)
  // a cycle of parents leaves its members out of the walk; list them flat.
  for (const t of order(tasks)) {
    if (!seen.has(t.id)) {
      out.push({ task: t, depth: 0, children: 0 })
    }
  }
  return out
}

/** What the rail badge shows for the setting: overdue tasks, or those due today or before. */
export function badgeCount(tasks: Task[], mode: TaskBadge, now: Date): number {
  if (mode === 'off') {
    return 0
  }
  return tasks.filter((t) => isOverdue(t, now) || (mode === 'today' && isDueToday(t, now))).length
}

/** The tasks due on a local day, done ones included, for the calendar. */
export function tasksOnDay(tasks: Task[], day: Date): Task[] {
  const key = dayKey(day)
  return tasks.filter((t) => dueDay(t) === key)
}

/**
 * Where the calendar draws a task: across its due day for a date, or as a
 * short block ending at its due time for one with a time.
 */
export function taskSpan(task: Pick<Task, 'due'>): { start: Date; end: Date; allDay: boolean } | null {
  const d = dueDate(task)
  if (!d) {
    return null
  }
  if (!dueHasTime(task.due)) {
    return { start: d, end: addDays(d, 1), allDay: true }
  }
  return { start: d, end: new Date(d.getTime() + 30 * 60000), allDay: false }
}

/** The tasks linked to a person, by address. */
export function tasksForContact(tasks: Task[], email: string): Task[] {
  const want = email.trim().toLowerCase()
  return want ? tasks.filter((t) => t.contacts.some((c) => c.email.toLowerCase() === want)) : []
}

/** A due value moved to another day, keeping a time of day it has. */
export function moveDue(due: string, day: Date): string {
  if (!dueHasTime(due)) {
    return dayKey(day)
  }
  const d = new Date(due)
  const moved = new Date(day.getFullYear(), day.getMonth(), day.getDate(), d.getHours(), d.getMinutes())
  return moved.toISOString()
}
