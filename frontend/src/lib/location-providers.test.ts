import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const getMessage = vi.fn()
const getFeedEntry = vi.fn()
vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  getMessage: (id: number) => getMessage(id),
  getFeedEntry: (id: number) => getFeedEntry(id),
  getProjectBoard: vi.fn(() => new Promise(() => {})),
  listEntries: vi.fn(async () => []),
  listFeedEntries: vi.fn(async () => []),
  chatSetViewing: vi.fn(async () => {}),
  chatTypists: vi.fn(async () => []),
  chatTimeline: vi.fn(async () => ({ messages: [], hasMore: false, fullyRead: '' })),
  setSetting: vi.fn(async () => {}),
  getSetting: vi.fn(async () => ({ value: '', found: false })),
}))

await import('./location-mail')
await import('./location-calendar')
await import('./location-tasks')
await import('./location-projects')
await import('./location-contacts')
await import('./location-notes')
await import('./location-feeds')
await import('./location-chat')

const { locationProvider, locationKey } = await import('./locations')
const { space } = await import('../stores/space')
const { prefs } = await import('../stores/prefs')
const { sidebar } = await import('../stores/accounts')
const { views } = await import('../stores/views')
const { selection, openMessageId, defaultSelection } = await import('../stores/selection')
const { messageDetail } = await import('../stores/message')
const { calendarFocus, calendarView } = await import('../stores/calendar')
const { tasks, openTaskId, taskFocus } = await import('../stores/tasks')
const { projects, openProjectId, projectTab, openCardUid, cardFocus } = await import('../stores/projects')
const { contacts } = await import('../stores/contacts')
const { selectedContactId } = await import('../stores/contactsview')
const { activeNote, vaultIndex } = await import('../stores/notes')
const { feedList, feedSelected, feedSidebar, feedFocus } = await import('../stores/feeds')
const { activeRoomId, chatRooms } = await import('../stores/chat')
const { calendarLabel } = await import('./location-calendar')
const { entryId } = await import('./location-feeds')
const { noteName } = await import('./location-notes')
const { listOf, sameList } = await import('./location-mail')
const { ready } = await import('./async')

import type { Contact, ChatRoom, Folder, Project, Task, View } from './types'
import type { SidebarData } from '../stores/accounts'

const provider = (key: Parameters<typeof locationProvider>[0]) => locationProvider(key)!

beforeEach(() => {
  getMessage.mockReset()
  getFeedEntry.mockReset()
  space.set('mail')
})

describe('mail', () => {
  const folder = { id: 7, accountId: 2, name: 'Receipts' } as Folder
  beforeEach(() => {
    sidebar.set(ready({ foldersByAccount: { 2: [folder] } } as unknown as SidebarData))
    views.set([{ id: 4, name: 'Big ones' } as View])
    selection.set(defaultSelection)
    openMessageId.set(null)
  })

  it('describes the list without its label and the open message', () => {
    selection.set({ kind: 'folder', folderId: 7, accountId: 2, label: 'whatever' })
    openMessageId.set(12)
    expect(provider('mail').describe()).toEqual({ space: 'mail', list: { kind: 'folder', folderId: 7, accountId: 2 }, message: 12 })
    expect(sameList(listOf({ kind: 'view', view: 'inbox', label: 'x' }), { kind: 'view', view: 'inbox' })).toBe(true)
  })

  it('names the open message by its subject once loaded, else the list', () => {
    const p = provider('mail')
    expect(p.label({ space: 'mail', list: { kind: 'folder', folderId: 7, accountId: 2 } })).toBe('Receipts')
    messageDetail.set(ready({ id: 12, subject: 'Invoice' } as never))
    expect(p.label({ space: 'mail', list: { kind: 'savedView', viewId: 4 }, message: 12 })).toBe('Invoice')
  })

  it('restores a folder and a message', async () => {
    space.set('calendar')
    getMessage.mockResolvedValue({ id: 12 })
    expect(await provider('mail').restore({ space: 'mail', list: { kind: 'folder', folderId: 7, accountId: 2 }, message: 12 })).toBe(true)
    expect(get(space)).toBe('mail')
    expect(get(selection)).toMatchObject({ kind: 'folder', folderId: 7, label: 'Receipts' })
    expect(get(openMessageId)).toBe(12)
  })

  it('says a deleted message, folder or saved view is gone', async () => {
    getMessage.mockRejectedValue(new Error('not found'))
    expect(await provider('mail').restore({ space: 'mail', list: { kind: 'view', view: 'inbox' }, message: 99 })).toBe(false)
    expect(await provider('mail').restore({ space: 'mail', list: { kind: 'folder', folderId: 8, accountId: 2 } })).toBe(false)
    expect(await provider('mail').restore({ space: 'mail', list: { kind: 'savedView', viewId: 5 } })).toBe(false)
    expect(get(selection)).toEqual(defaultSelection)
  })

  it('opening or closing a message in the same list refines the visit', () => {
    const p = provider('mail')
    const inbox = { kind: 'view' as const, view: 'inbox' as const }
    expect(p.refines?.({ space: 'mail', list: inbox }, { space: 'mail', list: inbox, message: 3 })).toBe(true)
    expect(p.refines?.({ space: 'mail', list: inbox, message: 2 }, { space: 'mail', list: inbox, message: 3 })).toBe(false)
    expect(p.refines?.({ space: 'mail', list: inbox }, { space: 'mail', list: { kind: 'savedView', viewId: 4 } })).toBe(false)
  })
})

