import { afterEach, describe, expect, it } from 'vitest'
import { get } from 'svelte/store'
import { composeSessions, openReply } from './compose'
import { sidebar } from './accounts'
import { idle, ready } from '../lib/async'
import type { Folder, MessageDetail } from '../lib/types'

const inbox = { id: 1, accountId: 7, name: 'INBOX', role: 'inbox' } as Folder
const sent = { id: 2, accountId: 7, name: 'Gesendet', role: 'sent' } as Folder

function message(folderId: number): MessageDetail {
  return {
    accountId: 7,
    folderId,
    fromAddress: 'me@example.com',
    toAddresses: 'ann@example.com, bob@example.com',
    ccAddresses: 'cat@example.com',
    subject: 'Plans',
    bodyQuote: 'See you there.',
    messageId: '<plans@example.com>',
    references: '',
  } as MessageDetail
}

function replyTo(folderId: number, replyAll: boolean) {
  const id = openReply(message(folderId), 'plaintext', replyAll)
  return get(composeSessions).find((s) => s.id === id)!
}

afterEach(() => {
  composeSessions.set([])
  sidebar.set(idle())
})

describe('reply recipients (#485)', () => {
  it('replies all from Sent to the people the message went to', () => {
    // named "Gesendet": the role decides, not the name.
    sidebar.set(ready({ accounts: [], foldersByAccount: { 7: [inbox, sent] }, views: [], pinned: [] }))

    const session = replyTo(sent.id, true)

    expect(session.to).toBe('ann@example.com, bob@example.com')
    expect(session.cc).toBe('cat@example.com')
  })

  it('replies to the sender everywhere else', () => {
    sidebar.set(ready({ accounts: [], foldersByAccount: { 7: [inbox, sent] }, views: [], pinned: [] }))

    expect(replyTo(inbox.id, true).to).toBe('me@example.com')
    expect(replyTo(sent.id, false).to).toBe('me@example.com')
  })

  it('falls back to the sender while the folders are not loaded', () => {
    expect(replyTo(sent.id, true).to).toBe('me@example.com')
  })
})
