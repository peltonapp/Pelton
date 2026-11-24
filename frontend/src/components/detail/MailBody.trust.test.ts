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
import { linkCheck, passports, shields } from '../../stores/trust'
import { passport, shield } from '../../test/trust'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

const wrapped = 'https://click.tracker.test/c?u=https%3A%2F%2Fshop.example%2Fsale'
const destination = 'https://shop.example/sale'

function detail(id: number): MessageDetail {
  return {
    id,
    accountId: 1,
    subject: 'Offer',
    fromName: 'Shop',
    fromAddress: 'shop@example.com',
    toAddresses: 'me@example.com',
    ccAddresses: '',
    date: new Date(2026, 10, 7, 9).toISOString(),
    isHtml: false,
    bodyPlain: `See ${wrapped} today.`,
    bodyQuote: '',
    bodyHtmlSafe: '',
    attachments: [],
    trackingPixels: [],
    remoteHosts: [],
    phishing: { level: 'none' },
    messageId: `<${id}@example.com>`,
  } as unknown as MessageDetail
}

function track(id: number): void {
  shields.update((m) =>
    new Map(m).set(
      id,
      shield({ links: 1, items: [{ kind: 'link', host: 'click.tracker.test', company: '', url: wrapped, reasons: ['redirect'], destination, status: 'click' }] }),
    ),
  )
}

beforeEach(() => {
  browser.openURL.mockReset()
  contextMenu.set(null)
  linkCheck.set(null)
  passports.set(new Map())
  shields.set(new Map())
  prefs.update((p) => ({ ...p, design: 'new', cleanLinks: false }))
})

describe('MailBody and the shield', () => {
  it('shows where a tracked link really goes on hover, and through whom', async () => {
    track(31)
    const { container } = render(MailBody, { detail: detail(31) })
    await userEvent.hover(screen.getByRole('link', { name: wrapped }))
    expect(container.ownerDocument.querySelector('.link-hover-text')).toHaveTextContent(destination)
    expect(container.ownerDocument.querySelector('.link-hover-tag')).toHaveTextContent('via click.tracker.test')
  })

  it('offers to open the destination directly, per click', async () => {
    track(32)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: vi.fn() } })
    render(MailBody, { detail: detail(32) })
    await userEvent.pointer({ keys: '[MouseRight]', target: screen.getByRole('link', { name: wrapped }) })
    const items = (get(contextMenu)?.entries ?? []).filter((e): e is MenuItem => typeof e === 'object' && 'label' in e)
    const direct = items.find((i) => i.label === 'Open destination directly')
    expect(direct).toBeDefined()
    direct?.action()
    expect(browser.openURL).toHaveBeenCalledWith(destination)
  })

  it('opens links as usual in mail without a strong warning', async () => {
    track(33)
    passports.update((m) => new Map(m).set(33, passport()))
    render(MailBody, { detail: detail(33) })
    await userEvent.click(screen.getByRole('link', { name: wrapped }))
    expect(browser.openURL).toHaveBeenCalledWith(wrapped)
    expect(get(linkCheck)).toBeNull()
  })

  it('asks before opening a link in mail with a strong warning', async () => {
    track(34)
    passports.update((m) => new Map(m).set(34, passport({ state: 'failed', warnings: ['dmarc_policy'] })))
    render(MailBody, { detail: detail(34) })
    await userEvent.click(screen.getByRole('link', { name: wrapped }))
    expect(browser.openURL).not.toHaveBeenCalled()
    expect(get(linkCheck)?.item?.destination).toBe(destination)
  })

  it('leaves the classic design as it was', async () => {
    track(35)
    passports.update((m) => new Map(m).set(35, passport({ state: 'failed', warnings: ['dmarc_policy'] })))
    prefs.update((p) => ({ ...p, design: 'classic' }))
    const { container } = render(MailBody, { detail: detail(35) })
    await userEvent.hover(screen.getByRole('link', { name: wrapped }))
    expect(container.ownerDocument.querySelector('.link-hover-tag')).toBeNull()
    await userEvent.click(screen.getByRole('link', { name: wrapped }))
    expect(browser.openURL).toHaveBeenCalledWith(wrapped)
  })
})
