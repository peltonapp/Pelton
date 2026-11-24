import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { PrivacyReport as Report } from '../../lib/types'

const api = vi.hoisted(() => ({
  privacyReport: vi.fn(),
  clearPrivacyHistory: vi.fn(),
  dismissStillSending: vi.fn(),
  blockUnsubscribedSender: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import PrivacyReport from './PrivacyReport.svelte'
import { confirmRequest } from '../../stores/confirm'
import { prefs } from '../../stores/prefs'
import { reportFilter } from '../../stores/trust'
import { expectAccessible } from '../../test/a11y'

const report: Report = {
  counting: true,
  trackers: 412,
  links: 80,
  mails: 500,
  senders: 37,
  rows: [
    { address: 'news@shop.example', name: 'Shop', mails: 40, pixels: 300, links: 60, receipts: 0, first: '2026-01-01T00:00:00Z', last: '2026-11-01T00:00:00Z', companies: ['Mailchimp'], unsub: null },
    {
      address: 'deals@toys.example',
      name: 'Toys',
      mails: 10,
      pixels: 12,
      links: 20,
      receipts: 1,
      first: '2026-02-01T00:00:00Z',
      last: '2026-11-15T00:00:00Z',
      companies: [],
      unsub: { at: '2026-11-03T00:00:00Z', stillSending: true, stillCount: 3, blocked: '', sender: 'deals@toys.example' },
    },
  ],
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.privacyReport.mockResolvedValue(report)
  api.clearPrivacyHistory.mockResolvedValue(undefined)
  api.blockUnsubscribedSender.mockResolvedValue(undefined)
  confirmRequest.set(null)
  reportFilter.set('')
  prefs.update((p) => ({ ...p, privacyCounters: true }))
})

function names(): string[] {
  return within(screen.getByRole('table'))
    .getAllByRole('rowheader')
    .map((h) => h.querySelector('.name')?.textContent ?? '')
}

describe('PrivacyReport', () => {
  it('shows the mailbox totals and the senders by how much they tracked', async () => {
    render(PrivacyReport)
    expect(await screen.findByText('412 trackers blocked in the last 30 days from 37 senders')).toBeInTheDocument()
    expect(api.privacyReport).toHaveBeenCalledWith(30)
    expect(names()).toEqual(['Shop', 'Toys'])
    expect(screen.getByText(/You unsubscribed from Toys on .* but they are still sending/)).toBeInTheDocument()
    expectAccessible()
  })

  it('changes period and sort, and filters by sender', async () => {
    const user = userEvent.setup()
    render(PrivacyReport)
    await screen.findByRole('table')
    await user.click(screen.getByRole('radio', { name: '7 days' }))
    await waitFor(() => expect(api.privacyReport).toHaveBeenLastCalledWith(7))
    await user.click(screen.getByRole('radio', { name: 'Recent' }))
    expect(names()).toEqual(['Toys', 'Shop'])
    await user.type(screen.getByRole('textbox', { name: 'Filter senders' }), 'mailchimp')
    expect(names()).toEqual(['Shop'])
  })

  it('opens filtered to the sender it was opened for', async () => {
    reportFilter.set('toys.example')
    render(PrivacyReport)
    await screen.findByRole('table')
    expect(names()).toEqual(['Toys'])
    expect(get(reportFilter)).toBe('')
  })

  it('clears the history after asking, and blocks a sender that is still sending', async () => {
    const user = userEvent.setup()
    render(PrivacyReport)
    await screen.findByRole('table')
    await user.click(screen.getByRole('button', { name: 'Clear history' }))
    const ask = get(confirmRequest)
    expect(ask?.body).toBe('Forget every counter? Unsubscribes are kept.')
    ask?.resolve(true)
    await waitFor(() => expect(api.clearPrivacyHistory).toHaveBeenCalled())

    await user.click(screen.getByRole('button', { name: 'Move their mail to junk' }))
    expect(api.blockUnsubscribedSender).toHaveBeenCalledWith('deals@toys.example', 'junk')
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('New mail from them goes to junk'))
  })

  it('stops keeping counters from its switch and says so', async () => {
    const user = userEvent.setup()
    api.privacyReport.mockResolvedValue({ ...report, counting: false })
    render(PrivacyReport)
    expect(await screen.findByText('Counting is off. The report shows what was counted before.')).toBeInTheDocument()
    await user.click(screen.getByRole('switch', { name: 'Keep a tracking report' }))
    expect(api.setSetting).toHaveBeenCalledWith('privacy_counters', 'false')
  })
})
