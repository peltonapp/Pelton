import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { MessageDetail, Occurrence } from '../../lib/types'

const api = vi.hoisted(() => ({
  listOccurrences: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  listOccurrences: api.listOccurrences,
  setSetting: api.setSetting,
}))

const actions = vi.hoisted(() => ({ draftEventFromMail: vi.fn(), taskFromMailAt: vi.fn(), openDayInCalendar: vi.fn() }))

vi.mock('../../lib/mailcalendar', () => actions)

import MailBody from './MailBody.svelte'
import { prefs } from '../../stores/prefs'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

const body = 'Hi Ada,\n\nCould we meet Thursday at 3? Otherwise 12 March, 14:00 to 15:30.\nSee https://example.com/plan for the plan.\n'

function detail(patch: Partial<MessageDetail> = {}): MessageDetail {
  return {
    id: 41,
    accountId: 1,
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject: 'Meeting',
    fromName: 'Bob',
    fromAddress: 'bob@example.com',
    snippet: '',
    // Saturday 7 November 2026.
    date: new Date(2026, 10, 7, 9).toISOString(),
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
    messageId: '<41@example.com>',
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
  } as MessageDetail
}

const clash: Occurrence = {
  eventId: 3,
  calendarId: 1,
  calendarColor: '',
  uid: 'x',
  summary: 'Design review',
  location: '',
  start: new Date(2026, 10, 12, 15, 30).toISOString(),
  end: new Date(2026, 10, 12, 16, 30).toISOString(),
  allDay: false,
  recurring: false,
  recurrenceId: '',
  override: false,
  status: '',
  transparency: '',
  readOnly: false,
  hasReminders: false,
  pending: false,
  conflict: false,
}

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  api.listOccurrences.mockReset().mockResolvedValue([clash])
  api.setSetting.mockReset().mockResolvedValue(undefined)
  for (const fn of Object.values(actions)) {
    fn.mockReset()
  }
  prefs.update((p) => ({ ...p, design: 'new', mailDates: true }))
})

describe('dates in a plain-text mail', () => {
  it('marks the dates and leaves the text exactly as it was', async () => {
    const { container } = render(MailBody, { props: { detail: detail() } })
    const marks = await screen.findAllByRole('button', { name: /show the calendar/ }, { timeout: 4000 })
    expect(marks.map((m) => m.textContent)).toEqual(['Thursday at 3', '12 March, 14:00 to 15:30'])
    expect(container.querySelector('pre')?.textContent).toBe(body)
    expect(screen.getByRole('link')).toHaveTextContent('https://example.com/plan')
  })

  it('marks nothing with the setting off', async () => {
    prefs.update((p) => ({ ...p, mailDates: false }))
    const { container } = render(MailBody, { props: { detail: detail() } })
    await new Promise((r) => setTimeout(r, 300))
    expect(screen.queryByRole('button', { name: /show the calendar/ })).toBeNull()
    expect(container.querySelector('pre')?.textContent).toBe(body)
  })

  it('opens the calendar around a date on Enter and offers the actions', async () => {
    const user = userEvent.setup()
    render(MailBody, { props: { detail: detail() } })
    const [thursday] = await screen.findAllByRole('button', { name: /show the calendar/ }, { timeout: 4000 })
    thursday.focus()
    await user.keyboard('{Enter}')
    const peek = await screen.findByRole('dialog', { name: /Calendar around/ })
    expect(peek).toHaveTextContent('Clashes with Design review')
    expect(api.listOccurrences).toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Create task' }))
    expect(actions.taskFromMailAt).toHaveBeenCalledWith([41], { start: new Date(2026, 10, 12, 15), end: new Date(2026, 10, 12, 16), allDay: false })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: /Calendar around/ })).toBeNull())
  })

  it('closes on Escape and hands the focus back to the date', async () => {
    const user = userEvent.setup()
    render(MailBody, { props: { detail: detail() } })
    const [thursday] = await screen.findAllByRole('button', { name: /show the calendar/ }, { timeout: 4000 })
    thursday.focus()
    await user.keyboard('{Enter}')
    await screen.findByRole('dialog', { name: /Calendar around/ })
    expect(screen.getByRole('button', { name: 'Create event' })).toHaveFocus()
    await user.keyboard('{Escape}')
    await waitFor(() => expect(screen.queryByRole('dialog', { name: /Calendar around/ })).toBeNull())
    expect(thursday).toHaveFocus()
  })

  it('offers to make an event and to open the day', async () => {
    const user = userEvent.setup()
    render(MailBody, { props: { detail: detail() } })
    const [, march] = await screen.findAllByRole('button', { name: /show the calendar/ }, { timeout: 4000 })
    await user.click(march)
    await user.click(await screen.findByRole('button', { name: 'Create event' }))
    expect(actions.draftEventFromMail).toHaveBeenCalledWith(41, { start: new Date(2027, 2, 12, 14), end: new Date(2027, 2, 12, 15, 30), allDay: false })
    await user.click(march)
    await user.click(await screen.findByRole('button', { name: 'Open day' }))
    expect(actions.openDayInCalendar).toHaveBeenCalledWith(new Date(2027, 2, 12, 14))
  })
})
