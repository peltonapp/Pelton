import { describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import type { MessageDetail } from '../../lib/types'
import MailBody from './MailBody.svelte'

function detail(over: Partial<MessageDetail> = {}): MessageDetail {
  return {
    id: 7,
    accountId: 1,
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject: 'Hello',
    fromName: 'InPost',
    fromAddress: 'info@paczkomaty.pl',
    snippet: '',
    date: '2026-09-21T12:00:00Z',
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
    toAddresses: '',
    ccAddresses: '',
    bodyPlain: '',
    bodyQuote: '',
    bodyHtmlSafe: '<p>hi</p><img src="https://cdn.example/logo.png">',
    isHtml: true,
    hasRemoteContent: true,
    remoteAllowed: false,
    remoteHosts: ['cdn.example'],
    trackingPixels: [],
    attachments: [],
    phishing: { level: 'none' },
    unsubscribe: null,
    charsetGuess: '',
    pgpState: '',
    bodyComplete: true,
    ...over,
  }
}

function frameCSP(container: HTMLElement): string {
  const doc = container.querySelector('iframe')?.getAttribute('srcdoc') ?? ''
  return /img-src ([^;]*);/.exec(doc)?.[1] ?? ''
}

describe('MailBody remote content', () => {
  it('blocks remote content again when a different message opens', async () => {
    const { container, rerender } = render(MailBody, { detail: detail({ remoteAllowed: true }) })
    expect(frameCSP(container)).toBe('data: https: http:')

    await rerender({ detail: detail({ id: 8, remoteAllowed: false }) })
    expect(frameCSP(container)).toBe('data:')
  })
})

describe('MailBody frame per message', () => {
  // WebKit can keep painting the previous document when one iframe's srcdoc is
  // swapped while an earlier swap is still settling, which left a new header
  // over the last message's body. Each message gets a frame of its own.
  it('replaces the body frame when another message opens', async () => {
    const { container, rerender } = render(MailBody, { detail: detail({ bodyHtmlSafe: '<p>first</p>' }) })
    const first = container.querySelector('iframe')

    await rerender({ detail: detail({ id: 8, bodyHtmlSafe: '<p>second</p>' }) })
    const second = container.querySelector('iframe')
    expect(second).not.toBeNull()
    expect(second).not.toBe(first)
    expect(second?.getAttribute('srcdoc')).toContain('second')
  })

  // marking the open message read refreshes it with the same body. That must
  // not rebuild the document and reload the frame.
  it('keeps the document when the same message refreshes with the same body', async () => {
    const { container, rerender } = render(MailBody, { detail: detail() })
    const frame = container.querySelector('iframe')
    const before = frame?.getAttribute('srcdoc')

    await rerender({ detail: detail({ seen: false }) })
    expect(container.querySelector('iframe')).toBe(frame)
    expect(container.querySelector('iframe')?.getAttribute('srcdoc')).toBe(before)
  })

  // the new frame stays hidden until its document has loaded, with a spinner
  // once that takes long enough to notice, so a slow switch shows that it is
  // loading rather than an empty or stale pane.
  it('hides a new frame until it loads and shows a spinner meanwhile', async () => {
    vi.useFakeTimers()
    // jsdom loads the srcdoc document at once. Holding back its load event and
    // the fallback readiness poll stands in for a document that is slow to load.
    vi.stubGlobal('requestAnimationFrame', () => 0)
    const holdLoad = (e: Event) => e.stopPropagation()
    document.addEventListener('load', holdLoad, true)
    try {
      const { container } = render(MailBody, { detail: detail() })
      const frame = container.querySelector('iframe')!
      expect(frame.classList.contains('loading')).toBe(true)
      expect(container.querySelector('[role="status"]')).toBeNull()

      await vi.advanceTimersByTimeAsync(200)
      expect(container.querySelector('[role="status"]')).not.toBeNull()

      document.removeEventListener('load', holdLoad, true)
      frame.dispatchEvent(new Event('load'))
      await vi.advanceTimersByTimeAsync(0)
      expect(frame.classList.contains('loading')).toBe(false)
      expect(container.querySelector('[role="status"]')).toBeNull()
    } finally {
      document.removeEventListener('load', holdLoad, true)
      vi.useRealTimers()
      vi.unstubAllGlobals()
    }
  })
})
