// myday.ts picks what the My Day panel lists for a day: on today, the open
// tasks that are overdue and everything due today; on another day, what is
// due then. It is pure, so the panel and its tests agree on the order.

import { dayKey, sameDay } from './caldate'
import { dueDate, dueDay, dueHasTime, isOverdue } from './tasklogic'
import type { Task } from './types'

// the narrowest the reader may get with the panel beside it, in css pixels.
const myDayMinReader = 440

/**
 * Whether the panel fits beside the mail: what is left for the reader once
 * the folder pane, the list (beside the reader, not above it) and the panel
 * have their widths must stay readable. A width not measured yet fits.
 */
export function myDayFits(mail: number, sidebar: number, list: number, stacked: boolean, panel: number): boolean {
  return mail === 0 || mail - sidebar - (stacked ? 0 : list) - panel >= myDayMinReader
}

/** A task as the panel lists it, marked when it is overdue. */
export interface MyDayTask {
  task: Task
  overdue: boolean
}

// due order: earlier first, a date without a time after the timed ones of
// that day, then by title.
function byDue(a: Task, b: Task): number {
  const da = dueDate(a)?.getTime() ?? 0
  const db = dueDate(b)?.getTime() ?? 0
  if (dueDay(a) !== dueDay(b)) {
    return da - db
  }
  const ta = dueHasTime(a.due) ? da : Number.POSITIVE_INFINITY
  const tb = dueHasTime(b.due) ? db : Number.POSITIVE_INFINITY
  return ta - tb || a.title.localeCompare(b.title) || a.id - b.id
}

/**
 * The tasks of a day. On today: the overdue ones first, then what is due
 * today with the done ones last. On another day: what is due that day. A
 * subtask is listed like any other task.
 */
export function myDayTasks(tasks: Task[], day: Date, now: Date): MyDayTask[] {
  const key = dayKey(day)
  const today = sameDay(day, now)
  const overdue = today ? tasks.filter((t) => isOverdue(t, now) && dueDay(t) !== key).sort(byDue) : []
  const due = tasks.filter((t) => dueDay(t) === key).sort((a, b) => Number(a.done) - Number(b.done) || byDue(a, b))
  return [...overdue.map((task) => ({ task, overdue: true })), ...due.map((task) => ({ task, overdue: today && isOverdue(task, now) }))]
}
