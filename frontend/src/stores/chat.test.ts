import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { ChatMessage, ChatTimelinePage } from '../lib/types'

const api = vi.hoisted(() => ({
  chatTimeline: vi.fn(),
  chatSend: vi.fn(),
  chatReact: vi.fn(),
  chatRedact: vi.fn(),
  chatMarkRead: vi.fn(),
  chatSetViewing: vi.fn(),
  chatTypists: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import {
  timelines,
  pending,
  chatRooms,
  activeRoomId,
  openRoom,
  loadOlder,
  reloadTimeline,
  sendMessage,
  retryPending,
  toggleReaction,
  markRoomRead,
  chatError,
  chatUnreadRooms,
  stopChat,
} from './chat'

function msg(id: string, patch: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id,
    sender: '@bob:x',
    senderName: 'Bob',
    senderAvatar: '',
    ts: 1,
    kind: 'text',
    body: id,
    html: '',
    edited: false,
    own: false,
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

function page(ids: string[], hasMore = false): ChatTimelinePage {
  return { messages: ids.map((id) => msg(id)), hasMore, fullyRead: '' }
}

beforeEach(() => {
  stopChat()
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.chatSetViewing.mockResolvedValue(undefined)
  api.chatTypists.mockResolvedValue([])
  api.chatMarkRead.mockResolvedValue(undefined)
})

describe('timelines', () => {
  it('opens a room once and pages older messages in front', async () => {
    api.chatTimeline.mockResolvedValueOnce(page(['$3', '$4'], true)).mockResolvedValueOnce(page(['$1', '$2'], false))
    await openRoom('!r')
    expect(get(activeRoomId)).toBe('!r')
    expect(api.chatSetViewing).toHaveBeenCalledWith('!r')
    await openRoom('!r')
    expect(api.chatTimeline).toHaveBeenCalledTimes(1)

    await loadOlder('!r')
    expect(api.chatTimeline).toHaveBeenLastCalledWith('!r', '$3', 40)
    const tl = get(timelines)['!r']
    expect(tl.messages.map((m) => m.id)).toEqual(['$1', '$2', '$3', '$4'])
    expect(tl.hasMore).toBe(false)
  })

  it('reloads as many messages as are on screen', async () => {
    timelines.set({ '!r': { messages: Array.from({ length: 70 }, (_, i) => msg(`$${i}`)), hasMore: true, fullyRead: '', loading: false, loadingOlder: false, error: '' } })
    api.chatTimeline.mockResolvedValue(page(['$a']))
    await reloadTimeline('!r')
    expect(api.chatTimeline).toHaveBeenCalledWith('!r', '', 70)
  })
})

describe('sending', () => {
  const req = { roomId: '!r', text: 'hi', markdown: true, emote: false, replyTo: '', edit: '', mentions: [] }

  it('shows a message as pending until the server echoes it back', async () => {
    api.chatSend.mockResolvedValue('$new')
    await sendMessage(req)
    expect(get(pending)['!r']).toHaveLength(1)
    expect(get(pending)['!r'][0].eventId).toBe('$new')
    api.chatTimeline.mockResolvedValue(page(['$old', '$new']))
    timelines.set({ '!r': { messages: [], hasMore: false, fullyRead: '', loading: false, loadingOlder: false, error: '' } })
    await reloadTimeline('!r')
    expect(get(pending)['!r']).toHaveLength(0)
  })

  it('keeps a failed message to retry', async () => {
    api.chatSend.mockRejectedValueOnce(new Error('chat: send: network down')).mockResolvedValueOnce('$ok')
    await sendMessage(req)
    const failed = get(pending)['!r'][0]
    expect(failed.state).toBe('failed')
    expect(failed.error).toBe('Send: network down')
    await retryPending('!r', failed.localId)
    expect(get(pending)['!r'][0].state).toBe('sending')
    expect(get(pending)['!r'][0].eventId).toBe('$ok')
  })

  it('sends an edit straight away without a pending row', async () => {
    api.chatSend.mockResolvedValue('$e')
    await sendMessage({ ...req, edit: '$1' })
    expect(get(pending)['!r'] ?? []).toHaveLength(0)
  })

  it('reacts, or takes back the user own reaction', async () => {
    const m = msg('$1', { reactions: [{ key: '👍', count: 1, mine: '$mine', senders: ['Me'] }] })
    await toggleReaction('!r', m, '👍')
    expect(api.chatRedact).toHaveBeenCalledWith('!r', '$mine')
    await toggleReaction('!r', m, '🎉')
    expect(api.chatReact).toHaveBeenCalledWith('!r', '$1', '🎉')
  })
})

describe('read markers', () => {
  it('marks the newest message read once and clears the room count', async () => {
    chatRooms.set([{ id: '!r', unread: 3, highlight: 1, membership: 'join' } as never])
    timelines.set({ '!r': { messages: [msg('$1'), msg('$2')], hasMore: false, fullyRead: '$1', loading: false, loadingOlder: false, error: '' } })
    expect(get(chatUnreadRooms)).toBe(1)
    await markRoomRead('!r')
    expect(api.chatMarkRead).toHaveBeenCalledWith('!r', '$2')
    expect(get(chatUnreadRooms)).toBe(0)
    await markRoomRead('!r')
    expect(api.chatMarkRead).toHaveBeenCalledTimes(1)
  })
})

describe('chatError', () => {
  it('drops the package prefix and capitalises', () => {
    expect(chatError(new Error('chat: sign-in failed: wrong password'))).toBe('Sign-in failed: wrong password')
    expect(chatError('pelton: chat is turned off')).toBe('Chat is turned off')
    expect(chatError(42)).toBe('Something went wrong')
  })
})
