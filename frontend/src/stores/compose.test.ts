import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

vi.mock('../lib/api', () => ({ getSetting: vi.fn(async () => ({ value: '', found: false })), setSetting: vi.fn() }))

import { closeCompose, composeSessions, getSession, openCompose, openDraftSession, openForward, openReply, reopenSession, restoreSession } from './compose'
import { buildRequest } from '../lib/mailcompose'
import { defaultSelection, openMessageId, searchQuery, selection } from './selection'
import type { ComposeRequest, Draft, EditorMode, MessageDetail, Selection } from '../lib/types'

function detail(overrides: Partial<MessageDetail>): MessageDetail {
  return {
    id: 42,
    accountId: 1,
    folderId: 20,
    folderName: 'Inbox',
    messageId: '',
    inReplyTo: [],
    references: [],
    accountEmail: 'me@example.com',
    fromName: 'Ann',
    fromAddress: 'ann@example.com',
    toAddresses: 'me@example.com, bob@example.com',
    ccAddresses: 'carol@example.com',
    subject: 'Plans',
    date: '2026-10-01',
    snippet: 'hello',
    seen: true,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    bodyPlain: 'hello',
    bodyQuote: 'hello',
    bodyHtmlSafe: '',
    isHtml: false,
    hasRemoteContent: false,
    remoteAllowed: false,
    remoteHosts: [],
    trackingPixels: [],
    attachments: [],
    phishing: { level: 'none' },
    charsetGuess: '',
    unsubscribe: null,
    pgpState: '',
    ...overrides,
  }
}

function last() {
  const list = get(composeSessions)
  return list[list.length - 1]
}

describe('reply quote', () => {
  beforeEach(() => composeSessions.set([]))

  it('quotes as html in the rich editor, so its lines do not run together', () => {
    openReply(detail({ bodyQuote: 'line one\nline two\n> older' }), 'wysiwyg', false)
    const body = last().body
    expect(body).not.toContain('&gt; ')
    expect(body).toContain('<blockquote><p>line one<br>line two</p><blockquote><p>older</p></blockquote></blockquote>')
  })

  it('keeps the plain "> " quote outside the rich editor', () => {
    openReply(detail({ bodyQuote: 'line one\nline two' }), 'plaintext', false)
    expect(last().body).toContain('> line one\n> line two')
  })

  it('writes a readable date in the attribution, not the raw timestamp', () => {
    openReply(detail({ date: '2026-10-05T16:03:44Z' }), 'plaintext', false)
    expect(last().body).not.toContain('2026-10-05T16:03:44Z')
    expect(last().body).toMatch(/On .*2026.*, Ann wrote:/)
  })
})


const modes: EditorMode[] = ['plaintext', 'markdown', 'wysiwyg']
const replyCases = modes.flatMap((mode) => [false, true].map((replyAll) => ({ mode, replyAll })))
const contexts: { name: string; selection: Selection; query: string }[] = [
  { name: 'another account folder', selection: { kind: 'folder', folderId: 10, accountId: 1, label: 'Inbox' }, query: '' },
  { name: 'search', selection: { kind: 'folder', folderId: 10, accountId: 1, label: 'Inbox' }, query: 'Plans' },
  { name: 'unified inbox', selection: { kind: 'view', view: 'inbox', label: 'Unified Inbox' }, query: '' },
]

function draft(): Draft {
  return {
    id: 7,
    accountId: 2,
    savedAt: '2026-10-27T12:00:00Z',
    locked: false,
    protection: 'none',
    request: {
      accountId: 2,
      to: [{ name: 'Ann', email: 'ann@example.com' }],
      cc: [],
      bcc: [],
      subject: 'Re: Plans',
      text: 'A reply',
      html: '',
      inReplyTo: 'parent@example.com',
      references: ['parent@example.com'],
      attachments: [],
      sendAt: '',
      protection: 'none',
      draftMessageId: '<draft@example.com>',
    },
  }
}

