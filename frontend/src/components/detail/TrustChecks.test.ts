import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { MessageDetail } from '../../lib/types'

const browser = vi.hoisted(() => ({ openURL: vi.fn() }))
vi.mock('../../lib/browser', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/browser')>()),
  openURL: browser.openURL,
}))

import TrustChecks from './TrustChecks.svelte'
import { composeSessions, openReply } from '../../stores/compose'
import { linkCheck, passports, replyCheck } from '../../stores/trust'
import { expectAccessible } from '../../test/a11y'
import { passport } from '../../test/trust'

// the dialog fades in, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

let nextId = 500

function detail(): MessageDetail {
  return {
    id: ++nextId,
    accountId: 1,
    folderId: 2,
    folderName: 'Inbox',
    accountEmail: 'me@example.org',
    messageId: 'urgent@gmail.test',
    inReplyTo: [],
    references: [],
    fromName: 'Anna Müller',
    fromAddress: 'anna.mueller.ceo@gmail.test',
    toAddresses: 'me@example.org',
    ccAddresses: '',
    subject: 'Urgent transfer',
    date: '2026-10-01T09:00:00Z',
    bodyPlain: 'Please pay.',
    bodyQuote: 'Please pay.',
    bodyHtmlSafe: '',
    isHtml: false,
    unsubscribe: null,
    attachments: [],
  } as unknown as MessageDetail
}

function warn(d: MessageDetail): void {
  passports.update((m) =>
    new Map(m).set(
      d.id,
      passport({ state: 'lookalike', warnings: ['lookalike'], replyGoesTo: 'pay@evil.test', lookalike: { kind: 'name', of: 'Anna Müller', address: 'anna@company.test' } }),
    ),
  )
}

beforeEach(() => {
  browser.openURL.mockReset()
  composeSessions.set([])
  replyCheck.set(null)
  linkCheck.set(null)
  passports.set(new Map())
  document.documentElement.setAttribute('data-design', 'new')
})

describe('reply friction', () => {
  it('asks first, showing where a reply to a warned mail really goes', async () => {
    const user = userEvent.setup()
    const d = detail()
    warn(d)
    expect(openReply(d, 'plaintext', false)).toBe(-1)
    expect(get(composeSessions)).toHaveLength(0)
    expect(get(replyCheck)?.to).toBe('pay@evil.test')

    render(TrustChecks)
    expect(screen.getByRole('dialog', { name: 'Check who you are replying to' })).toBeInTheDocument()
    expect(screen.getByText('Your reply goes to pay@evil.test.')).toBeInTheDocument()
    expectAccessible()
    await user.click(screen.getByRole('button', { name: 'Reply anyway' }))
    expect(get(composeSessions)).toHaveLength(1)
    expect(get(replyCheck)).toBeNull()

    // asked once per message.
    expect(openReply(d, 'plaintext', false)).toBeGreaterThan(0)
  })

  it('does not ask for mail without a strong warning, nor in the classic design', () => {
    const plain = detail()
    passports.update((m) => new Map(m).set(plain.id, passport()))
    expect(openReply(plain, 'plaintext', false)).toBeGreaterThan(0)

    const warned = detail()
    warn(warned)
    document.documentElement.setAttribute('data-design', 'classic')
    expect(openReply(warned, 'plaintext', false)).toBeGreaterThan(0)
    expect(get(replyCheck)).toBeNull()
  })
})

describe('link friction', () => {
  it('shows the real destination of a tracked link and opens it directly when asked', async () => {
    const user = userEvent.setup()
    linkCheck.set({
      messageId: 1,
      url: 'https://click.tracker.test/c?u=https://shop.example/',
      item: { kind: 'link', host: 'click.tracker.test', company: '', url: 'https://click.tracker.test/c?u=https://shop.example/', reasons: ['redirect'], destination: 'https://shop.example/', status: 'click' },
    })
    render(TrustChecks)
    expect(screen.getByText('This link opens https://shop.example/')).toBeInTheDocument()
    expect(screen.getByText('It passes through click.tracker.test first, which records the click.')).toBeInTheDocument()
    expectAccessible()
    await user.click(screen.getByRole('button', { name: 'Open destination directly' }))
    expect(browser.openURL).toHaveBeenCalledWith('https://shop.example/')
  })

  it('opens the link as written when the user picks that, and cancel opens nothing', async () => {
    const user = userEvent.setup()
    const url = 'https://login.paypa1.com/'
    linkCheck.set({ messageId: 2, url, item: null })
    render(TrustChecks)
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(browser.openURL).not.toHaveBeenCalled()
    linkCheck.set({ messageId: 2, url, item: null })
    await user.click(await screen.findByRole('button', { name: 'Open link' }))
    expect(browser.openURL).toHaveBeenCalledWith(url)
  })
})
