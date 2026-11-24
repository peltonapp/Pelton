import { describe, expect, it } from 'vitest'
import {
  dropOrder,
  entryRow,
  feedsIn,
  fileName,
  folderOrder,
  folderUnread,
  hostOf,
  mailRow,
  mediaKind,
  neighbour,
  parseScopeKey,
  sameScope,
  scopeKey,
  scopeTitle,
  scrolledPast,
  shareMail,
  type FeedRow,
} from './feedlogic'
import type { Feed, FeedEntry, FeedSidebar, MessageSummary } from './types'

function feed(patch: Partial<Feed>): Feed {
  return {
    id: 1,
    folderId: 0,
    url: 'https://a.example/feed',
    siteUrl: '',
    title: 'A',
    feedTitle: 'A',
    customTitle: '',
    description: '',
    icon: '',
    error: '',
    errorCount: 0,
    lastFetched: '',
    lastSuccess: '',
    nextFetch: '',
    intervalMinutes: 0,
    loadImages: false,
    notify: false,
    position: 0,
    unread: 0,
    ...patch,
  }
}

function row(key: string, read = false): FeedRow {
  return { key, id: Number(key.slice(1)), mail: false, feedId: 1, title: key, source: '', url: '', author: '', published: '', snippet: '', image: '', read, starred: false, readLater: false, media: [] }
}

const t = (key: string): string => `[${key}]`

describe('scopes', () => {
  it('names, compares and reads back scopes', () => {
    expect(scopeKey({ kind: 'feed', id: 4 })).toBe('feed:4')
    expect(scopeKey({ kind: 'today' })).toBe('today')
    expect(sameScope({ kind: 'folder', id: 2 }, { kind: 'folder', id: 2 })).toBe(true)
    expect(sameScope({ kind: 'folder', id: 2 }, { kind: 'feed', id: 2 })).toBe(false)
    expect(parseScopeKey('folder:0')).toEqual({ kind: 'folder', id: 0 })
    expect(parseScopeKey('later')).toEqual({ kind: 'later' })
    expect(parseScopeKey('feed:x')).toEqual({ kind: 'unread' })
    expect(parseScopeKey('nonsense')).toEqual({ kind: 'unread' })
  })

  it('titles a scope from the sidebar', () => {
    const side: FeedSidebar = { folders: [{ id: 3, name: 'Tech', position: 0 }], feeds: [feed({ id: 9, title: 'Nine' })], counts: { unread: 0, today: 0, starred: 0, readLater: 0 } }
    expect(scopeTitle({ kind: 'feed', id: 9 }, side, t)).toBe('Nine')
    expect(scopeTitle({ kind: 'folder', id: 3 }, side, t)).toBe('Tech')
    expect(scopeTitle({ kind: 'folder', id: 0 }, side, t)).toBe('[feeds.noFolder]')
    expect(scopeTitle({ kind: 'starred' }, side, t)).toBe('[feeds.smart.starred]')
    expect(scopeTitle({ kind: 'feed', id: 404 }, side, t)).toBe('[feeds.title]')
  })
})

describe('folders', () => {
  const feeds = [
    feed({ id: 1, folderId: 5, position: 1, unread: 2 }),
    feed({ id: 2, folderId: 5, position: 0, unread: 3 }),
    feed({ id: 3, folderId: 0, position: 0, unread: 7 }),
  ]

  it('lists a folder in order and sums its unread', () => {
    expect(feedsIn(feeds, 5).map((f) => f.id)).toEqual([2, 1])
    expect(folderUnread(feeds, 5)).toBe(5)
    expect(folderUnread(feeds, 0)).toBe(7)
  })

  it('places a dropped feed', () => {
    expect(dropOrder(feeds, 3, 5, 1)).toEqual([2, 3, 1])
    expect(dropOrder(feeds, 3, 5, null)).toEqual([2, 1, 3])
    expect(dropOrder(feeds, 1, 5, 2)).toEqual([1, 2])
    expect(dropOrder(feeds, 2, 0, 404)).toEqual([3, 2])
  })

  it('reorders folders', () => {
    expect(folderOrder([1, 2, 3], 3, 1)).toEqual([3, 1, 2])
    expect(folderOrder([1, 2, 3], 1, null)).toEqual([2, 3, 1])
  })
})

