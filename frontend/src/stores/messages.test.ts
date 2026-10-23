import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import { idle, ready } from '../lib/async'
import type { MessageSummary } from '../lib/types'

// the sidebar refresh is stubbed so the assertions are about which changes ask
// for one, not about the request it would make.
vi.mock('./sidebarcounts', () => ({ refreshCountsSoon: vi.fn() }))

const api = vi.hoisted(() => ({
  listFolderMessages: vi.fn(),
  fetchOlderMessages: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  listFolderMessages: api.listFolderMessages,
  fetchOlderMessages: api.fetchOlderMessages,
}))

import {
  messageList,
  neighbourInList,
  patchInList,
  refreshListHead,
  removeFromList,
  restoreToList,
  loadList,
  loadMore,
  loadOlder,
  listIsPaginated,
  PAGE_SIZE,
} from './messages'
import { refreshCountsSoon } from './sidebarcounts'

const refreshed = vi.mocked(refreshCountsSoon)

function summary(id: number, over: Partial<MessageSummary> = {}): MessageSummary {
  return {
    id,
    accountId: 1,
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject: `message ${id}`,
    fromName: 'Ada',
    fromAddress: 'ada@example.com',
    snippet: '',
    // descending by id, matching the newest-first order the list is kept in;
    // an hour apart so a list of any length stays ordered.
    date: new Date(Date.UTC(2026, 8, 30, 12) - id * 3_600_000).toISOString(),
    seen: false,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    ...over,
  }
}

function load(ids: number[], total = ids.length): void {
  messageList.set(
    ready({
      items: ids.map((id) => summary(id)),
      total,
      searching: false,
      hasOlder: false,
      backfilling: false,
    }),
  )
}

function ids(): number[] {
  return get(messageList).data?.items.map((m) => m.id) ?? []
}

beforeEach(() => {
  api.listFolderMessages.mockReset()
  api.fetchOlderMessages.mockReset()
  messageList.set(idle())
  refreshed.mockClear()
})

