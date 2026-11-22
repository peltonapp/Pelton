import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const api = vi.hoisted(() => ({ getMessageHtml: vi.fn() }))
vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))
import type { MessageDetail } from '../../lib/types'
import { messageDetail } from '../../test/fixtures'
import MailBody from './MailBody.svelte'

// detail is a complete HTML message with one blocked remote image.
function detail(over: Partial<MessageDetail> = {}): MessageDetail {
  return messageDetail({
    bodyPlain: '',
    bodyQuote: '',
    bodyHtmlSafe: '<p>hi</p><img src="https://cdn.example/logo.png">',
    isHtml: true,
    hasRemoteContent: true,
    remoteHosts: ['cdn.example'],
    bodyComplete: true,
    ...over,
  })
}

function frameCSP(container: HTMLElement): string {
  const doc = container.querySelector('iframe')?.getAttribute('srcdoc') ?? ''
  return /img-src ([^;]*);/.exec(doc)?.[1] ?? ''
}

describe('MailBody remote content', () => {
  // a stub can arrive with remoteAllowed false and only turn true once the body
  // is fetched and the message refreshes in place; images then show without
  // reopening it.
  it('follows remoteAllowed turning true when the same message refreshes', async () => {
    const { container, rerender } = render(MailBody, { detail: detail() })
    expect(frameCSP(container)).toBe('data:')
    expect(container.querySelector('.remote-bar')).not.toBeNull()

    await rerender({ detail: detail({ remoteAllowed: true }) })
    expect(frameCSP(container)).toBe('data: https: http:')
    expect(container.querySelector('.remote-bar')).toBeNull()
  })

  it('blocks remote content again when a different message opens', async () => {
    const { container, rerender } = render(MailBody, { detail: detail({ remoteAllowed: true }) })
    expect(frameCSP(container)).toBe('data: https: http:')

    await rerender({ detail: detail({ id: 8, remoteAllowed: false }) })
    expect(frameCSP(container)).toBe('data:')
  })
})

describe('MailBody remote load across messages', () => {
  it('does not mark the next message loaded when the first one finishes late', async () => {
    let resolve!: (html: string) => void
    api.getMessageHtml.mockReturnValueOnce(new Promise<string>((r) => (resolve = r)))
    const { container, rerender } = render(MailBody, { detail: detail() })

    await userEvent.click(screen.getByRole('button', { name: /Load once/ }))
    await rerender({ detail: detail({ id: 8 }) })
    expect(api.getMessageHtml).toHaveBeenCalledWith(7, true, false)
    resolve('<p>first, with images</p>')
    // let the load finish and the component flush.
    await new Promise((r) => setTimeout(r, 0))

    expect(frameCSP(container)).toBe('data:')
    expect(container.querySelector('.remote-bar')).not.toBeNull()
  })
})

function frameNonce(container: HTMLElement): string {
  const doc = container.querySelector('iframe')?.getAttribute('srcdoc') ?? ''
  return /'nonce-([^']+)'/.exec(doc)?.[1] ?? ''
}

// the nonce guards one document: when the body or the remote policy changes,
// the old nonce must not carry over to the new content.
describe('MailBody script nonce', () => {
  it('stays the same when the message refreshes with the same body', async () => {
    const { container, rerender } = render(MailBody, { detail: detail() })
    const before = frameNonce(container)

    await rerender({ detail: detail({ seen: false }) })

    expect(before).not.toBe('')
    expect(frameNonce(container)).toBe(before)
  })

  it('changes when the body changes', async () => {
    const { container, rerender } = render(MailBody, { detail: detail() })
    const before = frameNonce(container)

    await rerender({ detail: detail({ bodyHtmlSafe: '<p>full body</p>' }) })

    expect(frameNonce(container)).not.toBe(before)
  })

  it('changes when remote content is switched on', async () => {
    const { container, rerender } = render(MailBody, { detail: detail() })
    const before = frameNonce(container)

    await rerender({ detail: detail({ remoteAllowed: true }) })

    expect(frameNonce(container)).not.toBe(before)
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
