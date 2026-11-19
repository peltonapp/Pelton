import { describe, expect, it } from 'vitest'
import { buildTimeline, groupWindowMs } from './chattimeline'
import { prepareHTML, plainToHTML, isEmojiOnly } from './chathtml'
import { replaceShortcodes, shortcodeQuery, searchShortcodes } from './emoji'
import { mentionQuery, matchMembers, applyMention, mentionsIn } from './chatmentions'
import { roomSections, previewLine } from './chatrooms'
import { stateText } from './chatstate'
import type { ChatMember, ChatMessage, ChatRoom } from './types'

function msg(id: string, sender: string, ts: number, patch: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id,
    sender,
    senderName: sender.slice(1, 4),
    senderAvatar: '',
    ts,
    kind: 'text',
    body: id,
    html: '',
    edited: false,
    own: sender === '@me:x',
    replyTo: null,
    reactions: [],
    media: null,
    state: null,
    encrypted: false,
    decryptError: '',
    missingKeys: false,
    mentioned: false,
    readBy: [],
    ...patch,
  }
}

const day = new Date(2026, 10, 16, 10, 0).getTime()

describe('buildTimeline', () => {
  it('groups one sender within the window and breaks on a new day, a gap or a state row', () => {
    const items = buildTimeline(
      [
        msg('$1', '@bob:x', day),
        msg('$2', '@bob:x', day + 60_000),
        msg('$3', '@bob:x', day + 60_000 + groupWindowMs + 1),
        msg('$4', '@ann:x', day + 2 * groupWindowMs),
        msg('$s', '@ann:x', day + 2 * groupWindowMs + 1, { kind: 'state', state: { kind: 'joined', target: '@ann:x', targetName: 'Ann', value: '' } }),
        msg('$5', '@ann:x', day + 2 * groupWindowMs + 2),
        msg('$6', '@ann:x', day + 24 * 3600_000),
      ],
      { unreadAfter: '', ownId: '@me:x' },
    )
    const shape = items.map((i) => (i.type === 'message' ? `${i.key}${i.continued ? '+' : ''}` : i.type))
    expect(shape).toEqual(['day', '$1', '$2+', '$3', '$4', '$s', '$5', 'day', '$6'])
  })

  it('puts the unread marker after the last read message only when someone else wrote since', () => {
    const list = [msg('$1', '@bob:x', day), msg('$2', '@bob:x', day + 1000), msg('$3', '@me:x', day + 2000)]
    const marked = buildTimeline(list, { unreadAfter: '$1', ownId: '@me:x' })
    expect(marked.map((i) => i.type)).toEqual(['day', 'message', 'unread', 'message', 'message'])
    // after $2 only the user's own message follows: no marker.
    expect(buildTimeline(list, { unreadAfter: '$2', ownId: '@me:x' }).some((i) => i.type === 'unread')).toBe(false)
    expect(buildTimeline(list, { unreadAfter: '$gone', ownId: '@me:x' }).some((i) => i.type === 'unread')).toBe(false)
  })

  it('adds pending messages at the end, continuing the user own group', () => {
    const items = buildTimeline([msg('$1', '@me:x', day)], {
      unreadAfter: '',
      ownId: '@me:x',
      pending: [{ localId: 'l1', text: 'hi', createdAt: day + 1000 }],
    })
    expect(items.at(-1)).toEqual({ type: 'pending', key: 'l1', localId: 'l1', continued: true })
  })
})

describe('prepareHTML', () => {
  it('marks safe links, makes pills of people and holds back mxc images', () => {
    const out = prepareHTML(
      '<a href="https://example.org" target="_blank">site</a> <a href="https://matrix.to/#/%40bob%3Ax">Bob</a> <a href="mxc://x/y">odd</a><img src="mxc://x/abc"><img src="https://evil.example/p.gif">',
    )
    const tpl = document.createElement('template')
    tpl.innerHTML = out.html
    const links = tpl.content.querySelectorAll('a')
    expect(links[0].hasAttribute('data-chat-link')).toBe(true)
    expect(links[0].hasAttribute('target')).toBe(false)
    expect(links[1].getAttribute('data-chat-user')).toBe('@bob:x')
    expect(links[1].classList.contains('pill')).toBe(true)
    expect(links[2].hasAttribute('href')).toBe(false)
    const imgs = tpl.content.querySelectorAll('img')
    expect(imgs).toHaveLength(1)
    expect(imgs[0].hasAttribute('src')).toBe(false)
    expect(imgs[0].getAttribute('data-mxc')).toBe('mxc://x/abc')
    expect(out.images).toEqual(['mxc://x/abc'])
    expect(out.html).not.toContain('evil.example')
  })

  it('turns hex colours into inline colour and ignores anything else', () => {
    const out = prepareHTML('<font data-mx-color="#ff0000">red</font><span data-mx-color="red">no</span>')
    expect(out.html).toContain('color:#ff0000')
    expect(out.html).not.toContain('color:red')
  })

  it('escapes plain text and links its urls', () => {
    const html = plainToHTML('<b>hi</b> see https://example.org.')
    expect(html).toContain('&lt;b&gt;hi&lt;/b&gt;')
    expect(html).toContain('<a href="https://example.org" data-chat-link="">https://example.org</a>.')
  })

  it('knows emoji-only bodies', () => {
    expect(isEmojiOnly('🎉🎉')).toBe(true)
    expect(isEmojiOnly('👍🏽')).toBe(true)
    expect(isEmojiOnly('ok 🎉')).toBe(false)
    expect(isEmojiOnly('123')).toBe(false)
  })
})