describe('refreshListHead', () => {
  const inbox = { kind: 'folder', folderId: 1, accountId: 1, label: 'INBOX' } as const

  it('leaves a paged list alone when body sync announces existing rows', async () => {
    const firstPage = Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 1))
    api.listFolderMessages.mockResolvedValue({ messages: firstPage, total: 52, hasOlder: false })
    await loadList(inbox)
    // the user has paged ahead: rows 51 and 52 follow the first page.
    load([...firstPage.map((m) => m.id), 51, 52], 52)

    await refreshListHead(inbox)

    expect(get(messageList).data!.items).toHaveLength(52)
    expect(get(messageList).data!.items[0].id).toBe(1)
    expect(get(messageList).data!.items.at(-1)?.id).toBe(52)
    expect(api.listFolderMessages).toHaveBeenCalledTimes(2)
  })

  it('prepends stubs that arrived during body fill', async () => {
    api.listFolderMessages.mockResolvedValue({ messages: [summary(1), summary(2), summary(3)], total: 3, hasOlder: false })
    await loadList(inbox)
    load([2, 3])

    await refreshListHead(inbox)

    expect(ids()).toEqual([1, 2, 3])
    expect(get(messageList).data!.total).toBe(3)
  })

  it('updates rows already on screen once their body is stored', async () => {
    api.listFolderMessages.mockResolvedValueOnce({ messages: [summary(1), summary(2)], total: 2, hasOlder: false })
    await loadList(inbox)
    api.listFolderMessages.mockResolvedValueOnce({
      messages: [summary(1), summary(2, { snippet: 'full text', hasAttachments: true })],
      total: 2,
      hasOlder: false,
    })

    await refreshListHead(inbox)

    expect(ids()).toEqual([1, 2])
    expect(get(messageList).data!.items[1]).toMatchObject({ snippet: 'full text', hasAttachments: true })
  })

  it('drops loaded rows within the head that left the folder', async () => {
    const firstPage = Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 1))
    api.listFolderMessages.mockResolvedValueOnce({ messages: firstPage, total: 60, hasOlder: false })
    await loadList(inbox)
    api.listFolderMessages.mockResolvedValueOnce({
      messages: Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 51)),
      total: 60,
      hasOlder: false,
    })
    await loadMore()

    // 3 and 7 were deleted elsewhere; the head now reaches down to 52.
    const head = firstPage.filter((m) => m.id !== 3 && m.id !== 7).concat([summary(51), summary(52)])
    api.listFolderMessages.mockResolvedValueOnce({ messages: head, total: 58, hasOlder: false })
    await refreshListHead(inbox)

    expect(ids()).not.toContain(3)
    expect(ids()).not.toContain(7)
    expect(ids()).toContain(53)
    expect(get(messageList).data!.total).toBe(58)
  })

  it('keeps loaded rows older than the head even when the head lacks them', async () => {
    const firstPage = Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 1))
    api.listFolderMessages.mockResolvedValueOnce({ messages: firstPage, total: 52, hasOlder: false })
    await loadList(inbox)
    load([...firstPage.map((m) => m.id), 51, 52], 52)

    api.listFolderMessages.mockResolvedValueOnce({ messages: firstPage, total: 52, hasOlder: false })
    await refreshListHead(inbox)

    expect(ids()).toHaveLength(52)
  })

  it('drops every vanished row when the head is the whole folder', async () => {
    api.listFolderMessages.mockResolvedValueOnce({ messages: [summary(1), summary(2), summary(3)], total: 3, hasOlder: false })
    await loadList(inbox)
    api.listFolderMessages.mockResolvedValueOnce({ messages: [summary(1)], total: 1, hasOlder: false })

    await refreshListHead(inbox)

    expect(ids()).toEqual([1])
  })

  it('moves the next page back by the rows that left, so none is skipped', async () => {
    const firstPage = Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 1))
    api.listFolderMessages.mockResolvedValueOnce({ messages: firstPage, total: 120, hasOlder: false })
    await loadList(inbox)
    api.listFolderMessages.mockResolvedValueOnce({
      messages: Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 51)),
      total: 120,
      hasOlder: false,
    })
    await loadMore()
    expect(api.listFolderMessages).toHaveBeenLastCalledWith(1, PAGE_SIZE, PAGE_SIZE)

    // two rows left and one arrived: the server's rows shifted up by one.
    const head = [summary(0), ...firstPage.filter((m) => m.id !== 3 && m.id !== 7), summary(51)]
    api.listFolderMessages.mockResolvedValueOnce({ messages: head, total: 119, hasOlder: false })
    await refreshListHead(inbox)

    api.listFolderMessages.mockResolvedValueOnce({ messages: [], total: 119, hasOlder: false })
    await loadMore()
    expect(api.listFolderMessages).toHaveBeenLastCalledWith(1, PAGE_SIZE, 2 * PAGE_SIZE - 1)
  })

  it('queues one refresh while the next page loads and runs it after', async () => {
    const firstPage = Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 1))
    api.listFolderMessages.mockResolvedValueOnce({ messages: firstPage, total: 52, hasOlder: false })
    await loadList(inbox)
    let page!: (v: unknown) => void
    api.listFolderMessages.mockReturnValueOnce(new Promise((r) => (page = r)))
    const more = loadMore()

    await refreshListHead(inbox)
    await refreshListHead(inbox)
    expect(api.listFolderMessages).toHaveBeenCalledTimes(2)
    expect(get(messageList).status).toBe('loading')

    api.listFolderMessages.mockResolvedValueOnce({ messages: [summary(0), ...firstPage], total: 53, hasOlder: false })
    page({ messages: [summary(51), summary(52)], total: 52, hasOlder: false })
    await more
    await vi.waitFor(() => expect(ids()[0]).toBe(0))
    expect(api.listFolderMessages).toHaveBeenCalledTimes(3)
    expect(ids()).toHaveLength(53)
    expect(get(messageList).status).toBe('ready')
  })
})