describe('compose threading snapshots', () => {
  beforeEach(() => {
    composeSessions.set([])
    selection.set(defaultSelection)
    searchQuery.set('')
    openMessageId.set(null)
  })

  describe.each(contexts)('from $name', (context) => {
    it.each(replyCases)('reply and reply-all carry the parent through buildRequest ($mode, replyAll=$replyAll)', ({ mode, replyAll }) => {
      selection.set(context.selection)
      searchQuery.set(context.query)
      const parent = detail({ accountId: 2, messageId: 'parent@example.com' })
      openMessageId.set(parent.id)
      const id = openReply(parent, mode, replyAll)
      const req = buildRequest(getSession(id)!)
      expect(req.inReplyTo).toBe('parent@example.com')
      expect(req.references).toEqual(['parent@example.com'])
      expect(req.accountId).toBe(2)

      selection.set({ kind: 'folder', folderId: 30, accountId: 3, label: 'Other Inbox' })
      searchQuery.set('different message')
      openMessageId.set(99)
      parent.messageId = 'changed@example.com'
      parent.accountId = 3
      parent.inReplyTo.push('older@example.com')
      parent.references.push('older@example.com')
      const saved = buildRequest(getSession(id)!)
      expect(saved.inReplyTo).toBe('parent@example.com')
      expect(saved.references).toEqual(['parent@example.com'])
      expect(saved.accountId).toBe(2)
    })
  })

  it.each(modes)('new and forwarded messages have no threading (%s)', (mode) => {
    openCompose(2, mode)
    expect(buildRequest(last()).inReplyTo).toBe('')
    expect(buildRequest(last()).references).toEqual([])
    openForward(detail({ messageId: 'parent@example.com', references: ['older@example.com'] }), mode)
    expect(buildRequest(last()).inReplyTo).toBe('')
    expect(buildRequest(last()).references).toEqual([])
  })

  it.each([false, true])('missing legacy messageId has no threading (replyAll=%s)', (replyAll) => {
    const { messageId, ...legacy } = detail({ inReplyTo: ['older@example.com'], references: ['older@example.com'] })
    openReply(legacy as MessageDetail, 'plaintext', replyAll)
    expect(buildRequest(last()).inReplyTo).toBe('')
    expect(buildRequest(last()).references).toEqual([])
  })

  it('empty messageId has no threading even with ancestry', () => {
    openReply(detail({ inReplyTo: ['older@example.com'], references: ['older@example.com'] }), 'plaintext', false)
    expect(buildRequest(last()).inReplyTo).toBe('')
    expect(buildRequest(last()).references).toEqual([])
  })

  it('uses only the direct parent in Part 1', () => {
    openReply(detail({ messageId: 'parent@example.com', inReplyTo: ['older@example.com'], references: ['root@example.com'] }), 'plaintext', false)
    expect(buildRequest(last()).inReplyTo).toBe('parent@example.com')
    expect(buildRequest(last()).references).toEqual(['parent@example.com'])
  })

  describe('draft reopen and undo preserve an independent snapshot', () => {
    it('copies the source draft references and keeps the parent distinct from the draft ID', () => {
      const source = draft()
      openDraftSession(source)
      const session = last()
      const req = buildRequest(session)
      expect(req.inReplyTo).toBe('parent@example.com')
      expect(req.draftMessageId).toBe('<draft@example.com>')
      expect(req.inReplyTo).not.toBe(req.draftMessageId)
      source.request.inReplyTo = 'changed@example.com'
      source.request.references.push('changed@example.com')
      expect(session.inReplyTo).toBe('parent@example.com')
      expect(session.references).toEqual(['parent@example.com'])
      expect(req.references).toEqual(['parent@example.com'])
      expect(buildRequest(session).references).toEqual(['parent@example.com'])
    })

    it.each([
      { name: 'undo send', open: reopenSession, draftId: 0, draftMessageId: '' },
      { name: 'failed upload restore', open: restoreSession, draftId: 7, draftMessageId: '<draft@example.com>' },
    ])('$name copies the closed session references', ({ open, draftId, draftMessageId }) => {
      const id = openDraftSession(draft())
      const closed = getSession(id)!
      closeCompose(id)
      const restoredId = open(closed)
      closed.inReplyTo = 'changed@example.com'
      closed.references.push('changed@example.com')
      const session = getSession(restoredId)!
      const req = buildRequest(session)
      expect(req.inReplyTo).toBe('parent@example.com')
      expect(req.references).toEqual(['parent@example.com'])
      expect(req.accountId).toBe(2)
      expect(session.draftId).toBe(draftId)
      expect(req.draftMessageId).toBe(draftMessageId)
    })

    it('defaults missing legacy draft threading fields to empty', () => {
      const source = draft()
      const { inReplyTo, references, ...legacy } = source.request
      source.request = legacy as ComposeRequest
      openDraftSession(source)
      expect(last().inReplyTo).toBe('')
      expect(last().references).toEqual([])
      expect(buildRequest(last()).inReplyTo).toBe('')
      expect(buildRequest(last()).references).toEqual([])
    })
  })
})
