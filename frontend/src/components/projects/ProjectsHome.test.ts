import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Project } from '../../lib/types'

const api = vi.hoisted(() => ({
  getProjectBoard: vi.fn(),
  setSetting: vi.fn(),
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

import ProjectsHome from './ProjectsHome.svelte'
import { openProjectId, projects, projectsLoaded } from '../../stores/projects'
import { prefs } from '../../stores/prefs'
import { space } from '../../stores/space'

const now = new Date(2026, 10, 16, 10, 0)

function project(id: number, over: Partial<Project> = {}): Project {
  return {
    id,
    uid: `p${id}`,
    name: `Project ${id}`,
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

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  api.getProjectBoard.mockResolvedValue({ project: project(1), columns: [], cards: [] })
  api.setSetting.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, projectsView: 'grid' }))
  projectsLoaded.set(true)
  openProjectId.set(null)
  projects.set([
    project(1, { name: 'Website relaunch', description: 'New site for spring', cards: 8, done: 2, nextDue: '2026-11-15', overdue: 1, people: [{ email: 'ada@example.com', name: 'Ada' }] }),
    project(2, { name: 'Garden', cards: 0 }),
    project(3, { name: 'Taxes 2025', status: 'archived' }),
  ])
})

describe('the list of projects', () => {
  it('shows the active projects with their progress and next deadline', () => {
    render(ProjectsHome, { props: { now } })
    expect(screen.getByRole('heading', { name: 'Website relaunch' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Garden' })).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Taxes 2025' })).toBeNull()
    expect(screen.getByText('2 of 8 done')).toBeTruthy()
    expect(screen.getByText('Yesterday')).toBeTruthy()
  })

  it('filters, and switches to the archived ones', async () => {
    const user = userEvent.setup()
    render(ProjectsHome, { props: { now } })
    await user.type(screen.getByRole('searchbox', { name: 'Filter projects' }), 'spring')
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Garden' })).toBeNull())
    await user.clear(screen.getByRole('searchbox', { name: 'Filter projects' }))
    await user.click(screen.getByRole('button', { name: 'Archived (1)' }))
    expect(await screen.findByRole('heading', { name: 'Taxes 2025' })).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Garden' })).toBeNull()
  })

  it('opens a project on its board', async () => {
    const user = userEvent.setup()
    render(ProjectsHome, { props: { now } })
    await user.click(screen.getByRole('button', { name: 'Open Garden' }))
    expect(get(openProjectId)).toBe(2)
    expect(get(space)).toBe('projects')
  })

  it('asks for a new project', async () => {
    const user = userEvent.setup()
    const create = vi.fn()
    render(ProjectsHome, { props: { now }, events: { create } })
    await user.click(screen.getByRole('button', { name: 'New project' }))
    expect(create).toHaveBeenCalled()
  })

  it('shows rows in the list view and remembers the choice', async () => {
    const user = userEvent.setup()
    render(ProjectsHome, { props: { now } })
    await user.click(screen.getByRole('radio', { name: 'List' }))
    expect(get(prefs).projectsView).toBe('list')
    expect(api.setSetting).toHaveBeenCalledWith('projects_view', 'list')
    expect(await screen.findAllByRole('listitem')).toHaveLength(2)
  })

  it('invites to make the first project', () => {
    projects.set([])
    render(ProjectsHome, { props: { now } })
    expect(screen.getByText('No projects yet')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Create a project' })).toBeTruthy()
  })
})
