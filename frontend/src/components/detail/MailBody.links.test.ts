import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { MessageDetail } from '../../lib/types'

const browser = vi.hoisted(() => ({ openURL: vi.fn() }))
vi.mock('../../lib/browser', () => browser)

import MailBody from './MailBody.svelte'
import { prefs } from '../../stores/prefs'
import { contextMenu, type MenuItem } from '../../stores/contextmenu'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

const tracked = 'https://shop.example.com/p?id=7&utm_source=news&fbclid=abc'
const cleaned = 'https://shop.example.com/p?id=7'
const flow = 'https://verify.example.com?c=fhue389ew'

function detail(): MessageDetail {
  return {
    id: 5,
    accountId: 1,
    subject: 'Offer',
    fromName: 'Shop',
    fromAddress: 'shop@example.com',
    toAddresses: 'me@example.com',
    ccAddresses: '',
    date: new Date(2026, 10, 7, 9).toISOString(),
    isHtml: false,
    bodyPlain: `Look at ${tracked} and confirm via ${flow} please.`,
    bodyQuote: '',
    bodyHtmlSafe: '',
    attachments: [],
    trackingPixels: [],
    remoteHosts: [],
    phishing: { level: 'none' },
    messageId: '<5@example.com>',
  } as unknown as MessageDetail
}

beforeEach(() => {
  browser.openURL.mockReset()
  contextMenu.set(null)
  prefs.update((p) => ({ ...p, design: 'new', cleanLinks: false }))
})

describe('MailBody links', () => {
  it('opens a link as it is while cleaning is off', async () => {
    render(MailBody, { detail: detail() })
    await userEvent.click(screen.getByRole('link', { name: tracked }))
    expect(browser.openURL).toHaveBeenCalledWith(tracked)
  })

  it('opens the cleaned link when cleaning is on', async () => {
    prefs.update((p) => ({ ...p, cleanLinks: true }))
    render(MailBody, { detail: detail() })
    await userEvent.click(screen.getByRole('link', { name: tracked }))
    expect(browser.openURL).toHaveBeenCalledWith(cleaned)
  })

  it('opens a verification link untouched even with cleaning on', async () => {
    prefs.update((p) => ({ ...p, cleanLinks: true }))
    render(MailBody, { detail: detail() })
    await userEvent.click(screen.getByRole('link', { name: flow }))
    expect(browser.openURL).toHaveBeenCalledWith(flow)
  })

  it('shows the cleaned address and a quiet marker on hover', async () => {
    prefs.update((p) => ({ ...p, cleanLinks: true }))
    const { container } = render(MailBody, { detail: detail() })
    await userEvent.hover(screen.getByRole('link', { name: tracked }))
    expect(container.ownerDocument.querySelector('.link-hover-text')).toHaveTextContent(cleaned)
    expect(container.ownerDocument.querySelector('.link-hover-tag')).toHaveTextContent('cleaned')
  })

  it('shows the real address without a marker while cleaning is off', async () => {
    const { container } = render(MailBody, { detail: detail() })
    await userEvent.hover(screen.getByRole('link', { name: tracked }))
    expect(container.ownerDocument.querySelector('.link-hover-text')).toHaveTextContent(tracked)
    expect(container.ownerDocument.querySelector('.link-hover-tag')).toBeNull()
  })

  it('copies the cleaned link and offers the original from the menu', async () => {
    prefs.update((p) => ({ ...p, cleanLinks: true }))
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    render(MailBody, { detail: detail() })

    await userEvent.pointer({ keys: '[MouseRight]', target: screen.getByRole('link', { name: tracked }) })
    const items = (get(contextMenu)?.entries ?? []).filter((e): e is MenuItem => typeof e === 'object' && 'label' in e)
    expect(items.map((i) => i.label)).toEqual(['Copy link', 'Open original link'])

    items[0].action()
    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith(cleaned))
    items[1].action()
    expect(browser.openURL).toHaveBeenCalledWith(tracked)
  })

  it('copies the link as it is while cleaning is off and has no original to open', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    render(MailBody, { detail: detail() })

    await userEvent.pointer({ keys: '[MouseRight]', target: screen.getByRole('link', { name: tracked }) })
    const items = (get(contextMenu)?.entries ?? []).filter((e): e is MenuItem => typeof e === 'object' && 'label' in e)
    expect(items.map((i) => i.label)).toEqual(['Copy link'])
    items[0].action()
    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith(tracked))
  })
})
