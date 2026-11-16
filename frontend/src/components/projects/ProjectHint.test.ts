import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { MessageDetail, Project } from '../../lib/types'

const api = vi.hoisted(() => ({
  addMailToProject: vi.fn(),
  dismissProjectSuggestion: vi.fn(),
  listProjectMailMarks: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import ProjectHint from './ProjectHint.svelte'
import { projectDismissals, projectMailMarks, projects } from '../../stores/projects'
import { shownSuggestion } from '../../lib/projectpicker'
import { prefs } from '../../stores/prefs'

function project(id: number, name: string, people: string[]): Project {
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
    people: people.map((email) => ({ email, name: '' })),
  }
}

const detail = { id: 41, threadId: 7, fromAddress: 'Ada <ada@example.com>', fromName: 'Ada' } as unknown as MessageDetail

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  api.listProjectMailMarks.mockResolvedValue([])
  prefs.update((p) => ({ ...p, projectSuggestions: true }))
  projects.set([project(1, 'Launch', ['ada@example.com']), project(2, 'Garden', ['bob@example.com'])])
  projectMailMarks.set(new Map())
  projectDismissals.set(new Set())
  shownSuggestion.set(null)
})

describe('the reader hint', () => {
  it('offers the project of a known sender and files the conversation', async () => {
    const user = userEvent.setup()
    api.addMailToProject.mockResolvedValue(1)
    render(ProjectHint, { props: { detail } })
    expect(screen.getByText('Add this conversation to Launch?')).toBeTruthy()
    expect(get(shownSuggestion)).toEqual({ threadId: 7, projectId: 1, messageIds: [41] })
    await user.click(screen.getByRole('button', { name: /^Add/ }))
    expect(api.addMailToProject).toHaveBeenCalledWith(1, [41], '')
  })

  it('remembers a dismissal for the conversation', async () => {
    const user = userEvent.setup()
    api.dismissProjectSuggestion.mockResolvedValue(undefined)
    render(ProjectHint, { props: { detail } })
    await user.click(screen.getByRole('button', { name: 'Not for this conversation' }))
    await waitFor(() => expect(screen.queryByText('Add this conversation to Launch?')).toBeNull())
    expect(api.dismissProjectSuggestion).toHaveBeenCalledWith(7)
    expect(get(shownSuggestion)).toBeNull()
  })

  it('names the project a filed conversation is in instead', () => {
    projectMailMarks.set(new Map([[7, [2]]]))
    render(ProjectHint, { props: { detail } })
    expect(screen.queryByText('Add this conversation to Launch?')).toBeNull()
    expect(screen.getByRole('button', { name: 'Garden' })).toBeTruthy()
  })

  it('stays quiet when suggestions are off or nobody matches', () => {
    prefs.update((p) => ({ ...p, projectSuggestions: false }))
    const { unmount } = render(ProjectHint, { props: { detail } })
    expect(screen.queryByText(/Add this conversation/)).toBeNull()
    unmount()
    prefs.update((p) => ({ ...p, projectSuggestions: true }))
    render(ProjectHint, { props: { detail: { ...detail, fromAddress: 'carol@example.com' } } })
    expect(screen.queryByText(/Add this conversation/)).toBeNull()
  })
})