// mail:new reloads the list to one page unless it is paginated, so a page
// still loading must count, or the reload drops it and jumps back to the top.
describe('listIsPaginated', () => {
  const inbox = { kind: 'folder', folderId: 1, accountId: 1, label: 'INBOX' } as const

  it('is false on the first page alone', async () => {
    api.listFolderMessages.mockResolvedValue({ messages: [summary(1)], total: 60, hasOlder: false })
    await loadList(inbox)

    expect(listIsPaginated()).toBe(false)
  })

  it('is true while the first loadMore is in flight, and after it lands', async () => {
    const firstPage = Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 1))
    api.listFolderMessages.mockResolvedValueOnce({ messages: firstPage, total: 52, hasOlder: false })
    await loadList(inbox)
    let page!: (v: unknown) => void
    api.listFolderMessages.mockReturnValueOnce(new Promise((r) => (page = r)))

    const more = loadMore()
    expect(listIsPaginated()).toBe(true)

    page({ messages: [summary(51), summary(52)], total: 52, hasOlder: false })
    await more
    expect(listIsPaginated()).toBe(true)
  })

  it('is false again once a failed first loadMore settles', async () => {
    const firstPage = Array.from({ length: PAGE_SIZE }, (_, i) => summary(i + 1))
    api.listFolderMessages.mockResolvedValueOnce({ messages: firstPage, total: 52, hasOlder: false })
    await loadList(inbox)
    api.listFolderMessages.mockRejectedValueOnce(new Error('offline'))

    await loadMore()

    expect(listIsPaginated()).toBe(false)
  })

  it('is false while a new selection loads', async () => {
    api.listFolderMessages.mockResolvedValueOnce({ messages: [summary(1)], total: 60, hasOlder: false })
    await loadList(inbox)
    api.listFolderMessages.mockReturnValueOnce(new Promise(() => {}))

    void loadList({ ...inbox, folderId: 2 })

    expect(listIsPaginated()).toBe(false)
  })
})

// #403: the sidebar used to wait for the next sync, so a folder kept its unread
// count and its bold highlight long after the mail had been dealt with.
describe('sidebar counts follow local changes', () => {
  it('refreshes when a message leaves the list', () => {
    load([1, 2, 3])
    removeFromList(2)
    expect(refreshed).toHaveBeenCalledTimes(1)
  })

  // deleting from the reading pane can name a message off the loaded page. The
  // row is not there to remove, but the folder's count still changed.
  it('refreshes even when the row was not loaded', () => {
    load([1, 2])
    removeFromList(99)
    expect(refreshed).toHaveBeenCalledTimes(1)
  })

  it('refreshes when an undone delete puts a message back', () => {
    load([1, 3])
    restoreToList(summary(2))
    expect(refreshed).toHaveBeenCalledTimes(1)
  })

  it('refreshes on the two flags the sidebar counts', () => {
    load([1, 2])
    patchInList(1, { seen: true })
    patchInList(2, { flagged: true })
    expect(refreshed).toHaveBeenCalledTimes(2)
  })

  // a color label and an offline copy change nothing the sidebar shows, so
  // re-reading the whole tree for them would be work for no visible result.
  it('stays put for changes the sidebar does not show', () => {
    load([1, 2])
    patchInList(1, { flagColor: 3 })
    patchInList(2, { offline: true })
    patchInList(1, { snoozeUntil: '2026-10-01T09:00:00Z' })
    expect(refreshed).not.toHaveBeenCalled()
  })

  // marking an already-read message read is still a statement about read state;
  // the sidebar is asked either way rather than this guessing at the outcome.
  it('refreshes on a seen patch that changes nothing', () => {
    load([1])
    patchInList(1, { seen: false })
    expect(refreshed).toHaveBeenCalledTimes(1)
  })
})

describe('removeFromList', () => {
  it('drops the row and decrements the total', () => {
    load([1, 2, 3])
    removeFromList(2)
    expect(ids()).toEqual([1, 3])
    expect(get(messageList).data!.total).toBe(2)
  })

  // deleting from the reading pane can name a message that is not on the
  // loaded page; decrementing then would drift the total away from what the
  // server actually holds.
  it('leaves the total alone for a row that was not loaded', () => {
    load([1, 2, 3], 50)
    removeFromList(99)
    expect(ids()).toEqual([1, 2, 3])
    expect(get(messageList).data!.total).toBe(50)
  })

  it('never drives the total below zero', () => {
    load([1], 0)
    removeFromList(1)
    expect(get(messageList).data!.total).toBe(0)
  })

  it('does nothing while the list is not loaded', () => {
    removeFromList(1)
    expect(get(messageList).status).toBe('idle')
  })
})

