import { afterEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { Project } from './types'

const api = vi.hoisted(() => ({ getProjectBoard: vi.fn(), listProjects: vi.fn() }))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { openPeltonUrl } from './peltonopen'
import { cardFocus, openProjectId, projects, projectTab } from '../stores/projects'
import { toasts } from '../stores/toast'

function project(id: number, uid: string): Project {
  return {
    id,
    uid,
    name: `P${id}`,
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

afterEach(() => {
  projects.set([])
  openProjectId.set(null)
  cardFocus.set(null)
  vi.clearAllMocks()
})

describe('following a pelton://project link', () => {
  it('opens the project by its uid, or one of its cards', async () => {
    api.getProjectBoard.mockReturnValue(new Promise(() => {}))
    projects.set([project(1, 'u1'), project(2, 'u2')])
    expect(await openPeltonUrl('pelton://project/u2')).toBe(true)
    expect(get(openProjectId)).toBe(2)
    expect(get(projectTab)).toBe('board')
    await openPeltonUrl('pelton://project/u1/task%2F9')
    expect(get(openProjectId)).toBe(1)
    expect(get(cardFocus)).toBe('task/9')
  })

  it('loads the projects first and says when the project is gone', async () => {
    api.listProjects.mockResolvedValue([project(1, 'u1')])
    expect(await openPeltonUrl('pelton://project/nope')).toBe(true)
    expect(api.listProjects).toHaveBeenCalled()
    expect(get(openProjectId)).toBeNull()
    expect(get(toasts).at(-1)?.message).toBe('That item is no longer in Pelton.')
  })
})
