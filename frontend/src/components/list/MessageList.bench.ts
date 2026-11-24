// Relative timings of the message list under jsdom: a mailbox of 20k rows,
// scrolled a screen at a time. jsdom lays nothing out and is not
// the shipped webview, so compare these numbers with each other, not with a
// frame budget.
//
//	pnpm exec vitest bench --run src/components/list/MessageList.bench.ts
import { describe, expect, it, vi } from 'vitest'
import { flushSync } from 'svelte'
import { render, screen } from '@testing-library/svelte'
import type { MessageSummary } from '../../lib/types'

const ROWS = 20000

const api = vi.hoisted(() => ({ listViewMessages: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  listViewMessages: api.listViewMessages,
}))

import MessageList from './MessageList.svelte'
import { selection } from '../../stores/selection'
import { prefs } from '../../stores/prefs'

function summary(id: number): MessageSummary {
  return {
    id,
    accountId: 1 + (id % 3),
    folderId: 1,
    accountEmail: 'me@example.com',
    folderName: 'INBOX',
    subject: `Subject of message ${id}`,
    fromName: `Sender ${id % 300}`,
    fromAddress: `sender${id % 300}@example.com`,
    snippet: 'quarterly results feedback design proposal schedule contract renewal',
    date: new Date(Date.UTC(2026, 0, 1) - id * 60000).toISOString(),
    seen: id % 7 !== 0,
    flagged: id % 31 === 0,
    hasAttachments: id % 5 === 0,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    threadId: id,
    answered: false,
    forwarded: false,
  }
}

describe('message list, 20k rows', () => {
  it('scrolls and reloads', async ({ bench }) => {
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe(): void {}
        unobserve(): void {}
        disconnect(): void {}
      },
    )
    Object.defineProperty(HTMLElement.prototype, 'clientHeight', { configurable: true, get: () => 900 })
    const items = Array.from({ length: ROWS }, (_, i) => summary(i + 1))
    api.listViewMessages.mockResolvedValue({ messages: items, total: ROWS, hasOlder: false })
    prefs.update((p) => ({ ...p, design: 'classic', appVimMode: false }))
    selection.set({ kind: 'view', view: 'inbox', label: 'Unified Inbox' })

    render(MessageList)
    await screen.findByText('Subject of message 1')
    const listbox = screen.getByRole('listbox')
    let position = 0

    const r = await bench('scroll one screen', () => {
      position = (position + 900) % (ROWS * 60)
      listbox.scrollTop = position
      listbox.dispatchEvent(new Event('scroll'))
      flushSync()
    }).run()
    console.log(`scroll one screen: mean ${r.latency.mean.toFixed(3)} ms, p99 ${r.latency.p99?.toFixed(3)} ms, rme ${r.latency.rme.toFixed(1)}%, samples ${r.latency.samplesCount}`)
    expect(r.latency.mean).toBeGreaterThan(0)
  }, 600_000)
})
