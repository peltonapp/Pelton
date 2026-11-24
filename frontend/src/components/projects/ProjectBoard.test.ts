import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Project, ProjectBoard as Board, ProjectCard, Task } from '../../lib/types'

const api = vi.hoisted(() => ({
  moveProjectCard: vi.fn(),
  createProjectCard: vi.fn(),
  getProjectBoard: vi.fn(),
  listCardLinks: vi.fn(),
  listProjectNotes: vi.fn(),
  searchAddresses: vi.fn(),
  setTaskDone: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}
Element.prototype.getAnimations ??= function () {
  return []
}
Element.prototype.scrollIntoView ??= function () {}

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

import ProjectBoard from './ProjectBoard.svelte'
import { board, openProjectId } from '../../stores/projects'
import { expectAxeClean } from '../../test/axe'

const now = new Date(2026, 10, 16, 10, 0)

function task(uid: string, over: Partial<Task> = {}): Task {
  return {
    id: uid.charCodeAt(0),
    listId: 1,
    listName: '',
    listColor: '',
    uid,
    title: uid,
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
    estimate: 0,
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

const project: Project = {
  id: 1,
  uid: 'p1',
  name: 'Launch',
  color: '',
  icon: '',
  description: '',
  status: 'active',
  startDate: '',
  dueDate: '',
  position: 0,
  listId: 0,
  createdAt: '',
  updatedAt: '',
  archivedAt: '',
  cards: 3,
  done: 0,
  overdue: 0,
  nextDue: '',
  mail: 0,
  events: 0,
  notes: 0,
  people: [],
}

function sample(): Board {
  return {
    project,
    columns: [
      { id: 10, projectId: 1, name: 'To do', position: 0, wipLimit: 2, done: false },
      { id: 11, projectId: 1, name: 'Doing', position: 1, wipLimit: 0, done: false },
      { id: 12, projectId: 1, name: 'Done', position: 2, wipLimit: 0, done: true },
    ],
    cards: [card('Write copy', 10, 0, { priority: 1 }), card('Pick fonts', 10, 1, { due: '2026-11-10' }), card('Book venue', 11, 0)],
  }
}

function renderBoard(b: Board = sample()) {
  board.set(b)
  openProjectId.set(1)
  return render(ProjectBoard, { props: { project: b.project, columns: b.columns, cards: b.cards, now } })
}

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  api.listCardLinks.mockResolvedValue([])
  api.listProjectNotes.mockResolvedValue([])
})

describe('the board', () => {
  it('passes axe', async () => {
    renderBoard()
    await expectAxeClean()
  })

  it('shows the columns with their cards and counts', () => {
    renderBoard()
    const todo = screen.getByRole('list', { name: 'To do' })
    expect(within(todo).getByRole('article', { name: 'Write copy' })).toBeTruthy()
    expect(within(todo).getByRole('article', { name: 'Pick fonts' })).toBeTruthy()
    expect(within(screen.getByRole('list', { name: 'Doing' })).getByRole('article', { name: 'Book venue' })).toBeTruthy()
    expect(within(screen.getByRole('list', { name: 'Done' })).getByText('Drop cards here')).toBeTruthy()
    expect(screen.getByTitle('Limit 2').textContent).toContain('2')
  })

  it('moves the keyboard between cards with the arrows', async () => {
    const user = userEvent.setup()
    renderBoard()
    const first = screen.getByRole('article', { name: 'Write copy' })
    expect(first.getAttribute('tabindex')).toBe('0')
    first.focus()
    await user.keyboard('{ArrowDown}')
    await waitFor(() => expect(document.activeElement?.getAttribute('aria-label')).toBe('Pick fonts'))
    await user.keyboard('{ArrowRight}')
    await waitFor(() => expect(document.activeElement?.getAttribute('aria-label')).toBe('Book venue'))
  })

  it('carries a card to the next column with Alt and an arrow', async () => {
    const user = userEvent.setup()
    api.moveProjectCard.mockImplementation(async (uid: string, columnId: number, index: number) => ({ card: card(uid, columnId, index), advanced: false }))
    renderBoard()
    screen.getByRole('article', { name: 'Pick fonts' }).focus()
    await user.keyboard('{Alt>}{ArrowRight}{/Alt}')
    await waitFor(() => expect(api.moveProjectCard).toHaveBeenCalledWith('Pick fonts', 11, 1))
    screen.getByRole('article', { name: 'Write copy' }).focus()
    await user.keyboard('{Alt>}{ArrowLeft}{/Alt}')
    await user.keyboard('{Alt>}{ArrowUp}{/Alt}')
    expect(api.moveProjectCard).toHaveBeenCalledTimes(1)
  })

  it('adds a card at the bottom of a column', async () => {
    const user = userEvent.setup()
    api.createProjectCard.mockResolvedValue(card('Order banners', 11, 1))
    renderBoard()
    const doing = screen.getByRole('region', { name: 'Doing' })
    await user.click(within(doing).getByRole('button', { name: 'Add a card' }))
    await user.type(within(doing).getByRole('textbox', { name: 'Add a card to Doing' }), 'Order banners{Enter}')
    await waitFor(() => expect(api.createProjectCard).toHaveBeenCalled())
    expect(api.createProjectCard.mock.calls[0][0]).toMatchObject({ projectId: 1, columnId: 11, title: 'Order banners', index: -1 })
  })

  it('narrows the cards with the filter and the high priority switch', async () => {
    const user = userEvent.setup()
    renderBoard()
    await user.type(screen.getByRole('searchbox', { name: 'Filter cards' }), 'fonts')
    await waitFor(() => expect(screen.queryByRole('article', { name: 'Write copy' })).toBeNull())
    expect(screen.getByRole('article', { name: 'Pick fonts' })).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Clear filter' }))
    await user.click(screen.getByRole('button', { name: 'High priority' }))
    await waitFor(() => expect(screen.queryByRole('article', { name: 'Pick fonts' })).toBeNull())
    expect(screen.getByRole('article', { name: 'Write copy' })).toBeTruthy()
  })

  it('opens a card beside the board with what it holds', async () => {
    const user = userEvent.setup()
    api.listCardLinks.mockResolvedValue([{ id: 3, projectId: 1, taskUid: 'Book venue', kind: 'mail', accountId: 1, threadId: 4, ref: 'a@x', label: 'Venue offer', createdAt: '' }])
    renderBoard()
    await user.click(screen.getByRole('article', { name: 'Book venue' }))
    expect(await screen.findByText('Attached to this card')).toBeTruthy()
    expect(await screen.findByText('Venue offer')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove from project' })).toBeTruthy()
  })

  it('offers to add a column on an empty board', () => {
    renderBoard({ project, columns: [], cards: [] })
    expect(screen.getByText('This board has no columns')).toBeTruthy()
  })
})