describe('restoreToList', () => {
  it('puts a row back in date order', () => {
    load([1, 2, 4])
    restoreToList(summary(3))
    expect(ids()).toEqual([1, 2, 3, 4])
    expect(get(messageList).data!.total).toBe(4)
  })

  it('restores to the top when it is the newest', () => {
    load([2, 3])
    restoreToList(summary(1))
    expect(ids()).toEqual([1, 2, 3])
  })

  // undo can be pressed after a sync already brought the message back, and a
  // second copy would break the list, which renders rows keyed by id.
  it('refuses to add a row that is already there', () => {
    load([1, 2])
    restoreToList(summary(2))
    expect(ids()).toEqual([1, 2])
    expect(get(messageList).data!.total).toBe(2)
  })

  it('does nothing while the list is not loaded', () => {
    restoreToList(summary(1))
    expect(get(messageList).status).toBe('idle')
  })
})

describe('patchInList', () => {
  it('updates only the named row', () => {
    load([1, 2])
    patchInList(2, { seen: true })
    expect(get(messageList).data!.items.map((m) => m.seen)).toEqual([false, true])
  })

  it('leaves the other fields alone', () => {
    load([1])
    patchInList(1, { flagged: true })
    const row = get(messageList).data!.items[0]
    expect(row.flagged).toBe(true)
    expect(row.subject).toBe('message 1')
    expect(row.seen).toBe(false)
  })

  it('ignores an id that is not loaded', () => {
    load([1])
    patchInList(99, { seen: true })
    expect(get(messageList).data!.items[0].seen).toBe(false)
  })

  it('does nothing while the list is not loaded', () => {
    patchInList(1, { seen: true })
    expect(get(messageList).status).toBe('idle')
  })
})

describe('neighbourInList', () => {
  it('picks the row below', () => {
    load([1, 2, 3])
    expect(neighbourInList(2)).toBe(3)
  })

  it('falls back to the row above for the last one', () => {
    load([1, 2, 3])
    expect(neighbourInList(3)).toBe(2)
  })

  it('has nothing to pick in a list of one', () => {
    load([1])
    expect(neighbourInList(1)).toBeNull()
  })

  // a bulk delete would otherwise land on a row that is about to go as well.
  it('skips rows that are going too', () => {
    load([1, 2, 3, 4])
    expect(neighbourInList(1, new Set([2, 3]))).toBe(4)
  })

  it('searches upwards past skipped rows', () => {
    load([1, 2, 3, 4])
    expect(neighbourInList(3, new Set([4, 2]))).toBe(1)
  })

  it('has nothing to pick when the whole list is going', () => {
    load([1, 2, 3])
    expect(neighbourInList(2, new Set([1, 2, 3]))).toBeNull()
  })

  it('returns null for a row that is not loaded', () => {
    load([1, 2])
    expect(neighbourInList(99)).toBeNull()
  })

  it('returns null while the list is not loaded', () => {
    expect(neighbourInList(1)).toBeNull()
  })
})

