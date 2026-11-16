import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { Project, VaultNote, VaultSnapshot } from '../../lib/types'

const launch: Project = {
  id: 1,
  uid: 'u1',
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

const api = vi.hoisted(() => ({
  vaultOpen: vi.fn(),
  vaultSnapshot: vi.fn(),
  vaultRead: vi.fn(),
  vaultSave: vi.fn(),
  vaultSearch: vi.fn(),
  vaultCreate: vi.fn(),
  vaultUniquePath: vi.fn(),
  vaultBacklinks: vi.fn(),
  vaultUnlinkedMentions: vi.fn(),
  vaultRescan: vi.fn(),
  getSetting: vi.fn(),
  setSetting: vi.fn(),
  getCalendarSettings: vi.fn(),
  listOccurrences: vi.fn(),
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
vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

import NotesScreen from './NotesScreen.svelte'
import NoteSwitcher from './NoteSwitcher.svelte'
import { activeNote, openNotes, vaultIndex, vaultInfo } from '../../stores/notes'
import { projects } from '../../stores/projects'
import { runSpaceAction } from '../../stores/space'
import { spaceSearchProviders } from '../../lib/spacesearch'

function note(path: string, over: Partial<VaultNote> = {}): VaultNote {
  return { path, title: path.replace(/^.*\//, '').replace(/\.md$/, ''), mtime: Date.now(), size: 1, tags: [], aliases: [], headings: [], snippet: '', links: [], props: [], ...over }
}

let snapshot: VaultSnapshot

beforeEach(() => {
  snapshot = {
    folder: '/tmp/My Vault',
    ready: true,
    notes: [
      note('Projects/Alpha.md', { tags: ['work'], snippet: 'About alpha', aliases: ['First'] }),
      note('Beta.md', { snippet: 'Beta text', links: [{ kind: 'wiki', embed: false, target: 'Alpha', subpath: '', display: '', line: 0, resolved: 'Projects/Alpha.md' }] }),
    ],
    folders: ['Projects'],
    files: [],
  }
  api.vaultOpen.mockResolvedValue({ folder: '/tmp/My Vault', ready: true, default: false, error: '' })
  api.vaultSnapshot.mockImplementation(() => Promise.resolve(snapshot))
  api.vaultRead.mockImplementation((p: string) => Promise.resolve({ path: p, content: `# ${p}\ntext`, hash: 'h', mtime: 1, readOnly: false }))
  api.vaultSearch.mockResolvedValue([])
  api.vaultBacklinks.mockResolvedValue([{ source: 'Beta.md', line: 0, text: 'Links to [[Alpha]]', match: '' }])
  api.vaultUnlinkedMentions.mockResolvedValue([])
  api.getSetting.mockResolvedValue({ value: '', found: false })
  api.setSetting.mockResolvedValue(undefined)
  api.getCalendarSettings.mockResolvedValue({ weekStart: 1 })
  api.vaultUniquePath.mockImplementation((p: string) => Promise.resolve(p))
  api.vaultCreate.mockImplementation((p: string) => {
    snapshot = { ...snapshot, notes: [...snapshot.notes, note(p)] }
    return Promise.resolve(p)
  })
})

afterEach(() => {
  openNotes.set([])
  activeNote.set(null)
  vaultIndex.set(null)
  vaultInfo.set(null)
  vi.clearAllMocks()
})

describe('NotesScreen', () => {
  it('shows the folder, its notes, folders and tags', async () => {
    render(NotesScreen)
    const list = await screen.findByRole('listbox', { name: 'All notes' })
    expect(within(list).getAllByRole('option').map((o) => o.querySelector('.name')?.textContent)).toContain('Alpha')
    expect(screen.getByRole('tree', { name: 'Folders' })).toHaveTextContent('Projects')
    expect(screen.getByRole('button', { name: /^work/ })).toBeInTheDocument()
    expect(screen.getByText('My Vault')).toBeInTheDocument()
  })

  it('opens a note in the editor with its backlinks', async () => {
    render(NotesScreen)
    const list = await screen.findByRole('listbox', { name: 'All notes' })
    await userEvent.click(within(list).getByText('Alpha'))
    expect(get(activeNote)).toBe('Projects/Alpha.md')
    expect(await screen.findByRole('textbox', { name: 'Note name' })).toHaveValue('Alpha')
    await waitFor(() => expect(api.vaultRead).toHaveBeenCalledWith('Projects/Alpha.md'))
    expect(await screen.findByText('Links to [[Alpha]]')).toBeInTheDocument()
  })

  it('narrows the list to a tag', async () => {
    render(NotesScreen)
    await screen.findByRole('listbox', { name: 'All notes' })
    await userEvent.click(screen.getByRole('button', { name: /^work/ }))
    const list = await screen.findByRole('listbox', { name: '#work' })
    expect(within(list).getAllByRole('option')).toHaveLength(1)
  })

  it('narrows the list to a project and shows the project of the open note', async () => {
    snapshot = { ...snapshot, notes: [...snapshot.notes, note('Projects/Launch/Plan.md', { props: [{ key: 'project-id', value: 'u1', list: [] }] })] }
    projects.set([launch])
    render(NotesScreen)
    await screen.findByRole('listbox', { name: 'All notes' })
    const group = screen.getByRole('list', { name: 'Projects' })
    await userEvent.click(within(group).getByRole('button', { name: /Launch/ }))
    const list = await screen.findByRole('listbox', { name: 'Launch' })
    expect(within(list).getAllByRole('option')).toHaveLength(1)
    await userEvent.click(within(list).getByText('Plan'))
    expect(await screen.findByRole('button', { name: 'Launch' })).toBeInTheDocument()
    projects.set([])
  })

  it('answers the space search and the quick switcher shortcut', async () => {
    render(NotesScreen)
    await screen.findByRole('listbox', { name: 'All notes' })
    const provider = get(spaceSearchProviders).notes!
    const hits = await provider.search('first')
    expect(hits.map((h) => h.id)).toEqual(['Projects/Alpha.md'])
    expect(runSpaceAction('notes', 'notes-switcher')).toBe(true)
    const field = await screen.findByPlaceholderText('Type a note name')
    await userEvent.type(field, 'bet{Enter}')
    expect(get(activeNote)).toBe('Beta.md')
  })

  it('makes a new note in the chosen folder', async () => {
    render(NotesScreen)
    await screen.findByRole('listbox', { name: 'All notes' })
    await userEvent.click(within(screen.getByRole('tree', { name: 'Folders' })).getByText('Projects'))
    await userEvent.click(screen.getAllByRole('button', { name: 'New note' })[0])
    await waitFor(() => expect(api.vaultCreate).toHaveBeenCalledWith('Projects/Untitled.md', ''))
  })
})

describe('NoteSwitcher', () => {
  it('ranks by name and alias and offers to create a missing note', async () => {
    const notes = [note('Alpha.md', { aliases: ['Start'] }), note('Beta.md')]
    const onCreate = vi.fn()
    const onPick = vi.fn()
    render(NoteSwitcher, { props: { notes, placeholder: 'Find' }, events: { create: (e: CustomEvent<string>) => onCreate(e.detail), pick: (e: CustomEvent<{ path: string }>) => onPick(e.detail.path) } })
    const field = screen.getByPlaceholderText('Find')
    await userEvent.type(field, 'sta')
    expect(screen.getAllByRole('option')[0]).toHaveTextContent('Start')
    await userEvent.keyboard('{Enter}')
    expect(onPick).toHaveBeenCalledWith('Alpha.md')
    await userEvent.clear(field)
    await userEvent.type(field, 'Gamma')
    expect(screen.getAllByRole('option').at(-1)).toHaveTextContent('Gamma')
    await userEvent.keyboard('{Shift>}{Enter}{/Shift}')
    expect(onCreate).toHaveBeenCalledWith('Gamma')
  })
})
