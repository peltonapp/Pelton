import { beforeEach, describe, expect, it, vi } from 'vitest'

const api = vi.hoisted(() => ({
  listProjectNotes: vi.fn(),
  createProjectNote: vi.fn(),
  updateProjectNote: vi.fn(),
  deleteProjectNote: vi.fn(),
  attachProjectNote: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { localProjectNotes, noteExcerpt, noteTitle, projectNotes, useProjectNotes, type ProjectNotesSource } from './projectnotes'

const record = { id: 4, projectId: 2, title: 'Plan', body: '# Plan', createdAt: 'c', updatedAt: 'u' }

describe('the local notes source', () => {
  beforeEach(() => {
    Object.values(api).forEach((f) => f.mockReset())
  })

  it('hands out notes with string ids', async () => {
    api.listProjectNotes.mockResolvedValue([record])
    expect(await localProjectNotes.list(2)).toEqual([{ id: '4', projectId: 2, title: 'Plan', body: '# Plan', updatedAt: 'u' }])
  })

  it('turns the ids back into numbers for the backend', async () => {
    api.updateProjectNote.mockResolvedValue({ ...record, body: 'new' })
    const saved = await localProjectNotes.save({ id: '4', projectId: 2, title: 'Plan', body: 'new', updatedAt: '' })
    expect(api.updateProjectNote).toHaveBeenCalledWith(4, 'Plan', 'new')
    expect(saved.body).toBe('new')
    await localProjectNotes.remove(saved)
    expect(api.deleteProjectNote).toHaveBeenCalledWith(4)
    await localProjectNotes.attach(saved, 'task-uid')
    expect(api.attachProjectNote).toHaveBeenCalledWith(4, 'task-uid')
  })

  it('can be swapped for another source', async () => {
    const other: ProjectNotesSource = { ...localProjectNotes, list: async () => [] }
    const before = useProjectNotes(other)
    expect(projectNotes()).toBe(other)
    expect(await projectNotes().list(2)).toEqual([])
    useProjectNotes(before)
    expect(projectNotes()).toBe(localProjectNotes)
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
