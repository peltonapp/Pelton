import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { Task, VaultNote, VaultSnapshot } from '../lib/types'

const api = vi.hoisted(() => ({
  vaultOpen: vi.fn(),
  vaultSnapshot: vi.fn(),
  vaultCreate: vi.fn(),
  vaultUniquePath: vi.fn(),
  vaultMove: vi.fn(),
  vaultDelete: vi.fn(),
  vaultUndo: vi.fn(),
  vaultRead: vi.fn(),
  listOccurrences: vi.fn(),
  getSetting: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import {
  activeNote,
  closeNote,
  deleteNote,
  ensureVault,
  moveNote,
  newNoteAbout,
  openDaily,
  openNote,
  openNotes,
  recentNotes,
  starredNotes,
  tagCounts,
  toggleStar,
  vaultIndex,
  vaultInfo,
} from './notes'
import { prefs } from './prefs'
import { tasks } from './tasks'
import { space } from './space'
import { toasts } from './toast'

function note(path: string, tags: string[] = []): VaultNote {
  return { path, title: path.replace(/^.*\//, '').replace(/\.md$/, ''), mtime: 1, size: 1, tags, aliases: [], headings: [], snippet: '', links: [] }
}

let snapshot: VaultSnapshot

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(2026, 10, 16, 9, 0))
  snapshot = { folder: '/tmp/vault', ready: true, notes: [note('A.md'), note('dir/B.md')], folders: ['dir'], files: [] }
  api.vaultOpen.mockResolvedValue({ folder: '/tmp/vault', ready: true, default: true, error: '' })
  api.vaultSnapshot.mockImplementation(() => Promise.resolve(snapshot))
  api.getSetting.mockResolvedValue({ value: '', found: false })
  api.setSetting.mockResolvedValue(undefined)
  api.vaultUniquePath.mockImplementation((p: string) => Promise.resolve(p))
  api.vaultCreate.mockImplementation((p: string) => {
    snapshot = { ...snapshot, notes: [...snapshot.notes, note(p)] }
    return Promise.resolve(p)
  })
  api.listOccurrences.mockResolvedValue([])
})

afterEach(() => {
  vi.useRealTimers()
  openNotes.set([])
  activeNote.set(null)
  starredNotes.set([])
  recentNotes.set([])
  toasts.set([])
  tasks.set([])
  vaultIndex.set(null)
  vaultInfo.set(null)
  space.set('mail')
  vi.clearAllMocks()
})

describe('notes store', () => {
  it('opens the folder and loads its index', async () => {
    const info = await ensureVault()
    expect(info.folder).toBe('/tmp/vault')
    expect(get(vaultIndex)?.notes).toHaveLength(2)
  })

  it('opens notes in place or in a new tab and closes them', () => {
    openNote('A.md')
    openNote('dir/B.md')
    expect(get(openNotes)).toEqual(['dir/B.md'])
    openNote('A.md', { newTab: true })
    expect(get(openNotes)).toEqual(['dir/B.md', 'A.md'])
    expect(get(activeNote)).toBe('A.md')
    expect(get(recentNotes).slice(0, 2)).toEqual(['A.md', 'dir/B.md'])
    closeNote('A.md')
    expect(get(activeNote)).toBe('dir/B.md')
  })

  it('follows a rename into tabs and stars, and undoes it', async () => {
    await ensureVault()
    openNote('A.md')
    toggleStar('A.md')
    api.vaultMove.mockResolvedValue({ undoId: '7', moves: { 'A.md': 'dir/A.md' }, rewritten: ['dir/B.md'] })
    expect(await moveNote('A.md', 'dir/A.md')).toBe(true)
    expect(get(openNotes)).toEqual(['dir/A.md'])
    expect(get(starredNotes)).toEqual(['dir/A.md'])
    const toast = get(toasts).at(-1)!
    expect(toast.message).toBe('Moved, and updated the links in 1 note')
    toast.action!.run()
    await vi.waitFor(() => expect(api.vaultUndo).toHaveBeenCalledWith('7'))
    await vi.waitFor(() => expect(get(openNotes)).toEqual(['A.md']))
  })

  it('deletes into the trash folder with undo and closes the tab', async () => {
    await ensureVault()
    openNote('A.md')
    api.vaultDelete.mockResolvedValue({ undoId: '3', moves: {}, rewritten: [] })
    await deleteNote('A.md')
    expect(get(openNotes)).toEqual([])
    get(toasts).at(-1)!.action!.run()
    await vi.waitFor(() => expect(api.vaultUndo).toHaveBeenCalledWith('3'))
  })

  it('counts nested tags towards their parents once per note', () => {
    const counts = tagCounts([note('a.md', ['work/x', 'work/y']), note('b.md', ['Work'])])
    expect(counts).toEqual([
      { tag: 'work', count: 2 },
      { tag: 'work/x', count: 1 },
      { tag: 'work/y', count: 1 },
    ])
  })

  it("makes today's daily note with the day's events and due tasks", async () => {
    await ensureVault()
    prefs.update((p) => ({ ...p, notesDailyFolder: 'Daily', notesDailyFormat: 'YYYY-MM-DD', notesDailyTemplate: '', timeFormat: '24' }))
    api.listOccurrences.mockResolvedValue([
      { eventId: 1, calendarId: 1, calendarColor: '', uid: 'ev1', summary: 'Standup', location: '', start: new Date(2026, 10, 16, 10, 0).toISOString(), end: '', allDay: false, recurring: false, recurrenceId: '', override: false, status: 'CONFIRMED', transparency: '', readOnly: false, hasReminders: false, pending: false, conflict: false },
    ])
    tasks.set([{ id: 4, uid: 't4', title: 'Pay rent', due: '2026-11-16', done: false } as Task, { id: 5, uid: 't5', title: 'Later', due: '2026-11-20', done: false } as Task])
    const path = await openDaily(new Date())
    expect(path).toBe('Daily/2026-11-16.md')
    const content = api.vaultCreate.mock.calls[0][1] as string
    expect(content).toContain('- 10:00 [Standup](pelton://event/ev1)')
    expect(content).toContain('- [ ] [Pay rent](pelton://task/t4)')
    expect(content).not.toContain('Later')
    expect(get(activeNote)).toBe('Daily/2026-11-16.md')
    api.vaultCreate.mockClear()
    await openDaily(new Date())
    expect(api.vaultCreate).not.toHaveBeenCalled()
  })

  it('makes a note about a mail with its link and shows Notes', async () => {
    await ensureVault()
    const path = await newNoteAbout({ kind: 'mail', id: 'm1@host', title: 'Invoice 42' })
    expect(path).toBe('Invoice 42.md')
    expect(api.vaultCreate.mock.calls[0][1]).toContain('[Invoice 42](pelton://mail/m1%40host)')
    expect(get(space)).toBe('notes')
  })
})
