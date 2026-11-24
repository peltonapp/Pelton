// projectlogic.ts is the store-free part of the Projects space: ordering
// cards in their columns, moving a card, filtering and sorting a board,
// moving the keyboard between cards, the counts a tile shows and which
// project a mail could belong to. it imports no stores, so it can be tested
// and used anywhere.

import { compareTasks, dueDate, dueHasTime, isOverdue } from './tasklogic'
import { calendarColor, formatTime } from './calformat'
import { daysBetween } from './caldate'
import type { TimeFormat } from './format'
import type { Project, ProjectCard, ProjectColumn, ProjectPerson } from './types'

/**
 * A due date as a card or a tile says it: today, tomorrow, yesterday, a
 * weekday within the week, else the date, with the time when it has one.
 * due is a date or rfc3339; empty gives ''.
 */
export function dueLabel(due: string, now: Date, tr: (key: string) => string, timeFormat: TimeFormat = 'auto'): string {
  const d = dueDate({ due })
  if (!d) {
    return ''
  }
  const days = daysBetween(now, d)
  let day: string
  if (days === 0) {
    day = tr('tasks.due.today')
  } else if (days === 1) {
    day = tr('tasks.due.tomorrow')
  } else if (days === -1) {
    day = tr('tasks.due.yesterday')
  } else if (days > 1 && days < 7) {
    day = d.toLocaleDateString(undefined, { weekday: 'long' })
  } else {
    day = d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric' })
  }
  return dueHasTime(due) ? `${day} ${formatTime(d, timeFormat)}` : day
}

/** Whether a due date, a date or rfc3339, has passed: a date from the next day on. */
export function duePassed(due: string, now: Date): boolean {
  return due !== '' && isOverdue({ due, done: false }, now)
}

/** How the cards of a column are ordered. Manual is the order they were put in. */
export type BoardSort = 'manual' | 'due' | 'priority' | 'title'

/** The board sorts, in menu order. */
export const boardSorts: BoardSort[] = ['manual', 'due', 'priority', 'title']

/** Which due dates the board shows. */
export type DueFilter = 'any' | 'overdue' | 'week' | 'none'

/** The due filters, in menu order. */
export const dueFilters: DueFilter[] = ['any', 'overdue', 'week', 'none']

/** What the board is narrowed to. person is a lower-case address, '' for anyone. */
export interface BoardFilter {
  text: string
  due: DueFilter
  highPriority: boolean
  person: string
}

/** The filter that shows every card. */
export const emptyFilter: BoardFilter = { text: '', due: 'any', highPriority: false, person: '' }

/** True when a filter narrows the board at all. */
export function filterActive(f: BoardFilter): boolean {
  return f.text.trim() !== '' || f.due !== 'any' || f.highPriority || f.person !== ''
}

/** The cards of one column in their manual order. */
export function columnCards(cards: ProjectCard[], columnId: number): ProjectCard[] {
  return cards.filter((c) => c.columnId === columnId).sort((a, b) => a.position - b.position)
}

/**
 * Moves a card to index in a column, as the backend does: it leaves its old
 * place, the column it lands in is renumbered from 0, and so is the one it
 * left. An index past the end appends. The other cards are returned as they
 * were.
 */
export function moveCard(cards: ProjectCard[], uid: string, columnId: number, index: number): ProjectCard[] {
  const card = cards.find((c) => c.task.uid === uid)
  if (!card) {
    return cards
  }
  const from = card.columnId
  const target = columnCards(cards, columnId).filter((c) => c.task.uid !== uid)
  const at = Math.min(Math.max(index, 0), target.length)
  target.splice(at, 0, { ...card, columnId })
  const positions = new Map<string, { columnId: number; position: number }>()
  target.forEach((c, i) => positions.set(c.task.uid, { columnId, position: i }))
  if (from !== columnId) {
    columnCards(cards, from)
      .filter((c) => c.task.uid !== uid)
      .forEach((c, i) => positions.set(c.task.uid, { columnId: from, position: i }))
  }
  return cards.map((c) => {
    const place = positions.get(c.task.uid)
    return place ? { ...c, ...place } : c
  })
}

