import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import OutboxLog from './OutboxLog.svelte'
import { appendLogEntries } from '../../stores/outboxlog'
import type { OutboxLogEntry } from '../../lib/types'

// new lines ease in, and jsdom has no web animations. A finished stub lets
// svelte run the transition to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

function entry(seq: number, patch: Partial<OutboxLogEntry> = {}): OutboxLogEntry {
  return {
    seq,
    at: new Date(Date.UTC(2026, 10, 15, 9, 30, seq)).toISOString(),
    messageId: 1,
    step: 'queued',
    recipients: ['ann@example.com'],
    detail: '',
    retryAt: '',
    ...patch,
  }
}

// layout jsdom does not do: a log 100 high holding 500 of content.
function giveLayout(log: HTMLElement, scrollTop: number): void {
  Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 500 })
  Object.defineProperty(log, 'clientHeight', { configurable: true, value: 100 })
  log.scrollTop = scrollTop
}

describe('OutboxLog', () => {
  it('shows one line per step with its time, recipient and reason', () => {
    render(OutboxLog, {
      props: {
        entries: [
          entry(1),
          entry(2, { step: 'connecting' }),
          entry(3, { step: 'failed', detail: 'smtp: authentication failed', recipients: ['ann@example.com', 'bob@example.com'] }),
          entry(4, { step: 'retry', retryAt: new Date(Date.UTC(2026, 10, 15, 9, 31)).toISOString() }),
        ],
      },
    })

    const lines = within(screen.getByRole('log', { name: 'Live log' })).getAllByRole('listitem')
    expect(lines).toHaveLength(4)
    expect(lines[0]).toHaveTextContent('Queued')
    expect(lines[0]).toHaveTextContent('ann@example.com')
    expect(lines[0].querySelector('time')?.getAttribute('datetime')).toBe(entry(1).at)
    expect(lines[1]).toHaveTextContent('Connecting to the server')
    expect(lines[2]).toHaveTextContent('Failed')
    expect(lines[2]).toHaveTextContent('ann@example.com +1')
    expect(lines[2]).toHaveTextContent('smtp: authentication failed')
    expect(lines[3]).toHaveTextContent('Next try at')
  })

  it('says when there is nothing yet and cannot be copied empty', () => {
    render(OutboxLog, { props: { entries: [] } })

    expect(screen.getByText('Nothing has been sent since Pelton started.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Copy log' })).toBeDisabled()
  })

  it('copies the whole log as text', async () => {
    const user = userEvent.setup()
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    render(OutboxLog, { props: { entries: [entry(1), entry(2, { step: 'sent' })] } })

    await user.click(screen.getByRole('button', { name: 'Copy log' }))

    expect(writeText).toHaveBeenCalledTimes(1)
    const text = writeText.mock.calls[0][0]
    expect(text.split('\n')).toHaveLength(2)
    expect(text).toMatch(/Queued {2}ann@example\.com\n.*Sent {2}ann@example\.com$/)
  })

  it('follows the newest line until scrolled up, then offers a way back', async () => {
    const { rerender } = render(OutboxLog, { props: { entries: [entry(1)] } })
    const log = screen.getByRole('log')

    giveLayout(log, 400)
    await fireEvent.scroll(log)
    await rerender({ entries: [entry(1), entry(2)] })
    expect(log.scrollTop).toBe(500)
    expect(screen.queryByRole('button', { name: 'Jump to latest' })).toBeNull()

    giveLayout(log, 120)
    await fireEvent.scroll(log)
    await rerender({ entries: [entry(1), entry(2), entry(3)] })
    expect(log.scrollTop).toBe(120)

    await userEvent.click(screen.getByRole('button', { name: 'Jump to latest' }))
    expect(log.scrollTop).toBe(500)
  })
})

describe('appendLogEntries', () => {
  it('adds only lines it does not have, in order, and keeps the newest', () => {
    const list = appendLogEntries([], [entry(2), entry(1)])
    expect(list.map((e) => e.seq)).toEqual([1, 2])
    expect(appendLogEntries(list, [entry(2)])).toBe(list)

    const many = Array.from({ length: 450 }, (_, i) => entry(i + 3))
    const trimmed = appendLogEntries(list, many)
    expect(trimmed).toHaveLength(400)
    expect(trimmed[trimmed.length - 1].seq).toBe(452)
  })
})