describe('emoji shortcodes', () => {
  it('replaces known codes outside code spans', () => {
    expect(replaceShortcodes('nice :tada: and `:tada:` :nope:')).toBe('nice 🎉 and `:tada:` :nope:')
  })
  it('finds a code being typed and suggests matches', () => {
    expect(shortcodeQuery('hi :thu', 7)).toEqual({ start: 3, query: 'thu' })
    expect(shortcodeQuery('time 10:30', 10)).toBeNull()
    expect(searchShortcodes('thumbs')[0]).toEqual({ code: 'thumbsup', emoji: '👍' })
  })
})

describe('mentions', () => {
  const members: ChatMember[] = [
    { userId: '@me:x', name: 'Me', avatar: '', membership: 'join', powerLevel: 0 },
    { userId: '@bob:x', name: 'Bob Builder', avatar: '', membership: 'join', powerLevel: 0 },
    { userId: '@rob:x', name: 'Robert', avatar: '', membership: 'join', powerLevel: 0 },
  ]
  it('finds the query and matches members but never the user', () => {
    expect(mentionQuery('hey @bo', 7)).toEqual({ start: 4, query: 'bo' })
    expect(mentionQuery('mail a@b', 8)).toBeNull()
    expect(matchMembers(members, 'ob', '@me:x').map((m) => m.userId)).toEqual(['@bob:x', '@rob:x'])
    expect(matchMembers(members, 'me', '@me:x')).toEqual([])
  })
  it('inserts the name and keeps only mentions still in the text', () => {
    const q = mentionQuery('hey @bo', 7)!
    const next = applyMention('hey @bo', q, 7, members[1])
    expect(next).toEqual({ text: 'hey @Bob Builder ', caret: 17 })
    const chosen = [
      { userId: '@bob:x', name: 'Bob Builder' },
      { userId: '@rob:x', name: 'Robert' },
    ]
    expect(mentionsIn(next.text, chosen)).toEqual([chosen[0]])
  })
})

function room(id: string, patch: Partial<ChatRoom> = {}): ChatRoom {
  return {
    id,
    name: id,
    topic: '',
    avatar: '',
    alias: '',
    kind: 'room',
    directUser: '',
    encrypted: false,
    membership: 'join',
    inviterId: '',
    inviterName: '',
    favourite: false,
    lowPriority: false,
    unread: 0,
    highlight: 0,
    lastTs: 0,
    preview: '',
    previewSender: '',
    previewKind: '',
    spaces: [],
    memberCount: 0,
    fullyRead: '',
    ...patch,
  }
}

describe('room sections', () => {
  const rooms = [
    room('inv', { membership: 'invite' }),
    room('fav', { favourite: true, kind: 'direct' }),
    room('dm', { kind: 'direct', directUser: '@bob:x' }),
    room('garden', { spaces: ['!sp'] }),
    room('low', { lowPriority: true }),
    room('!sp', { kind: 'space' }),
  ]
  it('files each room once, in order, without spaces', () => {
    expect(roomSections(rooms, '', '').map((s) => [s.key, s.rooms.map((r) => r.id)])).toEqual([
      ['invites', ['inv']],
      ['favourites', ['fav']],
      ['people', ['dm']],
      ['rooms', ['garden']],
      ['low', ['low']],
    ])
  })
  it('filters by space and by query', () => {
    expect(roomSections(rooms, '', '!sp').flatMap((s) => s.rooms.map((r) => r.id))).toEqual(['inv', 'garden'])
    expect(roomSections(rooms, 'bob', '').flatMap((s) => s.rooms.map((r) => r.id))).toEqual(['dm'])
  })
  it('words the preview line', () => {
    const t = (k: string): string => k
    expect(previewLine(room('a', { preview: 'hi', previewSender: 'Bob', previewKind: 'text' }), t)).toBe('Bob: hi')
    expect(previewLine(room('a', { kind: 'direct', preview: 'hi', previewSender: 'Bob', previewKind: 'text' }), t)).toBe('hi')
    expect(previewLine(room('a', { preview: 'cat.png', previewKind: 'image' }), t)).toBe('chat.preview.you: chat.preview.image: cat.png')
    expect(previewLine(room('a', { previewKind: 'encrypted', previewSender: 'Bob' }), t)).toBe('Bob: chat.preview.encrypted')
  })
})

describe('stateText', () => {
  const t = (k: string): string => ({ 'chat.state.invited': '{actor} invited {target}', 'chat.state.name': '{actor} renamed the room to {value}' })[k] ?? k
  it('fills actor, target and value', () => {
    expect(stateText(msg('$s', '@ann:x', day, { senderName: 'Ann', kind: 'state', state: { kind: 'invited', target: '@bob:x', targetName: 'Bob', value: '' } }), t)).toBe('Ann invited Bob')
    expect(stateText(msg('$s', '@ann:x', day, { senderName: 'Ann', kind: 'state', state: { kind: 'name', target: '', targetName: '', value: 'Garden' } }), t)).toBe('Ann renamed the room to Garden')
    expect(stateText(msg('$x', '@ann:x', day), t)).toBe('')
  })
})
