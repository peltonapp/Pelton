import { describe, expect, it } from 'vitest'
import {
  addressOf,
  boardLayout,
  cardMatches,
  cardPlace,
  columnCards,
  dropIndex,
  dueLabel,
  duePassed,
  emptyFilter,
  filterActive,
  focusTarget,
  keyboardMove,
  moveCard,
  nextDeadlines,
  priorityLevel,
  progressOf,
  sortCards,
  suggestProjects,
  visibleProjects,
  wipState,
} from './projectlogic'
import type { Project, ProjectCard, ProjectColumn, Task } from './types'

// Monday 16 November 2026, 10:00.
const now = new Date(2026, 10, 16, 10, 0)

function task(uid: string, over: Partial<Task> = {}): Task {
  return {
    id: uid.charCodeAt(0),
    listId: 1,
    listName: '',
    listColor: '',
    uid,
    title: uid.toUpperCase(),
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

function card(uid: string, columnId: number, position: number, over: Partial<Task> = {}): ProjectCard {
  return { task: task(uid, over), projectId: 1, columnId, position, mail: 0, events: 0, notes: 0, sourceKind: '', sourceRef: '' }
}

function column(id: number, over: Partial<ProjectColumn> = {}): ProjectColumn {
  return { id, projectId: 1, name: `C${id}`, position: id, wipLimit: 0, done: false, ...over }
}

function project(id: number, over: Partial<Project> = {}): Project {
  return {
    id,
    uid: `p${id}`,
    name: `P${id}`,
    color: '',
    icon: '',
    description: '',
    status: 'active',
    startDate: '',
    dueDate: '',
    position: id,
    listId: 0,
    createdAt: '',
    updatedAt: '',
    archivedAt: '',
    cards: 0,
    done: 0,
    overdue: 0,
    nextDue: '',
    mail: 0,
    events: 0,
    notes: 0,
    people: [],
    ...over,
  }
}

const uids = (cards: ProjectCard[]): string[] => cards.map((c) => c.task.uid)

describe('moving cards', () => {
  const board = [card('a', 1, 0), card('b', 1, 1), card('c', 1, 2), card('x', 2, 0)]

  it('keeps each column in manual order', () => {
    expect(uids(columnCards([...board].reverse(), 1))).toEqual(['a', 'b', 'c'])
  })

  it('moves within a column and renumbers it', () => {
    const moved = moveCard(board, 'c', 1, 0)
    expect(uids(columnCards(moved, 1))).toEqual(['c', 'a', 'b'])
    expect(columnCards(moved, 1).map((c) => c.position)).toEqual([0, 1, 2])
  })

  it('moves across columns and closes the gap it leaves', () => {
    const moved = moveCard(board, 'a', 2, 5)
    expect(uids(columnCards(moved, 2))).toEqual(['x', 'a'])
    expect(columnCards(moved, 1).map((c) => [c.task.uid, c.position])).toEqual([
      ['b', 0],
      ['c', 1],
    ])
    expect(cardPlace(moved, 'a')).toEqual({ columnId: 2, index: 1 })
  })

  it('leaves the board alone for a card it does not have', () => {
    expect(moveCard(board, 'nope', 2, 0)).toBe(board)
    expect(cardPlace(board, 'nope')).toBeNull()
  })

  it('moves by keyboard one place or one column at a time', () => {
    const cols = [1, 2, 3]
    expect(keyboardMove(cols, board, 'b', 'ArrowUp')).toEqual({ columnId: 1, index: 0 })
    expect(keyboardMove(cols, board, 'a', 'ArrowUp')).toBeNull()
    expect(keyboardMove(cols, board, 'c', 'ArrowDown')).toBeNull()
    expect(keyboardMove(cols, board, 'c', 'ArrowRight')).toEqual({ columnId: 2, index: 1 })
    expect(keyboardMove(cols, board, 'x', 'ArrowRight')).toEqual({ columnId: 3, index: 0 })
    expect(keyboardMove(cols, board, 'a', 'ArrowLeft')).toBeNull()
    expect(keyboardMove(cols, board, 'a', 'ArrowLeft', true)).toEqual({ columnId: 2, index: 0 })
  })

  it('finds where a dragged card drops from the cards it passes', () => {
    expect(dropIndex([10, 30, 50], 5)).toBe(0)
    expect(dropIndex([10, 30, 50], 31)).toBe(2)
    expect(dropIndex([10, 30, 50], 99)).toBe(3)
    expect(dropIndex([], 99)).toBe(0)
  })
})

describe('moving focus', () => {
  const cards = [card('a', 1, 0), card('b', 1, 1), card('c', 1, 2), card('x', 3, 0)]
  const layout = boardLayout([column(1), column(2), column(3)], cards, emptyFilter, 'manual', now)
  const cols = [1, 2, 3]

  it('walks a column and jumps to its ends', () => {
    expect(focusTarget(cols, layout, 'a', 'ArrowDown')).toBe('b')
    expect(focusTarget(cols, layout, 'b', 'ArrowUp')).toBe('a')
    expect(focusTarget(cols, layout, 'a', 'ArrowUp')).toBeNull()
    expect(focusTarget(cols, layout, 'a', 'End')).toBe('c')
    expect(focusTarget(cols, layout, 'c', 'Home')).toBe('a')
  })

  it('skips empty columns and keeps about the same height', () => {
    expect(focusTarget(cols, layout, 'c', 'ArrowRight')).toBe('x')
    expect(focusTarget(cols, layout, 'x', 'ArrowLeft')).toBe('a')
    expect(focusTarget(cols, layout, 'x', 'ArrowRight')).toBeNull()
    expect(focusTarget(cols, layout, 'c', 'ArrowLeft', true)).toBe('x')
  })
})

describe('filtering and sorting', () => {
  const overdue = card('o', 1, 0, { due: '2026-11-10', priority: 1, title: 'Send invoice', contacts: [{ name: 'Ada', email: 'Ada@Example.com' }] })
  const soon = card('s', 1, 1, { due: '2026-11-18', priority: 5, categories: ['design'] })
  const later = card('l', 1, 2, { due: '2026-12-30', priority: 9 })
  const none = card('n', 1, 3)
  const doneLate = card('d', 1, 4, { due: '2026-11-01', done: true })
  const all = [overdue, soon, later, none, doneLate]

  it('narrows by due date', () => {
    const due = (d: 'overdue' | 'week' | 'none') => all.filter((c) => cardMatches(c, { ...emptyFilter, due: d }, now)).map((c) => c.task.uid)
    expect(due('overdue')).toEqual(['o'])
    expect(due('week')).toEqual(['o', 's'])
    expect(due('none')).toEqual(['n'])
  })

  it('narrows by priority, person and words', () => {
    expect(all.filter((c) => cardMatches(c, { ...emptyFilter, highPriority: true }, now)).map((c) => c.task.uid)).toEqual(['o'])
    expect(all.filter((c) => cardMatches(c, { ...emptyFilter, person: 'ada@example.com' }, now)).map((c) => c.task.uid)).toEqual(['o'])
    expect(all.filter((c) => cardMatches(c, { ...emptyFilter, text: 'INVOICE ada' }, now)).map((c) => c.task.uid)).toEqual(['o'])
    expect(all.filter((c) => cardMatches(c, { ...emptyFilter, text: 'design' }, now)).map((c) => c.task.uid)).toEqual(['s'])
    expect(filterActive(emptyFilter)).toBe(false)
    expect(filterActive({ ...emptyFilter, text: ' x ' })).toBe(true)
  })

  it('sorts by due date, priority and title, and keeps board order for manual', () => {
    const shuffled = [none, later, soon, overdue]
    expect(uids(sortCards(shuffled, 'manual'))).toEqual(['o', 's', 'l', 'n'])
    expect(uids(sortCards(shuffled, 'due'))).toEqual(['o', 's', 'l', 'n'])
    expect(uids(sortCards(shuffled, 'priority'))[0]).toBe('o')
    expect(uids(sortCards(shuffled, 'title'))).toEqual(['l', 'n', 's', 'o'])
  })

  it('lays the board out per column', () => {
    const layout = boardLayout([column(1), column(2)], [...all, card('z', 2, 0)], { ...emptyFilter, due: 'none' }, 'manual', now)
    expect(uids(layout.get(1) ?? [])).toEqual(['n'])
    expect(uids(layout.get(2) ?? [])).toEqual(['z'])
  })

  it('lists the next deadlines of open cards, soonest first', () => {
    expect(uids(nextDeadlines(all, 2))).toEqual(['o', 's'])
  })
})

describe('counts and labels', () => {
  it('reads a WIP limit', () => {
    expect(wipState(column(1), 9)).toBe('none')
    expect(wipState(column(1, { wipLimit: 3 }), 2)).toBe('ok')
    expect(wipState(column(1, { wipLimit: 3 }), 3)).toBe('full')
    expect(wipState(column(1, { wipLimit: 3 }), 4)).toBe('over')
  })

  it('computes progress without dividing by zero', () => {
    expect(progressOf({ cards: 0, done: 0 })).toBe(0)
    expect(progressOf({ cards: 4, done: 1 })).toBe(0.25)
  })

  it('names priorities', () => {
    expect([0, 1, 4, 5, 6, 9].map(priorityLevel)).toEqual(['none', 'high', 'high', 'medium', 'low', 'low'])
  })

  it('says due dates the way the task list does', () => {
    const tr = (key: string) => ({ 'tasks.due.today': 'Today', 'tasks.due.tomorrow': 'Tomorrow', 'tasks.due.yesterday': 'Yesterday' })[key] ?? key
    expect(dueLabel('', now, tr)).toBe('')
    expect(dueLabel('2026-11-16', now, tr)).toBe('Today')
    expect(dueLabel('2026-11-17', now, tr)).toBe('Tomorrow')
    expect(dueLabel('2026-11-15', now, tr)).toBe('Yesterday')
    expect(duePassed('2026-11-15', now)).toBe(true)
    expect(duePassed('2026-11-16', now)).toBe(false)
    expect(duePassed('', now)).toBe(false)
  })
})

describe('listing projects', () => {
  const projects = [
    project(1, { name: 'Website relaunch', description: 'new site', people: [{ email: 'ada@example.com', name: 'Ada Lovelace' }] }),
    project(2, { name: 'Taxes', status: 'archived' }),
    project(3, { name: 'Garden' }),
  ]

  it('shows active or archived projects matching the filter', () => {
    expect(visibleProjects(projects, '', false).map((p) => p.id)).toEqual([1, 3])
    expect(visibleProjects(projects, '', true).map((p) => p.id)).toEqual([2])
    expect(visibleProjects(projects, 'lovelace', false).map((p) => p.id)).toEqual([1])
    expect(visibleProjects(projects, 'site NEW', false).map((p) => p.id)).toEqual([1])
  })
})

describe('suggestions', () => {
  const projects = [
    project(1, { people: [{ email: 'ada@example.com', name: 'Ada' }] }),
    project(2, { people: [{ email: 'ADA@example.com', name: '' }], status: 'archived' }),
    project(3, { people: [{ email: 'ada@example.com', name: '' }, { email: 'bob@example.com', name: '' }] }),
  ]

  it('reads a bare address out of a sender', () => {
    expect(addressOf('Ada Lovelace <Ada@Example.com>')).toBe('ada@example.com')
    expect(addressOf(' bob@x ')).toBe('bob@x')
  })

  it('offers the active projects the sender belongs to', () => {
    expect(suggestProjects('Ada <ada@EXAMPLE.com>', 7, projects, new Map(), new Set()).map((p) => p.id)).toEqual([1, 3])
    expect(suggestProjects('bob@example.com', 7, projects, new Map(), new Set()).map((p) => p.id)).toEqual([3])
    expect(suggestProjects('carol@example.com', 7, projects, new Map(), new Set())).toEqual([])
  })

  it('stays quiet for filed, dismissed and unthreaded conversations', () => {
    expect(suggestProjects('ada@example.com', 7, projects, new Map([[7, [3]]]), new Set())).toEqual([])
    expect(suggestProjects('ada@example.com', 7, projects, new Map([[7, []]]), new Set()).length).toBe(2)
    expect(suggestProjects('ada@example.com', 7, projects, new Map(), new Set([7]))).toEqual([])
    expect(suggestProjects('ada@example.com', 0, projects, new Map(), new Set())).toEqual([])
  })
})
