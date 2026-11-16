import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { Project, ProjectBoard, ProjectCard, Task } from '../lib/types'

const api = vi.hoisted(() => ({
  listProjects: vi.fn(),
  listProjectMailMarks: vi.fn(),
  listProjectDismissals: vi.fn(),
  getProjectBoard: vi.fn(),
  moveProjectCard: vi.fn(),
  removeProjectCard: vi.fn(),
  addTaskToProject: vi.fn(),
  addMailToProject: vi.fn(),
  dismissProjectSuggestion: vi.fn(),
  setProjectStatus: vi.fn(),
  deleteProject: vi.fn(),
  reorderProjects: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import {
  board,
  dismissSuggestion,
  fileMail,
  loadProjectMarks,
  moveBoardCard,
  openProject,
  openProjectId,
  projectDismissals,
  projectMailMarks,
  projects,
  removeCard,
  removeProject,
  reorder,
  setArchived,
} from './projects'
import { space } from './space'
import { toasts } from './toast'

function task(uid: string): Task {
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
    created: '',
    readOnly: false,
    pending: false,
    conflict: false,
    syncError: '',
  }
}

function card(uid: string, columnId: number, position: number): ProjectCard {
  return { task: task(uid), projectId: 1, columnId, position, mail: 0, events: 0, notes: 0, sourceKind: '', sourceRef: '' }
}

function project(id: number, name: string): Project {
  return {
    id,
    uid: `p${id}`,
    name,
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
  }
}

const sample: ProjectBoard = {
  project: project(1, 'Launch'),
  columns: [
    { id: 10, projectId: 1, name: 'To do', position: 0, wipLimit: 0, done: false },
    { id: 11, projectId: 1, name: 'Done', position: 1, wipLimit: 0, done: true },
  ],
  cards: [card('a', 10, 0), card('b', 10, 1)],
}

const order = (): string[] =>
  (get(board)?.cards ?? [])
    .slice()
    .sort((x, y) => x.columnId - y.columnId || x.position - y.position)
    .map((c) => `${c.columnId}:${c.task.uid}`)

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  toasts.set([])
  projects.set([project(1, 'Launch'), project(2, 'Garden'), project(3, 'Taxes')])
  openProjectId.set(1)
  board.set(structuredClone(sample))
})

describe('moving cards', () => {
  it('moves at once and keeps what the backend says', async () => {
    api.moveProjectCard.mockResolvedValue({ card: { ...card('a', 11, 0), task: { ...task('a'), done: true } }, advanced: false })
    const pending = moveBoardCard('a', 11, 0)
    expect(order()).toEqual(['10:b', '11:a'])
    await pending
    expect(api.moveProjectCard).toHaveBeenCalledWith('a', 11, 0)
    expect(get(board)?.cards.find((c) => c.task.uid === 'a')?.task.done).toBe(true)
  })

  it('puts the board back when the move is refused', async () => {
    api.moveProjectCard.mockRejectedValue(new Error('read only'))
    await moveBoardCard('b', 11, 0)
    expect(order()).toEqual(['10:a', '10:b'])
    expect(get(toasts).some((x) => x.kind === 'error')).toBe(true)
  })

  it('sends a repeating task back when it moved on instead of completing', async () => {
    api.moveProjectCard.mockResolvedValue({ card: card('a', 10, 0), advanced: true })
    await moveBoardCard('a', 11, 0)
    expect(order()).toEqual(['10:a', '10:b'])
    expect(get(toasts).some((x) => x.kind === 'info')).toBe(true)
  })

  it('does nothing for a drop where the card already is', async () => {
    await moveBoardCard('b', 10, 1)
    expect(api.moveProjectCard).not.toHaveBeenCalled()
  })

  it('takes a card off the board and offers to put it back', async () => {
    api.removeProjectCard.mockResolvedValue(undefined)
    await removeCard(sample.cards[0])
    expect(order()).toEqual(['10:b'])
    const toast = get(toasts).find((x) => x.kind === 'success')
    expect(toast?.action?.label).toBeTruthy()
  })
})

describe('projects', () => {
  it('opens a project in the Projects space on its board', () => {
    api.getProjectBoard.mockResolvedValue(sample)
    space.set('mail')
    openProject(2, 'notes')
    expect(get(space)).toBe('projects')
    expect(get(openProjectId)).toBe(2)
    expect(get(board)).toBeNull()
    expect(api.getProjectBoard).toHaveBeenCalledWith(2)
  })

  it('archives with undo and puts it back when that fails', async () => {
    api.setProjectStatus.mockResolvedValue(undefined)
    await setArchived(get(projects)[1], true)
    expect(get(projects)[1].status).toBe('archived')
    api.setProjectStatus.mockRejectedValue(new Error('no'))
    await setArchived(get(projects)[0], true)
    expect(get(projects)[0].status).toBe('active')
  })

  it('reorders at once', async () => {
    api.reorderProjects.mockResolvedValue(undefined)
    await reorder([3, 1])
    expect(get(projects).map((p) => p.id)).toEqual([3, 1, 2])
    expect(api.reorderProjects).toHaveBeenCalledWith([3, 1])
  })

  it('closes a deleted project that was open', async () => {
    api.deleteProject.mockResolvedValue(undefined)
    api.listProjectMailMarks.mockResolvedValue([])
    expect(await removeProject(get(projects)[0])).toBe(true)
    expect(get(openProjectId)).toBeNull()
    expect(get(projects).map((p) => p.id)).toEqual([2, 3])
  })
})

describe('mail', () => {
  it('groups the conversation marks by thread', async () => {
    api.listProjectMailMarks.mockResolvedValue([
      { threadId: 5, projectId: 1 },
      { threadId: 5, projectId: 2 },
      { threadId: 6, projectId: 1 },
    ])
    await loadProjectMarks()
    expect(get(projectMailMarks).get(5)).toEqual([1, 2])
    expect(get(projectMailMarks).get(6)).toEqual([1])
  })

  it('files mail and says where it went', async () => {
    api.addMailToProject.mockResolvedValue(2)
    api.listProjectMailMarks.mockResolvedValue([])
    expect(await fileMail(2, [7, 8])).toBe(2)
    expect(api.addMailToProject).toHaveBeenCalledWith(2, [7, 8], '')
    expect(get(toasts).at(-1)?.message).toContain('Garden')
  })

  it('remembers a dismissed suggestion at once', async () => {
    api.dismissProjectSuggestion.mockResolvedValue(undefined)
    projectDismissals.set(new Set())
    await dismissSuggestion(9)
    expect(get(projectDismissals).has(9)).toBe(true)
    expect(api.dismissProjectSuggestion).toHaveBeenCalledWith(9)
  })
})
