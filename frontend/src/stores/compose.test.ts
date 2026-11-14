import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

vi.mock('../lib/api', () => ({ getSetting: vi.fn(async () => ({ value: '', found: false })), setSetting: vi.fn() }))

import { closeCompose, composeSessions, getSession, openCompose, openDraftSession, openForward, openReply, openReplyWith, replySession, reopenSession, restoreSession, switchReplyKind, typedPart, updateCompose } from './compose'
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
    threadId: 7,
    answered: false,
    forwarded: false,
    replyIds: [],
    forwardIds: [],
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

describe('reply recipients', () => {
  beforeEach(() => composeSessions.set([]))

  it('addresses a plain sender by name and address', () => {
    openReply(detail({}), 'plaintext', false)
    expect(last().to).toBe('Ann <ann@example.com>')
  })

  it('quotes a sender name with a comma', () => {
    openReply(detail({ fromName: 'Doe, Jane', fromAddress: 'jane@example.com' }), 'plaintext', false)
    expect(last().to).toBe('"Doe, Jane" <jane@example.com>')
  })

  it('escapes a quote in the sender name', () => {
    openReply(detail({ fromName: 'Jane "JD" Doe', fromAddress: 'jane@example.com' }), 'plaintext', false)
    expect(last().to).toBe('"Jane \\"JD\\" Doe" <jane@example.com>')
  })

  it('quotes a comma name in the reply-all cc', () => {
    openReply(detail({ toAddresses: 'me@example.com', ccAddresses: 'Doe, Jane <jane@example.com>, bob@example.com' }), 'plaintext', true)
    expect(last().cc).toBe('"Doe, Jane" <jane@example.com>, bob@example.com')
  })

  it('keeps the original To recipients on reply all', () => {
    openReply(detail({ toAddresses: 'me@example.com, Bob <bob@example.com>, dave@example.com' }), 'plaintext', true)
    expect(last().to).toBe('Ann <ann@example.com>, Bob <bob@example.com>, dave@example.com')
    expect(last().cc).toBe('carol@example.com')
  })

  it('leaves the own address and duplicates out of reply all', () => {
    openReply(detail({ toAddresses: 'ME@example.com, ann@example.com', ccAddresses: 'me@example.com, Ann <ann@example.com>, carol@example.com' }), 'plaintext', true)
    expect(last().to).toBe('Ann <ann@example.com>')
    expect(last().cc).toBe('carol@example.com')
  })

  it('replies all to the recipients of a message the account sent', () => {
    openReply(detail({ fromAddress: 'me@example.com', fromName: 'Me', toAddresses: 'bob@example.com', ccAddresses: '' }), 'plaintext', true)
    expect(last().to).toBe('bob@example.com')
  })

  it('addresses only the sender on a plain reply', () => {
    openReply(detail({}), 'plaintext', false)
    expect(last().cc).toBe('')
  })
})

describe('switching the reply type', () => {
  beforeEach(() => composeSessions.set([]))

  it('keeps the typed text and swaps recipients and quote', () => {
    const id = openReply(detail({ messageId: '<m1@x>' }), 'wysiwyg', false)
    const session = getSession(id)!
    updateCompose(id, { body: '<p>Thanks</p>' + session.fold })
    switchReplyKind(id, 'replyAll')
    expect(getSession(id)!.to).toBe('Ann <ann@example.com>, bob@example.com')
    expect(getSession(id)!.body.startsWith('<p>Thanks</p>')).toBe(true)

    switchReplyKind(id, 'forward')
    const forward = getSession(id)!
    expect(forward.to).toBe('')
    expect(forward.subject).toBe('Fwd: Plans')
    expect(forward.inReplyTo).toBe('')
    expect(forward.body.startsWith('<p>Thanks</p>')).toBe(true)
    expect(forward.body).toContain('Forwarded message')
    expect(typedPart(forward)).toBe('<p>Thanks</p>')

    switchReplyKind(id, 'reply')
    expect(getSession(id)!.subject).toBe('Re: Plans')
    expect(getSession(id)!.inReplyTo).toBe('<m1@x>')
  })

  it('keeps a subject the user changed', () => {
    const id = openReply(detail({}), 'plaintext', false)
    updateCompose(id, { subject: 'New topic' })
    switchReplyKind(id, 'forward')
    expect(getSession(id)!.subject).toBe('New topic')
  })
})