/** Where a card sits now: its column and its index there in manual order. */
export function cardPlace(cards: ProjectCard[], uid: string): { columnId: number; index: number } | null {
  const card = cards.find((c) => c.task.uid === uid)
  if (!card) {
    return null
  }
  return { columnId: card.columnId, index: columnCards(cards, card.columnId).findIndex((c) => c.task.uid === uid) }
}

/** True for the priorities that count as high: 1 to 4 of iCalendar's 1 to 9. */
export function highPriority(priority: number): boolean {
  return priority >= 1 && priority <= 4
}

/** The level a priority shows as. */
export function priorityLevel(priority: number): 'high' | 'medium' | 'low' | 'none' {
  if (priority <= 0) {
    return 'none'
  }
  if (priority <= 4) {
    return 'high'
  }
  return priority === 5 ? 'medium' : 'low'
}

/** Whether a card passes a filter at a moment. */
export function cardMatches(card: ProjectCard, f: BoardFilter, now: Date): boolean {
  const task = card.task
  if (f.highPriority && !highPriority(task.priority)) {
    return false
  }
  if (f.person && !task.contacts.some((c) => c.email.toLowerCase() === f.person)) {
    return false
  }
  if (f.due !== 'any') {
    const due = dueDate(task)
    if (f.due === 'none' && due) {
      return false
    }
    if (f.due === 'overdue' && !isOverdue(task, now)) {
      return false
    }
    if (f.due === 'week') {
      const end = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 7)
      if (!due || task.done || due.getTime() >= end.getTime()) {
        return false
      }
    }
  }
  const words = f.text.toLocaleLowerCase().split(/\s+/).filter(Boolean)
  if (words.length) {
    const hay = [task.title, task.notes, ...task.categories, ...task.contacts.map((c) => `${c.name} ${c.email}`)].join('\n').toLocaleLowerCase()
    if (!words.every((w) => hay.includes(w))) {
      return false
    }
  }
  return true
}

/** Orders a column's cards. Manual keeps the board's own order. */
export function sortCards(cards: ProjectCard[], sort: BoardSort): ProjectCard[] {
  const out = [...cards]
  if (sort === 'manual') {
    return out.sort((a, b) => a.position - b.position)
  }
  return out.sort((a, b) => compareTasks(a.task, b.task, sort) || a.position - b.position)
}

/** The cards each column shows, filtered and sorted, by column id. */
export function boardLayout(columns: ProjectColumn[], cards: ProjectCard[], f: BoardFilter, sort: BoardSort, now: Date): Map<number, ProjectCard[]> {
  const out = new Map<number, ProjectCard[]>()
  for (const col of columns) {
    out.set(col.id, sortCards(cards.filter((c) => c.columnId === col.id && cardMatches(c, f, now)), sort))
  }
  return out
}

/** How full a column is against its WIP limit. */
export function wipState(column: Pick<ProjectColumn, 'wipLimit'>, count: number): 'none' | 'ok' | 'full' | 'over' {
  if (column.wipLimit <= 0) {
    return 'none'
  }
  if (count > column.wipLimit) {
    return 'over'
  }
  return count === column.wipLimit ? 'full' : 'ok'
}

/** How far along a project is, 0 to 1; 0 for one without cards. */
export function progressOf(p: Pick<Project, 'cards' | 'done'>): number {
  return p.cards > 0 ? Math.min(1, p.done / p.cards) : 0
}

/** The color a project is drawn in: its own, or the palette color its id picks. */
export function projectColor(p: Pick<Project, 'id' | 'color'>): string {
  return calendarColor(p)
}

/** The open cards with a due date, the soonest first, at most limit of them. */
export function nextDeadlines(cards: ProjectCard[], limit = 5): ProjectCard[] {
  return cards
    .filter((c) => !c.task.done && c.task.due)
    .sort((a, b) => (dueDate(a.task)?.getTime() ?? 0) - (dueDate(b.task)?.getTime() ?? 0))
    .slice(0, limit)
}

