import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { ChatRoom, MessageDetail } from '../../lib/types'

const api = vi.hoisted(() => ({
  chatSend: vi.fn(),
  chatRooms: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import ShareToChatDialog from './ShareToChatDialog.svelte'
import { shareToChat } from '../../stores/chatshare'
import { chatRooms } from '../../stores/chat'
import { buildShareText } from '../../lib/chatshare'

// the dialog scales in, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

function room(id: string, name: string, patch: Partial<ChatRoom> = {}): ChatRoom {
  return { id, name, kind: 'room', membership: 'join', ...patch } as ChatRoom
}

const message = {
  id: 7,
  subject: 'Quarterly plan',
  fromName: 'Ann Example',
  fromAddress: 'Ann Example <ann@example.com>',
  date: '2026-10-01T09:00:00Z',
  bodyPlain: 'Hello team',
  bodyQuote: '',
} as MessageDetail

const rooms = [room('!a:x', 'Design'), room('!b:x', 'Ops'), room('!c:x', 'Invited', { membership: 'invite' })]

beforeEach(() => {
  api.chatSend.mockReset().mockResolvedValue('$event')
  api.chatRooms.mockReset().mockResolvedValue(rooms)
  chatRooms.set(rooms)
  shareToChat.set(message)
})

describe('ShareToChatDialog', () => {
  it('previews the exact text and sends nothing on open', async () => {
    render(ShareToChatDialog)
    const preview = await screen.findByLabelText('This is what will be posted')
    expect(preview.textContent).toBe(buildShareText(message))
    expect(api.chatSend).not.toHaveBeenCalled()
  })

  it('lists joined rooms and filters them', async () => {
    const user = userEvent.setup()
    render(ShareToChatDialog)
    expect(await screen.findByRole('radio', { name: 'Design' })).toBeInTheDocument()
    expect(screen.queryByRole('radio', { name: 'Invited' })).toBeNull()
    await user.type(screen.getByRole('searchbox', { name: 'Filter rooms' }), 'ops')
    expect(screen.queryByRole('radio', { name: 'Design' })).toBeNull()
    expect(screen.getByRole('radio', { name: 'Ops' })).toBeInTheDocument()
  })

  it('keeps Post disabled until a room is picked', async () => {
    render(ShareToChatDialog)
    expect(await screen.findByRole('button', { name: 'Post' })).toBeDisabled()
  })

  it('includes the note in the preview and posts it as markdown to the chosen room', async () => {
    const user = userEvent.setup()
    render(ShareToChatDialog)
    await user.click(await screen.findByRole('radio', { name: 'Ops' }))
    await user.type(screen.getByLabelText('Note (optional)'), 'fyi')
    const preview = screen.getByLabelText('This is what will be posted')
    const expected = `fyi\n\n${buildShareText(message)}`
    expect(preview.textContent).toBe(expected)
    expect(api.chatSend).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Post' }))
    await waitFor(() => expect(api.chatSend).toHaveBeenCalledTimes(1))
    expect(api.chatSend).toHaveBeenCalledWith({
      roomId: '!b:x',
      text: expected,
      markdown: true,
      emote: false,
      replyTo: '',
      edit: '',
      mentions: [],
    })
    await waitFor(() => {
      let open: MessageDetail | null = message
      shareToChat.subscribe((v) => (open = v))()
      expect(open).toBeNull()
    })
  })
})