describe('replies started outside the composer', () => {
  beforeEach(() => composeSessions.set([]))

  it('records what a forward forwards and nothing for a reply', () => {
    openForward(detail({ id: 42 }), 'plaintext')
    expect(buildRequest(last()).forwardOf).toBe(42)
    openReply(detail({ id: 43 }), 'plaintext', false)
    expect(buildRequest(last()).forwardOf).toBe(0)
    openCompose(1, 'plaintext')
    expect(buildRequest(last()).forwardOf).toBe(0)
  })

  it('builds a quick reply without opening a composer', () => {
    const session = replySession(detail({}), 'plaintext', 'reply', 'Sounds good')
    expect(get(composeSessions)).toEqual([])
    expect(session.to).toBe('Ann <ann@example.com>')
    expect(session.subject).toBe('Re: Plans')
    expect(session.body.startsWith('Sounds good\n\nOn ')).toBe(true)
    expect(session.body).toContain('> hello')
    expect(typedPart(session)).toBe('Sounds good\n\n')
  })

  it('carries typed text into a rich composer as paragraphs', () => {
    const id = openReplyWith(detail({}), 'wysiwyg', 'replyAll', 'Yes\nsee you')
    const session = getSession(id)!
    expect(session.body.startsWith('<p>Yes<br>see you</p><p></p><p>On ')).toBe(true)
    expect(session.cc).toContain('carol@example.com')
  })
})

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

  describe.each(replyCases)('ancestry selection ($mode, replyAll=$replyAll)', ({ mode, replyAll }) => {
    it.each([
      { name: 'references ancestry wins over inReplyTo', inReplyTo: ['other@example.com'], references: ['a@example.com', 'b@example.com'], want: ['a@example.com', 'b@example.com', 'parent@example.com'] },
      { name: 'one inReplyTo is a fallback', inReplyTo: ['a@example.com'], references: [], want: ['a@example.com', 'parent@example.com'] },
      { name: 'multiple inReplyTo is not a fallback', inReplyTo: ['a@example.com', 'b@example.com'], references: [], want: ['parent@example.com'] },
      { name: 'duplicate inReplyTo is not a fallback', inReplyTo: ['a@example.com', 'a@example.com'], references: [], want: ['parent@example.com'] },
      // Go invalidates malformed In-Reply-To and maps legacy NULL ancestry to these same empty DTO lists.
      { name: 'empty DTO ancestry uses only the parent', inReplyTo: [], references: [], want: ['parent@example.com'] },
      { name: 'references keep duplicates and parent position for Go to normalize', inReplyTo: [], references: ['parent@example.com', 'a@example.com', 'a@example.com'], want: ['parent@example.com', 'a@example.com', 'a@example.com', 'parent@example.com'] },
    ])('$name', ({ inReplyTo, references, want }) => {
      openReply(detail({ messageId: 'parent@example.com', inReplyTo, references }), mode, replyAll)
      expect(last().inReplyTo).toBe('parent@example.com')
      expect(last().references).toEqual(want)
      expect(buildRequest(last()).inReplyTo).toBe('parent@example.com')
      expect(buildRequest(last()).references).toEqual(want)
    })

    it.each([
      { name: 'References', inReplyTo: ['other@example.com'], references: ['a@example.com', 'b@example.com'], want: ['a@example.com', 'b@example.com', 'parent@example.com'] },
      { name: 'In-Reply-To fallback', inReplyTo: ['a@example.com'], references: [], want: ['a@example.com', 'parent@example.com'] },
    ])('reply ancestry is a snapshot ($name)', ({ inReplyTo, references, want }) => {
      const parent = detail({ messageId: 'parent@example.com', inReplyTo, references })
      const id = openReply(parent, mode, replyAll)
      const session = getSession(id)!
      const request = buildRequest(session)
      parent.messageId = 'changed@example.com'
      parent.inReplyTo.splice(0, parent.inReplyTo.length, 'changed@example.com')
      parent.references.splice(0, parent.references.length, 'changed@example.com')
      expect(session.inReplyTo).toBe('parent@example.com')
      expect(session.references).toEqual(want)
      expect(request.inReplyTo).toBe('parent@example.com')
      expect(request.references).toEqual(want)
      expect(buildRequest(session).references).toEqual(want)
    })
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