describe('calendar', () => {
  it('describes and restores the view and the date', async () => {
    calendarView.set('week')
    calendarFocus.set(new Date(2026, 10, 17, 15, 30))
    const loc = provider('calendar').describe()
    expect(loc).toEqual({ space: 'calendar', view: 'week', date: '2026-11-17' })
    calendarView.set('day')
    calendarFocus.set(new Date(2020, 0, 1))
    expect(await provider('calendar').restore(loc)).toBe(true)
    expect(get(calendarView)).toBe('week')
    expect(get(calendarFocus).getDate()).toBe(17)
    expect(get(space)).toBe('calendar')
  })

  it('labels by view and treats days of the same month page as one place', () => {
    const t = (k: string): string => (k === 'navHistory.calendar.week' ? 'Week of {date}' : k)
    expect(calendarLabel({ space: 'calendar', view: 'week', date: '2026-11-17' }, t)).toMatch(/^Week of /)
    expect(calendarLabel({ space: 'calendar', view: 'year', date: '2026-11-17' }, t)).toBe('2026')
    expect(calendarLabel({ space: 'calendar', view: 'day', date: 'nonsense' }, t)).toBe('')
    const p = provider('calendar')
    expect(p.refines?.({ space: 'calendar', view: 'month', date: '2026-11-02' }, { space: 'calendar', view: 'month', date: '2026-11-20' })).toBe(true)
    expect(p.refines?.({ space: 'calendar', view: 'month', date: '2026-11-02' }, { space: 'calendar', view: 'month', date: '2026-12-02' })).toBe(false)
  })
})

describe('tasks', () => {
  beforeEach(() => {
    tasks.set([{ id: 5, title: 'Pay rent' } as Task])
    taskFocus.set(null)
  })

  it('describes the selected task and restores it through the task focus', async () => {
    openTaskId.set(5)
    const loc = provider('tasks').describe()
    expect(loc).toEqual({ space: 'tasks', task: 5 })
    expect(provider('tasks').label(loc)).toBe('Pay rent')
    expect(await provider('tasks').restore(loc)).toBe(true)
    expect(get(taskFocus)).toBe(5)
    expect(get(space)).toBe('tasks')
  })

  it('says a removed task is gone', async () => {
    expect(await provider('tasks').restore({ space: 'tasks', task: 6 })).toBe(false)
    expect(get(taskFocus)).toBeNull()
  })
})

describe('projects', () => {
  beforeEach(() => {
    projects.set([{ id: 3, name: 'Website' } as Project])
    openProjectId.set(null)
    openCardUid.set(null)
    cardFocus.set(null)
  })

  it('describes the project, its tab and the open card', () => {
    openProjectId.set(3)
    projectTab.set('board')
    openCardUid.set('u1')
    expect(provider('projects').describe()).toEqual({ space: 'projects', project: 3, tab: 'board', card: 'u1' })
    projectTab.set('people')
    const loc = provider('projects').describe()
    expect(loc).toEqual({ space: 'projects', project: 3, tab: 'people' })
    expect(provider('projects').label(loc)).toBe('Website: People')
  })

  it('restores a tab or a card, and says a removed project is gone', async () => {
    expect(await provider('projects').restore({ space: 'projects', project: 3, tab: 'mail' })).toBe(true)
    expect(get(openProjectId)).toBe(3)
    expect(get(projectTab)).toBe('mail')
    expect(await provider('projects').restore({ space: 'projects', project: 3, tab: 'board', card: 'u2' })).toBe(true)
    expect(get(cardFocus)).toBe('u2')
    expect(await provider('projects').restore({ space: 'projects', project: 9 })).toBe(false)
    expect(await provider('projects').restore({ space: 'projects' })).toBe(true)
    expect(get(openProjectId)).toBeNull()
  })
})

