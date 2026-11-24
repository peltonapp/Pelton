import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Project, ProjectVaultNotes } from '../../lib/types'

const api = vi.hoisted(() => ({
  listProjectVaultNotes: vi.fn(),
  createProjectVaultNote: vi.fn(),
  addNoteToProject: vi.fn(),
  removeNoteFromProject: vi.fn(),
  trashProjectNote: vi.fn(),
  vaultUndo: vi.fn(),
  vaultSnapshot: vi.fn(),
  vaultRead: vi.fn(),
  vaultSave: vi.fn(),
  vaultRescan: vi.fn(),
  listProjectNotes: vi.fn(),
  getSetting: vi.fn(),
  setSetting: vi.fn(),
}))

const confirm = vi.hoisted(() => ({ askConfirm: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/confirm', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/confirm')>()),
  ...confirm,
}))

Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}
Element.prototype.getAnimations ??= function () {
  return []
}
vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

import ProjectNotes from './ProjectNotes.svelte'
import { vaultIndex } from '../../stores/notes'

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
  cards: 0,
  done: 0,
  overdue: 0,
  nextDue: '',
  mail: 0,
  events: 0,
  notes: 1,
  people: [],
}

let folder: ProjectVaultNotes

beforeEach(() => {
  folder = {
    available: true,
    notes: [{ path: 'Projects/Launch/Plan.md', title: 'Plan', snippet: 'the steps', mtime: Date.UTC(2026, 10, 16) }],
    linked: [{ path: 'Journal.md', title: 'Journal', snippet: 'met about the launch', mtime: Date.UTC(2026, 10, 15) }],
  }
  api.listProjectVaultNotes.mockImplementation(() => Promise.resolve(folder))
  api.vaultSnapshot.mockResolvedValue({ folder: '/tmp/v', ready: true, notes: [], folders: [], files: [] })
  api.vaultRead.mockImplementation((p: string) => Promise.resolve({ path: p, content: `# ${p}\n`, hash: 'h', mtime: 1, readOnly: false }))
  api.vaultSave.mockResolvedValue({ hash: 'h', mtime: 1, conflict: false, disk: '', diskHash: '' })
  api.getSetting.mockResolvedValue({ value: '', found: false })
  api.createProjectVaultNote.mockImplementation((_id: number, title: string) => {
    const made = { path: `Projects/Launch/${title}.md`, title, snippet: '', mtime: Date.now() }
    folder = { ...folder, notes: [made, ...folder.notes] }
    return Promise.resolve(made)
  })
  api.removeNoteFromProject.mockResolvedValue(undefined)
  api.trashProjectNote.mockResolvedValue({ undoId: '1', moves: {}, rewritten: [] })
  confirm.askConfirm.mockResolvedValue(true)
})

afterEach(() => {
  vaultIndex.set(null)
  vi.clearAllMocks()
})

describe('the Notes tab of a project', () => {
  it('lists the project notes and the notes linking to it, and opens one in the Notes editor', async () => {
    render(ProjectNotes, { props: { project } })
    const own = await screen.findByRole('listbox', { name: 'Notes' })
    expect(within(own).getByText('Plan')).toBeInTheDocument()
    expect(within(own).getByText('the steps')).toBeInTheDocument()
    const linked = screen.getByRole('listbox', { name: 'Linked from other notes' })
    expect(within(linked).getByText('Journal')).toBeInTheDocument()
    await waitFor(() => expect(api.vaultRead).toHaveBeenCalledWith('Projects/Launch/Plan.md'))
    await waitFor(() => expect(document.querySelector('.cm-editor')).not.toBeNull())

    await userEvent.click(within(linked).getByText('Journal'))
    await waitFor(() => expect(api.vaultRead).toHaveBeenCalledWith('Journal.md'))
    expect(screen.getByText('Links to this project')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add to project' })).toBeInTheDocument()
  })

  it('makes a new note from the project template', async () => {
    render(ProjectNotes, { props: { project } })
    await screen.findByRole('listbox', { name: 'Notes' })
    await userEvent.click(screen.getByRole('button', { name: 'New note' }))
    await userEvent.type(screen.getByRole('textbox', { name: 'Title' }), 'Kick off{Enter}')
    await waitFor(() => expect(api.createProjectVaultNote).toHaveBeenCalled())
    const [id, title, body] = api.createProjectVaultNote.mock.calls[0]
    expect([id, title]).toEqual([1, 'Kick off'])
    expect(body).toContain('# Kick off')
    expect(body).toContain('[Launch](pelton://project/p1)')
    await waitFor(() => expect(api.vaultRead).toHaveBeenCalledWith('Projects/Launch/Kick off.md'))
  })

  it('takes a note out of the project, and moves one to the trash only when confirmed', async () => {
    render(ProjectNotes, { props: { project } })
    await screen.findByRole('listbox', { name: 'Notes' })
    await userEvent.click(await screen.findByRole('button', { name: 'Remove from project' }))
    expect(api.removeNoteFromProject).toHaveBeenCalledWith('Projects/Launch/Plan.md', 1)
    expect(api.trashProjectNote).not.toHaveBeenCalled()

    confirm.askConfirm.mockResolvedValueOnce(false)
    await userEvent.click(screen.getByRole('button', { name: 'Move to trash' }))
    expect(api.trashProjectNote).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Move to trash' }))
    await waitFor(() => expect(api.trashProjectNote).toHaveBeenCalledWith('Projects/Launch/Plan.md', 1))
  })

  it('keeps the plain editor while no notes folder is open', async () => {
    api.listProjectVaultNotes.mockResolvedValue({ available: false, notes: [], linked: [] })
    api.listProjectNotes.mockResolvedValue([{ id: 4, projectId: 1, title: 'Old', body: 'kept here', createdAt: '', updatedAt: '' }])
    render(ProjectNotes, { props: { project } })
    expect(await screen.findByRole('textbox', { name: 'Title' })).toHaveValue('Old')
    expect(screen.getByRole('radio', { name: 'Preview' })).toBeInTheDocument()
    expect(document.querySelector('.cm-editor')).toBeNull()
  })
})
