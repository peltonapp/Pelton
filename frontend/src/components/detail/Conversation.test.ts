import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { MessageDetail, MessageSummary, ThreadMessage } from '../../lib/types'

const api = vi.hoisted(() => ({
  getThread: vi.fn(),
  getMessage: vi.fn(),
  setSeen: vi.fn(),
  setSetting: vi.fn(),
}))

const actions = vi.hoisted(() => ({ sendSession: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  getThread: api.getThread,
  getMessage: api.getMessage,
  setSeen: api.setSeen,
  setSetting: api.setSetting,
}))

vi.mock('../../lib/composeactions', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/composeactions')>()),
  sendSession: actions.sendSession,
}))

import Conversation from './Conversation.svelte'
import { prefs } from '../../stores/prefs'
import { invalidateThreads } from '../../stores/threads'
import { composeSessions, type ComposeSession } from '../../stores/compose'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

function summary(id: number, patch: Partial<MessageSummary> = {}): MessageSummary {
  return {
    id,
    accountId: 1,
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject: 'Trip plans',
    fromName: 'Ada',
    fromAddress: 'ada@example.com',
    snippet: `snippet ${id}`,
    date: `2026-09-${String(10 + id).padStart(2, '0')}T12:00:00Z`,
    seen: true,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    threadId: 7,
    answered: false,
    forwarded: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    ...patch,
  }
}

function full(m: MessageSummary, body: string, patch: Partial<MessageDetail> = {}): MessageDetail {
  return {
    ...m,
    messageId: `<${m.id}@example.com>`,
    inReplyTo: [],
    references: [],
    toAddresses: 'me@example.com',
    ccAddresses: '',
    bodyPlain: body,
    bodyQuote: body,
    bodyHtmlSafe: '',
    isHtml: false,
    hasRemoteContent: false,
    remoteAllowed: false,
    remoteHosts: [],
    trackingPixels: [],
    attachments: [],
    phishing: { level: 'none' } as MessageDetail['phishing'],
    charsetGuess: '',
    unsubscribe: null,
    pgpState: '',
    replyIds: [],
    forwardIds: [],
    ...patch,
  }
}

function member(m: MessageSummary, fromMe = false): ThreadMessage {
  return { ...m, folderRole: fromMe ? 'sent' : 'inbox', fromMe }
}

const first = summary(1, { snippet: 'Shall we go in May?' })
const mine = summary(2, { fromName: 'Me', fromAddress: 'me@example.com', snippet: 'May works' })
const bob = summary(3, { fromName: 'Bob', fromAddress: 'bob@example.com', snippet: 'Count me in', seen: false })
const latest = summary(4, { snippet: 'Booking it' })
const opened = full(latest, 'Booking it now.\n\nOn Monday, Bob wrote:\n> Count me in', {
  ccAddresses: 'bob@example.com',
  attachments: [{ id: 9, filename: 'tickets.pdf', contentType: 'application/pdf', sizeBytes: 2048, inline: false }],
})

beforeEach(() => {
  // jsdom has no Web Animations, so the transitions are stilled the way the
  // reduce motion setting stills them.
  document.documentElement.setAttribute('data-reduce-motion', '')
  invalidateThreads()
  composeSessions.set([])
  api.getThread.mockReset().mockResolvedValue({
    threadId: 7,
    messages: [member(first), member(mine, true), member(bob), member(latest)],
  })
  api.getMessage.mockReset().mockImplementation(async (id: number) => {
    const m = [first, mine, bob, latest].find((x) => x.id === id)!
    return full(m, id === 3 ? '' : `Body of ${id}`)
  })
  api.setSeen.mockReset().mockResolvedValue(undefined)
  api.setSetting.mockReset().mockResolvedValue(undefined)
  actions.sendSession.mockReset().mockResolvedValue(true)
  prefs.update((p) => ({
    ...p,
    design: 'new',
    threadedList: true,
    markReadOnOpen: true,
    messageFontSize: 14,
    readerWidth: 'full',
    defaultEditorMode: 'plaintext',
  }))
})

function card(name: string): HTMLElement {
  return screen.getByRole('article', { name })
}