describe('rows', () => {
  it('builds rows from entries and newsletter mail', () => {
    const e: FeedEntry = { id: 7, feedId: 1, title: 'T', url: 'u', author: 'a', published: 'p', snippet: 's', image: '', read: true, starred: true, readLater: false, media: [] }
    const r = entryRow(e, [feed({ id: 1, title: 'Blog' })])
    expect(r).toMatchObject({ key: 'e7', id: 7, mail: false, source: 'Blog', read: true, starred: true })
    const m = mailRow({ id: 7, subject: 'Weekly', fromName: '', fromAddress: 'news@x.example', seen: false, flagged: true, date: 'd', snippet: 's' } as MessageSummary)
    expect(m).toMatchObject({ key: 'm7', mail: true, title: 'Weekly', source: 'news@x.example', read: false, starred: true, readLater: false })
  })

  it('moves to the next and previous row, or the next unread one', () => {
    const rows = [row('e1', true), row('e2'), row('e3', true), row('e4')]
    expect(neighbour(rows, 'e1', 1)?.key).toBe('e2')
    expect(neighbour(rows, 'e2', -1)?.key).toBe('e1')
    expect(neighbour(rows, 'e2', 1, true)?.key).toBe('e4')
    expect(neighbour(rows, 'e4', 1)).toBeNull()
    expect(neighbour(rows, 'e4', -1, true)?.key).toBe('e2')
    expect(neighbour(rows, null, 1, true)?.key).toBe('e2')
    expect(neighbour(rows, null, -1)?.key).toBe('e4')
    expect(neighbour(rows, 'gone', 1)?.key).toBe('e1')
  })

  it('finds the unread rows scrolled past the top', () => {
    const rows = [row('e1'), row('e2', true), row('e3')]
    const bottoms = new Map([
      ['e1', 60],
      ['e2', 120],
      ['e3', 180],
    ])
    expect(scrolledPast(rows, bottoms, 130).map((r) => r.key)).toEqual(['e1'])
    expect(scrolledPast(rows, bottoms, 0)).toEqual([])
  })
})

describe('media and links', () => {
  it('tells enclosures apart by type, then by file name', () => {
    expect(mediaKind({ url: 'x', type: 'audio/mpeg', length: 0, title: '' })).toBe('audio')
    expect(mediaKind({ url: 'https://x/ep.MP3?t=1', type: '', length: 0, title: '' })).toBe('audio')
    expect(mediaKind({ url: 'https://x/v.webm', type: '', length: 0, title: '' })).toBe('video')
    expect(mediaKind({ url: 'https://x/p.jpg', type: 'image/jpeg', length: 0, title: '' })).toBe('image')
    expect(mediaKind({ url: 'https://x/doc.pdf', type: 'application/pdf', length: 0, title: '' })).toBe('file')
  })

  it('names files and hosts', () => {
    expect(fileName('https://cdn.example.com/a/My%20Episode.mp3?x=1')).toBe('My Episode.mp3')
    expect(fileName('https://cdn.example.com/')).toBe('cdn.example.com')
    expect(fileName('not a url')).toBe('not a url')
    expect(hostOf('https://www.example.com/x')).toBe('example.com')
    expect(hostOf('nope')).toBe('')
  })

  it('writes the share mail for each editor', () => {
    expect(shareMail('A <b> & c', 'https://x.example/?a=1&b=2', 'wysiwyg')).toEqual({
      subject: 'A <b> & c',
      body: '<p>A &lt;b&gt; &amp; c</p><p><a href="https://x.example/?a=1&amp;b=2">https://x.example/?a=1&amp;b=2</a></p>',
    })
    expect(shareMail('Post [1]', 'https://x.example/p', 'markdown').body).toBe('[Post \\[1\\]](https://x.example/p)\n')
    expect(shareMail('Post', 'https://x.example/p', 'plaintext').body).toBe('Post\n\nhttps://x.example/p\n')
    expect(shareMail('  ', 'https://x.example/p', 'plaintext').subject).toBe('https://x.example/p')
  })
})