describe('contacts', () => {
  beforeEach(() => {
    contacts.set([{ id: 1, fullName: '', emails: [{ value: 'ada@example.org', label: '' }] } as unknown as Contact])
    selectedContactId.set(null)
  })

  it('restores a contact and names it, and says a removed one is gone', async () => {
    expect(await provider('contacts').restore({ space: 'contacts', contact: 1 })).toBe(true)
    expect(get(selectedContactId)).toBe(1)
    expect(provider('contacts').label(provider('contacts').describe())).toBe('ada@example.org')
    expect(await provider('contacts').restore({ space: 'contacts', contact: 2 })).toBe(false)
  })
})

describe('notes', () => {
  it('names a note by its title or file name', () => {
    expect(noteName('Projects/Plan.md')).toBe('Plan')
    vaultIndex.set({ notes: [{ path: 'a.md', title: 'Alpha' }], files: [] } as never)
    expect(provider('notes').label({ space: 'notes', note: 'a.md' })).toBe('Alpha')
    expect(provider('notes').label({ space: 'notes', note: 'dir/b.md' })).toBe('b')
  })

  it('restores a note in the vault and says a deleted one is gone', async () => {
    vaultIndex.set({ notes: [{ path: 'a.md', title: 'Alpha' }], files: [] } as never)
    expect(await provider('notes').restore({ space: 'notes', note: 'b.md' })).toBe(false)
    expect(await provider('notes').restore({ space: 'notes', note: 'a.md' })).toBe(true)
    expect(get(activeNote)).toBe('a.md')
    expect(get(space)).toBe('notes')
  })
})

describe('feeds', () => {
  beforeEach(() => {
    feedSidebar.set({ feeds: [{ id: 2, title: 'Blog' }], folders: [], counts: { unread: 0 } } as never)
    feedList.update((s) => ({ ...s, scope: { kind: 'feed', id: 2 }, rows: [] }))
    feedSelected.set(null)
    feedFocus.set(null)
  })

  it('reads an entry id from a row key', () => {
    expect(entryId('e42')).toBe(42)
    expect(entryId('m42')).toBeNull()
    expect(entryId(undefined)).toBeNull()
  })

  it('describes the scope and the open row, named by the scope', () => {
    feedSelected.set('e5')
    const loc = provider('feeds').describe()
    expect(loc).toEqual({ space: 'feeds', scope: { kind: 'feed', id: 2 }, entry: 'e5' })
    expect(provider('feeds').label({ space: 'feeds', scope: { kind: 'feed', id: 2 } })).toBe('Blog')
  })

  it('restores an entry through the feed focus, and says a removed feed or entry is gone', async () => {
    getFeedEntry.mockResolvedValue({ id: 5, feedId: 2 })
    expect(await provider('feeds').restore({ space: 'feeds', scope: { kind: 'feed', id: 2 }, entry: 'e5' })).toBe(true)
    expect(get(feedFocus)).toBe(5)
    expect(await provider('feeds').restore({ space: 'feeds', scope: { kind: 'feed', id: 3 } })).toBe(false)
    getFeedEntry.mockRejectedValue(new Error('gone'))
    expect(await provider('feeds').restore({ space: 'feeds', scope: { kind: 'feed', id: 2 }, entry: 'e6' })).toBe(false)
  })
})

describe('chat', () => {
  beforeEach(() => {
    chatRooms.set([{ id: '!a', name: 'Team' } as ChatRoom])
    activeRoomId.set(null)
  })

  it('restores a room only while chat is on and the room is there', async () => {
    prefs.update((p) => ({ ...p, chatEnabled: true }))
    expect(await provider('chat').restore({ space: 'chat', room: '!a' })).toBe(true)
    expect(get(activeRoomId)).toBe('!a')
    expect(provider('chat').label(provider('chat').describe())).toBe('Team')
    expect(await provider('chat').restore({ space: 'chat', room: '!gone' })).toBe(false)
    prefs.update((p) => ({ ...p, chatEnabled: false }))
    expect(await provider('chat').restore({ space: 'chat', room: '!a' })).toBe(false)
  })
})

describe('location keys', () => {
  it('are equal for the same place whatever the key order', () => {
    expect(locationKey({ space: 'mail', message: 1, list: { kind: 'view', view: 'inbox' } })).toBe(
      locationKey({ space: 'mail', list: { view: 'inbox', kind: 'view' }, message: 1 }),
    )
    expect(locationKey({ space: 'notes' })).toBe(locationKey({ space: 'notes', note: undefined }))
  })
})
