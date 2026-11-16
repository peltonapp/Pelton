import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { ChatMessage, ChatRoom } from '../../lib/types'

const api = vi.hoisted(() => ({
  chatSend: vi.fn(),
  chatMedia: vi.fn(),
  chatInlineImage: vi.fn(),
  chatAvatar: vi.fn(),
  chatJoin: vi.fn(),
  chatLeave: vi.fn(),
  chatRooms: vi.fn(),
  chatTyping: vi.fn(),
  chatMembers: vi.fn(),
  chatConfirmSAS: vi.fn(),
  chatCancelVerification: vi.fn(),
  chatDismissVerification: vi.fn(),
  chatAcceptVerification: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const opened = vi.hoisted(() => vi.fn())
vi.mock('../../lib/browser', () => ({ openURL: opened }))

import MessageRow from './MessageRow.svelte'
import RoomList from './RoomList.svelte'
import Composer from './Composer.svelte'
import VerificationDialog from './VerificationDialog.svelte'
import { chatRooms, verifications, pending, stopChat } from '../../stores/chat'
import { prefs } from '../../stores/prefs'

Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

function msg(patch: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: '$1',
    sender: '@bob:x',
    senderName: 'Bob',
    senderAvatar: '',
    ts: Date.now(),
    kind: 'text',
    body: 'hello',
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

beforeEach(() => {
  stopChat()
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  opened.mockReset()
  api.chatAvatar.mockResolvedValue('')
  api.chatTyping.mockResolvedValue(undefined)
  api.chatMembers.mockResolvedValue([])
})

describe('MessageRow', () => {
  it('shows sender, body and the edited mark, and opens links in the browser', async () => {
    render(MessageRow, { props: { message: msg({ html: '<p>see <a href="https://example.org">this</a></p>', edited: true }), roomId: '!r' } })
    expect(screen.getByText('Bob')).toBeInTheDocument()
    expect(screen.getByText('(edited)')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('link', { name: 'this' }))
    expect(opened).toHaveBeenCalledWith('https://example.org/')
  })

  it('leaves out name and avatar when it continues a group', () => {
    render(MessageRow, { props: { message: msg(), roomId: '!r', continued: true } })
    expect(screen.queryByText('Bob')).not.toBeInTheDocument()
    expect(screen.getByText('hello')).toBeInTheDocument()
  })

  it('offers a retry for a message whose keys may still come', async () => {
    const retry = vi.fn()
    render(MessageRow, { props: { message: msg({ kind: 'encrypted', body: '', missingKeys: true }), roomId: '!r' }, events: { retry } })
    await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(retry).toHaveBeenCalledOnce()
  })

  it('toggles a reaction from its chip and only lets the user edit their own text', async () => {
    const react = vi.fn()
    render(MessageRow, {
      props: { message: msg({ reactions: [{ key: '👍', count: 2, mine: '$m', senders: ['Me', 'Ann'] }] }), roomId: '!r' },
      events: { react },
    })
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /👍/ }))
    expect(react.mock.calls[0][0].detail.key).toBe('👍')
  })

  it('loads an image thumbnail through the backend', async () => {
    api.chatMedia.mockResolvedValue({ filename: 'cat.png', contentType: 'image/png', sizeBytes: 3, data: 'AAAA', tooLarge: false })
    render(MessageRow, { props: { message: msg({ kind: 'image', body: '', media: { name: 'cat.png', mime: 'image/png', size: 3, width: 10, height: 10, duration: 0, thumbnail: true, encrypted: true } }), roomId: '!r' } })
    await waitFor(() => expect(screen.getByRole('img', { name: 'cat.png' })).toHaveAttribute('src', 'data:image/png;base64,AAAA'))
    expect(api.chatMedia).toHaveBeenCalledWith('!r', '$1', true)
  })
})

describe('RoomList', () => {
  it('sorts rooms into sections, filters by search and opens a room', async () => {
    chatRooms.set([
      room('!inv', { name: 'Party', membership: 'invite', inviterName: 'Carol' }),
      room('!dm', { name: 'Bob', kind: 'direct', unread: 2 }),
      room('!g', { name: 'Garden', preview: 'hi', previewSender: 'Ann', previewKind: 'text' }),
    ])
    const open = vi.fn()
    render(RoomList, { events: { open } })
    expect(screen.getByRole('heading', { name: 'Invites' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'People' })).toBeInTheDocument()
    expect(screen.getByText('Invited by Carol')).toBeInTheDocument()
    expect(screen.getByText('Ann: hi')).toBeInTheDocument()

    await userEvent.type(screen.getByRole('searchbox', { name: 'Search rooms' }), 'gard')
    expect(screen.queryByText('Bob')).not.toBeInTheDocument()
    await userEvent.keyboard('{ArrowDown}{Enter}')
    expect(open.mock.calls[0][0].detail).toBe('!g')
  })

  it('accepts an invite by joining it', async () => {
    chatRooms.set([room('!inv', { name: 'Party', membership: 'invite' })])
    api.chatJoin.mockResolvedValue('!inv')
    api.chatRooms.mockResolvedValue([room('!inv', { name: 'Party' })])
    const open = vi.fn()
    render(RoomList, { events: { open } })
    await userEvent.click(screen.getByRole('button', { name: 'Accept' }))
    await waitFor(() => expect(open).toHaveBeenCalled())
    expect(api.chatJoin).toHaveBeenCalledWith('!inv')
  })
})

describe('Composer', () => {
  it('sends Markdown with emoji codes replaced, and a reply', async () => {
    prefs.update((p) => ({ ...p, chatMarkdown: true, chatSendTyping: false }))
    api.chatSend.mockResolvedValue('$new')
    render(Composer, { props: { room: room('!r', { name: 'Garden' }), replyTo: msg() } })
    expect(screen.getByText(/Replying to Bob/)).toBeInTheDocument()
    const box = screen.getByRole('textbox', { name: 'Message to Garden' })
    await userEvent.type(box, 'great :tada:{Enter}')
    await waitFor(() => expect(api.chatSend).toHaveBeenCalled())
    expect(api.chatSend.mock.calls[0][0]).toMatchObject({ roomId: '!r', text: 'great 🎉', markdown: true, replyTo: '$1', edit: '' })
    expect(box).toHaveValue('')
    expect(api.chatTyping).not.toHaveBeenCalled()
    expect(Object.values(pendingSnapshot()).flat()).toHaveLength(1)
  })

  it('keeps a line break on Shift+Enter and sends /me as an action', async () => {
    api.chatSend.mockResolvedValue('$new')
    render(Composer, { props: { room: room('!r') } })
    const box = screen.getByRole('textbox')
    await userEvent.type(box, '/me waves{Shift>}{Enter}{/Shift}')
    expect(api.chatSend).not.toHaveBeenCalled()
    await userEvent.keyboard('{Enter}')
    await waitFor(() => expect(api.chatSend).toHaveBeenCalled())
    expect(api.chatSend.mock.calls[0][0]).toMatchObject({ text: 'waves', emote: true })
  })
})

function pendingSnapshot(): Record<string, unknown[]> {
  let out: Record<string, unknown[]> = {}
  pending.subscribe((v) => (out = v))()
  return out
}

describe('VerificationDialog', () => {
  it('shows the emoji to compare and confirms a match', async () => {
    api.chatConfirmSAS.mockResolvedValue(undefined)
    verifications.set([
      {
        id: 'txn',
        userId: '@me:x',
        userName: 'me',
        deviceId: 'OTHER',
        self: true,
        incoming: false,
        state: 'sas',
        emojis: [
          { emoji: '🐶', description: 'Dog' },
          { emoji: '🔑', description: 'Key' },
        ],
        decimals: [],
        reason: '',
      },
    ])
    render(VerificationDialog)
    expect(screen.getByText('Dog')).toBeInTheDocument()
    expect(screen.getByText('Key')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'They match' }))
    expect(api.chatConfirmSAS).toHaveBeenCalledWith('txn')
  })

  it('declines an incoming request', async () => {
    api.chatCancelVerification.mockResolvedValue(undefined)
    api.chatDismissVerification.mockResolvedValue(undefined)
    verifications.set([{ id: 't2', userId: '@bob:x', userName: 'Bob', deviceId: 'D', self: false, incoming: true, state: 'requested', emojis: [], decimals: [], reason: '' }])
    render(VerificationDialog)
    expect(screen.getByText('Bob wants to verify you.')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Decline' }))
    expect(api.chatCancelVerification).toHaveBeenCalledWith('t2', false)
    expect(api.chatAcceptVerification).not.toHaveBeenCalled()
  })
})
