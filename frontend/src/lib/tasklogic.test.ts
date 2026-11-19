import { describe, expect, it } from 'vitest'
import { badgeCount, compareTasks, dueDay, inSmartList, isOverdue, moveDue, taskRows, taskSpan, tasksForContact, tasksInScope, tasksOnDay } from './tasklogic'
import type { Task } from './types'

let nextId = 1
function task(over: Partial<Task> = {}): Task {
  const id = nextId++
  return {
    id,
    listId: 1,
    listName: '',
    listColor: '',
    uid: `u${id}`,
    title: `Task ${id}`,
    notes: '',
    url: '',
    due: '',
    start: '',
    timeZone: '',
    done: false,
    completed: '',
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
    created: '',
    readOnly: false,
    pending: false,
    conflict: false,
    syncError: '',
    ...over,
  }
}

const now = new Date(2026, 10, 15, 12, 0)

describe('due dates', () => {
  it('reads dates and times in local time', () => {
    expect(dueDay(task({ due: '2026-11-15' }))).toBe('2026-11-15')
    expect(dueDay(task({ due: new Date(2026, 10, 16, 9, 0).toISOString() }))).toBe('2026-11-16')
    expect(dueDay(task())).toBe('')
  })

  it('makes a date overdue only from the next day on, a time once it passes', () => {
    expect(isOverdue(task({ due: '2026-11-15' }), now)).toBe(false)
    expect(isOverdue(task({ due: '2026-11-14' }), now)).toBe(true)
    expect(isOverdue(task({ due: new Date(2026, 10, 15, 11, 0).toISOString() }), now)).toBe(true)
    expect(isOverdue(task({ due: new Date(2026, 10, 15, 13, 0).toISOString() }), now)).toBe(false)
    expect(isOverdue(task({ due: '2026-11-14', done: true }), now)).toBe(false)
  })
})

describe('smart lists', () => {
  const tasks = [
    task({ title: 'today', due: '2026-11-15' }),
    task({ title: 'later', due: '2026-11-20' }),
    task({ title: 'past', due: '2026-11-01' }),
    task({ title: 'none' }),
    task({ title: 'done', due: '2026-11-15', done: true }),
  ]
  const titles = (key: Parameters<typeof inSmartList>[1]): string[] => tasks.filter((t) => inSmartList(t, key, now)).map((t) => t.title)

  it('sorts each task into one list', () => {
    expect(titles('today')).toEqual(['today'])
    expect(titles('upcoming')).toEqual(['later'])
    expect(titles('overdue')).toEqual(['past'])
    expect(titles('nodate')).toEqual(['none'])
    expect(titles('completed')).toEqual(['done'])
  })

  it('agrees with the calendar day about today', () => {
    const onDay = tasksOnDay(tasks, now).filter((t) => !t.done).map((t) => t.title)
    expect(onDay).toEqual(tasksInScope(tasks, { kind: 'smart', key: 'today' }, now).map((t) => t.title))
  })

  it('counts the badge by the setting', () => {
    expect(badgeCount(tasks, 'today', now)).toBe(2)
    expect(badgeCount(tasks, 'overdue', now)).toBe(1)
    expect(badgeCount(tasks, 'off', now)).toBe(0)
  })
})

describe('order and nesting', () => {
  it('sorts by due, priority, title and manual order', () => {
    const a = task({ title: 'b', due: '2026-11-20', priority: 9, sortOrder: 20 })
    const b = task({ title: 'a', due: '2026-11-16', priority: 1, sortOrder: 10 })
    const c = task({ title: 'c' })
    expect([a, b, c].sort((x, y) => compareTasks(x, y, 'due')).map((t) => t.title)).toEqual(['a', 'b', 'c'])
    expect([c, a, b].sort((x, y) => compareTasks(x, y, 'priority')).map((t) => t.title)).toEqual(['a', 'b', 'c'])
    expect([c, a, b].sort((x, y) => compareTasks(x, y, 'title')).map((t) => t.title)).toEqual(['a', 'b', 'c'])
    expect([c, a, b].sort((x, y) => compareTasks(x, y, 'manual')).map((t) => t.title)).toEqual(['a', 'b', 'c'])
  })

  it('nests subtasks under their parent and sinks done tasks', () => {
    const parent = task({ title: 'parent', sortOrder: 10 })
    const child = task({ title: 'child', parent: parent.uid })
    const done = task({ title: 'done', done: true, sortOrder: 1 })
    const orphan = task({ title: 'orphan', parent: 'missing', sortOrder: 20 })
    const rows = taskRows([child, done, orphan, parent], 'manual')
    expect(rows.map((r) => [r.task.title, r.depth])).toEqual([
      ['parent', 0],
      ['child', 1],
      ['orphan', 0],
      ['done', 0],
    ])
    expect(rows[0].children).toBe(1)
  })

  it('survives a cycle of parents', () => {
    const a = task({ title: 'a' })
    const b = task({ title: 'b', parent: a.uid })
    a.parent = b.uid
    expect(taskRows([a, b], 'title').map((r) => r.task.title).sort()).toEqual(['a', 'b'])
  })
})

describe('calendar helpers', () => {
  it('spans a date over its day and a time as a short block', () => {
    expect(taskSpan(task({ due: '2026-11-15' }))?.allDay).toBe(true)
    const timed = taskSpan(task({ due: new Date(2026, 10, 15, 9, 0).toISOString() }))
    expect(timed?.allDay).toBe(false)
    expect(timed?.start.getHours()).toBe(9)
    expect(taskSpan(task())).toBeNull()
  })

  it('moves a due date and keeps the time', () => {
    expect(moveDue('2026-11-15', new Date(2026, 10, 18))).toBe('2026-11-18')
    const moved = new Date(moveDue(new Date(2026, 10, 15, 9, 30).toISOString(), new Date(2026, 10, 18)))
    expect([moved.getDate(), moved.getHours(), moved.getMinutes()]).toEqual([18, 9, 30])
  })

  it('finds the tasks of a person', () => {
    const t = task({ contacts: [{ name: 'Ada', email: 'Ada@Example.com' }] })
    expect(tasksForContact([t, task()], 'ada@example.com')).toEqual([t])
  })

  it('lists the open tasks of accepted promises by who owes them', () => {
    const now = new Date(2026, 10, 18, 10)
    const mine = task({ due: '2026-11-17' })
    const theirs = task()
    const closed = task({ done: true })
    const plain = task()
    const owed = new Map<string, 'mine' | 'theirs'>([
      [mine.uid, 'mine'],
      [theirs.uid, 'theirs'],
      [closed.uid, 'theirs'],
    ])
    const all = [mine, theirs, closed, plain]
    expect(tasksInScope(all, { kind: 'smart', key: 'iowe' }, now, owed)).toEqual([mine])
    expect(tasksInScope(all, { kind: 'smart', key: 'owedtome' }, now, owed)).toEqual([theirs])
    expect(tasksInScope(all, { kind: 'smart', key: 'iowe' }, now)).toEqual([])
    expect(inSmartList(mine, 'overdue', now, owed)).toBe(true)
  })
})
