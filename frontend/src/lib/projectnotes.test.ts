import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  listProjectNotes: vi.fn(),
  createProjectNote: vi.fn(),
  updateProjectNote: vi.fn(),
  deleteProjectNote: vi.fn(),
  attachProjectNote: vi.fn(),
  listProjectVaultNotes: vi.fn(),
  createProjectVaultNote: vi.fn(),
  removeNoteFromProject: vi.fn(),
  attachVaultNote: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { localProjectNotes, noteExcerpt, noteTitle, projectNotes, useProjectNotes, vaultProjectNotes, type ProjectNotesSource } from './projectnotes'

const record = { id: 4, projectId: 2, title: 'Plan', body: '# Plan', createdAt: 'c', updatedAt: 'u' }
const file = { path: 'Projects/Launch/Plan.md', title: 'Plan', snippet: 'steps', mtime: Date.UTC(2026, 10, 16) }

describe('the local notes source', () => {
  beforeEach(() => {
    Object.values(api).forEach((f) => f.mockReset())
  })

  it('hands out notes with string ids and no path', async () => {
    api.listProjectNotes.mockResolvedValue([record])
    expect(await localProjectNotes.list(2)).toEqual({
      files: false,
      notes: [{ id: '4', projectId: 2, title: 'Plan', body: '# Plan', snippet: 'Plan', path: '', updatedAt: 'u' }],
      linked: [],
    })
  })

  it('turns the ids back into numbers for the backend', async () => {
    api.updateProjectNote.mockResolvedValue({ ...record, body: 'new' })
    const saved = await localProjectNotes.save({ id: '4', projectId: 2, title: 'Plan', body: 'new', snippet: '', path: '', updatedAt: '' })
    expect(api.updateProjectNote).toHaveBeenCalledWith(4, 'Plan', 'new')
    expect(saved.body).toBe('new')
    await localProjectNotes.remove(saved)
    expect(api.deleteProjectNote).toHaveBeenCalledWith(4)
    await localProjectNotes.attach(saved, 'task-uid')
    expect(api.attachProjectNote).toHaveBeenCalledWith(4, 'task-uid')
  })
})

describe('the notes folder source', () => {
  beforeEach(() => {
    Object.values(api).forEach((f) => f.mockReset())
  })

  it('is the source in use', () => {
    expect(projectNotes()).toBe(vaultProjectNotes)
  })

  it('lists files by path, with the notes that link to the project', async () => {
    api.listProjectVaultNotes.mockResolvedValue({ available: true, notes: [file], linked: [{ ...file, path: 'Other.md', title: 'Other' }] })
    const list = await vaultProjectNotes.list(2)
    expect(list.files).toBe(true)
    expect(list.notes).toEqual([{ id: file.path, projectId: 2, title: 'Plan', body: '', snippet: 'steps', path: file.path, updatedAt: '2026-11-16T00:00:00.000Z' }])
    expect(list.linked.map((n) => n.id)).toEqual(['Other.md'])
    expect(api.listProjectNotes).not.toHaveBeenCalled()
  })

  it('falls back to the database while no notes folder is open', async () => {
    api.listProjectVaultNotes.mockResolvedValue({ available: false, notes: [], linked: [] })
    api.listProjectNotes.mockResolvedValue([record])
    const list = await vaultProjectNotes.list(2)
    expect(list.files).toBe(false)
    expect(list.notes[0].id).toBe('4')

    api.createProjectVaultNote.mockRejectedValue(new Error('notes: no notes folder is open'))
    api.createProjectNote.mockResolvedValue(record)
    const made = await vaultProjectNotes.create(2, 'Plan', 'body')
    expect(api.createProjectNote).toHaveBeenCalledWith(2, 'Plan', 'body')
    expect(made.path).toBe('')
  })

  it('reports a failed create when the folder is open', async () => {
    api.createProjectVaultNote.mockRejectedValue(new Error('disk full'))
    api.listProjectVaultNotes.mockResolvedValue({ available: true, notes: [], linked: [] })
    await expect(vaultProjectNotes.create(2, 'Plan', 'body')).rejects.toThrow('disk full')
    expect(api.createProjectNote).not.toHaveBeenCalled()
  })

  it('takes a file out of the project without deleting it, and attaches it by path', async () => {
    const note = { id: file.path, projectId: 2, title: 'Plan', body: '', snippet: '', path: file.path, updatedAt: '' }
    await vaultProjectNotes.remove(note)
    expect(api.removeNoteFromProject).toHaveBeenCalledWith(file.path, 2)
    expect(api.deleteProjectNote).not.toHaveBeenCalled()
    await vaultProjectNotes.attach(note, 'task-uid')
    expect(api.attachVaultNote).toHaveBeenCalledWith(file.path, 'task-uid')
    await expect(vaultProjectNotes.save(note)).rejects.toThrow()
  })

  it('hands database notes to the database', async () => {
    const note = { id: '4', projectId: 2, title: 'Plan', body: 'x', snippet: '', path: '', updatedAt: '' }
    await vaultProjectNotes.remove(note)
    expect(api.deleteProjectNote).toHaveBeenCalledWith(4)
    await vaultProjectNotes.attach(note, 'task-uid')
    expect(api.attachProjectNote).toHaveBeenCalledWith(4, 'task-uid')
  })

  it('can be swapped for another source', async () => {
    const other: ProjectNotesSource = { ...localProjectNotes, list: async () => ({ files: false, notes: [], linked: [] }) }
    const before = useProjectNotes(other)
    expect(projectNotes()).toBe(other)
    expect((await projectNotes().list(2)).notes).toEqual([])
    useProjectNotes(before)
    expect(projectNotes()).toBe(vaultProjectNotes)
  })
})

describe('naming notes', () => {
  it('uses the title, else the first line, else the fallback', () => {
    expect(noteTitle({ title: ' Spec ', body: 'x' }, 'Untitled')).toBe('Spec')
    expect(noteTitle({ title: '', body: '\n## Agenda\nmore' }, 'Untitled')).toBe('Agenda')
    expect(noteTitle({ title: '', body: '  \n' }, 'Untitled')).toBe('Untitled')
  })

  it('makes a plain excerpt', () => {
    expect(noteExcerpt('# Title\n- **one**\n> two `three`')).toBe('Title one two three')
    expect(noteExcerpt('a'.repeat(200), 10)).toHaveLength(10)
  })
})