describe('the conversation reader', () => {
  it('stacks the conversation with the newest and unread messages open', async () => {
    render(Conversation, { props: { detail: opened } })
    expect(await screen.findByRole('article', { name: 'Bob' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Trip plans' })).toBeInTheDocument()
    expect(screen.getByText('4 messages')).toBeInTheDocument()

    // the older read messages are one line each.
    expect(within(card('Me')).getByText('May works')).toBeInTheDocument()
    expect(within(card('Me')).getByRole('button', { expanded: false })).toBeInTheDocument()
    const [oldest, open] = screen.getAllByRole('article', { name: 'Ada' })
    expect(within(oldest).getByText('Shall we go in May?')).toBeInTheDocument()
    expect(within(oldest).queryByText(/Body of 1/)).toBeNull()

    // the opened message is open, with its quote folded.
    expect(within(open).getByText(/Booking it now\./)).toBeInTheDocument()
    expect(within(open).queryByText(/Count me in/)).toBeNull()

    // the unread reply is open, read on opening, and says it has no text.
    await waitFor(() => expect(api.setSeen).toHaveBeenCalledWith(3, true))
    expect(await within(card('Bob')).findByText('No text')).toBeInTheDocument()
  })

  it('unfolds the quoted text on request', async () => {
    const user = userEvent.setup()
    render(Conversation, { props: { detail: opened } })
    await user.click(await screen.findByRole('button', { name: 'Show quoted text' }))
    expect(screen.getByText(/> Count me in/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Hide quoted text' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('opens a closed message on a click and loads it', async () => {
    const user = userEvent.setup()
    render(Conversation, { props: { detail: opened } })
    await screen.findByRole('article', { name: 'Bob' })
    await user.click(within(card('Me')).getByRole('button'))
    expect(api.getMessage).toHaveBeenCalledWith(2)
    expect(await within(card('Me')).findByText('Body of 2')).toBeInTheDocument()
  })

  it('shows the attachments as chips', async () => {
    render(Conversation, { props: { detail: opened } })
    const chips = await screen.findByRole('list', { name: 'Attachments' })
    expect(within(chips).getByText('tickets.pdf')).toBeInTheDocument()
    expect(within(chips).getByRole('button', { name: 'Preview tickets.pdf' })).toBeInTheDocument()
    expect(within(chips).getByRole('button', { name: 'Download tickets.pdf' })).toBeInTheDocument()
  })

  it('sends the floating bar actions for the opened message', async () => {
    const user = userEvent.setup()
    const action = vi.fn()
    render(Conversation, { props: { detail: opened }, events: { action } })
    const bar = await screen.findByRole('toolbar', { name: 'Message actions' })
    await user.click(within(bar).getByRole('button', { name: 'Forward' }))
    expect(action).toHaveBeenCalledWith(expect.objectContaining({ detail: { kind: 'forward', message: opened } }))
    await user.click(within(bar).getByRole('button', { name: 'Archive' }))
    expect(action).toHaveBeenLastCalledWith(expect.objectContaining({ detail: { kind: 'archive', message: opened } }))
  })

  it('sends a quick reply without opening the composer', async () => {
    const user = userEvent.setup()
    render(Conversation, { props: { detail: opened } })
    const box = await screen.findByRole('textbox', { name: 'Quick reply' })
    await user.type(box, 'See you there')
    await user.click(screen.getByRole('button', { name: 'Send' }))
    expect(actions.sendSession).toHaveBeenCalledTimes(1)
    const session = actions.sendSession.mock.calls[0][1] as ComposeSession
    expect(session.to).toBe('Ada <ada@example.com>')
    expect(session.subject).toBe('Re: Trip plans')
    expect(session.body.startsWith('See you there\n\n')).toBe(true)
    expect(get(composeSessions)).toEqual([])
    await waitFor(() => expect(box).toHaveValue(''))
  })

  it('carries a quick reply into the composer, to everyone when asked', async () => {
    const user = userEvent.setup()
    render(Conversation, { props: { detail: opened } })
    const box = await screen.findByRole('textbox', { name: 'Quick reply' })
    await user.type(box, 'Longer answer')
    await user.click(screen.getByRole('button', { name: 'Reply all', pressed: false }))
    await user.click(screen.getByRole('button', { name: 'Continue in the composer' }))
    const sessions = get(composeSessions)
    expect(sessions).toHaveLength(1)
    expect(sessions[0].cc).toContain('bob@example.com')
    expect(sessions[0].body.startsWith('Longer answer')).toBe(true)
    expect(actions.sendSession).not.toHaveBeenCalled()
  })

  it('changes the text size and the reading width', async () => {
    const user = userEvent.setup()
    render(Conversation, { props: { detail: opened } })
    await user.click(await screen.findByRole('button', { name: 'Larger text' }))
    expect(get(prefs).messageFontSize).toBe(16)
    await user.click(screen.getByRole('radio', { name: 'Narrow column' }))
    expect(get(prefs).readerWidth).toBe('narrow')
    expect(api.setSetting).toHaveBeenCalledWith('reader_width', 'narrow')
    expect(screen.getByRole('radio', { name: 'Narrow column' })).toHaveAttribute('aria-checked', 'true')
  })

  it('folds a long run of older messages into one line', async () => {
    const many = [1, 2, 3, 4, 5, 6].map((id) => summary(id, { fromName: `P${id}`, fromAddress: `p${id}@example.com` }))
    api.getThread.mockResolvedValue({ threadId: 7, messages: many.map((m) => member(m)) })
    const user = userEvent.setup()
    render(Conversation, { props: { detail: full(many[5], 'Last') } })
    const more = await screen.findByRole('button', { name: '3 older messages' })
    expect(screen.queryByRole('article', { name: 'P2' })).toBeNull()
    await user.click(more)
    expect(screen.getByRole('article', { name: 'P3' })).toBeInTheDocument()
  })

  it('shows the message alone when the list is not grouped', async () => {
    prefs.update((p) => ({ ...p, threadedList: false }))
    render(Conversation, { props: { detail: opened } })
    expect(await screen.findByText(/Booking it now\./)).toBeInTheDocument()
    expect(api.getThread).not.toHaveBeenCalled()
    expect(screen.getAllByRole('article')).toHaveLength(1)
  })
})
