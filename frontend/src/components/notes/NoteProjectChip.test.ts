import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Project } from '../../lib/types'

const api = vi.hoisted(() => ({ getProjectBoard: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import NoteProjectChip from './NoteProjectChip.svelte'
import { openProjectId, projectTab, projects } from '../../stores/projects'
import { space } from '../../stores/space'

function project(id: number, uid: string, name: string): Project {
  return {
    id,
    uid,
    name,
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
  vi.clearAllMocks()
})

describe('the project chip of a note', () => {
  it('names the project the front matter points at and opens it on its notes', async () => {
    api.getProjectBoard.mockReturnValue(new Promise(() => {}))
    projects.set([project(1, 'u1', 'Launch'), project(2, 'u2', 'Relaunch')])
    render(NoteProjectChip, { props: { props: [{ key: 'project', value: 'Old name', list: [] }, { key: 'project-id', value: 'u2', list: [] }] } })
    const chip = screen.getByRole('button', { name: 'Relaunch' })
    await userEvent.click(chip)
    expect(get(openProjectId)).toBe(2)
    expect(get(projectTab)).toBe('notes')
    expect(get(space)).toBe('projects')
  })

  it('matches a hand-typed project name', () => {
    projects.set([project(1, 'u1', 'Launch')])
    render(NoteProjectChip, { props: { props: [{ key: 'project', value: '[[launch]]', list: [] }] } })
    expect(screen.getByRole('button', { name: 'Launch' })).toBeInTheDocument()
  })

  it('shows nothing for a note in no project or in one Pelton does not know', () => {
    projects.set([project(1, 'u1', 'Launch')])
    const { container } = render(NoteProjectChip, { props: { props: [{ key: 'project-id', value: 'gone', list: [] }] } })
    expect(container.querySelector('button')).toBeNull()
    render(NoteProjectChip, { props: { props: [] } })
    expect(screen.queryByRole('button')).toBeNull()
  })
})
