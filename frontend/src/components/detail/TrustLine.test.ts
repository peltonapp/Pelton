import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { MessageDetail, ShieldItem } from '../../lib/types'

const api = vi.hoisted(() => ({
  senderPassport: vi.fn(),
  privacyShield: vi.fn(),
  setSenderMark: vi.fn(),
  blockUnsubscribedSender: vi.fn(),
  dismissStillSending: vi.fn(),
  markSpam: vi.fn(),
  unsubscribeMessage: vi.fn(),
  noteUnsubscribeLink: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import TrustLine from './TrustLine.svelte'
import { imageRequest, passports, shields } from '../../stores/trust'
import { mailScreen } from '../../stores/mailscreen'
import { expectAccessible } from '../../test/a11y'
import { passport, shield } from '../../test/trust'

let nextId = 100

function detail(patch: Partial<MessageDetail> = {}): MessageDetail {
  return {
    id: ++nextId,
    accountId: 1,
    folderId: 2,
    fromName: 'Anna Beck',
    fromAddress: 'Anna Beck <anna@example.org>',
    subject: 'Plans',
    date: '2026-10-01T09:00:00Z',
    unsubscribe: null,
    attachments: [],
    ...patch,
  } as MessageDetail
}

const pixel: ShieldItem = {
  kind: 'pixel',
  host: 'x.list-manage.com',
  company: 'Mailchimp',
  url: 'https://x.list-manage.com/open',
  reasons: ['tiny'],
  destination: '',
  status: 'blocked',
}
const link: ShieldItem = {
  kind: 'link',
  host: 'click.tracker.test',
  company: '',
  url: 'https://click.tracker.test/c?u=https://shop.example/',
  reasons: ['redirect'],
  destination: 'https://shop.example/',
  status: 'click',
}
const image: ShieldItem = { ...pixel, kind: 'image', host: 'img.shop.example', company: '', reasons: [], url: 'https://img.shop.example/a.png' }

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  passports.set(new Map())
  shields.set(new Map())
  mailScreen.set(null)
  api.senderPassport.mockResolvedValue(passport())
  api.privacyShield.mockResolvedValue(shield())
  api.setSenderMark.mockResolvedValue(undefined)
})

describe('TrustLine', () => {
  it('says who sent the mail in one line and keeps a live region mounted', async () => {
    const { container } = render(TrustLine, { props: { detail: detail() } })
    const line = await screen.findByRole('button', { name: /Verified: really from example.org, and you have written to each other 23 times since 2024/ })
    expect(line).toHaveAttribute('aria-expanded', 'false')
    expect(container.querySelector('[aria-live="polite"]')).not.toBeNull()
    expectAccessible()
  })

  it('opens the evidence from the keyboard and records "I know this sender"', async () => {
    const user = userEvent.setup()
    api.senderPassport.mockResolvedValue(passport({ state: 'unchecked', reason: 'none', first: true, sent: 0, received: 1 }))
    const d = detail()
    render(TrustLine, { props: { detail: d } })
    const line = await screen.findByRole('button', { name: /Could not be verified: your mail server did not check this message/ })
    line.focus()
    await user.keyboard('{Enter}')
    expect(line).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByRole('region', { name: 'Who sent this' })).toBeInTheDocument()
    expect(screen.getByText('Without a check by your mail server, the From line is only what the sender wrote.')).toBeInTheDocument()
    expectAccessible()

    api.senderPassport.mockResolvedValue(passport({ state: 'unchecked', reason: 'none', first: true, sent: 0, received: 1, mark: 'known' }))
    await user.click(screen.getByRole('button', { name: 'I know this sender' }))
    expect(api.setSenderMark).toHaveBeenCalledWith('anna@example.org', 'known')
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Saved for anna@example.org'))
    expect(await screen.findByRole('button', { name: 'Forget my answer' })).toBeInTheDocument()

    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Who sent this' })).not.toBeInTheDocument())
  })

  it('says what was verified and what was not', async () => {
    const user = userEvent.setup()
    render(TrustLine, { props: { detail: detail() } })
    await user.click(await screen.findByRole('button', { name: /^Verified/ }))
    expect(screen.getByText('Your mail server checked that example.org sent this message.')).toBeInTheDocument()
    expect(screen.getByText('That does not confirm who at example.org wrote it, or that the content is safe.')).toBeInTheDocument()
    expect(screen.getByText('Technical details')).toBeInTheDocument()
  })

  it('shows the shield, what each tracker would reveal, and loads images without the trackers', async () => {
    const user = userEvent.setup()
    api.privacyShield.mockResolvedValue(shield({ trackers: 1, links: 1, items: [image, pixel, link] }))
    const d = detail()
    render(TrustLine, { props: { detail: d } })
    const chip = await screen.findByRole('button', { name: '1 tracker blocked' })
    await user.click(chip)
    expect(await screen.findByText('Tracking pixel')).toBeInTheDocument()
    expect(screen.getByText('Mailchimp (x.list-manage.com)')).toBeInTheDocument()
    expect(screen.getByText('Tells the sender you opened the mail, when, roughly where and on what device.')).toBeInTheDocument()
    expect(screen.getByText('Goes to https://shop.example/')).toBeInTheDocument()
    expectAccessible()

    await user.click(screen.getByRole('button', { name: 'Load images, keep trackers blocked' }))
    expect(get(imageRequest)).toBe(d.id)

    await user.click(screen.getByRole('button', { name: 'Tracking report' }))
    expect(get(mailScreen)).toBe('privacy')
  })

  it('tells the user a sender is still sending and offers the choices', async () => {
    const user = userEvent.setup()
    const unsub = { at: '2026-11-03T10:00:00Z', stillSending: true, stillCount: 2, blocked: '' as const, sender: 'anna@example.org' }
    api.privacyShield.mockResolvedValue(shield({ links: 1, items: [link], unsub }))
    api.blockUnsubscribedSender.mockResolvedValue(undefined)
    const d = detail({ unsubscribe: { kind: 'oneclick', target: 'https://example.org/u', done: true } })
    render(TrustLine, { props: { detail: d } })
    expect(await screen.findByText(/You unsubscribed from Anna Beck on .* but they are still sending/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'What to do' }))
    await user.click(await screen.findByRole('button', { name: 'Move their mail to junk' }))
    expect(api.blockUnsubscribedSender).toHaveBeenCalledWith('anna@example.org', 'junk')
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Report as spam' })).toBeInTheDocument()
    expectAccessible()
  })

  it('says nothing when neither could be read', async () => {
    api.senderPassport.mockRejectedValue(new Error('demo'))
    api.privacyShield.mockRejectedValue(new Error('demo'))
    render(TrustLine, { props: { detail: detail() } })
    await waitFor(() => expect(api.privacyShield).toHaveBeenCalled())
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})