// A folder on a simulated server: rows kept in the order the real query reads
// them (date DESC, uid DESC), changed between and during page reads the way
// sync and the user change them. Whatever happens, paging to the end has to
// show every row the server holds, each once.
describe('paging against a changing server', () => {
  const inbox = { kind: 'folder', folderId: 1, accountId: 1, label: 'INBOX' } as const
  type Row = { id: number; uid: number; date: string }

  let rows: Row[]
  // older rows the server has not cached yet; fetchOlderMessages moves them in.
  let uncached: Row[]
  // runs once, inside the next page read, before the server answers it.
  let duringRead: (() => Promise<void> | void) | null

  const at = (hour: number): string => new Date(Date.UTC(2026, 8, 30, 12) - hour * 3_600_000).toISOString()
  const sort = (): void => {
    rows.sort((a, b) => (a.date !== b.date ? (a.date < b.date ? 1 : -1) : b.uid - a.uid))
  }
  // id doubles as uid: a later uid is newer mail.
  const add = (...list: Row[]): void => {
    rows.push(...list)
    sort()
  }
  const drop = (id: number): void => {
    rows = rows.filter((r) => r.id !== id)
  }
  const row = (id: number, hour: number): Row => ({ id, uid: id, date: at(hour) })

  beforeEach(() => {
    rows = []
    uncached = []
    duringRead = null
    api.listFolderMessages.mockImplementation(async (_folder: number, limit: number, offset: number) => {
      const hook = duringRead
      duringRead = null
      if (hook) {
        await hook()
      }
      return {
        messages: rows.slice(offset, offset + limit).map((r) => summary(r.id, { date: r.date })),
        total: rows.length,
        hasOlder: uncached.length > 0,
      }
    })
    api.fetchOlderMessages.mockImplementation(async () => {
      const batch = uncached.splice(0, PAGE_SIZE)
      add(...batch)
      return { fetched: batch.length, hasOlder: uncached.length > 0 }
    })
  })

  // scrollToEnd pages on until the list says there is nothing more.
  async function scrollToEnd(): Promise<void> {
    for (let i = 0; i < 20; i++) {
      const d = get(messageList).data!
      if (d.items.length >= d.total && !d.hasOlder) {
        return
      }
      await loadMore()
    }
  }

  function expectEveryRowOnce(): void {
    const shown = ids()
    expect(new Set(shown).size).toBe(shown.length)
    expect([...shown].sort((a, b) => a - b)).toEqual(rows.map((r) => r.id).sort((a, b) => a - b))
  }

  it('new mail saved while a page is read', async () => {
    add(...Array.from({ length: 150 }, (_, i) => row(i + 1, i + 10)))
    await loadList(inbox)

    // three new messages land before the server answers the page.
    duringRead = () => add(row(1001, 1), row(1002, 2), row(1003, 3))
    await loadMore()
    await refreshListHead(inbox)
    await scrollToEnd()

    expectEveryRowOnce()
  })

  it('a local delete before the next page', async () => {
    add(...Array.from({ length: 150 }, (_, i) => row(i + 1, i + 10)))
    await loadList(inbox)

    drop(10)
    removeFromList(10)
    await scrollToEnd()

    expectEveryRowOnce()
  })

  it('an undone delete before the next page', async () => {
    add(...Array.from({ length: 150 }, (_, i) => row(i + 1, i + 10)))
    await loadList(inbox)
    const gone = get(messageList).data!.items.find((m) => m.id === 10)!

    drop(10)
    removeFromList(10)
    add(row(10, 19))
    restoreToList(gone)
    await scrollToEnd()

    expectEveryRowOnce()
  })

  it('a head merge while older mail is backfilled', async () => {
    add(...Array.from({ length: 50 }, (_, i) => row(i + 1, i + 10)))
    uncached = Array.from({ length: 120 }, (_, i) => row(i + 101, i + 100))
    await loadList(inbox)

    // the backfill's page read is answered, then new mail arrives and one
    // message leaves before that answer reaches the list.
    api.listFolderMessages.mockImplementationOnce(async (_f: number, limit: number, offset: number) => {
      const page = {
        messages: rows.slice(offset, offset + limit).map((r) => summary(r.id, { date: r.date })),
        total: rows.length,
        hasOlder: uncached.length > 0,
      }
      add(row(1001, 1), row(1002, 2))
      drop(5)
      await refreshListHead(inbox)
      return page
    })
    await loadOlder()
    await scrollToEnd()

    expectEveryRowOnce()
  })

  it('rows tying on date across the head boundary', async () => {
    // rows 46 to 60 all carry the same date; the server orders them by uid.
    add(...Array.from({ length: 45 }, (_, i) => row(i + 1, i + 10)))
    add(...Array.from({ length: 15 }, (_, i) => row(i + 46, 80)))
    add(...Array.from({ length: 40 }, (_, i) => row(i + 61, i + 100)))
    await loadList(inbox)
    await loadMore()

    add(row(1001, 1))
    await refreshListHead(inbox)
    await scrollToEnd()

    expectEveryRowOnce()
  })
})