/** The projects to list: matching the query, archived ones only when asked for. */
export function visibleProjects(projects: Project[], query: string, archived: boolean): Project[] {
  const words = query.toLocaleLowerCase().split(/\s+/).filter(Boolean)
  return projects.filter((p) => {
    if ((p.status === 'archived') !== archived) {
      return false
    }
    if (!words.length) {
      return true
    }
    const hay = [p.name, p.description, ...p.people.map((x) => `${x.name} ${x.email}`)].join('\n').toLocaleLowerCase()
    return words.every((w) => hay.includes(w))
  })
}

/** The bare lower-case address of "Name <a@b>" or "a@b". */
export function addressOf(from: string): string {
  const m = /<([^>]+)>/.exec(from)
  return (m ? m[1] : from).trim().toLowerCase()
}

/**
 * The active projects a mail could be filed under: those with its sender among
 * their people. Nothing is suggested for a conversation already in a project,
 * one whose suggestion was dismissed, or one not threaded yet.
 */
export function suggestProjects(
  fromAddress: string,
  threadId: number,
  projects: Project[],
  marks: Map<number, number[]>,
  dismissed: Set<number>,
): Project[] {
  if (threadId <= 0 || dismissed.has(threadId) || (marks.get(threadId)?.length ?? 0) > 0) {
    return []
  }
  const from = addressOf(fromAddress)
  if (!from) {
    return []
  }
  return projects.filter((p) => p.status === 'active' && p.people.some((x: ProjectPerson) => x.email.toLowerCase() === from))
}

/** The arrow keys that move between cards. */
export type BoardKey = 'ArrowUp' | 'ArrowDown' | 'ArrowLeft' | 'ArrowRight' | 'Home' | 'End'

/**
 * The card the keyboard moves to from uid: up and down in its column, left
 * and right to the nearest card at the same height in the next column that
 * has any, Home and End to the ends of the column. rtl swaps left and right.
 * Returns null when there is nowhere to go.
 */
export function focusTarget(columns: number[], layout: Map<number, ProjectCard[]>, uid: string, key: BoardKey, rtl = false): string | null {
  const col = columns.findIndex((id) => layout.get(id)?.some((c) => c.task.uid === uid))
  if (col < 0) {
    return null
  }
  const list = layout.get(columns[col]) ?? []
  const index = list.findIndex((c) => c.task.uid === uid)
  switch (key) {
    case 'ArrowUp':
      return list[index - 1]?.task.uid ?? null
    case 'ArrowDown':
      return list[index + 1]?.task.uid ?? null
    case 'Home':
      return list[0]?.task.uid ?? null
    case 'End':
      return list[list.length - 1]?.task.uid ?? null
  }
  const step = (key === 'ArrowRight') !== rtl ? 1 : -1
  for (let i = col + step; i >= 0 && i < columns.length; i += step) {
    const next = layout.get(columns[i]) ?? []
    if (next.length) {
      return next[Math.min(index, next.length - 1)].task.uid
    }
  }
  return null
}

/**
 * Where a card goes when moved by keyboard, in manual order: up and down by
 * one place, left and right to the same place in the next column, which may
 * be empty. rtl swaps left and right. Null when it cannot go further.
 */
export function keyboardMove(columns: number[], cards: ProjectCard[], uid: string, key: 'ArrowUp' | 'ArrowDown' | 'ArrowLeft' | 'ArrowRight', rtl = false): { columnId: number; index: number } | null {
  const place = cardPlace(cards, uid)
  if (!place) {
    return null
  }
  const count = columnCards(cards, place.columnId).length
  if (key === 'ArrowUp') {
    return place.index > 0 ? { columnId: place.columnId, index: place.index - 1 } : null
  }
  if (key === 'ArrowDown') {
    return place.index < count - 1 ? { columnId: place.columnId, index: place.index + 1 } : null
  }
  const step = (key === 'ArrowRight') !== rtl ? 1 : -1
  const next = columns[columns.indexOf(place.columnId) + step]
  if (next === undefined) {
    return null
  }
  return { columnId: next, index: Math.min(place.index, columnCards(cards, next).length) }
}

/**
 * The index a dragged card would drop at in a column, from the vertical
 * middles of the cards there (the dragged one left out) and the pointer.
 */
export function dropIndex(middles: number[], y: number): number {
  const at = middles.findIndex((m) => y < m)
  return at < 0 ? middles.length : at
}
