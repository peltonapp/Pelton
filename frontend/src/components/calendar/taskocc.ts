// taskocc.ts turns the tasks due in a window into entries the calendar views
// place like events: across their day for a date, as a short block at their
// time otherwise. each carries its task, which the views draw with a checkbox
// and the screen acts on instead of opening an event.

import { dayKey, toRFC3339 } from '../../lib/caldate'
import { calendarColor } from '../../lib/calformat'
import { taskSpan } from '../../lib/tasklogic'
import type { Occurrence, Task, TaskList } from '../../lib/types'

/** The calendar entries of the tasks due in [from, to). */
export function taskOccurrences(tasks: Task[], lists: TaskList[], from: Date, to: Date): Occurrence[] {
  const byId = new Map(lists.map((l) => [l.id, l]))
  const out: Occurrence[] = []
  for (const task of tasks) {
    const span = taskSpan(task)
    if (!span || span.start >= to || span.end <= from) {
      continue
    }
    const list = byId.get(task.listId)
    out.push({
      eventId: -task.id,
      calendarId: task.listId,
      calendarColor: list ? calendarColor(list) : calendarColor({ id: task.listId, color: task.listColor }),
      uid: task.uid,
      summary: task.title,
      location: '',
      start: span.allDay ? dayKey(span.start) : toRFC3339(span.start),
      end: span.allDay ? dayKey(span.end) : toRFC3339(span.end),
      allDay: span.allDay,
      recurring: false,
      recurrenceId: '',
      override: false,
      status: task.done ? 'COMPLETED' : '',
      transparency: 'TRANSPARENT',
      readOnly: task.readOnly,
      hasReminders: task.reminders.length > 0,
      pending: task.pending,
      conflict: task.conflict,
      task,
    })
  }
  return out
}
